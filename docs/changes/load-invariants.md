# V0.load-invariants: the load guard checks seven invariants on its tick, and a broken one puts a card with the evidence in Needs you, whose Fix it starts a fix thread in Fyr's own checkout

→ decisions 0029, 0032 and 0036; checks what V0.oomd-guard, V0.guard-wake, V0.guard-tidy, V0.guard-agents-stall, V0.job-limits, V0.admission-memory and V0.psi-trigger-slice promise. A broken invariant becomes a Needs-you card only: fyrd shows the evidence and the owner presses Fix it; fyrd never starts an agent by itself.

(1) **The invariants.** Each has a stable id; the thresholds are constants in `internal/engine/invariants.go`.
- `thaw-into-critical`: the guard thawed work while the agents' memory stall was critical, other than at the 30 min ceiling (V0.guard-tidy) or because its processes were gone.
- `freeze-held-long`: work stayed frozen longer than 30 min (the ceiling should have thawed it).
- `freeze-cycling`: one thread was thawed more than 3 times within 60 min.
- `short-nothing-frozen`: memory short for more than 10 min with flagged work running, and nothing frozen and no command held by the gate.
- `limit-under-prior`: a job's or undeclared work's `memory.high` was written below the prior, the agents' budget / N (the host's `slots`), for a class not yet learned from `slots.LearnRuns` runs (V0.admission-memory). `JobGroups.Limited` hears each `guard: job limit` decision.
- `probe-silent`: the cgroups provider runs but the slowdown has been absent for more than 5 min.
- `psi-trigger-missing`: in the scope layout, an hour after fyrd started, the PSI trigger on the agents' memory stall is still not set.

(2) **A violation** writes one `invariant: <id> <evidence>` line to fyrd's log and counts. It opens a card unless one of that invariant is open, one opened in the last 24 h, or the owner dismissed one in the last 24 h. A condition that lasts (a freeze, a silence, a short episode) is one violation, not one per tick. The records are kept in the host's kv, so a restart does not reopen a card.

(3) **The card** is at the top of Needs you, on the desktop and the phone (one component, `web/src/invariants-ui.tsx`; the phone's buttons are 44 px for touch): the invariant in one sentence, when, the evidence (the signals and the facts), and the guard's log lines of the 10 minutes before, folded. fyrd keeps its last 200 `guard:` lines in memory from its own log, so the card does not read the log file. Two buttons, the owner's host commands `host.invariantFix` and `host.invariantDismiss`:
- **Fix it** starts a thread, as the owner, in the host's project whose folder is a Fyr checkout (it has `cmd/fyrd` and `docs/changes/README.md`; the one named Fyr first). Its first message is the invariant, the evidence, the log lines, decisions 0029, 0032 and 0036 and `docs/changes/` to read, and "reproduce it in a test first, fix it, run scripts/ci-local.sh, open a PR". Without such a project the button is off and the card says to add one.
- **Dismiss** closes the card; that invariant shows no card for a day.

(4) **Settings → Automation** lists the invariants per host with each one's count and last violation (folded; desktop and phone share it).

(5) **Not built: `oomd-kill`** (a thread's cgroup killed by systemd-oomd). oomd kills a whole cgroup with SIGKILL, so the kernel's `memory.events` counts no `oom_kill` (V0.153's evidence); in the scope layout the scope is collected once empty, so any mark oomd leaves on the cgroup is gone before fyrd could read it; and a CLI that ends on SIGKILL may have been killed by Fyr itself. The reliable source is oomd's own journal line ("Killed … due to memory pressure"), which needs a journal reader in fyrd. Left for a later change.

Tests: `internal/engine/invariants_test.go` (TestInvariantThawIntoCritical, TestInvariantThawIntoCriticalNotForGoneOrCeiling, TestInvariantFreezeHeldLong, TestInvariantFreezeCyclingFourThawsInAnHour, TestInvariantProbeSilent, TestInvariantLimitUnderPrior, TestInvariantPSITriggerMissing, TestInvariantShortNothingFrozen, TestInvariantCardOncePerDayAndDismiss, TestInvariantCardsAreKept, TestInvariantFixStartsAThreadWithThePrompt); `web/test/invariants.test.ts`.
