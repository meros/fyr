# V0.settled-self-turn: a turn the CLI starts by itself on a settled thread revives it

→ SPEC sections 3, 5, 6 and 7; the transition invariant `thread-settled-run-started` (V0.thread-invariants).

(1) **The bug.** The owner settled a thread whose CLI still lived on for a background task. When the task ended, the CLI started a turn by itself and the manager observed `run.started`. The reducer set `Running` on the settled thread: `thread-settled-run-started`.

(2) **The fix.** `run.started` clears `Settle.At` (and the "when done" Rule) on a settled thread, as a message does. The run is real work the owner should see; the thread settles again by the usual ways.

Test: `TestSelfStartedTurnRevivesSettledThread` (`internal/core/settled_self_turn_test.go`).
