# V0.slot-queued-placement-invariant: the placement invariant accepts a thread that waits in the slot queue

→ SPEC sections 3, 5, 6 and 7; V0.slot-queued-waits, V0.thread-invariants.

The cause: `thread-attention-placement` had no case for `SlotQueued`, so a thread with a heavy job held in the host's queue (rightly `waiting` in Moving without you, V0.slot-queued-waits) fell to the idle default and was reported as "idle thread placed in waiting/working want done/seen". It fired when `run.ended` came while the thread still had its place in the queue.

The fix: the invariant expects `waiting`/Moving without you for `SlotQueued`, in the same order as `ComputeWith`.

Tests: `TestSlotQueuedPlacementIsNotViolation`.
