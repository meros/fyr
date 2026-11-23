# V0.invariant-running-command-wait: the placement invariant knows the wait for a running command

→ V0.waiting-for-command, V0.thread-invariants.

The cause: V0.waiting-for-command made a thread `waiting` while the command the owner pressed Run on runs, but `thread-attention-placement` did not know that case, so its idle-thread branch wanted done/seen and reported a violation on the first press.

The fix: the check has a case for a running request that wants Waiting in Moving without you.

Tests: `TestPlacementInvariantRunningCommandWait`.
