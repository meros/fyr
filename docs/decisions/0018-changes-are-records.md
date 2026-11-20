# 0018 Changes are records

Date: 2026-11-15 · Status: accepted  · Amends V0.8, V0.12, V0.25, V0.36 · Keeps D5

**Context.** Fyr had one hard-coded "PR of the thread" and one hard-coded "Fyr deploy". A server change with `kubectl`, a NixOS rebuild, a migration, a design or a document left no record; a second PR replaced the first; a local commit without a PR had no state; nothing followed a merge to production.

**Decisions.**

| Question | Decision |
|---|---|
| What a change is | A record on its thread: an id, the agent's `key`, a `kind` (code, infra, host, data, doc, design, other), a title, targets, revisions, a **phase**, a free label, conditions, evidence, an optional group and the changes it builds on (`after`). |
| The phases | `working`, `review`, `landed`, `live`, and the ends `abandoned`, `reverted`. A kind's own word ("planned", "patch set 3") goes in `label`. |
| Facts and claims | A phase or condition that Fyr observed (git, GitHub) is never overwritten by an agent report. Reported ones show as "reported". |
| Approval | No agent tool marks a change approved or a review ok. The owner, or an observed GitHub review, does (D5). |
| The agent's tools | Six: `change_open`, `change_revise`, `change_status`, `change_attach`, `change_request`, `change_list`. `pr_link` and `effect_request` stay as short forms of `change_open` and `change_request`, so running agents keep working. |
| Git first | Fyr observes git itself: after each run the thread's branch, commits ahead of the base, unpushed commits and changed files; every PR of the thread (not only one) through the GitHub poller. The tools add intent: title, group, stack, asks. |
| Where it lives | In the thread's record, like the PR before it. A table comes when a change must outlive or move between threads. |
| Attention | A failed condition on a change not ended, or a change that needs the owner (`needs: owner`), blocks the thread with the change's title and reason. An open PR waiting for CI does not. |
| Actions  | A change carries buttons. Three ways to run: **fyr** (Fyr's own effects: merge, re-run checks), **script** (an exact argv the agent declared; no shell; bound to a revision when asked, like a saved plan) and **agent** (a prompt for this thread's agent or a new thread). Script for exact, repeatable steps; agent for steps that need judgement. Only the owner presses; the press carries a digest of what the owner saw, so a changed action never runs on an old press. Fyr adds Merge, Re-run failed checks, Fix CI, Address review and Rebase to PRs from what it observed. The same model ("offers") holds actions on the thread and on its live view: one tool `offer_action {on}`, one command `action.run {on, digest}`, one runner. A view page runs only actions declared on the view. |
| Landed but not live | No reminder by default. |
| Settle | `settle_when merged` keeps its meaning; `settle_when {change, phase}` settles when that change reaches the phase. |
| Fyr applies infra changes | Later. The record must prove useful first. |

**Why actions live on the change, not the project.** 0017 dropped project actions because nothing used them. An action on a change is declared by the agent that knows the change, at the moment it is useful, and it goes away with the change. Project-level templates ("deploy this project") can come later as a source of declared actions.

**Consequences.** `Thread.PR` stays as the thread's main PR for the PR tab (V0.43) and the merge gate; it is also a `code` change. Several PRs per thread are polled. The Changes tab lists the change records above the branch diff. Not in the first build: the interdiff from the last revision seen (`git range-diff`), per-file "viewed" marks, inline CI annotations, GitHub Deployments for `live`, the cross-thread Changes screen and Fyr-run applies; research section 4 holds their order.

## Addendum (#35, 2026-11-15): observed branches are host records

One observed change per repo and branch on the host, shown in every thread in that folder and once on the Changes board; agent-opened changes stay per thread. Live data showed why: branch `v0` of fyr was one change id on two threads, and it stayed `working` with "7 commits not pushed" after it merged and was deleted.

| Question | Decision |
|---|---|
| What an observed change is | A host record (decision 0020, part 1): a Change keyed by repo and branch, stored once in its own table with its own events. Its `workspace {repo, path, branch, base, madeByFyr}` (0019) says where git is read. New work after it lands is a new record (`key~2`). |
| Who shows it | Each thread holds a reference `{id, repo, branch}`. The thread whose run observed it and every thread whose folder is in its checkout get one while it is in flight. The thread DTO lists it after the thread's own changes, with `threads`, the threads that show it; the Changes board shows it once. |
| Notices | A record's move is sent to every thread that shows it as one Input (`change.observed`) through that thread's reduce, so each thread gets its own notice: "Branch v0: merged." (next). |
| An agent change on the same branch | The thread's code change (the agent's, or a PR's) on the record's repo and branch **follows** the record: it keeps its own id, key, intent, PR and actions, and Fyr copies the record's git facts onto it (workspace, pushed, clean, newest revision, and the branch's end unless a PR says how it ended). The thread shows that change instead of the record, so the branch is one row in the thread. The link is made when the record moves, when the agent opens the change and when a PR arrives. This is V0.70's in-thread join one level up: within a thread, an agent change still joins a PR change of the branch (`aliases`); across threads, only the record is shared. |
| What the observer reads | After each run: the thread's folder when it is a checkout (a folder of many repos yields nothing, as before), the workspace of each in-flight change, and for each in-flight record the thread shows, whether its branch ended. |
| When it ends | `landed` when the branch is merged into its base: the head is an ancestor of `origin/<base>`, or a PR of the branch merged (a squash merge leaves the head outside the base). `abandoned` when the branch is gone without a merge. Pushed and clean are dropped at the end. Ended records go after 30 days. |
| Stored threads | On load, a thread's observed git change moves into a host record with the same id and the thread gets a reference. One the agent or a PR added to (a PR, an alias, actions, evidence, a label, a Rule) stays in the thread and follows the record. |
| Actions | Removing an action drops its `action:<id>` condition; its runs stay as history. |

Not in this change: making worktrees and the `change_open` workspace argument (#44); Rules over a host record (a settle on a record's phase).
