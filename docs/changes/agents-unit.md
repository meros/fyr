# V0.agents-unit: the agents run in fyr-agents.slice, one scope per thread, and fyrd's own unit is never an oomd victim

→ V0.80 (the cgroup tree), decision 0023 (the runner holds the agents), V0.146, V0.151 and V0.164 (slots, job cgroups, undeclared work), decision 0032 slice 3. Decision 0036 (B), slice 2. Closes #221.

(1) **The cause.** The agents ran in `fyr.service/agents/<thread>`, a cgroup that fyrd made inside its own delegated unit. systemd attaches an oomd rule to a unit only, so the host's rule sat on all of `fyr.service`. systemd-oomd takes as a candidate every leaf below a monitored cgroup and every cgroup with `memory.oom.group=1`, ranked by reclaim scans, not by size. On 18 Nov at 15:30:18 it marked `fyr.service/server` (fyrd, 2.2 GB, its own pressure 0) for killing because `fyr.service` was at 74.8 % > 70 %, after killing agent leaves of 3.7 MB five times in four minutes. An omit xattr cannot fix that: oomd honours it only on a root-owned cgroup (or, on a pressure kill, one with the monitored cgroup's owner), and `server/` is meros's. `/`, `/system.slice` and `/agents.slice` are monitored too on this host, all root's.

(2) **The layout.**

```
system.slice/fyr.service          the launcher and fyrd: not delegated (root's),
                                  ManagedOOMPreference=omit, OOMScoreAdjust=-500
fyr.slice/fyr-agents.slice        the agents' budget (CPUQuota, MemoryHigh/Max/SwapMax)
                                  and the oomd rule (default 80 %)
  fyr-agent-<thread>.scope        one thread: Delegate=yes, OOMPolicy=kill
    job/ undeclared/              (memory.oom.group=1), PartOf=fyr.service
  fyr-agent-<thread>.1.scope      a second process of the thread while the first runs (a handover)
  fyr-deploy-<pid>-<n>.scope      one command of a self-deploy
```

oomd under the slice can only take a whole thread: its CLI and everything it started, never a job alone and never fyrd. fyrd's unit is root-owned, so its omit holds against every rule, and the kernel's OOM killer passes it over. Stopping `fyr.service` stops the scopes (PartOf), as stopping the unit ended the agents before. The agents set their `oom_score_adj` back to 0 as they start, so they do not inherit fyrd's.

(3) **How a thread gets its scope.** A process in `fyr.service` cannot start in, or move into, a cgroup under another slice: the kernel wants write access to the common ancestor, which is root's. So fyrd starts the CLI as `systemd-run --scope --unit=fyr-agent-<thread>.scope --slice=fyr-agents.slice -p Delegate=yes -p User=<user> -p OOMPolicy=kill -p PartOf=fyr.service -p CPUWeight=<w> -- <cli>`. systemd-run asks the system manager to make the scope with its own pid and then execs the CLI there, so the CLI and its children are in the scope from the first instruction, as with CLONE_INTO_CGROUP. The runner (decision 0023) starts that command line unchanged: the runner needs no change and no restart. Inside the delegated scope fyrd makes `job/` and `undeclared/` and moves processes as before. The scope's own `cpu.weight` (focus, project priority) and `memory.high` (the thread share) are root's files, so fyrd sets them with `systemctl set-property --runtime`. The scope goes by itself once its last process ends (`--collect`).

(4) **The permission: a polkit rule, not a root helper.** systemd passes the unit name and the verb to polkit on `StartTransientUnit` and `SetUnitProperties` (systemd v262 `src/core/dbus-util.c`, `bus_verify_manage_units_async_impl`). The module's rule allows the service user `start` and `set-property` on units named `fyr-(agent|deploy)-….scope`, nothing else: no other unit, no stop, no kill, and a scope runs no command of its own. A root helper would be a new root daemon with a protocol of its own to get right, for the same two calls. What the rule still allows: systemd does not check whose pids a new scope takes (`unit_pid_attachable`), so the service user could put another user's process into such a scope and so into a cgroup it owns. That user is the owner's own login on this host. Should a host run Fyr as a user that must not do that, turn `cgroups.agentsUnit` off.

