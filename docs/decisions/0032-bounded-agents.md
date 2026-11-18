# 0032 Bounded agents: one thread's heavy work cannot starve the host, and the guard says what it did

Date: 2026-11-18 · Status: **proposed; slice 1 is built** (owner, 18 Nov: "Can we protect the daemons from the workloads?", "Be creative - this is such a common problem for me", "Do a real plan and start implementing", "Add relevant logging as well so we can tune this going forward") · Amends V0.164 (7) · Builds on decision 0029 and V0.151

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
- Not measured, to do: lowering a limit below the current use (the one run was inconclusive), and `memory.reclaim` written into a frozen cgroup to push its pages to swap before the next slice starts.

## Decision

Bound the work, not only the daemon. In four layers, built in slices, each testable alone.

1. **The guard keeps acting (slice 1, built).** The freeze is a time slice of a long episode, not a once-only verdict. Rules in V0.188 below. Every decision leaves a log line with the signals it acted on.
2. **Freeze by job, rotate by measured slice (slice 2).** A thread's heavy roots get one cgroup each. When a thread's `agents/<thread>` cgroup thrashes (its own memory PSI `full` over a share, its swap near its cap) all jobs but one are frozen and the rest rotate; a slice is 3 × (the job's resident size ÷ the measured swap-in rate), never under 10 s. The test above is the evidence for the 10 s floor.
3. **A small default budget per thread, a reservation raises it (slice 3).** Each thread's cgroup gets `memory.high` of about 12 % of the agents' budget (a share, never a number: decision 0029 section 0). A slot reservation (`slot_acquire`) raises it live to the class's learned peak and the release lowers it again. Past the reservation a job is throttled, and `memory.max` at the old ceiling is the fast failure. The livelock case (high with no swap) is avoided by the rule that the thread's swap share is never below its high.
4. **The machine backstop (slice 0, in the NixOS repo).** `agentBudget.pressureLimit` 80 → 70 so systemd-oomd kills the heaviest agent cgroup within about 20 s of a stall like this one (50 % was too eager on 17 Nov, 80 % never fired at 75 %).

Not chosen: a fixed small `memory.max` per thread (kills the agent CLI when the OOM killer picks it); SIGSTOP of single processes (misses children born later, and a stopped process keeps its pages until reclaim takes them); more slots (the incident had no slot).

## Slice plan

| slice | what | state |
|---|---|---|
| 0 | `agent-budget.nix` pressure limit, an option for the thread share | in the nixos repo |
| 1 | V0.188: re-freeze, release an empty freeze, somebody-else-gains rule, guard log | built (this PR) |
| 2 | per-root job cgroups, rotation with the measured slice | next |
| 3 | per-thread `memory.high` share, reservation raises it, `/api/load` shows it | after 2 |
| 4 | tune from the log (below) | ongoing |

## What the log gives for tuning

Every line starts with `guard:` in `fyrd`'s log (`journalctl -u fyr | grep guard:`):
- `memory short / recovered` with headroom, stall, slowdown, the number flagged, held, running threads and what is frozen: how often and how long the host is short.
- `froze undeclared work` / `froze job`: thread, the freeze's number in its episode, the episode's age, the command, memory and cores, the signals, and how many threads were running: the data to choose `RefreezeAfter`, `MaxFreezes` and the thresholds.
- `thawed …`: how long it was frozen and why (memory fine for 30 s, its processes are gone, the 10 min limit, the episode ended).
- `not freezing …` and `could not freeze …`: the reason, once a minute at most.

Questions the log should answer: how long does a slice need (time from freeze to thaw to refreeze against the swap-in rate); how often does the cap of 8 hit; does a freeze ever release memory (compare headroom at the freeze and at the thaw).
