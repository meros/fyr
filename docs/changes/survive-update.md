# V0.survive-update: the audit of what an update wipes is written down, and the last items it marks as kept (one-time links, told holds, container notices) outlive a restart; the rerun kills are no longer wiped at start, a launcher restarted during a probation still ends it, and a launcher too old to say it does gets the 65 s fallback back

→ #156, V0.persist-runtime-state, V0.122, V0.budget-restart, V0.120, #148. *Amends V0.persist-runtime-state (3): a launcher from before probation-ok gets the 65 s fallback back, and probation-ok is written for every start of a current build, not only after a switch.*

(1) **The cause.** Every update restarts fyrd. V0.persist-runtime-state kept most of the long-lived state across a restart, and its audit was only in a PR description. This entry is that audit, checked again against main after the file splits and V0.dedup-kv-backoff, with the items it had left open.

(2) **Kept now** (each through `kvstore.Table`, with its expiry):

| Item | Key | Expiry | Why |
|---|---|---|---|
| One-time device links (`auth.Store.once`) | `auth.once`, by the SHA-256 of the token | the link's own end (60 s, or 10 min for Add a device) | a link handed out just before an update failed after it |
| Holds told they are heavier than predicted (`Engine.slotOverTold`) | `engine.slotOverTold` | the hold's end (an entry whose hold is gone is dropped) | the same hold was told again after a restart |
| Container notices (`Services.noticed`) | `service-noticed` | `serviceNoticeEvery` (1 h) | the hourly notice came again at once |

(3) **A bug the audit found.** `Engine.UseSlots`, which fyrd calls after `Load`, made `rerunKills` anew, so the rerun cap of decision 0036 (F) counted from zero after every start although the kills were in the store. It now keeps what `Load` read.

(4) **The deploy report.** V0.persist-runtime-state removed the 65 s fallback and the report waits for the launcher's `probation-ok`. Two cases were left where it never comes, and with `pending_deploy` held no auto-update starts again:
- **A launcher from before `probation-ok`.** The launcher is the installed `fyr`, which an auto-update does not replace, so a host installed before #165 runs fyrd builds under a launcher that never writes the file. Such a launcher also sets no `FYR_LAUNCHER_PID` (#172 came after #165), and neither does running without a launcher. For those fyrd keeps the fallback: the report comes after `OldLauncherProbation` (65 s: the old launcher's 60 s probation and a margin). A launcher that says its pid gets no fallback.
- **A launcher restarted during a switch's probation** starts the new `current` as a plain start and wrote no `probation-ok`. The launcher now writes it for any start of a current build that runs its probation.

(5) **The audit.** Persist: where it is kept. Fine to lose: why.

