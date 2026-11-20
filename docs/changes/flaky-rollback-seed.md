# V0.flaky-rollback-seed: TestLauncherRollsBackToSeed gives its crashing build a probation a busy runner fits in

→ a test fix; amends no behaviour.

`TestLauncherRollsBackToSeed` failed on CI (PR #431): "current still points at b after a crash with no previous".

1. The cause is a wall-time budget. The test took the 500 ms probation of `launcher()`, and the launcher rolls back a promoted build only when it exits inside its probation. A crashing build never becomes ready, so the watch window does not catch it either. On a busy runner, starting the fake binary alone can take longer than 500 ms, so the crash landed after probation, nothing rolled back, and `current` still pointed at `b`.
2. Found: 50 runs alone beside four busy loops all passed on a quiet laptop; with the probation set to 1 ms the test fails every time with the CI message, which shows the mechanism.
3. Fix: the test's probation is 5 s, as `TestLauncherRollsBackCrash` has had since #141 for the same reason. The crash still ends the run at once, so the test takes no longer.
