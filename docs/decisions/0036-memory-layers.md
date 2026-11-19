# 0036 Memory layers: the daemon is never a victim, jobs size themselves, services are not jobs, and a rerunnable job may be killed

Date: 2026-11-19 · Status: **proposed; the owner chose a decision doc and opt-in kill-and-retry on 19 Nov** · Builds on decisions 0029 and 0032, V0.164, V0.188, V0.194, V0.198, V0.freeze-old-work, V0.load-tuning · Makes #221 urgent

The owner, 19 Nov: "Prior art and tech detail research for the anti swapp deload fixes.... What is the best way to manage this? What are we missing?"

## What the host did (meros-laptop, 17–19 Nov)

The host: Linux 7.3, 8 cores, 30 GB RAM, zram 20 GB (priority 5) in front of 34 GB disk swap, zswap off, MGLRU on with `min_ttl_ms` 0. `fyr.service` has `Delegate=cpu memory`, `ManagedOOMMemoryPressure=kill` at 70 % for 20 s, and `MemoryLow=1G`. `agents/` has `memory.high` 12 GB, `memory.max` 14 GB and `memory.swap.max` 10 GB. Sources: `oomctl`, `journalctl -u systemd-oomd` and the `guard:` lines in `~/.local/share/fyr/daemon.log`.

