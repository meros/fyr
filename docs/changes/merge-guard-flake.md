# V0.merge-guard-flake: TestMergeOneGuard reads what the agent read, so a merge result that joins a waiting message passes

→ #31, V0.60 (4), V0.95 (1). `TestMergeOneGuard` failed on CI ("timed out waiting for the agent heard the merge") and locally 1 time in 50 under load (`go test -count=50 -run TestMergeOneGuard ./internal/engine` beside six busy loops); alone it passed 30 of 30.

1. **The cause, from the events of a failing run.** The conflict notice ("Merge conflicts on PR #9: conflicting → resolved") went out as a message, and its `user` event was logged with that text. The merge result came while that message still waited for the process; the turn took it in front of the message's words (V0.95 (1)) after the event was written. The agent read both: the fake CLI echoed "[Fyr] The owner ran "Merge" … it succeeded: merged." followed by the conflict notice. The test looked only at `user` events, so it waited for a line that never comes there. Nothing in the code is wrong; no wait was too short.
2. **The fix.** The test also reads the fake CLI's echo (`msg` events), which is the text the agent read.

Tests: `internal/engine/change_tools_test.go` (TestMergeOneGuard), 0 failures in 100 runs under the same load.
