# V0.freeze-controller: the load simulation gets the 26 Nov incident and a load ladder; three pressure-driven freeze rules were simulated and not taken

→ V0.load-sim, V0.sim-fixes, V0.freeze-cap-holds-memory.

Why: on a 30 GiB laptop the agents sat in a critical stall for minutes while the guard froze at most as many threads as it allowed. The owner asked for one feedback loop in place of the fixed limit, the 60 s wait and newest-first, simulated and researched.

(1) **Scenarios.** `incident-26nov` (one thread's four lint parts of 7.5 GB, a 5 GB smoke run, six small jobs, nine agents, swap cap 10 GB); `incident-retry` (the same with the desktop at 9 GB and a new lint run every 30 s, which is what the log showed); `ladder-2/4/6/8` (n threads, each a 3 GB undeclared job). The first model of the incident did not reproduce it (35 s of critical stall against 2110 s with no Fyr); the retry and the desktop did (6 of 37 jobs done, 31 threads lost to oomd). That is listed in the scenario's `expect` as a finding.

(2) **Tried, not taken.** Each was run against the whole table:
- the freeze limit gives way to pressure (all heavy threads may freeze, whole, when the unit is near oomd's limit or swap is full in a critical stall): `ladder-6` lost a thread to oomd that it kept before, and `incident-retry` did not improve (31 killed, critical stall 184 s against 189 s);
- the thread holding the most goes first, and the wait between freezes shrinks from 60 s to none as the unit's stall goes from 10 % to oomd's limit: `ladder-4` took 7 min (5 before) with 8 freezes (3), `ladder-6` and `ladder-8` lost threads to oomd.
Faster freezing flaps: a thaw pages the work back in and raises the stall again, so more freezes cost more than they save in this model. The model counts the pages the kernel reclaimed from a frozen cgroup as its pgscan, so oomd picks the frozen thread; how real oomd ranks it is not measured.

(3) **Built: the pool counts a thread's runs at once.** A thread held one admission whatever number of heavy commands it ran, so its lint run started every 30 s counted as one need (a learned command never waited either, once the thread held a slot). Now `Holder.Instances` and the open heavy calls per thread (`Engine.HeavyOpenCalls`, published without a lock like `HeavyOpen`) make admission count each run's need (`Admission.OpenCalls`), and the gate asks the pool for room for both before a second command starts beside one that runs (`Pool.GrowFor`), learned commands included: a concurrent run that finds no room waits and is told when it is admitted. A hold's need was the thread's class need; with several runs of mixed classes it is the hold's class times the number of runs.

Simulated (`concurrent-4/8/16` against `-old`, the tree before; `incident-retry-class`): the 20 earlier scenarios are unchanged. `concurrent-4` has no critical stall (31 s before), no freeze and no swap, in the same 8 min. `incident-retry-class` finishes 9 of 37 jobs (7 before) in 7 min (11). `concurrent-8` and `concurrent-16` barely move (critical stall 1374 s against 1405 s, 288 against 319): the stall there comes from what the pool admits together (the lint runs and the small threads' tests, with the budget shrinking while memory is short), not from the runs at once. Not solved.

(4) **What the simulation says is next.** The retry storm is not stopped by freezing: the parts start whatever the guard holds. What it needs is something that holds what the gate does not see (a thread over its share that keeps starting heavy parts), or a rerunnable class (decision 0036 (F)) killed instead of frozen. Neither is built here.

Research (control laws that were considered): Senpai's integral controller with a small probe and a quadratic backoff; Buck2's 10 %/5 % hysteresis, youngest first, oldest never; lmkd's one kill at a time. The report is in the PR description.
