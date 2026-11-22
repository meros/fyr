# 0040 Review and land: the diff is where the owner's time goes, so the review is a first-class surface in the thread

Date: 2026-11-22 · Status: **proposed** (owner decides) · Builds on decision 0018 (changes are records), 0019 (worktree per change), 0024 (one card per work), V0.65 (the Changes look), V0.136 (the merge ask held for CI), 5.13 (actions and the press) · Spec: a new section 13.6, V0.review-note, V0.change-accept, V0.merge-queue

## Context

A survey of the leading open-source agent orchestrators (November 2026) found the same conclusion reached independently by almost every serious project: once agents generate work faster than a person can integrate it, **the bottleneck is review and landing, not generation**.

| Project | What it built for that bottleneck |
|---|---|
| T3 Code | per-turn diffs, line comments, review guides, PR stacks, one-button commit→PR |
| Vibe Kanban | inline line comments routed back to the agent; its whole thesis is review latency |
| Gas Town | the Refinery: a Bors-style batch-then-bisect merge queue; agents never push to main |
| multiclaude | a CI-gated merge queue; "Humans approve. Agents propose." |
| container-use | explicit `diff` / `merge` / `apply` / `delete`; work never auto-applies |
| Coder Agents | plan mode confines writes; the control plane holds the credentials |
| Orca, Nimbalyst | line-level review notes and step-through diffs as the core surface |

Fyr is the strongest of these at *which thread needs me* — one inbox, computed attention, one vocabulary — and the thinnest at *what I do when I get there*. Its review surface is a branch diff with per-file Viewed marks (V0.65) and a PR tab. There is no way to leave a note on a line that becomes agent context, no accept step on a worktree's work, and no queue in front of merges. Ten agents finishing at once means ten merge asks and ten serial CI runs.

The survey also found Fyr **ahead** on the axis nobody else has reached: no open-source project has admission control for agent workloads as its purpose (Kueue is generic batch; mngr's limits are experimental and provider-delegated). That is the thing not to trade away here.

## Decision

Three additions, in dependency order, each fitting the existing model rather than adding a view.

| # | What | Model | Why this shape |
|---|---|---|---|
| 1 | **A review note on a diff line** | A typed Input `review.note {change, path, blob, line, side, text}` reduced onto the change; it reaches the agent as a `Next` notice with the exact anchored content, never as a chat message | The anchor is `{path, blob, line}` — the same key discipline as V0.65's Viewed marks, so a note is void when the content moves, exactly as a Viewed mark resets |
| 2 | **An accept step on a change** | The owner's `change.accept` / `change.discard` on a change with a Fyr worktree; accept commits and leaves the worktree, discard ends the change and removes the worktree when nothing is unpushed | The worktree equivalent of container-use's `merge`/`apply`. Fyr already observes the worktree and already has digest-and-press (5.13) for actions |
| 3 | **A merge queue** | A queue in front of the `pr.merge` effect: rebase the stack, test the tip, land; the owner approves a batch, Fyr serializes | Gas Town's Refinery and multiclaude's queue converge on serialization rather than cleverness. Fyr's merge ask already holds for CI on the exact head (V0.136); the queue is that guard applied to a set |

**Explicitly not:** a kanban board. Fyr's inbox is better than every board in the survey. The lesson lives inside the thread, not in a new view. Also not, in this decision: a general code review UI (GitHub's PR tab already reads review threads), and any change to the resource guard.

### 1. A review note on a diff line

- **Where it lives.** On the change, beside its revisions, not in the conversation. It is state, so it survives a restart and a compaction.
- **The Input.** `review.note {change, path, blob, line, side: old|new, text}`. Field rights: owner only. `blob` is the file's git blob id at the time of the note (V0.65 already returns it per file).
- **Void on drift.** A note whose `blob` no longer matches the file is *stale*, shown greyed with "the file changed since this note", and never delivered to the agent as current. Same rule as a Viewed mark resetting.
- **To the agent.** A `Next` notice (decision 0020 (5)), one line per note, coalesced by change: `[Fyr] The owner left 2 review notes on feat/x: internal/core/mergeask.go:42 "this drops the error"; ...`. It never wakes an idle agent; it arrives at the next turn or tool call.
- **The agent's reply.** The agent answers with `change_update` or a revision; resolving a note is the agent's `review.resolve {note}` (agent may write the resolved flag, never the text).
- **Fronts.** Desktop: click a diff line, a note field docks under the hunk. Phone: tap a line, the same card as a Decision (U8). One component for both.

### 2. An accept step on a change

- **Today.** Fyr makes a worktree (`change_open {workspace: true}`), observes it after each run, and removes it only when nothing is uncommitted or unpushed. An agent can leave uncommitted work that Fyr only reports.
- **The commands.** `change.accept {change, message?}` commits the worktree's work on its branch (the message from the owner, else the agent's, else a generated one) and marks the change accepted. `change.discard {change, confirm}` ends the change and removes the worktree, refusing while anything is unpushed unless the owner confirms.
- **Not auto-apply.** Nothing lands without the owner's press. This is container-use's rule and the survey's clearest consensus.
- **What it does not do.** Accept does not merge. Merge stays the `pr.merge` effect with its guard and its CI hold; accept is "this work is worth pushing and asking about", which is the step Fyr is missing between "the agent stopped" and "there is a PR".

### 3. A merge queue

- **The problem.** N green PRs from N threads. Today: N merge asks, N CI runs on N heads, and a conflict discovered at merge N because PR 1 moved the base.
- **The queue.** The owner selects changes (or one thread's stack) and presses "Merge these". Fyr:
  1. orders them by dependency (`after`, V0.64) and then by ask time;
  2. rebases each onto the base after the previous one lands;
  3. runs CI on the resulting tip;
  4. lands in order, stopping at the first failure and reporting which change failed and which were not attempted.
- **One press, one approval.** The owner approves the batch once; each merge's guard is still checked at the press, as for a single merge.
- **Where it runs.** As a Job (8.1) of class `heavy`, so the queue's rebase and test work is admitted like any other heavy work and cannot starve the host — this is the one place the new feature touches the guard, and it touches it as a client, not a change.
- **Not a GitHub merge queue.** If the repo already has one (`merge_queue` in `blockedWhy`), Fyr adds the PR to it rather than duplicating it.

## Consequences

- **Spec.** A new section 13.6 (review and landing) with U26–U29; V0.review-note, V0.change-accept, V0.merge-queue as change files. `change` gains `notes[]` and `acceptedAt`.
- **Model.** Three new Input kinds, one new effect (`git.commit`), one new Job kind. No new entity.
- **Desktop and phone in the same PR** (CLAUDE.md): the note field and the accept press share one component on both fronts.
- **Not built by this decision.** The work is three changes, each spec → red test → code. This record decides *whether* and *in what shape*.
- **What stays as it is.** The inbox, attention, the resource guard, and the at-most-once effect model. The survey's runner-up lessons — credential isolation (Coder, microsandbox) and the process-bound/replay-safe effect taxonomy (Restate, Temporal, DBOS) — are named here and left to their own decisions.

## Open questions for the owner

1. **Scope.** All three, or start with the review note alone? The note is the smallest change with the largest effect on the daily loop; the queue is the largest.
2. **The note's destination.** A `Next` notice (never wakes the agent) or a `Wake` (the agent acts at once)? The survey suggests review latency is the bottleneck, which argues for `Wake` on a note the owner marks blocking.
3. **The queue's failure policy.** Stop at the first failure (safe, slow) or skip the failed change and land the rest (faster, risks a stack that no longer builds)?
