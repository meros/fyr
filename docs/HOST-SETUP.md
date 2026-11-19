# Recommended host settings

These are the settings for a machine that runs Fyr's agents. They come from decisions 0029, 0032 and 0036 and from what meros-laptop did on 17–19 Nov. Fyr reads what the host gives it and works without them, but its memory guard works best with them. The NixOS lines go in the machine's own configuration, not in Fyr's module, except where they name `services.fyr`.

## Linux

### Kernel

| Setting | Recommended | Why |
|---|---|---|
| cgroup v2, PSI | on (`psi=1` if the kernel turns it off by default) | The guard's memory stall, the CPU slowdown and the per-thread numbers come from them (decision 0029 section 0). Without them Fyr falls back to the process tree and `/proc`. |
| Swap | **zswap with its shrinker, plus a disk swap. No zram.** | zram in front of disk swap fills with cold pages first; after that, pages still in use go to disk (decision 0036). zswap writes back the least recently used pages, and it has per-cgroup limits (`memory.zswap.max`, `memory.zswap.writeback`). Meta's guidance (Chris Down, March 2026): prefer zswap, and do not run zram alongside disk swap. |
| Disk swap size | about half of RAM or more | `agents/memory.swap.max` needs room under it. Without swap, `memory.high` livelocks instead of slowing a job (decision 0032). |
| `vm.swappiness` | the kernel default (60), unless the guard's log says otherwise | 120 and above are tuned for zram. |
| `vm.page-cluster` | the default (3) with zswap | 0 suits zram only. |
| MGLRU `min_ttl_ms` | **0** (off) | It calls the global OOM killer from kswapd. That killer picks by `oom_score`, can pick fyrd, and races Fyr's guard and oomd. |

NixOS:

```nix
zramSwap.enable = false;
boot.kernelParams = [ "zswap.enabled=1" "zswap.shrinker_enabled=1" "zswap.compressor=zstd" ];
swapDevices = [ { device = "/dev/disk/by-label/swap"; } ];  # or a swap file
```

### systemd-oomd

oomd is the last line, after Fyr's guard. Until the agents run in a unit of their own (#221, decision 0036 slice 2), these rules apply:

- **Put the oomd rule on `fyr.service` at a limit above what Fyr's own throttling produces.** Throttling at `memory.high` counts as memory stall in `fyr.service` too, and oomd ranks victims by reclaim scans, so a rule set too low kills the threads Fyr is throttling. 70 % killed `fyr.service/server` (fyrd) on 18 Nov; use a limit above the system-wide rules (80 % on NixOS), for example 85 %. The system-wide rules then fire first, on a cgroup that is not fyrd.
- Decision 0036 slice 0 (not built yet) has fyrd set `user.oomd_omit=1` on `fyr.service/server`. That will protect it from the rule on `fyr.service` only, not from rules on `/` or `system.slice`, and not from a swap kill.
- Keep `MemoryLow` on the unit (`services.fyr.cgroups.serverMemoryLow`, default 1 G) so the kernel reclaims from the agents before fyrd.

```nix
systemd.services.fyr.serviceConfig = {
  ManagedOOMMemoryPressure = "kill";
  ManagedOOMMemoryPressureLimit = "85%";
};
```

Slice 0 also adds a log line, `guard: near oomd's limit`, for when the unit's stall comes within 10 points of the limit. Until then, compare `cat /sys/fs/cgroup/system.slice/fyr.service/memory.pressure` (full avg10) with the limit.

### Fyr's cgroup budget (`services.fyr.cgroups`)

| Option | Recommended | Why |
|---|---|---|
| `enable` | true | Each thread gets its own cgroup, which the guard, the job cgroups and the measurements all need. |
| `agentsMemoryHigh` | about 30–40 % of RAM | Where the kernel starts reclaiming from the agents. The rest is for the desktop, the browser and the page cache. |
| `agentsMemoryMax` | `agentsMemoryHigh` + about 6 % of RAM | Keep the gap narrow. Past it the kernel OOM-kills an agent process. |
| `agentsMemorySwapMax` | about 20–35 % of RAM, below the swap device | Keeps the agents' cold pages from filling the disk swap. |
| `agentsCpuCores` | cores − 1 or more | The rest is for the desktop. |
| `threadMemoryHighPercent` | 0 until #221 | Each throttled thread would become oomd's next victim (decision 0036 (B)). |

## macOS

There are no cgroups, PSI or oomd. Fyr reads `kern.memorystatus_level`, the page counts and the run queue (V0.mac-load-signals) and pauses work with SIGSTOP. Nothing needs setting. Keep enough free disk for the system's own swap files.

## Checking a host

- `oomctl`: which cgroups oomd watches, and at what limit.
- `swapon --show` and `cat /sys/module/zswap/parameters/enabled`.
- `grep 'guard:' ~/.local/share/fyr/daemon.log` (or `journalctl -u fyr | grep guard:`): every decision the guard made, with the signals it acted on (decision 0032).
- `journalctl -u systemd-oomd | grep Marked`: every cgroup oomd killed. A line naming `fyr.service/server` means the settings above are not in place.
