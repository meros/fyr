# V0.change-worktree-cleanup: a worktree Fyr made for a change goes an hour after the change lands or ends, also while its thread is awake

→ decision 0019 (Cleanup), V0.change-worktree, V0.worktree-sweep. *Amends V0.worktree-sweep (1) and (3) for worktrees Fyr made.* (issue #44: "Fyr removes the worktree when its change lands or ends, never while it has uncommitted or unpushed work.")

(1) **The cause.** Under V0.worktree-sweep (1) a worktree on the branch of any change of an awake thread is in use, so a thread that runs many changes keeps every worktree until it settles, and (3) then waits 24 h after HEAD last moved.

(2) **Which.** A worktree whose change carries `workspace.madeByFyr` (V0.change-worktree), when that change has landed, gone live or ended, or is done (V0.98). Its branch no longer makes it in use for that thread. A worktree the agent or the owner made, and one of a change still in flight, stay as V0.worktree-sweep says.

(3) **When.** The sweep removes it when both an hour has passed since the change landed (else since it ended) and an hour has passed since its HEAD last moved. Every other check of V0.worktree-sweep (2) and (3) still holds: nothing uncommitted, HEAD on a remote branch or inside a PR head, not locked, no live process working in it, and no awake thread whose folder is in it or that has another change on its branch. The list shows "Fyr removes it in …" until then, and the line goes to the thread of the change.

(4) A thread that starts in the folder again gets it back, as V0.worktree-sweep (4) says; change_open with the same key makes it again (V0.change-worktree (3)).

*Tests:* `internal/cgroup`: `TestWorktreeSweepRemovesFyrsWorktreeOfAnEndedChange`. `internal/engine`: `TestTempThreadsNameFyrsWorktreesOfEndedChanges`.