1. **systemd-oomd killed fyrd's own cgroup.** On 18 Nov at 15:30:18 it marked `fyr.service/server` for killing (2.2 GB, its own pressure 0) because `fyr.service` was at 74.8 % > 70 %. In the four minutes before that, it marked agent cgroups of 3.7 MB five times. On 17 Nov it marked agent cgroups at 50 %, and on 19 Nov at 10:26 one of 134 MB. #221 says "oomd happens to pick agents/<thread> and never server/". That is no longer true.
2. **The guard did not see what oomd saw.** On 18 Nov its last line before the kills was at 15:23:29: `memory short: headroom=5% stall=ok … flagged=0 … running=1`. It wrote nothing until fyrd restarted at 15:30.
3. **A timed thaw lets a job go into a critical stall.** All six thaws for "10 min limit" in the log happened while both the host's and the agents' stall were critical. On 19 Nov from 09:58 to 10:31 the agents' headroom was 0–1 % and the stall critical for 33 minutes. The guard held one freeze at a time and released each after 10 min. Six times in a row it logged `could not freeze … no process to move` for a thread whose memory lived somewhere else.
4. **The guard froze things that do not hold the memory, or must not stop:**
   - a `docker compose` client, whose containers sit in docker's own cgroups, outside `agents/`;
   - `curl -fsS http://127.0.0.1:8000/health` at 2.5 GB (the thread's memory, named by its newest command);
   - three `process-compose` dev stacks of 3.6–4.4 GB, which a thread's e2e tests talk to.

## What the kernel and systemd do (read from source)

Sources: systemd v262 `src/oom/` and kernel master (7.4-rc3) `mm/memcontrol.c`, `mm/vmscan.c`, `kernel/sched/psi.c`.

- **Who oomd may kill.** oomd walks down from each monitored cgroup. Every leaf is a candidate, and so is any cgroup with `memory.oom.group=1` (oomd-manager.c, `recursively_get_cgroup_context`). A pressure kill needs `memory.pressure` full avg10 over the limit for the set duration, plus some reclaim in the last 30 s. Candidates are ranked by preference, then by the raw pgscan delta since the last poll, then by `memory.current` (oomd-util.h, `compare_pgscan_rate_and_memory_usage`). The delta is not scaled by size, so a small cgroup that kswapd just scanned can come first. That explains the 3.7 MB victims.
- **Who can opt out.** On a pressure kill, oomd honours `user.oomd_omit` / `user.oomd_avoid` on a cgroup that is root-owned or has the same owner as the monitored cgroup. On a swap kill, only on a root-owned cgroup. The xattr is not inherited. This host's `/`, `/system.slice` and `/agents.slice` are also monitored, at 80 %. An xattr that meros writes on `server/` would therefore protect it from the `fyr.service` rule only, not from those three.
- **Fyr's own limit is stall.** Reclaim at `memory.high` (`reclaim_high`) and the penalty sleep (`__mem_cgroup_handle_over_high`) are both wrapped in `psi_memstall_enter/leave`, and PSI charges every ancestor. That throttling also raises pgscan. So every `memory.high` that fyrd sets on purpose (decision 0032 slice 3, or one per job) pushes `fyr.service` toward oomd's limit, and makes the throttled cgroup oomd's first choice. `memory.reclaim` also counts in pgscan.
- **zram in front of disk swap inverts the LRU.** zram fills first with cold pages. After that, pages that are still in use go to disk. zram writeback (`backing_dev`) runs only when something triggers it, not from reclaim. zswap has a shrinker, per-cgroup `memory.zswap.max` and `memory.zswap.writeback` (6.8+), and writes back the least recently used pages. Meta's guidance (Chris Down, March 2026): "If in doubt, prefer to use zswap", "Do not run zram alongside disk swap". The same post says zram memory is not charged to cgroups (not checked in source). If true, per-cgroup swap figures are understated, and `memory.reclaim` into zram moves pages out of the cgroup's account, not out of RAM. That would also explain why decision 0032 measured no gain from it.
- **PSI triggers.** Writing `"full 150000 2000000"` to a `memory.pressure` file and polling for `POLLPRI` notifies within one window. An unprivileged trigger needs a window that is a multiple of 2 s. With `CAP_SYS_RESOURCE` on the opener the window can be 500 ms. Today the guard reads avg10 on a 10 s tick, and oomd adds its 20 s duration on top.
- **Time spent frozen.** `cgroup.stat.local` has `frozen_usec` (present on 7.3). It was added because watchdogs read a freeze as a missed deadline. The kernel maintainers left deadline compensation to whoever sets the deadline. A time namespace cannot be changed for a process that is already running.

## What others do

| System | What applies to Fyr |
|---|---|
| **Buck2 resource control** (`app/buck2_resource_control/src/scheduler.rs`, `forecast.rs`) | The strategy is `cgroup_freeze` or `kill_and_retry`. Holding back an action that has not started yet is preferred over freezing a running one. The oldest action is never suspended ("guarantees forward progress"). The oldest suspended action wakes first, at most once every 3 s. It wakes when current memory plus its memory at suspension fits under a cap learned as total memory at the moment PSI rose above 10. Hysteresis: decrease above 10 % (5 s average), increase only below 5 % (30 s average). It forecasts when oomd would fire and spreads suspensions over that time. |
| **Node, Go, JVM, test runners** | libuv sizes Node's heap from the smaller of `memory.max` and `memory.high` of the process's **own** cgroup (no ancestors). Go 1.25's GOMAXPROCS, and so `go test -p`, follow `cpu.max`. GOMEMLIMIT is not derived from cgroups. Jest (and Vitest, it seems) follow `availableParallelism()`, which honours `cpu.max`. Playwright counts host CPUs and must be capped. pytest-xdist reads `PYTEST_XDIST_AUTO_NUM_WORKERS`. turbo's `--concurrency` defaults to 10. The JVM reads `memory.max` only. |
| **Google Autopilot, Kubernetes VPA** | Memory is estimated from peaks: Autopilot uses max or p98 with a 48 h half-life, VPA p90 + 15 %. After an overrun the estimate is raised at once (VPA ×1.2 or +100 MB) and lowered slowly. Serving and batch jobs get different statistics. Kubernetes evicts first the pods that are furthest over their requests. |
| **Android lmkd, ChromeOS resourced** | They react to PSI triggers with 50–700 ms thresholds in 1 s windows, not to averages. They act in steps by priority band. resourced works out a reclaim target in KB and discards until the target is met, instead of "one victim". lmkd frees a killed process's memory at once with `process_mrelease`. |
| **HTCondor, Slurm** | A job over its request is held and rerun with a larger request. Slurm's suspend keeps the memory charged. |
| **Jobservers** | GNU make 4.4's fifo jobserver is used by ninja 1.13, cargo and rustc. A host-wide CUSE jobserver ("steve", Gentoo 2025) takes tokens back when the holder exits. Go, tsc, Playwright and pytest ignore it. |
| **Other multi-agent coding tools** | container-use, Sculptor and the worktree write-ups have no CPU or memory control between agents. Fyr seems to be ahead here. |

## Decision

Six layers, each acting before the next one has to.

| Layer | Acts on | Today | Change |
|---|---|---|---|
| 0. Host setup | swap, the kernel's and oomd's backstops | zram + disk swap; oomd on `fyr.service` | (A), (B) |
| 1. A job sizes itself | the job's own process | nothing | (C) |
| 2. Admission | the queue | 0029, V0.209 | services reserved (D) |
| 3. Pause | a job, before or while it runs | freeze, turns, a 10 min thaw | (E) |
| 4. Kill and rerun | an opted-in class | never | (F) |
| 5. oomd | a whole thread | any leaf under `fyr.service`, including `server/` | only `fyr-agents`, a whole thread (B) |

**(A) zswap instead of zram, in the NixOS repo.** Turn zswap on with its shrinker, keep the disk swap, and remove zram. Re-check `vm.swappiness` (120 is tuned for zram). Leave `min_ttl_ms` at 0: it calls the global OOM killer, which picks by `oom_score`, could pick fyrd, and races the guard and oomd. With zswap, `memory.zswap.max` and `memory.zswap.writeback` are available per cgroup, and what a cgroup puts in swap is charged to it.

**(B) The daemon is never an oomd candidate (#221).**
- fyrd runs in a unit that is root-owned and not delegated, with `ManagedOOMPreference=omit` and a negative `OOMScoreAdjust`.
- The agents run in `fyr-agents.slice`. It carries the limits from `agent-budget.nix` and the oomd rule. Each thread is a transient scope started through the system manager (`StartTransientUnit`, existing processes passed with `PIDFDs`, `Delegate=yes` so fyrd can still make `job/` and `undeclared/` inside it). meros needs a polkit rule, or a small root helper, to start them.
- Each thread scope sets `memory.oom.group=1`, so oomd ends a whole thread (its CLI and its work), not a stray leaf, and the 3.7 MB pattern stops.
- oomd's limit on the slice must sit above what the guard's own throttling produces: (E) has to act first. The guard logs a warning when the agents' full stall comes within 10 points of oomd's limit.
- Until (B) ships: fyrd sets `user.oomd_omit` on `server/` (same owner as `fyr.service`, so the `fyr.service` rule honours it). The NixOS module adds `MemoryMin` for the daemon and raises the `fyr.service` limit above 80 %, so the system-wide rules fire first. The slice-3 shares (V0.198) stay off until (B), because each throttled thread would become oomd's next victim.

**(C) A job sizes itself.** When fyrd moves an admitted or flagged job into its `job/` or `undeclared/` cgroup, it also sets:
- `memory.high` = the class's learned peak × 1.25 (unknown class: the free budget, at most half the agents' budget). Node reads it and sizes its heap. No `memory.max`, so nothing is killed by it.
- `cpu.max` = the class's learned demand, rounded up (at least 1 core). Go 1.25, `go test -p` and Jest follow it. *(V0.admission-memory: a class learned from under 3 runs is sized from at least the prior, 1/N of the agents, as admission counts it.)*

For tools that do not read cgroups, the thread's environment, set when fyrd spawns the CLI, gets caps derived from the same shares: `GOMEMLIMIT`, `PYTEST_XDIST_AUTO_NUM_WORKERS` and a Playwright worker count. The gate hook does not rewrite commands (decision 0029, S5 as built). Where a tool has no variable (Playwright's default, turbo's `--concurrency`), the gate hook's notice tells the agent the flag to add. The values are shares of the host's capacity (decision 0029 section 0). A limit that a job hits shows in `budget over` lines with the class, so the learned peak rises.

The swap valve of decision 0032 slice 3 applies: every job limit lifts while the agents' swap is at 90 % of its cap, because `memory.high` with no swap room livelocks.

**(D) Services are not jobs.** A command matching a service pattern (`process-compose up`, `docker compose up`, a dev server, `npm run dev`; a host setting like the heavy list) gets the service class:
- its memory is reserved from its learned peak (decaying max with a 48 h half-life) for as long as it runs, so admission counts it before a batch job starts;
- the guard never freezes it and never names it in a turn;
- containers it starts are accounted to the thread: fyrd reads the compose project from the command and sums `memory.current` of the matching `docker-*.scope` cgroups. A thread whose containers hold the memory is told so, and the guard does not freeze its compose client.

**(E) Pause better.**
1. *Delay a start before freezing a run.* Under the memory guard, a newly matched command is held at the gate (it has not started, so no timer is running) before any running job is frozen.
2. *No timed thaw into a critical stall.* The 10 min limit thaws only if the stall is not critical. Otherwise the frozen job stays frozen and the thread is told it waits for memory. (F) is the way out for a class that may be rerun.
3. *Wake by Buck2's rule.* The cap is the agents' memory at the moment the stall first went critical in this episode. A frozen job wakes, oldest first and at most one every 10 s, when the agents' 5 s memory plus the job's memory at the freeze is under the cap, and the stall has been below *elevated* for 30 s.
4. *More than one freeze.* Up to N frozen jobs per host (N = number of running threads − 1). The oldest running job and services are never frozen.
5. *Triggers, not a tick.* fyrd keeps a PSI trigger on `agents/memory.pressure` (full, 150 ms in a 2 s window, unprivileged). A notification makes the guard tick at once; the 10 s tick stays as a fallback and on hosts without PSI.
6. *Say how long it was frozen.* After a thaw, the hook output for that thread names the time frozen (`frozen_usec`), so the agent can tell a timeout caused by the freeze from a real failure.

**(F) Kill and rerun, for classes the owner opts in.** The owner, 19 Nov: yes, for opted-in classes.
- A class is rerunnable only when it is marked so in Settings → Automation (desktop and phone), for example `tsc`, `yarn lint`, `go test ./internal/...`, `make test`. Nothing is rerunnable by default. Services never are.
- Under the guard, a rerunnable job is killed instead of frozen when it was frozen and could not wake within 2 min, or when it is the job (E) would freeze and its learned wall time is under 5 min. The job's cgroup gets `cgroup.kill`, and `process_mrelease` releases the memory.
- The agent's tool result says it was killed by Fyr to free memory and that it will be admitted again. The gate holds the rerun until the memory guard is green and the class's learned peak fits.
- The class's learned peak is raised by ×1.2 (VPA) after a kill for overrun.
- A class killed 3 times in an hour stops being killed for that hour and is frozen instead, with a notice to the owner.

## Not chosen

- **MGLRU `min_ttl_ms`.** It calls the global OOM killer from kswapd, with no per-cgroup setting, and races fyrd and oomd (A).
- **Proactive reclaim (Senpai, `memory.reclaim`) while oomd watches an ancestor.** It raises pgscan, so oomd would pick the cgroup fyrd just reclaimed. Revisit after (A) and (B), and only on a frozen job or an idle service.
- **A host-wide jobserver.** It covers make, ninja, cargo and rustc, not Go or JS, which is most of the load here. Revisit if Rust or C++ projects become common.
- **CRIU checkpoint to disk.** Fragile with browsers and network sockets (Playwright, dev stacks).

## Slices

| Slice | What | Where | Risk |
|---|---|---|---|
| 0 | `user.oomd_omit` on `server/`; module: daemon `MemoryMin`, `fyr.service` oomd limit above the system rules; (E2) no timed thaw into a critical stall; guard warning near oomd's limit | fyr + NixOS repo | Low |
| 1 | (A) zswap with shrinker, disk swap, no zram; check swappiness | NixOS repo | Swap behaviour changes for the whole desktop; measure a day of `guard:` lines before and after |
| 2 | (B) #221: daemon unit, `fyr-agents.slice`, a transient scope per thread, `memory.oom.group`, migration that keeps running agents | fyr + NixOS repo | Changes V0.80's tree; polkit rule; macOS and plain Linux keep today's layout |
| 3 | (C) job `memory.high` and `cpu.max`, environment caps, the notice for tools without a variable | fyr | A wrong learned peak slows a job; the swap valve and `budget over` lines bound it |
| 4 | (D) the service class, its reservation, docker accounting | fyr | Pattern mistakes; the owner can edit the list |
| 5 | (E1, E3–E6) delayed start, Buck2 wake rule, several freezes, PSI trigger, frozen time in hook output | fyr | More frozen work at once; the oldest job and services still run |
| 6 | (F) rerunnable classes, kill, `process_mrelease`, rerun gate, the owner's switch on desktop and phone | fyr | A class wrongly marked rerunnable loses work; nothing is marked by default |

The host settings these slices assume (zswap, oomd limits, the cgroup budget) are written down for the owner in `docs/HOST-SETUP.md`.

Each slice adds its `guard:` log lines first, as decision 0032 did, so it can be tuned from the log.

## The owner's answers (19 Nov)

1. **How to take the findings forward:** a decision doc (this one), then PRs by slice.
2. **Kill and rerun:** yes, for classes the owner opts in (F).

## Sources

- systemd v262 `src/oom/oomd-util.c`, `oomd-util.h`, `oomd-manager.c`: https://github.com/systemd/systemd/tree/v262/src/oom · `ManagedOOMPreference=`: https://www.freedesktop.org/software/systemd/man/latest/systemd.resource-control.html · https://www.freedesktop.org/software/systemd/man/latest/systemd-oomd.service.html
- Kernel: cgroup v2 (`memory.high`, `memory.reclaim`, `memory.zswap.writeback`, `cgroup.freeze`, `cgroup.stat.local`): https://docs.kernel.org/admin-guide/cgroup-v2.html · PSI and triggers: https://docs.kernel.org/accounting/psi.html · MGLRU: https://docs.kernel.org/admin-guide/mm/multigen_lru.html · zswap: https://docs.kernel.org/admin-guide/mm/zswap.html · zram: https://docs.kernel.org/admin-guide/blockdev/zram.html · `frozen_usec`: https://patchew.org/linux/20250822013749.3268080-6-ynaffit@google.com
- zswap vs zram: https://chrisdown.name/2026/03/24/zswap-vs-zram-when-to-use-what.html
- Buck2: https://github.com/facebook/buck2/blob/main/app/buck2_resource_control/src/scheduler.rs, `scheduler/forecast.rs`, `scheduler/suspend_timing.rs`, https://github.com/facebook/buck2/blob/main/app/buck2_common/src/init.rs
- Bazel: https://bazel.build/reference/command-line-reference
- libuv: https://github.com/libuv/libuv/blob/v1.x/src/unix/linux.c · Node: https://github.com/nodejs/node/blob/main/doc/api/cli.md · Go: https://go.dev/blog/container-aware-gomaxprocs · JDK: https://github.com/openjdk/jdk/blob/master/src/hotspot/os/linux/cgroupV2Subsystem_linux.cpp · Jest: https://github.com/jestjs/jest/blob/main/packages/jest-config/src/getMaxWorkers.ts · Playwright: https://playwright.dev/docs/api/class-testconfig · pytest-xdist: https://pytest-xdist.readthedocs.io/en/stable/distribution.html · turbo: https://turborepo.dev/docs/reference/run
- Autopilot: https://research.google/pubs/autopilot-workload-autoscaling-at-google-scale/ · VPA: https://github.com/kubernetes/autoscaler/blob/master/vertical-pod-autoscaler/docs/flags.md · Kubernetes eviction: https://kubernetes.io/docs/concepts/scheduling-eviction/node-pressure-eviction/
- lmkd: https://source.android.com/docs/core/perf/lmkd · ChromeOS resourced: https://cos.googlesource.com/third_party/platform2/+/refs/heads/main/resourced/src/memory/psi_monitor.rs
- HTCondor hold and rerun: https://www-auth.cs.wisc.edu/lists/htcondor-users/2018-January/msg00136.shtml · Slurm preemption: https://slurm.schedmd.com/preempt.html
- Jobservers: https://www.gnu.org/software/make/manual/html_node/POSIX-Jobserver.html · https://blogs.gentoo.org/mgorny/2025/11/30/one-jobserver-to-rule-them-all/
