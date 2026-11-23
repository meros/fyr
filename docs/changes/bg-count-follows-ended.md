# V0.bg-count-follows-ended: a background task that ends leaves the agent's count at once, so the process stops and `thread-phantom-tasks` stays quiet

→ SPEC sections 3, 5 and 7; follows V0.133 and V0.thread-invariants.

The agent's background count (`session.bg`) was set only from the CLI's `background_tasks_changed`. When a task ended by `task_notification` or `task_updated` and no further `background_tasks_changed` followed, the engine showed 0 running tasks while the count stayed at 1: the idle stop and the refresh waited for work that was gone, and after 30 seconds `thread-phantom-tasks` fired (`runningTasks=0 agent.bg=1 idle`).

The session now keeps the ids behind the count (`bgIDs`). An ended task leaves them, and the idle stop or refresh that waited goes ahead when the last one leaves. A task the list never named changes nothing.

Test: `internal/agent/bgcount_test.go` (TestBackgroundCountFollowsEndedTasks).
