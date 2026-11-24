# V0.gate-hold-visible: a heavy command the gate holds back shows its thread as Waiting, with the reason

→ V0.209, V0.guard-wake, V0.slot-queued-waits.

The cause: while memory is short, the gate holds a heavy command of a thread that already holds an admission (and a learned command). That thread is not in the pool's queue, so no queue row and no `slotQueued` marked it. An agent that ended its turn on the held command sat in Finished with no sign that it waited, or for what.

The fix: the gate records `GateHold` on the thread (`agent.gateHold`) when it holds a command back. The thread is `waiting` in Moving without you, reason "Held: memory is short on host-a; the build command runs when it recovers". The hold ends when memory recovers (the gate's retry), when the gate admits the thread's next heavy command, when the process is lost, and on settle.

Tests: `TestGateHoldWaits` (core). The engine wiring (set at the gate, cleared on recovery or the next admission) has no test of its own yet.