| Item | Where | Verdict |
|---|---|---|
| repo has CI (`ciRepos`), base rules, default branches, repo deploys, final deploy statuses | internal/github cache.go | persist: `github.*` keys (V0.persist-runtime-state) |
| GitHub budget: remaining, reset, rate-limited until, poll pause (the poller's backoff) | github/budget.go | persist: `github.budget` |
| poller deploy-watch times, failing-check notes | github/github.go | persist: `github.deployAt`, `github.notes` |
| poller `missing` (first "no such PR") | github/github.go | fine: the grace starts again, so a PR is marked missing later, never sooner |
| PR detail cache (30 s), Meter, Budget tokens | github/detail.go, meter.go | fine: 30 s and live counters |
| REST ETags and bodies | github/etag.go | fine: one counted call per path after a restart; keeping them would write up to 1000 bodies on every answer |
| release API hold | deploy/releasehttp.go | persist: `deploy.apiHold` |
| release ETags, tags, watcher errors, the running deploy | deploy | fine: one read again; a running deploy ends with the daemon and `pending_deploy` reports it |
| deploy pause, bad commit, pending report | deploy | persist: kv (before V0.persist-runtime-state) |
| model check time (V0.120, once a day) | provider `TriedAt` | persist: the providers table; test added here |
| quota backoff and data (V0.122) | internal/quota | persist: kv (`TestBackoffSurvivesRestart`) |
| heavy-job budget (#358), slot pool and queue | internal/slots | persist: kv (`TestBudgetKeptAcrossARestart`, `TestV086QueueSurvivesRestart`) |
| `thread_new` / `thread_send` windows | engine/threads_tools.go | persist: `engine.newLimit`, `engine.sendLimit` |
| screenshot window (`shotLimit`, 1 min) | engine | fine: one minute |
| quota windows and announced resets | engine/quotaresets.go | persist: `engine.quotaResets` |
| rerun kills | engine/rerun.go | persist: `engine.rerunKills` (now also after `UseSlots`, (3)) |
| holds told heavier than predicted | engine/slots.go | persist: `engine.slotOverTold` (new) |
| gate held for memory | engine | persist: open in #537 |
| deploy asks seen | engine | persist: open in #539 |
| rerun wait, a job's peak raise | engine, cgroup/jobs.go | persist: open in #542 |
| `last`, `revs`, `quietSent`, `repos` (5 min), `presses`, `rootWhy` (30 s), `slotIdle`, `quietWaits`, the heavy call maps, `frozen`/`undFroze` (thawed at start), the guard clocks and lines, `frozeHolds`, `jobThawed`, `queuedAt`, `titling`, the invariant windows | engine | fine: rebuilt by `Load` or from live processes, a short cache, or a log throttle |
| leftover first-seen and idle-since | cgroup/leftovers.go | persist: `leftover-timers` |
| PR-head fetch times | cgroup/worktrees.go | persist: `worktrees-fetched` |
| stopped pids, service peaks, undeclared episodes, kept leftovers | cgroup | persist: kv (before) |
| container notices | cgroup/services.go | persist: `service-noticed` (new) |
| compose projects of a thread | cgroup/services.go | persist: open in #541 |
| undeclared detector idle level | cgroup/undeclared.go | persist: open in #540 |
| jobs seen / seenAt | cgroup/jobs.go | persist: open in #543 |
| Temps sizes, Docker (10 s), Tree, Sampler, Slowdown, job holds and marks | cgroup | fine: read from the host again |
| native push queue | push/native.go | persist: `push.nativeQueue` |
| delayed done/blocked and quota-reset pushes (`time.AfterFunc`) | front/notify.go | persist: open in #538 |
| one-time device links | auth | persist: `auth.once` (new) |
| devices, modes, nodes, audit | auth | persist: kv (before) |
| wrong-code and ask rate limits, `handed` | auth | fine: a reset rate limit only allows the first tries again, and `handed` is a minute |
| front host cache, settings reads | front | persist: disk cache (V0.54, V0.127) |
| front receipts, link sync, presence, streams, refused-log throttle | front | fine: live connections and log throttles |
| `/loop` threads (`Manager.loops`) | agent/timers.go | fine: an adopted process keeps its timers through the runner's note, and the next `/loop` message marks the thread again before its process starts |
| agent sessions, watchdog and idle timers, approval cache, `ghAuth` (5 min) | agent | fine: sessions are adopted and their timers re-armed |
| launcher probation timer | deploy/launcher.go | fine since (4): a restarted launcher runs a new probation |
| peer invites, peerlink mesh backoff, stream hub, mcp streams, lockwatch, preview Chrome, hostfs scan (5 min) | various | fine: live connections, or read again |

Tests: `internal/auth/auth_test.go` (TestOnceLinkSurvivesRestart), `internal/engine/persist_test.go` (TestSlotOverToldSurvivesRestart, TestRerunKillsSurviveRestart now wires the pool as fyrd does), `internal/cgroup/persist_test.go` (TestServiceNoticeSurvivesRestart), `internal/provider/catalog_test.go` (TestDailyCheckSurvivesRestart), `internal/deploy/launcher_test.go` (TestLauncherRestartedInProbationWritesProbationOK), `internal/deploy/persist_test.go` (TestReportPendingUnderAnOldLauncher; TestReportPendingWaitsForProbationOKAcrossRestart holds for a current launcher).
