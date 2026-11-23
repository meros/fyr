# V0.guard-finish: the rest of the resource guard's policy moves into internal/guard, a host without cgroups has a Mechanism of its own, and fyrd's guard wiring holds no closures; nothing the guard decides changes

→ V0.guard-mechanism (4), V0.guard-tick (3), decision 0029, decision 0032, decision 0036. Last slice of #458.

V0.guard-tick left four things outside the guard's owner: the slots' side of the tick, the job limits' sizing, the process table inside cgroup's Mechanism, and closures in `cmd/fyrd/wire_guard.go`. This change moves them and changes no decision. The 17 scenarios of the load simulation (V0.load-sim) give the same table, and the guard log of every scenario is the same line for line (thread ids and wall times aside), before and after each step.

(1) **The slots' side of the tick** (`internal/guard/sweep.go`). Each is a pure function of what the engine gathers; the engine applies the result.
- `guard.Sweep(SweepIn) → (drop, idle)`: the idle sweep (V0.86 (3), V0.201, V0.209, V0.sim-fixes (1)). It returns the threads whose admission goes back and the next idle clocks; `SlotIdle` and `HeavyCallStale` are `guard`'s tunables now.
- `guard.HeavierThanPredicted(held, told, measure)`: the notice that a job uses more than predicted, once per hold (decision 0029 S3).
- `guard.GateHolds(short, holds, priority, soft)`: the gate's hold while memory is short (V0.guard-wake E1).
- `guard.Budget(aimd, BudgetIn) → BudgetMove`: the budget's step (decision 0029 S4): whether the pool's budget is set, to what, and the line that logs the move. The budget's file (V0.budget-restart) stays inside `guard.AIMD`, which was already the policy's.

They are not folded into `Tick`: the sweep runs before the guard's tick and the budget's step after it, each on its own reading of the signals, so a single `Tick` would read them in another order. Kept as functions, the calls and readings stay where they were.

(2) **Job limits are a policy decision** (`internal/guard/joblimit.go`). `guard.JobLimit(resource.LimitAsk) → resource.LimitAnswer` decides a job's memory.high and cpu.max (V0.job-limits, V0.joblimit-slice, V0.admission-memory, V0.freeze-charge, V0.sim-fixes (2), V0.guard-tidy's turns): the learned size and the 3-run prior (`SizeFor`), the limits of a class not learned yet (sized once and kept), the swap valve, the raise after a hit, the lift in the job's own stall, and no memory.high while its parts take turns. `cgroup.JobGroups` reads what it decides from (the agents' budget, use and cores, the valve, the job's stall and memory, its turns), asks `JobGroups.Limit` (wired to `guard.JobLimit`), and writes the answer. `JobSize`, `HoldMargin` and `UnknownShare` are `resource`'s, as both sides read them.

(3) **The process table's Mechanism** (`internal/cgroup/procstop.go`, `mechanism.go`). `ProcStops` holds the SIGSTOP path that was inside `JobGroups` behind `canStop()`: the stopped pids and their store, the job's command, the holders, and freeze, thaw, kill, the empty checks, the resume at start and shutdown. `ProcMechanism` implements `guard.Mechanism` over it. `JobGroups` and `cgroup.Mechanism` are cgroups only. fyrd picks one at wiring: `ProcMechanism` when there is no cgroup tree, `Mechanism` otherwise. The sampler reads either through `cgroup.Control`. Its tests run without cgroups (a fixed `ps` and recorded signals), and `GOOS=darwin go vet ./...` builds them.

(4) **No guard closures in fyrd.** What `cmd/fyrd/wire_guard.go` computed in closures is now a method of a package:
- the roots' memory, the work units and their turns, and the read-back counter are `guard.Parts`, which both mechanisms implement (without the tree there are no parts);
- the rerun wall and the class's raise after a kill are `guard.Admission.RerunWall` and `RerunRaise` (`guard.Jobs` gains `RaisePeak`);
- the job hints (GOMEMLIMIT, the runners' workers, the services' notice) are `cgroup.JobHints`.

The file now holds wiring only.

(5) **What is left.** The engine still calls the host through `SlotDeps`, `UndeclaredDeps` and `RerunDeps` (functions wired from the Mechanism and `Parts`), not through one `guard.Mechanism` value. Changing that touches every engine test that fakes a control and changes no decision, so it is left for when one of those tests changes anyway.

(6) The file-cache finding of V0.guard-mechanism (5) stands: the policy should take a signal that tells reclaim stalls of the agents' file cache from a shortage of anonymous memory. This change keeps the guard's behaviour as it is.

Tests: `internal/guard/sweep_test.go` (the sweep's drops and clocks, the notice once per hold, the gate's hold, the budget's moves); `internal/guard/joblimit_test.go` (the limits from the learned class, moved from cgroup, the 3-run prior, and `JobLimit`'s kept size, valve, prior, undeclared work, raise, own stall and turns); `internal/guard/admission_test.go` (the rerun wall and raise); `internal/cgroup/procmech_test.go` (`ProcMechanism` freezes, thaws and kills undeclared work without cgroups, `ProcStops` resumes a job when its hold ends, a nil `ProcStops` stops nothing, `JobHints`); the process-table tests in `proctree_test.go` and `rerun_test.go`, now on `ProcStops`; every cgroup job-limit test through `guard.JobLimit`; the load simulation `internal/engine/sim_test.go` and `TestSimFixes`, unchanged; the real-cgroup tests in a delegated scope.
