# V0.waiting-for-command: a thread whose requested command runs is Waiting, in Moving without you

→ V0.149 (5), V0.ci-wait-moves.

The cause: after the owner pressed Run on a command the agent asked for, the request had a run, so it was no longer pending, and an agent that ended its turn on it sat in Finished though it waits for that command.

The fix: while that command runs and the agent does not, the thread is `waiting` in Moving without you, reason "Waiting for command: <why>". The end of the command ends the wait.

Tests: `TestRunningRequestWaits`.
