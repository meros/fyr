# V0.gate-order-arrival: the gate releases two threads held at the same instant in the order they were held

→ V0.gate-release-paced, CLAUDE.md (a flaky test is fixed at its root).

(1) **What happened.** After V0.gate-release-paced (#746) `TestGuardWakeTheGateHoldsANewCommandWhileMemoryIsShort` (`internal/engine/heavy_test.go`) failed at "both held threads were released at once" on most runs, alone as well as under load (`go test -count=10 -run …` failed about 9 of 10), and main's `test.yml` was red from that merge on. The log of a failing run shows the guard released the thread held second first.

(2) **The cause.** The gate kept each held thread with only the time it was first held, and `releaseGate` broke a tie in that time by comparing thread ids. Thread ids are random, so two holds that read the same clock went out in random order. The test's clock is fake and does not move between the two holds, so it hit the tie every run and passed only when the ids happened to sort the right way. A real host meets the same tie on a coarse clock or with two holds between clock ticks. The "n ahead" count in the held agent's message also compared only the times, so both tied threads read "0 ahead".

(3) **The rule.** Each hold also takes a sequence number in the order the gate held it (`gateTurn`). The release order is project priority, then the time of the hold, then that number. A released thread held again keeps both, and so its place. The "n ahead" count uses the same order (`gateBefore`), so it now counts project priority too and matches the order the threads are released in.

(4) **Never interrupt the owner.** Nothing visible changes but which held thread goes first in a tie, and the count in the agent's message.

Checked: `go test -count=200 -run TestGuardWakeTheGateHoldsANewCommandWhileMemoryIsShort ./internal/engine` next to six busy loops passes 200 of 200; `go test ./internal/engine ./internal/guard` passes.
