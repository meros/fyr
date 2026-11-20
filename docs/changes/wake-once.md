# V0.wake-once: the guard's wake rule counts a frozen job's memory once: the thread's memory now comes out of the agents' before its memory at the freeze is added

→ V0.guard-wake (decision 0036 E3). Seen in the live synthetic test on home-laptop, 19 Nov 20:12–20:15: a 10.8 GB hog in a thread was frozen, then `guard: not waking thread=… yet: agents=12.8 GB + its 11.6 GB >= cap=12.8 GB` while the other agents held about 1.2 GB.

1. **The cause.** A frozen job's pages stay charged to the agents' cgroup while they are in RAM. The rule added the job's memory at the freeze to the agents' memory, which already held it, so a job still in RAM could never fit under the cap. It woke in the test only because the freeze's reclaim (V0.guard-swap) had moved its 11 GB to swap; near oomd's limit, where V0.guard-tidy skips that reclaim, it would have waited for the 30 min ceiling.
2. **The fix.** The rule takes the frozen thread's memory now (the same reading the freeze kept, `SlotMem`) out of the agents' memory, then adds its memory at the freeze: `others + its ≥ cap` holds it. The log line names the others' memory (`agents=` is now the agents without the frozen thread). Without a reading of the thread the rule is as before.

Tests: `internal/engine/wake_once_test.go` (TestWakeOnceCountsTheFrozenJobOnce).
