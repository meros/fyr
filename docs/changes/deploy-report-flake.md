# V0.deploy-report-flake: the deploy tests that read the thread's report wait for the report, not for an earlier step of the same goroutine

→ V0.fix-flakes (the same kind of cause), #159 (a local build pauses auto-update), V0.82 (2) (a successful deploy ships).

`TestLocalBuildSmokesAndPauses` failed on an idle home-server with `reports []` and passed alone. The "update:" lines around it in the log were other tests' goroutines logging; they share no state with it (each test has its own `Deployer`, `host` and store).

(1) **The cause: the tests waited for one step and read the result of a later one.** After the probation, `ReportPending`'s goroutine pauses auto-update (for a local build) and calls `OnShipped`, then records the update as live, prunes old builds, and only then tells the thread (`tell` → `Host.Report`). `TestLocalBuildSmokesAndPauses` waited for `PausedBy` and read the reports at once; `TestV082DeployRecordsCommitAndShips` waited for `OnShipped`, slept 20 ms and read them. On a slow disk the prune can outlast either, and the report is not there yet. The product order is right (the thread hears only once the update is recorded); the tests were wrong.

(2) **The fix.** Both tests wait for the report itself with `host.reportsAfter` (up to 5 s, read under the host's lock), as `TestReportPending` already does.

(3) **Numbers.** Not reproduced by repetition on the laptop: 0 of 300 (`-run 'TestLocalBuild|TestV082SuccessShipsOnce|TestReleaseInstall'`, which missed the ship test by name) and 0 of 30 whole-package runs, at nice 19. With the window widened by a temporary 20 ms sleep before `finish` in that goroutine, `TestLocalBuildSmokesAndPauses` failed 20 of 20 with the server's message (`update_test.go:143: reports []`); at 60 ms, the whole package failed 10 of 10 in both tests. After the fix, with the 60 ms sleep still in: 0 of 10 whole-package runs; without it: 0 of 300 targeted (`TestLocalBuild|TestV082|TestReleaseInstall`) and 0 of 30 whole-package runs.