(5) **Without the slice.** fyrd tries the scopes at start only when the config names the slice (`cgroups.agentsSlice`): a probe process must land in `fyr-agents.slice/fyr-agent-probe-<pid>.scope` and be able to make a child cgroup there. Otherwise it keeps the V0.80 split when the unit is delegated, else cgroups are off, exactly as before. macOS, a dev run in a terminal and plain Linux without the module keep their layout. The log says which: `cgroups: on, layout scope: …`, `cgroups: the agents' slice … is not usable (…); keeping the unit layout`, or `cgroups: on, layout unit, …`. `/api/load` has `cgroupLayout` (`"scope"` or `"unit"`).

(6) **The switch keeps running agents.** A rebuild changes `config.json`, which reloads fyrd (V0.180) while the runner keeps the agents. The new fyrd still finds its unit split (the unit's own settings change at its next restart), so it keeps `fyr.service/agents/` as the legacy place: a thread from before runs on there, and the guard, the job cgroups, the leftovers and the load read it there until it ends. New threads, and a thread's next process, start in scopes. After the next restart of `fyr.service` only the scope layout is left.

(7) **What reads the thread cgroups.** All of it goes through the tree now: the load and its per-thread rows (a thread with two cgroups is one row, their sum), the capacity, headroom and stall signals (from the slice), the slots' job records, the job cgroups and their freeze (decision 0029 S5), the undeclared-work roots and freeze (V0.164), reclaim and the swap valve, the thread share (decision 0032 slice 3), the leftovers (V0.kept-not-leftover) and a thread's CPU time (V0.195). In the scope layout the newest scope of a thread is its cgroup.

(8) **The module.** `services.fyr.cgroups.agentsUnit` (default on) makes `fyr-agents.slice` with the existing `agentsCpuCores`, `agentsMemoryHigh`, `agentsMemoryMax` and `agentsMemorySwapMax`, and the oomd rule from `agentsOomPressureLimit` (default 80 %, null for none) and `agentsOomPressureDuration`. `fyr.service` loses `Delegate` and gets `ManagedOOMPreference=omit` and `OOMScoreAdjust=-500`; `serverMemoryLow` is its `MemoryLow`. The polkit rule and `security.polkit.enable` come with it. Off, the module writes V0.80's unit as before.

(9) **What the owner does on the host.**
- Remove the oomd rule on `fyr.service` from the machine's configuration (agent-budget.nix): the slice has its own now, and fyrd's unit is omitted.
- Point a QoS script that rewrites the agents' CPU cap (agents-lock-cap) at `/sys/fs/cgroup/fyr.slice/fyr-agents.slice/cpu.max`.
- `nixos-rebuild switch`. New threads go to scopes at once; check `journalctl -u fyr` or `~/.local/share/fyr/daemon.log` for `layout scope`, and `systemctl list-units 'fyr-agent-*'`.
- Restart `fyr.service` once at a quiet moment (it ends running turns, as any restart does) so fyrd's unit is no longer delegated. Until then `fyr.service/server` is still meros's and not protected from the system-wide rules.
- Check with `oomctl` that `fyr-agents.slice` is monitored and `fyr.service` is not a candidate.

(10) **Limits.** A self-deploy now starts each build and test command in a scope of its own; the guard sees them as `self-deploy` as before. A thread's CLI that a previous fyrd started is counted where it runs. If systemd-run fails for one thread after the probe worked (polkit was changed under a running fyrd), that thread's CLI does not start and the agent reports the error; fyrd does not fall back per thread.

Tests: `internal/cgroup/layout_test.go` (TestAgentsUnitScopeLayoutWhenTheSliceWorks, TestAgentsUnitFallsBackToTheUnitLayout, TestAgentsUnitScopeNames, TestAgentsUnitSecondProcessGetsTheNextScope, TestAgentsUnitSwitchKeepsRunningThreads, TestAgentsUnitScopeWeightsGoThroughSystemctl, TestAgentsUnitLoadShowsTheLayoutAndReadsTheSlice), `internal/cgroup/agents_unit_oomd_test.go` (TestAgentsUnitOomdKillOfTheSliceNeverTakesFyrd walks the tree as systemd-oomd does; TestAgentsUnitUnitLayoutLetsOomdTakeServer is 18 Nov in the old layout), `internal/cgroup/agents_unit_real_test.go` (TestRealAgentsUnit: systemd's user manager and the kernel, `FYR_REAL_SCOPES=1`), and the module check in `flake.nix`.
