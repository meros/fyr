# 0032 Bounded agents: one thread's heavy work cannot starve the host, and the guard says what it did

Date: 2026-11-18 · Status: **slices 1, 2 and 3 are built (3 is off until a share is set); the owner accepted the two relaxed V0.164 rules on 18 Nov** (owner, 18 Nov: "Can we protect the daemons from the workloads?", "Be creative - this is such a common problem for me", "Do a real plan and start implementing", "Add relevant logging as well so we can tune this going forward") · Amends V0.164 (7) · Builds on decision 0029 and V0.151

## The incident

On 18 Nov 06:30–07:30 on `meros-laptop` one thread (a `/norban:fix` agent) ran `turbo run lint lint:types`: two `tsc` and two `tsgolint` processes, 9.7 GB together, which is the whole of `agents/` (`memory.high` 9 GB, `memory.swap.max` 6 GB). They ran 28 minutes. The Fyr UI showed messages as "Waiting" for the whole time. The owner's desktop was fine (`user.slice` memory pressure 0.1 %); `server/` was fine (0 %). Only `agents/` was at 75 % `some` / 53 % `full`.

**What the cgroup did.** The four processes evicted each other's pages: about 25 000 major faults a second and 26 GB of file pages refaulted in 20 s, while each got 30–60 % of a core. The agents' swap was at 5.2 of 6 GB. Every other agent's CLI started slowly ("the agent starts slowly under memory pressure; waiting"; "Stop not answered within 20 s").

**Why V0.164 did not stop it.** The guard acted once, at 06:28 (`undeclared/` of the thread was created and frozen, and the thread was told). It never acted again, for three reasons that were each a deliberate rule:
1. *Once per episode* (`undFroze[thread] == Since`). The episode never ended because heavy work kept starting, so there was one episode for the whole hour.
2. *One frozen thing per host* (`e.frozen`). The 06:28 freeze held no process by then (`populated 0`) but counted as holding until memory was fine for 30 s, which never came, or 10 minutes passed.
3. *"Never when it helps nobody"*: without an admitted job or a second flagged thread, no freeze. It ignored that a thrashing cgroup starves every other thread's agent.

## What was measured (this laptop, 4 jobs of 300 MB, 4 M random touches each, `memory.high` 450 MB; zram swap)

| strategy | all four finish | first finishes |
|---|---|---|
| all at once | 53.6 s | 44.6 s |
| round-robin, 1 s slices | 97.5 s | 94.9 s |
| round-robin, 3 s slices | 16.9 s | 2.9 s |
| round-robin, 10 s slices | **9.6 s** | 2.9 s |
| one at a time | 11.5 s | 3.2 s |
| all at once, budget 1.5 GB (fits) | 3.5 s | 2.7 s |

- A budget the jobs do not fit in turns into thrash, and thrash is worse than the sum of the jobs run one at a time (4.7×). Slicing by stop and continue wins back that loss, but only when a slice is long next to the time to swap the job's working set back in; at 1 s it is worse than doing nothing.
- `memory.max` with no swap kills in 0.4 s (rc 137). `memory.high` with no swap, or with a full swap cap, never made progress in 25 s: it is a livelock, not a slowdown.
- With swap, one job under a limit it cannot fit slows smoothly: 8× at 75 % of its working set, 20× at 50 %.
- Raising `memory.high` of a running job takes effect at once (1.5 M touches/s within a second of the raise).
- `memory.reclaim` written into a frozen job's cgroup, to push its pages to swap before the next slice starts: one run each, 4 jobs in their own cgroups under a shared slice at 450 MB. 3 s slices: 34.6 s without, 27.1 s with; 1 s slices: 28.7 s without, 28.5 s with. No consistent gain. The same setup also did not repeat the 1 s collapse of the table above (97.5 s there), so the slice length rule rests on one setup (SIGSTOP of processes in one cgroup) and needs repeat runs with per-job cgroups before slice 2 fixes the floor.

