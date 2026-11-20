# Recommended host settings

These are the settings for a machine that runs Fyr's agents. They come from decisions 0029, 0032 and 0036 and from what home-laptop did on 17–19 Nov. Fyr reads what the host gives it and works without them, but its memory guard works best with them. The NixOS lines go in the machine's own configuration, not in Fyr's module, except where they name `services.fyr`.

## Linux

### Kernel

| Setting | Recommended | Why |
|---|---|---|
| cgroup v2, PSI | on (`psi=1` if the kernel turns it off by default) | The guard's memory stall, the CPU slowdown and the per-thread numbers come from them (decision 0029 section 0). Without them Fyr falls back to the process tree and `/proc`. |
| Swap | **zswap with its shrinker, plus a disk swap. No zram.** | zram in front of disk swap fills with cold pages first; after that, pages still in use go to disk (decision 0036). zswap writes back the least recently used pages, and it has per-cgroup limits (`memory.zswap.max`, `memory.zswap.writeback`). Meta's guidance (Chris Down, March 2026): prefer zswap, and do not run zram alongside disk swap. |
| Disk swap size | about half of RAM or more | The agents' `memory.swap.max` needs room under it. Without swap, `memory.high` livelocks instead of slowing a job (decision 0032). |
| `vm.swappiness` | about 100 with zswap; tune from the guard's log | 120 and above are tuned for zram. home-laptop went from 120 to 100 (nixos-config#5). |
| `vm.page-cluster` | 0 | A fault takes one page from the zswap pool; readahead after a writeback is rarely worth it. |
| MGLRU `min_ttl_ms` | **0** (off) | It calls the global OOM killer from kswapd. That killer picks by `oom_score`, can pick fyrd, and races Fyr's guard and oomd. |

NixOS:

```nix
zramSwap.enable = false;
boot.kernelParams = [ "zswap.enabled=1" "zswap.shrinker_enabled=1" "zswap.compressor=zstd" ];
swapDevices = [ { device = "/dev/disk/by-label/swap"; } ];  # or a swap file
```

`boot.kernelParams` apply at the next boot. To turn zswap on without a reboot, write its module parameters as root: `echo 1 > /sys/module/zswap/parameters/enabled`, `echo Y > /sys/module/zswap/parameters/shrinker_enabled`, `echo zstd > /sys/module/zswap/parameters/compressor` (a new compressor applies to pages stored from then on). Those writes last until the reboot, where the kernel parameters take over. Removing zram (`swapoff /dev/zram0`) moves its pages back to RAM or disk first, so do it with memory to spare.

### The agents' slice and systemd-oomd

