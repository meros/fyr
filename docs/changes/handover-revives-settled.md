# V0.handover-revives-settled: handing over a settled thread revives it, and settling waits for a handover in progress

→ SPEC sections 3, 5, 6 and 7; amends V0.45 and V0.handover-first-message-starting; the invariants `thread-settled-contradiction` and `thread-settled-run-started` (V0.thread-invariants).

(1) **The bug.** `thread.handover` was allowed on a settled thread without clearing `Settle.At` (or an active snooze). When the handover finished on rungs 2 or 3 (`FirstMessage: true`), `handover.done` set `Starting` directly on the still-settled thread (`thread-settled-contradiction` and `thread-settled-run-started`), and the manager then sent the handover's first message to start a run on it. Separately, `thread.settle` and `WhenIdle` (for change-based settle Rules) could settle a thread while `Handover != nil`, hitting the same contradiction when `handover.done` arrived.

(2) **The fix.**
- `thread.handover` clears `Settle.At`, `SettleAsk`, and any snooze (keeping `Failure` until `handover.done`, so a failed handover does not lose the failure).
- `thread.settle` refuses while `Handover != nil` ("Wait for the handover to finish"), matching `thread.archive` and `thread.send`.
- `WhenIdle` requires `Handover == nil`, so a change-based settle Rule waits for a handover in progress to finish.
- `handover.done` uses `markStarting`, which never sets `Starting` on an asleep thread, and the agent manager skips sending the handover first message if the thread is asleep.
- `thread-settled-contradiction` also flags `Handover != nil` on a settled thread, and the randomized state-machine test now exercises `HandoverDoneArgs.FirstMessage`.

Tests: `TestHandoverOnSettledThreadRevives`, `TestSettleWaitsForHandover` (`internal/core/handover_test.go`); `TestThreadInvariantsPureStateViolations`, `TestThreadInvariantsUnderRandomStateMachine` (`internal/core/thread_invariants_test.go`); `TestHandoverSettledThreadAdoptNoViolation` (`internal/engine/handover_test.go`).
