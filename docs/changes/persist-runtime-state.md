# V0.persist-runtime-state: the caches, rate-limit waits, runaway limits and queues an update used to wipe outlive a restart of fyrd, each with its own expiry, and the deploy report waits only for the launcher's probation-ok

→ #156, V0.122 (the quota backoff was the first such item), V0.budget-restart, #148. *Amends V0.13: the success report has no 65 s fallback.*

(1) **The cause.** Every update restarts fyrd, and fyrd restarted several times a day. Its long-lived in-memory state began from nothing each time. A repo's CI fact, branch rules and default branch were read from GitHub again. A rate-limit wait GitHub had announced was forgotten, so the new daemon's first polls were refused calls. The runaway limits of `thread_new` and `thread_send` reset. A quota reset that was due during the restart woke nobody. Pushes waiting for a native device that was not listening were lost. The audit of every such item is in the PR (#156); this entry lists what is now kept.

(2) **What is kept, in the store's key-value table, with its expiry.** An entry past its expiry is dropped when it is loaded and when it is next saved.

| Key | Holds | Expiry |
|---|---|---|
| `github.ciRepos` | a repo has CI / has none | has: 7 days; none: `CIRecheck` (10 min) |
| `github.baseRules` | a base branch's rules | `RulesRecheck` (30 min) |
| `github.defaultBranches` | a repo's default branch | 24 h |
| `github.repoDeploys` | a repo uses deployments / does not | uses: 7 days; does not: `RepoDeploysRecheck` (24 h) |
| `github.finalStatus` | a deployment's final status | 7 days, newest 500 |
| `github.budget` | per-resource remaining, reset and rate-limited-until; the poll pause | the reset or the wait, whichever is later |
| `github.deployAt`, `github.notes` | the poller's deploy-watch times; failing-check annotations by PR | 30 min; 24 h |
| `deploy.apiHold` | the release API's "wait until" | the time GitHub named |
| `engine.newLimit`, `engine.sendLimit` | `thread_new` and `thread_send` calls per thread (V0.22) | the window (1 h) |
| `engine.quotaResets` | exhausted windows' reset times and the resets already announced | 24 h past the reset |
| `engine.rerunKills` | rerun kills per class (decision 0036 (F)) | `RerunCapFor` |
| `leftover-timers` | when a leftover was first seen; since when a thread is idle | 24 h |
| `worktrees-fetched` | the last PR-head fetch per repository | 1 h |
| `push.nativeQueue` | pushes waiting for a native device | 12 h, newest 20 per device |

The budget writes a rate-limit wait or a pause at once; a changed remaining count alone at most once a minute, since every gh call changes it.

(3) **The deploy report.** After a switch the new daemon reports success only when the launcher writes `probation-ok` for its build (#148). The 65 s fallback that counted a deploy good for a launcher from before #148 is gone. A daemon that stops before `probation-ok` leaves `pending_deploy` in the store, and the next start reports it.

Tests: `internal/github/persist_test.go`, `internal/deploy/persist_test.go`, `internal/engine/persist_test.go`, `internal/cgroup/persist_test.go`, `internal/push/persist_test.go`.
