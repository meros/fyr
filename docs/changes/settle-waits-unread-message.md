# V0.settle-waits-unread-message: a settle rule waits while a message to the agent is unread

→ SPEC sections 3, 5, 6 and 7; found by `thread-settled-run-started` (V0.thread-invariants).

A message that the process had taken, but that the agent had not read yet (`Thread.Outbox`), is a run about to start. A settle rule whose condition held in that gap (a merged PR, say) saw an idle thread (`WhenIdle`: no run, no `Starting`, no failure), settled it, and then the message started its run on a settled thread: `thread-settled-run-started`.

`WhenIdle` is now false while `Outbox` holds an unread message, so the settle rule fires after the message is read and its run has ended. Test: `TestSettleWaitsForAnUnreadMessage` in `internal/core/settle_test.go`.
