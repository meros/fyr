# 0018 Changes are records

Date: 2026-11-15 · Status: accepted (owner: "Polish this plan, build it") · Research: [`research/change-sets.md`](../../research/change-sets.md) · Amends V0.8, V0.12, V0.25, V0.36 · Keeps D5

**Context.** The owner: "git obviously is the most common case, and we should visualize that sota and best. But it's not the only thing we use agents to ask for things. Change-sets handling should have its own set of fyr tools the agent could and should use." Fyr had one hard-coded "PR of the thread" and one hard-coded "Fyr deploy". A server change with `kubectl`, a NixOS rebuild, a migration, a design or a document left no record; a second PR replaced the first; a local commit without a PR had no state; nothing followed a merge to production.

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
| Actions (owner: "Changes should include action. Scripted and llm-driven.") | A change carries buttons. Three ways to run: **fyr** (Fyr's own effects: merge, re-run checks), **script** (an exact argv the agent declared; no shell; bound to a revision when asked, like a saved plan) and **agent** (a prompt for this thread's agent or a new thread). Script for exact, repeatable steps; agent for steps that need judgement. Only the owner presses; the press carries a digest of what the owner saw, so a changed action never runs on an old press. Fyr adds Merge, Re-run failed checks, Fix CI, Address review and Rebase to PRs from what it observed. The same model ("offers") holds actions on the thread and on its live view: one tool `offer_action {on}`, one command `action.run {on, digest}`, one runner. A view page runs only actions declared on the view. |
| Landed but not live | No reminder by default. |
| Settle | `settle_when merged` keeps its meaning; `settle_when {change, phase}` settles when that change reaches the phase. |
| Fyr applies infra changes | Later. The record must prove useful first. |

**Why actions live on the change, not the project.** 0017 dropped project actions because nothing used them. An action on a change is declared by the agent that knows the change, at the moment it is useful, and it goes away with the change. Project-level templates ("deploy this project") can come later as a source of declared actions.

**Consequences.** `Thread.PR` stays as the thread's main PR for the PR tab (V0.43) and the merge gate; it is also a `code` change. Several PRs per thread are polled. The Changes tab lists the change records above the branch diff. Not in the first build: the interdiff from the last revision seen (`git range-diff`), per-file "viewed" marks, inline CI annotations, GitHub Deployments for `live`, the cross-thread Changes screen and Fyr-run applies; research section 4 holds their order.
