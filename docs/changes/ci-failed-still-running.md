# V0.ci-failed-still-running: a failed check with checks still running is Waiting for CI

→ V0.ci-wait-moves, V0.101, V0.112.

(1) **The cause.** `Summarize` reads a PR with one red check and others still running as failed. Five minutes after the run ended the thread was Stopped on a failure, in Needs you / Unblock, though a fix job (an external "fix" workflow that pushes to the PR) or a slower check was still moving the head.

(2) **The fix.** `PR.CIRunning` is pending, or failed with `ChecksDone < ChecksTotal`. A change whose PR it holds has a muted waiting need (no failure need), the stop clock does not count it, and the thread is `waiting` in Moving: "Waiting for CI on #846: 4 of 5 checks done, 1 failed". The agent's CI-failed notice is unchanged. When every check is done the failure is the owner's as before.

Tests: `TestCIFailedStillRunning`.
