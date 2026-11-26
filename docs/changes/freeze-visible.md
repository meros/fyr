# V0.freeze-visible: work the memory guard froze shows its thread as Waiting with "Frozen by Fyr", not as running

→ SPEC 6.2a (claim 11a). Amends V0.thread-claims, V0.195; the same kind of wait as V0.gate-hold-visible.

The cause: when the memory guard froze a thread's work (`slot.pause` with `paused`), Fyr logged the event and queued a notice for the agent, and nothing else. The thread kept no record of the freeze, so a thread whose run had ended on a background command still read "Waiting · Running in background: …" while every process of that command sat frozen. In one case the row said so for half an hour, until the owner asked. A thaw the agent is not told about (its processes are gone, the episode or hold ended) left no event at all.

The change:

1. **A record.** `Thread.Frozen` (`agent.frozen`, `internal/core/frozen.go`) holds since when, the reason, and which freezes are on: `job` (the slotted job) and `work` (undeclared heavy work). The engine sets it when the guard's pause reaches the thread, with the reason "memory is short on host-a; it resumes when memory recovers", and ends a freeze on every thaw the guard decides (`guard.Thaw`), whether or not the agent is told. A `resumed` or `killed` slot event ends it too.
2. **A claim.** `frozen` sits after `gate` and before `tasks` in the one list of claims, so the row on both fronts, the header chip and the line above the composer read "Frozen by Fyr: memory is short on host-a; it resumes when memory recovers" while it lasts. It is work going on, as the tasks under it were.
3. **It ends** with the last freeze's thaw or kill, when the last running task ends outside a run, when the process is lost, when the host restarts, and on settle. A settled thread takes no freeze.

Not changed: when the guard freezes and thaws, and the notices the agent gets. No new notice is sent.

Found beside it and filed apart (#724): the task in that case read as running for so long also because its agent process was killed between turns and Fyr recorded no process loss, so the task never ended.

Tests: `TestFrozenWorkWaits` (core), `TestFreezeVisibleWhileFrozenAndClearedOnAnyThaw` (engine), `TestClaimOrderIsPinned` and `TestEachClaimAloneAgrees` with the new claim.
