# V0.freeze-cycle-cap: a thread thawed 3 times within an hour is let run, not frozen a fourth time

→ decisions 0029, 0032 and 0036; the load invariant `freeze-cycling` (V0.load-invariants) and V0.multi-freeze, V0.guard-wake.

(1) **What happened.** A job's freeze is once per hold (`FrozeHolds`), but a thread that runs a check again and again (a pre-push command) takes a new hold each time, so each run could be frozen. A PSI trigger that fired for 2 s froze the job; memory was calm 25 s later and the wake rule thawed it 65 s after the freeze. The fourth such thaw of one thread in the hour broke `freeze-cycling`. Undeclared work had its own cap (`MaxFreezes` per episode, 8), which is above the invariant's 3.

(2) **The rule.** The guard keeps each thread's thaws of the last `ThawWindow` (1 h, jobs and undeclared work together; `State.Thaws`). A thread with `MaxThawsPerHour` (3) of them is not picked by `freezeVictim` and `freezeUndeclared` skips it with a log line ("letting it run"), until its oldest thaw leaves the window. The window and the limit are the invariant's own numbers (`InvCycleWindow`, `InvCycles`), so a freeze that would be the fourth thaw is never made.

(3) **What it costs.** A thread let run is not protected by the guard for the rest of the window; oomd's limit and the gate still are. Flapping cost a swap-in on every thaw and still ended in a thaw, so the work is no worse off.

Tests: `internal/guard/tick_test.go` (TestTickDoesNotFreezeAThreadThawedOftenThisHour, TestTickCountsEachThaw).
