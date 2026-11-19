# V0.multi-freeze: the guard freezes one more thread for each minute memory stays short, thaws them one at a time, and gives turns to heavy parts only

→ V0.guard-swap (4), V0.164 section 7, decision 0029 S5, decision 0032 slices 1 and 2. *Amends "one at a time on the host" (V0.164 section 7) and the turn order (decision 0032 slice 2).* (owner, 19 Nov: "multi-freeze not done? I need everything we know about this type of thrashing to be fixed".)

(1) **The cause.** On 19 Nov memory was short from 14:21:55 to 14:39 without a break. The guard held one thread frozen at a time: one dev stack for its 10 minutes, then the other. Meanwhile the second dev stack, a dozen vitest runs and two Python jobs ran on. Each freeze was the only one the guard could make, however long memory stayed short.

(2) **More freezes.** When memory is still short AddFreezeAfter (60 s) after the newest freeze, the guard freezes one more: the newest flagged undeclared work that is not frozen yet, else the job that grew most, as before. It never freezes more than MaxFrozen (3) at once, and never the last job that runs. With V0.guard-swap each freeze also moves that work's memory to swap, so each one gives RAM back.

(3) **Thaws one at a time.** A freeze still thaws at once when its work or hold ends, its processes are gone, or after FreezeMax (10 min). When memory has been fine for ThawAfterOK (30 s), only the newest freeze thaws. The next waits for another 30 s of fine memory, so the thawed work does not all page back in together. The log's thaw line adds `still_frozen=`; freeze lines add `frozen_now=`; "memory short" names every frozen thread.

(4) **Turns for heavy parts.** In a rotation only the parts that hold TurnMinRSS (128 MiB) or more get turns. The small ones stay frozen with the rest (all parts take turns when none is that large). On 19 Nov the turns of a frozen dev stack went to a crash handler, `curl`, `tail` and `pg_isready`, 30 s each, among up to 44 parts.

(5) **Not changed.** The 10-minute limit still thaws a freeze while memory is short. It is the time slice of decision 0032 slice 1: the next tick may freeze another thread, and this one again after RefreezeAfter, at most MaxFreezes times an episode.

Tests: `internal/engine/multifreeze_test.go` (TestMultiStillShortAMinuteLaterASecondThreadIsFrozen, TestMultiNoSecondFreezeOnceMemoryIsFine, TestMultiAtMostMaxFrozen, TestMultiThawOneAtATimeNewestFirst, TestMultiTurnsSkipSmallParts, TestMultiSmallPartsOnlyStillTakeTurns); the T and F tests read the freezes as a list.