With `services.fyr.cgroups.agentsUnit` (the default, V0.agents-unit, decision 0036 (B)) the module splits Fyr in two. On a Linux without Nix, `scripts/install-linux.sh` writes the same unit, slice, polkit rule and `ExecStartPre` helper when polkit reads JavaScript rules, else the V0.80 unit ([INSTALL](INSTALL.md#linux-without-nix)); what follows holds for both:

```
system.slice/fyr.service          fyrd and its launcher: root's, not delegated,
                                  ManagedOOMPreference=omit, OOMScoreAdjust=-500, MemoryLow
fyr.slice/fyr-agents.slice        the agents' budget and oomd's rule
  fyr-agent-<thread>.scope        one thread: its CLI and everything it started
  fyr-deploy-<pid>-<n>.scope      one command of a self-deploy
```

- **oomd's rule sits on `fyr-agents.slice`**, from `agentsOomPressureLimit` (default 80 %) and `agentsOomPressureDuration`. Each thread's scope has `memory.oom.group=1`, so oomd ends a whole thread, never a job alone and never fyrd. Keep the limit above what Fyr's own throttling produces: throttling at `memory.high` counts as stall too, and the guard has to act first. fyrd logs `guard: near oomd's limit` when the slice's stall comes within 10 points of it (V0.oomd-guard).
- **fyrd's unit is omitted.** It is root-owned, so its `ManagedOOMPreference=omit` holds against every rule (`/`, `system.slice` and the slice's), and its negative `OOMScoreAdjust` keeps the kernel's OOM killer off it. Do not put an oomd rule on `fyr.service`: with agentsUnit it holds only fyrd. Remove one the machine's configuration still sets (agent-budget.nix had one at 70 %, which killed fyrd on 18 Nov).
- **A polkit rule** lets the service user `start` and `set-property` units named `fyr-agent-….scope` and `fyr-deploy-….scope`, nothing else (no other unit, no stop, no kill). The module adds it and turns on `security.polkit`. fyrd starts each thread with `systemd-run --scope --slice=fyr-agents.slice`.
- **`ExecStartPre` hands over the slice's `memory.pressure`** (V0.psi-trigger-slice). The file is root's, and fyrd needs to open it to set the guard's PSI trigger. The module's script, run as root before each start of `fyr.service`, chowns that one file to the service user; `fyr.service` wants and starts after the slice so the file exists. Without it the guard reads every 10 s instead of waking on the trigger, and fyrd tries the trigger again each minute.
- **A QoS script** that rewrites the agents' CPU cap writes `/sys/fs/cgroup/fyr.slice/fyr-agents.slice/cpu.max`.

**The switch.** `nixos-rebuild switch` does not restart `fyr.service` (that would end every running turn). It reloads fyrd, which starts new threads in scopes at once and keeps running threads where they are (V0.agents-unit (6)). Restart `fyr.service` **once**, at a quiet moment, after the switch: until then the unit is still delegated, `fyr.service/server` (fyrd) is still the service user's and not protected from the system-wide rules, and the slice's `memory.pressure` has not been handed over. Later switches need no restart for this.

**Checking it:**

- `oomctl`: `fyr-agents.slice` is monitored at its limit, and `fyr.service` is not listed.
- `systemctl show fyr.service -p ManagedOOMPreference -p OOMScoreAdjust -p Delegate`: `omit`, `-500`, `no`. `systemctl show fyr-agents.slice -p ManagedOOMMemoryPressure -p ManagedOOMMemoryPressureLimit`: `kill` and the limit (systemd 262 prints the limit as a fraction of 2^32, so 85 % is `3650722201`, V0.oomd-limit-parse).
- `systemctl list-units 'fyr-agent-*'`: a scope per running thread.
- `ls -l /sys/fs/cgroup/fyr.slice/fyr-agents.slice/memory.pressure`: owned by the service user, and `grep 'guard: PSI trigger' ~/.local/share/fyr/daemon.log` shows the trigger set.
- `grep 'cgroups: on, layout' ~/.local/share/fyr/daemon.log`: `cgroups: on, layout scope: …`. `layout unit` means the slice was not usable (the line before it says why) and the agents still run in `fyr.service`.
- `GET /api/load`: `cgroupLayout` is `"scope"`, `slowdown` is a number, not null (the probe runs in a scope of its own, V0.probe-scope), and `oomdLimit` is set. The host's load card (the rail's hover card, the phone's load sheet) shows the same limit on its `oomd` row with the stall now.

With agentsUnit off the module writes V0.80's delegated unit (fyrd in `fyr.service/server`, the agents in `fyr.service/agents/<thread>`). oomd can then only watch all of `fyr.service`, fyrd included, which is what decision 0036 (B) removed; turn it off only where the service user must not have the polkit rule (V0.agents-unit (4)).

### Fyr's cgroup budget (`services.fyr.cgroups`)

A size in percent is a share of the machine: of the RAM for `memory.high` and `memory.max`, of the swap for `memory.swap.max` (systemd reads it so on the slice; fyrd resolves it from `/proc/meminfo` in the unit layout). A size like `12G` is taken as it is. fyrd's `cgroups: on` line at start states the budget it runs with (V0.agents-budget-share).

| Option | Recommended | Why |
|---|---|---|
| `enable` | true | Each thread gets its own cgroup, which the guard, the job cgroups and the measurements all need. |
| `agentsUnit` | true | The agents in `fyr-agents.slice`, fyrd out of oomd's reach (above). |
| `agentsOomPressureLimit` | 80 % (the default) or a little above, e.g. 85 % | Above what the guard's own throttling produces, so the guard acts first. `null` sets no rule. |
| `serverMemoryLow` | 1 G (the default) | `MemoryLow` of `fyr.service`, so the kernel reclaims from the agents before fyrd. |
| `agentsMemoryHigh` | about 30–40 % of RAM (default `40%`) | Where the kernel starts reclaiming from the agents. The rest is for the desktop, the browser and the page cache. |
| `agentsMemoryMax` | `agentsMemoryHigh` + about 6 % of RAM (default `46%`) | Keep the gap narrow. Past it the kernel OOM-kills a whole thread. |
| `agentsMemorySwapMax` | about 20–35 % of RAM, below the swap device (default `30%`, a share of the swap) | Keeps the agents' cold pages from filling the disk swap. |
| `agentsCpuCores` | none (the default) or cores − 1 | The agents' low `cpu.weight` already gives the desktop the CPU first; a cap only bounds them when the desktop is idle. |
| `threadMemoryHighPercent` | 0 (off) | Each thread's own `memory.high` (decision 0032 slice 3). A throttled thread stalls and becomes the first one oomd's rule takes (decision 0036 (B)); with agentsUnit that is the whole thread, never fyrd. *Removed by V0.share-shadow: fyrd ignores it (one log line) and the module only warns when it is not 0; the `guard: budget over … mode=shadow` lines measure a 12 % share instead.* |

## macOS

There are no cgroups, PSI or oomd. Fyr reads `kern.memorystatus_level`, the page counts, the swap traffic as the memory stall (V0.mac-swap-stall) and the run queue (V0.mac-load-signals) and pauses work with SIGSTOP. Nothing needs setting. Keep enough free disk for the system's own swap files.

## Checking a host

- `oomctl`: which cgroups oomd watches, and at what limit.
- `swapon --show` and `cat /sys/module/zswap/parameters/enabled`.
- `grep 'guard:' ~/.local/share/fyr/daemon.log` (or `journalctl -u fyr | grep guard:`): every decision the guard made, with the signals it acted on (decision 0032).
- `journalctl -u systemd-oomd | grep Marked`: every cgroup oomd killed. With agentsUnit each names a `fyr-agent-<thread>.scope`. A line naming `fyr.service` means the settings above are not in place.
