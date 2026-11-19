# V0.share-shadow: the per-thread memory share is only measured: fyrd no longer writes a thread's memory.high, and the shadow log stays

→ amends decision 0032 slice 3 (V0.198) and V0.job-limits (3).

The owner (19 Nov): "Keep shadow log only." The thread share of decision 0032 slice 3 was off by default, and the per-job limits of V0.job-limits now bound what it was meant to bound: the job that grows, in its own cgroup, sized from its class. A thread-wide `memory.high` beside them was a second limit on the same memory, written through `systemctl set-property` in the scope layout of V0.agents-unit and as a file in the unit layout. The real runs of 18 Nov (decision 0032, `TestRealThreadBudget`) showed the limit enforced but no gain for the neighbouring thread.

(1) **Removed.** No thread's `memory.high` is written, in either layout. The `threadMemoryHighPercent` setting, the slot raise (1.25 × the learned peak while a slot is held), the thread swap valve (`guard: budget limits lifted/back`) and the `guard: budget limit …` lines are gone. `/api/load` no longer has a thread's `memoryHigh` (no page showed it). The swap valve of the job limits stays in `joblimit.go` (`SwapFullShare`, the one constant for "swap is nearly full").

(2) **Kept.** `guard: budget over thread=… mode=shadow working=… limit=…` once a minute per thread whose working memory is over 12 % of the agents' budget, and `guard: budget summary mode=shadow share=… budget=… threads=… over=…` once a minute while someone is over: the tuning data decision 0032 asks for. A thread holding a slot runs admitted work and is not called over, as before. The `slot=` field of the over line and `limited=` of the summary are dropped: in shadow they were always `none` and 0.

(3) **An old config.** A `config.json` that still names `threadMemoryHighPercent` starts fyrd as before, with one log line that it is ignored. The NixOS option stays, hidden and unused, so a host config that sets it still evaluates; a value other than 0 gives a build warning.

Tests: `internal/cgroup/threadbudget_test.go` (TestShareShadowTheRemovedSettingIsIgnoredWithOneLine, TestShareShadowLogsTheShareAndWritesNoMemoryHigh, TestShareShadowSetsNoMemoryHighOnAScope, and the shadow cases kept from V0.198). `TestRealThreadBudget` is removed: it compared runs with and without the enforced share, and its run without the share measured nothing of the shadow log.
