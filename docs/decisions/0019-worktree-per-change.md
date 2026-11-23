# 0019 A worktree per change

Date: 2026-11-15 · Status: accepted; built (V0.change-worktree, V0.change-worktree-cleanup, V0.worktree-sweep)  · Issue: #44 · Amends 0015 (Workspace), 0018 · Builds on 0017

**Context.** Threads and their subagents often work in the same folder at once, so one thread's branch switch moves another thread's next edit onto the wrong branch. Some worktrees hold many repos, and one thread may run several changes, in parallel or one after another.

Today:
- Decision 0015 defines a workspace as one path with one branch per thread.
- A folder that holds many repos (the Acme workspace is not itself a git repo) gives Fyr no git facts.
- Fyr makes no worktrees. A hook in the owner's Claude config blocks a branch switch in a busy folder and tells the agent to add a worktree by hand.

**Options.**
1. A worktree per change and repo.
2. Leases on a repo in the shared folder.
3. A full copy of every repo per thread.

The owner chose 1.

**Decisions.**

| Question | Decision |
|---|---|
| Who owns a worktree | A code change. A Change carries a `workspace {repo, path, branch, base, madeByFyr}`. A thread holds any number of changes, so it holds any number of worktrees. |
| Parallel changes | Each code change has its own worktree, so two changes never share a working tree, also when they are in one thread or run in subagents. |
| Serial changes | A change with `after` (a stack) starts its branch from the branch of the change it builds on; without `after`, from the repo's default branch on `origin`. |
| Several repos | A change that spans repos is a `group` of changes, one per repo, each with its own worktree. |
| How the agent gets one | `change_open {kind: code, repo, branch?, after?, workspace: true}` makes the worktree and returns its path. The agent and its subagents work there. |
| What Fyr observes | Git facts are read per change from its workspace, and the observed record is keyed by repo and branch (one record per repo and branch on the host, #35). The thread's own folder is observed as before when it is a repo. |
| The thread's workspace | The list of its changes' worktrees, plus the folder it started in. The header line (#26) shows the active worktrees. |
| Cleanup | Fyr removes a worktree it made when its change lands or ends, and only when it has no uncommitted or unpushed work. Worktrees the agent or the owner made are never removed. |

**Consequences.**
- Solid core models it: the Change gets the `workspace` part and the observer reads per change (#35); the Thread does not get a workspace field (#33).
- Making worktrees, the `change_open` argument, the header line and cleanup are built in milestone Projects and workspaces (#44, #26, #27, #25).
- When this lands, the owner's hook that blocks a branch switch in a busy folder can go.