### Repeated with one cgroup per job (18 Nov, for slice 2)

4 jobs of 300 MB under a shared `memory.high` of 450 MB, each in its own scope, taking turns by `cgroup.freeze`; 40 M touches, about 13 s per job alone. Two runs each, total seconds. The first run overlapped with compiling on the same host and is slower for every rotation; it was not repeated in isolation, so read the second as the cleaner one.

| turn | run 1 | run 2 | first job done (run 2) |
|---|---|---|---|
| 1 s | not done in 150 s | not done in 150 s | none |
| 3 s | 107.4 | 65.4 | 57.4 |
| 10 s | 84.4 | 56.3 | 44.2 |
| 30 s | 82.0 | 53.7 | 14.3 |
| one at a time | 55.3 | 50.9 | 13.5 |

With 4 M touches, all at once took 40.8 and 45.4 s against 5.3 s one at a time (one run). This settles the open point above: **1 s collapses with per-job cgroups too**, and a longer turn was never worse. Slice 2's floor is 30 s, not 10 s: the parts belong to one thread, so no one else waits for a turn.

- `memory.reclaim` on the job just frozen, 10 s turns: 56.4 and 58.5 s without, 61.7 and 56.0 s with. No gain; slice 2 does not use it.
- Lowering `memory.high` under a running 300 MB job (10 M touches, swap available, two runs): unchanged 5.1 and 4.9 s; to 75 % 24.5 and 29.0 s (about 5×); to 50 % 42.1 and 40.8 s (about 8×). Its memory was at the new limit 2 s later and the job finished. Lowering works and is a slowdown, not a hang, as long as swap has room.

### Slice 3 under real cgroups (18 Nov, `TestRealThreadBudget`, not proof of benefit)

`agents/` limited to 1200 MB, a share of 25 % (300 MB), a second thread's small job beside the big work; one run of each load.

| load | small job, no share | small job, share | big work held in memory |
|---|---|---|---|
| one 1500 MB job, small job 200 MB | 2.7 s | 1.9 s | 1006 → 299 MB |
| four 500 MB jobs, small job 250 MB | 18.3 s | 17.1 s | 1000 → 300 MB |

A repeat of the second load gave 17.7 s without and 16.5 s with the share. The share slowed the big work 1.7 times in the first run of that load and 1.1 times in the repeat, so that cost is not settled either. The limit is enforced, but these runs did not show that it protects another thread: the small job was never timed alone, so how much the neighbour hurt it is unknown. The owner chose to build it anyway and tune it from the log (18 Nov: "make sure we can tune and tweak from logs later"). Measure first: the share is off by default, and the log reports what it would meet.

### Slice 2 under real pressure (18 Nov, `TestRealGuardTakesTurns`)

The guard's engine at its 10 s tick and the real cgroup code in a delegated service, `agents/` limited to 450 MB, one runner with four 300 MB children. Without the guard the jobs had not finished after 4 minutes and had read back 163.9 GB. With it they finished in 77 s, read back 3.9 GB, all four had a turn, and at no tick did two run at once. One run. The test sets "memory is short" itself and the jobs are synthetic, so it proves what the guard does once it acts, not when a real stall makes it act.

## Decision

Bound the work, not only the daemon. In four layers, built in slices, each testable alone.

