# V0.handover-first-message-starting: a finished handover holds the thread Starting while Fyr sends the new session its first message, so a settle Rule cannot settle it first

→ SPEC sections 3, 5, 6 and 7; the transition invariant `thread-settled-run-started` (V0.thread-invariants).

(1) **The bug.** `handover.done` clears `Handover`, which was the only work a "settle when done" Rule still waited for. The Rule fired in the same commit and settled the thread. The manager then sent the new session its first message (rungs 2 and 3 of a handover, and every cross-host handover), and that message started a run on a settled thread: `thread-settled-run-started`.

(2) **The fix.** `HandoverDoneArgs.FirstMessage` is set when a first message follows. The reducer sets `Starting` (as `thread.send` and `thread.retry` do), which counts as work, so the Rule waits. The run's start clears `Starting`; a failed send clears it too (`send.failed`).

Test: `TestHandoverDoneWithFirstMessageDoesNotSettle` (`internal/core/handover_test.go`).
