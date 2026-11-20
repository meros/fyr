# V0.ci-wait-moves: a thread waiting for CI moves without the owner, and every row in Moving says why

→ V0.136, V0.starting-moves, V0.153, V0.195, V0.your-move. 

(1) **The cause.** A thread was in Moving without you only while its agent ran, started, waited for the machine or had a background task. An agent that pushed, asked for the merge and ended its turn left a thread in Finished, unread, though Fyr still waited for CI before it could ask the owner anything.

(2) **The state.** After every cause that needs the owner (asks, a failure, Ready or Stopped, a command to run, a question in the text) and before Finished, a thread waits for CI, state `waiting` in Moving without you, when: a merge ask is held for CI (V0.136), reason "Waiting for CI on #846: 2 of 5 checks done" ("no check has reported yet" when none did); or else a change still going has an open PR whose checks the poller read as pending, reason "Waiting for CI on #846". A PR the poller has not read yet does not count, so a repo with no CI never waits. Green CI moves the thread on (a held ask opens and the thread is the owner's move); red closes the ask, the agent hears it and runs again.

(3) **Why it moves.** The row of a thread in Moving always says why it is there, on both fronts (one `OneRowBody`): the live activity while the agent runs, else the host's reason, else "Working". Before, a row with no activity and no running task said nothing, so a start or a wait for the machine looked the same as a run. The host's reasons read as a status: "Running in background: <command>", "3 background tasks running", "1 subagent running" (was "Background: …", "3 background tasks", "1 agent"). The order of the section does not change.

Tests: `TestCIWaitMoves`, `web/test/tasks.test.ts`.