1. **The guard keeps acting (slice 1, built).** The freeze is a time slice of a long episode, not a once-only verdict. Rules in V0.188 below. Every decision leaves a log line with the signals it acted on.
2. **Freeze by part, rotate by measured turn (slice 2, built).** When the guard freezes undeclared work with two or more heavy parts, one part runs and the others are frozen, each in `undeclared/u<pid>/`. A part is found by walking down from each root to where the memory fans out (at least 10 % of the subtree per child), because a runner such as `turbo` is one root with several heavy children; a root-level split would not have helped in the incident. The runner itself keeps running. A turn lasts 3 × (the next part's resident size ÷ the read-back rate measured in the last turn), at least 30 s and at most 2 min, and ends early when its part ended. The trigger is the guard's existing one, not a new per-thread signal.
3. **A default budget per thread, a slot raises it (slice 3, built, off until the owner sets a share).** With `threadMemoryHighPercent` set, each thread's `memory.high` is that share of the agents' budget (a share, never a number: decision 0029 section 0). A slot raises it live by 1.25 × the job class's learned peak, and the release lowers it; a job of unknown size runs without a limit of the thread's own. The livelock case (high with no swap) is avoided by lifting every thread's limit while the agents' swap is at 90 % of its cap. No thread `memory.max`: a job is slowed, never killed by this.
4. **The machine backstop (slice 0, in the NixOS repo).** `agentBudget.pressureLimit` 80 → 70 so systemd-oomd kills the heaviest agent cgroup within about 20 s of a stall like this one (50 % was too eager on 17 Nov, 80 % never fired at 75 %).

Not chosen: a fixed small `memory.max` per thread (kills the agent CLI when the OOM killer picks it); SIGSTOP of single processes (misses children born later, and a stopped process keeps its pages until reclaim takes them); more slots (the incident had no slot).

## Slice plan

| slice | what | state |
|---|---|---|
| 0 | `agent-budget.nix` pressure limit, an option for the thread share | in the nixos repo |
| 1 | V0.188: re-freeze, release an empty freeze, somebody-else-gains rule, guard log | built (this PR) |
| 1b | V0.188: `slots.guard` in `/api/load`; the row's words and Settings → Automation → Memory guard (chart, summary, last decisions) on the desktop and the phone | built (this PR) |
| 1c | V0.191: the second freeze of an episode works (slice 1 could not re-freeze on a real cgroup) | built |
| 2 | V0.194: parts of the work in their own cgroups, turns of a measured length | built |
| 3 | V0.198: per-thread `memory.high` share, a slot raises it, `/api/load` shows it, the log measures it even while off | built, off by default |
| 4 | tune from the log (below) | ongoing |

## What the log gives for tuning

Every line starts with `guard:` in `fyrd`'s log (`journalctl -u fyr | grep guard:`):
- `memory short / recovered` with headroom, stall, slowdown, the number flagged, held, running threads and what is frozen: how often and how long the host is short.
- `froze undeclared work` / `froze job`: thread, the freeze's number in its episode, the episode's age, the command, memory and cores, the signals, and how many threads were running: the data to choose `RefreezeAfter`, `MaxFreezes` and the thresholds.
- `thawed …`: how long it was frozen and why (memory fine for 30 s, its processes are gone, the 10 min limit, the episode ended).
- `not freezing …` and `could not freeze …`: the reason, once a minute at most.
- `taking turns …` and `turn …`: the parts, whose turn it is, its memory, the turn's length and the read-back rate it was measured from.

- `budget limit`: a thread's `memory.high` changed: the new and old value, why (`share`, `slot`, `slot-unknown-size`, `slot-fills-budget`, `swap-full`, `off`), the job's learned peak and the thread's memory.
- `budget over` (once a minute per thread): working memory (without inactive file cache) over the limit, or with `mode=shadow` over the 12 % a share would give while the share is off: working, memory, limit, swap, read-back rate, `memory.events` high count since the last line, the thread's memory PSI, and its slot.
- `budget summary` (once a minute while someone is over): the mode, share, budget, how many threads are limited and over, and the agents' memory and swap. `budget limits lifted/back`: the swap valve.

Questions for the share: in `mode=shadow`, which threads would be over, how often, and are they the ones that hurt others (compare with `memory short` lines)? Once on: does `psi_full` of an over thread stay tolerable, do `memory short` episodes get rarer, how often do the limits lift, and does a known job still hit its raised limit (then `holdMargin` is too small)?

Questions the log should answer: how long does a slice need (time from freeze to thaw to refreeze against the swap-in rate); how often does the cap of 8 hit; does a freeze ever release memory (compare headroom at the freeze and at the thaw).
