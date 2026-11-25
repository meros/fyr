# V0.bg-list-after-ended: a background task list that names a task already seen ended does not count it, so `thread-phantom-tasks` stays quiet

→ SPEC sections 3, 5 and 7; follows V0.bg-count-follows-ended.

The CLI's `background_tasks_changed` can reach Fyr after the `task_notification` or `task_updated` that ended a task it still names. The session took the list as is, so its count (`bg`) stayed at 1 while the engine held 0 running tasks. The idle stop waited for work that was gone, and after 30 seconds `thread-phantom-tasks` fired (`runningTasks=0 agent.bg=1 idle`).

The session now remembers the ids whose end it saw (`bgEnded`) and leaves them out of a later list. Task ids are not reused.

Test: `internal/agent/bgcount_test.go` (TestBackgroundListAfterEndedKeepsCountZero).
