# V0.psi-stale-fire: a PSI trigger fire without the stall it stands for is dropped, so a freshly set trigger no longer makes memory short

→ V0.guard-wake (E5, the trigger), V0.psi-trigger-slice, V0.freeze-fast-class-name (a fresh fire is a critical stall). *Amends what a fire means: one the cgroup's stall total does not back is not a stall.*

(1) **Why.** On 20 Nov each of fyrd's 9 starts (deploys) saw its new trigger on the agents' memory.pressure fire 0 to 9 s after it was set, at 48 to 85 % headroom. Each fire made memory short at once, cut the memory budget ("kept across a restart: memory is short") and held the gate. A trigger set by hand on the same slice three times fired at +0.3 s, +2.3 s and +4.2 s with 13, 17 and 0 ms of full stall since it was set; one left open fired at +9 s with 5 ms. A fire stands for 200 ms in 2 s. The kernel starts an unprivileged trigger's window from the cgroup's real-time poll total, which no one updates while no privileged trigger is set, and compares it with the averaging total on the first update: the first window or two count the cgroup's stall since long before the trigger.

(2) **The check.** When fyrd sets the trigger it reads the full `total=` of the same memory.pressure, and at each fire reads it again. A fire with less growth than the trigger's threshold (200 ms) since the trigger was set or since its last fire is dropped, with one line `guard: PSI trigger fired with <n> ms of the agents' stall since it was set or last fired: not a stall, dropped`. A real fire is never dropped: the kernel fires at most once a window and a real fire's window holds at least the threshold, so the growth since the last fire covers it. Without a reading of the total every fire counts, as before.

(3) **Not changed.** The trigger, its threshold, avg10 as the tick's fallback, and what a kept fire does.

Tests: `internal/cgroup/psitrigger_test.go` (TestPSIStaleFireIsDroppedARealOneIsNot).
