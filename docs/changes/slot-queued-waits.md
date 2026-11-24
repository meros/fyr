# V0.slot-queued-waits: a thread queued for a heavy job is Waiting, in Moving without you

→ V0.185, V0.ci-wait-moves.

The cause: a thread whose heavy job (a screenshot run, a build) the host held for lack of room sat in Your move when its PR was ready, because the ready PR's Merge ask was read before the queue. The owner saw "Merge" on a thread whose agent still owed the work it was asked for.

The fix: while the thread has a place in the host's heavy-job queue (`SlotQueued`), it is `waiting` in Moving without you, reason "Queued for a heavy job: waiting for room on the host" (`QueuedText`, worded like the CI wait: state, colon, what it waits for), ahead of a ready PR's Merge ask. Admission ends the wait. A failure, an ask or an unresponsive agent still come first.

Not changed: a running command the owner pressed Run on and an armed watch stay after the ready-PR ask; a watch that waits for the merge itself would otherwise hide the Merge ask.

Tests: `TestSlotQueuedWaitsAheadOfReadyMerge`.
