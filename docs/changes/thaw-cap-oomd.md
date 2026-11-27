# V0.thaw-cap-oomd: near oomd's limit the thaw cap yields, and the simulation fails a guard that stalls worse than no Fyr

→ V0.freeze-cycle-cap, V0.load-sim, V0.freeze-controller, decision 0036 (B, E2).

(1) **What happened.** On 27 Nov, between 11:16 and 11:46, systemd-oomd killed 10 cgroups under the agents' slice of a 30 GB laptop. Four kills were of one thread that ran a 5–9 GB smoke run. The guard logged "not freezing thread=…: thawed 3 times in the last 1h0m0s, letting it run" for that thread 70 times that day, also while it logged "near oomd's limit: unit_full=78 %". Its three thaws in the hour were one wake and two freezes whose processes had gone while frozen (the first a `yarn install`). V0.freeze-cycle-cap stops a fourth freeze because a flap costs a swap-in, and it said "work let run is still protected by oomd's limit". Near that limit, though, oomd kills the whole thread scope, agent included, within its duration (20 s).

(2) **The rule.** The cap holds as before, except while the agents' unit stall is within `OomdNearMargin` (10 points) of oomd's limit (`Signals.NearOomd`, read with the PSI trigger's freshness like every other freeze decision). Then a thread thawed `MaxThawsPerHour` times may be frozen again, as a job (`freezeVictim`) or as undeclared work (`freezeUndeclared`). The guard logs once a minute: "thread=… may be frozen again though it was thawed 3 times in the last 1h0m0s: memory stalls near oomd's limit, where oomd kills all of it (…)". Every other rule still decides whether the thread is the one to freeze: the most frozen at once, one heavy work always runs, never a service.

(3) **The simulation.** A new scenario, `incident-27nov`, models that hour: a check (undeclared, 3.5 GB), an e2e job (learned 4.9 GB), a make check (learned 2.7 GB), and one thread whose three yarn installs are killed by its agent 150 s after they start, frozen or not (the new `simUnit.Abandon`), followed by two smoke runs of 7.5 GB. The host has 30 GB of RAM, a 12 GB budget, a 10 GB swap cap, the desktop at 9 GB, and agents of 0.6 GB. A new invariant, `worse-than-none`, fails a run whose agents stalled critically more than a tenth and a minute longer with Fyr than with none. It flags `concurrent-8-old` and `concurrent-16-old`, the trees before V0.freeze-controller, which are listed as known.

| scenario | before | now |
|---|---|---|
| `incident-27nov` | 66 min, 2835 s critical stall (no Fyr: 55 min, 2449 s, 3 killed by oomd) | 33 min, 502 s critical stall, none killed |

No other row of the table changed.

(4) **Tried, not taken: count only thaws of work that goes on.** A thaw whose processes were gone, or whose episode ended, is no flap, so it could be left out of the cap. Alone it gave `incident-27nov` 38 min and 950 s; with the rule above it gave the same, because the cap is then seldom reached. The thread's smoke runs are then frozen and thawed more often away from oomd's limit, and in this model each such cycle costs more than it saves (V0.freeze-controller (2) found the same). The cap counts every thaw, as before.

(5) **What the host log shows is next.** Not fixed here, and listed so the next change can start from it:
- *oomd kills frozen work.* At 11:29:12 oomd picked `fyr-agent-<thread>.1.scope` (pgscan 6.5 M) of the thread whose `yarn check` the guard had frozen since 11:10:52. It is the model's `whyFrozenKill`, now seen on a host. oomd's `ManagedOOMPreference=avoid` is no way out: systemd-oomd reads the `user.oomd_avoid` xattr only on cgroups that root owns, and the thread scopes are the owner's (delegated).
- *A full swap lifts the jobs' limits.* At 11:15:31 and 11:20:15 `why=swap-full` set `memory.high=max` on the e2e job and the make check, as V0.job-limits does at 90 % of the swap cap. They grew to the agents' 12 GB within seconds.
- *The one heavy work that runs.* At 11:21 the guard held one thread frozen, the most it may freeze with two threads that have heavy work. The other thread's smoke run was the work that always runs, and the stall went past oomd's limit with it running.

Tests: `internal/guard/tick_test.go` (TestTickFreezesAThreadThawedOftenNearOomd), `internal/engine/sim_test.go` (TestSimLoad: `incident-27nov`, `worse-than-none`).
