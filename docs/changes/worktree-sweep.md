# V0.worktree-sweep: Fyr removes a worktree nobody works in once its work is kept elsewhere, and makes it again when a thread works there

→ V0.176 (6), (7), (10); decision 0019 (Cleanup). 

That day this repository had 145 worktrees (8.1 GB). The V0.176 check (no uncommitted file, HEAD on a remote branch) passed 59 of them, for three reasons. History had been rewritten, so 81 clean worktrees whose PRs had merged had no commit in common with `origin/main`, and patch ids no longer matched either. Subagent worktrees (`.claude/worktrees/agent-*`) belong to no thread's folder or change branch. And threads asleep for more than 14 days were never looked at. The head of a GitHub PR proves 133 of them kept: 97 HEADs were a merged PR's head and 33 were inside one. Of the other 12, 5 had uncommitted files, 6 were 0 to 2 days old (two of them were the working folder of a live process), and 1 had work nowhere else.

(1) **Which worktrees.** The linked worktrees of every repository Fyr knows: a project's folder and the folder of any thread it looks at (V0.176 (6)). A worktree is in use, and is not listed, when an awake thread works in it: its folder is inside it, or it is on one of that thread's changes' branches. Every other worktree is listed, with the thread when an asleep one works there, and without one otherwise.

(2) **Kept elsewhere.** All of these hold:
- nothing is modified or untracked;
- every ignored file is under a folder a build makes again (`node_modules`, `dist`, `build`, `target`, `bin`, `out`, `result`, `.direnv`, `__pycache__`, `.next`, `.venv`, `coverage`, and the like) or is a `.pyc` file. Any other ignored file keeps the worktree ("kept: ignored files: .env").
- HEAD is on a remote branch, or inside the head of a GitHub pull request. The sweep fetches `refs/pull/*/head` of `origin` into `refs/fyr/pull/*` at most once an hour per repository. GitHub keeps those refs after the branch is deleted and after history is rewritten.

The owner's Remove (V0.176 (6)) uses the same check.

(3) **Removed by itself.** The sweep runs every 10 min. It removes a listed worktree that is kept elsewhere when all of these hold:
- it is not locked (`git worktree lock` is how the owner or an agent keeps one);
- no live process has its working folder inside it (`/proc/*/cwd`, `lsof` on macOS; when neither can be read, nothing is removed);
- the thread that works there has no live CLI;
- its HEAD has not moved (a commit, checkout or reset, read from the worktree's `HEAD` and `logs/HEAD`) for 24 h.

The sweep never writes the index (`--no-optional-locks`), so its own look does not count as work. Before the removal it pins HEAD at `refs/fyr/worktrees/<name>-<commit>` and keeps the branch. The removal is a plain `git worktree remove`, which refuses any change. The list shows when Fyr removes a worktree ("pushed and clean · Fyr removes it in 5 h"), or "in use".

(4) **Made again.** Fyr remembers each worktree it removed (KV `worktrees-removed`, kept 90 days; the pin stays in the repository). When a thread starts in a folder that is missing and was inside one of them, Fyr first runs `git worktree add <path> <branch>`, or `--detach <pin>` when the branch is gone or checked out elsewhere. When that fails, the start fails as before and the error says why.

(5) **The line.** A removal leaves a Fyr line on the thread that worked there ("Fyr removed the worktree … (412 MB): nothing was uncommitted and the branch b is kept and on the remote. Fyr makes it again when this thread works there; to keep a worktree, run git worktree lock."). The agent hears it at its next run, like V0.176 (4). A worktree with no thread gets a line in the log only.

(6) **Settings → Automation** (desktop and phone, one component) titles the group "Worktrees nobody works in" and says when Fyr removes each one.

(7) `worktrees: "list"` in the config keeps V0.176's behaviour on a host: the worktrees are listed, and only the owner's Remove removes one. `leftovers: "off"` still turns everything off.

*Tests:* `internal/cgroup`:
- `TestWorktreeSweepRemovesOnlyIdleCleanKeptWorktreesAndPinsThem`: pushed, PR-head-only and `node_modules` worktrees go; dirty, unpushed, `.env`, locked, in-use and recent ones stay; an awake thread's worktree is not listed; pins and branches stay; one line.
- `TestWorktreeSweepRestoreMakesARemovedWorktreeAgain`: on its branch, and at the pin when the branch is gone.
- `TestWorktreeSweepWithoutAutoOrCwdsRemovesNothing`.
- `TestV176CWorktreesOfSettledThreadsAreListedOnlyAPushedCleanOneCanGo`, which now also lists a worktree no thread works in.

`internal/agent`: `TestWorktreeSweepAStartInARemovedFolderMakesItAgain`. `web/test/leftovers.test.ts`, and the layout test's `V0.176 desktop` and `V0.176 phone`.
