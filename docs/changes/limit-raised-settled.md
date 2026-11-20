# V0.limit-raised-settled: a limit raised on a class learned from three runs is not "under the prior"

→ V0.load-invariants (`limit-under-prior`), V0.admission-memory (4), decision 0029.

(1) **The cause.** The guard wrote a learned limit (`why=learned`, runs ≥ 3) and then raised it after the job hit `memory.high` (`why=raised`). The invariant counted only `why=learned` as settled, so the raise, still far under the prior, was reported as a violation. A raise on a class learned from three runs follows a learned limit; it is not sizing below the prior.

(2) **The fix.** `cgroup.LimitSettled(why, runs)` is true for `learned` and `raised` once the class has `slots.LearnRuns` runs. A `raised` limit on a class with fewer runs, and `unknown`, stay violations when under the prior.

Tests: `internal/cgroup/limit_settled_test.go` (TestLimitSettled).
