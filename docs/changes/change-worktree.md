# V0.change-worktree: change_open makes a git worktree for a code change, so two changes never share a working tree

→ decision 0019 (How the agent gets one, Serial changes, Who owns a worktree), V0.changes-code-only, V0.worktree-sweep; issue #44. (owner, 15 Nov: "we need a robust mechanism for threads to use worktrees when other threads or subagents are also working in same folder", and "one thread may run multiple changes in parallel or serial".)

(1) **The call.** `change_open {title, branch, workspace: true, repo?, after?, key?}`. `repo` is the folder of the repo, absolute or relative to the thread's folder, and defaults to the thread's folder; a folder that holds many repos and is not one itself needs it. The key defaults to the branch. Fyr makes the worktree before it records the change and answers with its path: "Fyr made its worktree at … on branch … from origin/main: work there". The agent and its subagents on that change work there.

(2) **Where and from what.** The worktree goes at `<main checkout>/.claude/worktrees/<branch>` (`feat/x` is `feat-x`), also when the call comes from inside another worktree. Fyr adds `/.claude/worktrees/` to the repo's `info/exclude` once, so the main checkout stays clean.
- A new branch starts from the repo's default branch on `origin` after a fetch (`origin/HEAD`, else `origin/main` or `origin/master`; a failed fetch starts from what was fetched last; a repo with no origin starts from `HEAD`). It tracks nothing, so a plain `git push` never goes to the base.
- With `after`, it starts from the branch of the first change it builds on, which must be a change of the thread with a branch the repo has.
- A branch that exists and is not checked out gets a worktree on it as it is.
- A branch already checked out somewhere (the main checkout or another worktree) is not made again: Fyr answers with that path, and that workspace is not Fyr's.

(3) **The record.** The change carries `workspace {repo, path, branch, base, madeByFyr}` and the target `repo:<origin>`. `madeByFyr` is true only for a worktree Fyr made; it stays true when the change follows the record the git observer reads from that worktree. The observer reads the worktree after each run (decision 0019, What Fyr observes). The same key again finds the change and its worktree and makes nothing; when that worktree went (V0.worktree-sweep), Fyr makes it again and the change points at it. A kind that is not code makes no worktree. A bad branch name, a folder that is not in a git repo, a path that exists and is not that worktree, and an unknown `after` are refused with the reason.

(4) **Its project.** A thread that works in a worktree Fyr made is filed in the project of the repo (#25): the worktree is inside the main checkout, so the folder rule of V0.55 matches it.

(5) **The prompt** says to start a code change with `change_open {workspace: true}` and to work in the worktree it returns.

Not yet: removing a worktree Fyr made when its change lands or ends (V0.worktree-sweep removes it once it is kept elsewhere and idle), and the header line (#26).

*Tests:* `internal/agent`: `TestMakeWorktree`. `internal/engine`: `TestChangeOpenMakesAWorktreePerChange`, `TestChangeOpenMakesAGoneWorktreeAgain`. `internal/core`: `TestFyrWorktreeStaysFyrsWhenItFollowsARecord`, `TestMatchFolder`.
