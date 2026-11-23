# 0040 Review and land: a note on a diff line written in Fyr, and testing what lands together

Date: 2026-11-22, revised 2026-11-23 · Status: **accepted 2026-11-23**: nothing is built now; the note is deferred (owner answered the three questions below) · Builds on decision 0018 (changes are records), 0024 (one card per work), V0.65 (the Changes look), V0.102 (auto-merge per repo), V0.136 (the merge ask held for CI), 5.13 (actions and the press) · Spec: V0.review-note, and a change for question 3 once it is answered

**Revised.** The first version overstated the gap and proposed an accept step that contradicts Fyr's design. What changed:
- Fyr already routes line comments to the agent: the PR tab shows GitHub's unresolved review threads and its Address review action sends them (5.13). The gap is narrower: a note can only be written on GitHub, after a PR exists.
- The accept step (`change.accept` / `change.discard`) is withdrawn. The agent prompt makes the PR and the merge ask the owner's gate ("do not stop at a local commit to ask"); container-use's `merge`/`apply` corresponds to Fyr's merge ask, not to a new step.
- "Ten merge asks and ten serial CI runs" was wrong. CI runs on each push in parallel, and with auto-merge on (V0.102) the asks need no press. The real gap is that PRs which are each green against an older base land without their combination being tested.
- The merge queue was sketched as a heavy Job on the host; the tests run on the CI runner, so Fyr's part is ordering and waiting, not host work. It is now an open question with options.

## Context

The surveyed orchestrators that put many agents in front of one person mostly build their surface around review and landing:

| Project | What it built |
|---|---|
| T3 Code | per-turn diffs, line comments, review guides, PR stacks |
| Vibe Kanban | inline line comments routed back to the agent |
| Gas Town | a Refinery: agents submit merge requests, which it batches, tests at the tip and bisects on failure |
| multiclaude | a CI-gated merge queue; "Humans approve. Agents propose." |
| container-use | explicit `diff` / `merge` / `apply` / `delete`; nothing applies by itself |

Fyr already has most of this, in its own shape:
- **Review.** The Changes tab: the branch diff, "New since you looked", per-file Viewed marks keyed by blob, the range diff after a rebase (V0.65). The PR tab: checks, reviews and unresolved review threads.
- **Routing.** Built-in actions on a change with a PR: Fix CI, Address review, Rebase (5.13).
- **Landing.** The merge ask opens only when CI is green on the head that will be merged (V0.136), and a repo can auto-approve it (V0.102).

Two gaps remain.
1. **A note can only be written on GitHub, after a PR exists.** Not from Fyr's own diff, not before the push, and not comfortably from the phone, which is where Fyr answers most cards.
2. **What lands together is not tested together.** Each PR is green against the base it was pushed on. A text conflict shows as a Rebase action; a semantic conflict (two PRs that pass alone and fail together) shows only as a red main after both merged. With auto-merge on, CI on each head is the only check on what lands.

## Decision

| # | What | Model |
|---|---|---|
| 1 | **A review note on a diff line, written in Fyr** | A typed Input `review.note {change, path, blob, line, side, text}`, owner only, kept on the change. It reaches the agent as a notice with the anchored line; a note whose `blob` no longer matches the file is stale and shown greyed, as a Viewed mark resets (V0.65). The agent resolves it with `review.resolve {note}`. One component on desktop (click a line) and phone (tap a line). |
| 2 | *Withdrawn:* an accept step on a worktree | See *Revised* above. |
| 3 | **Test what lands together** | Open: question 3 below. |

Not part of this decision: a kanban board (the inbox does that job), a general review UI (GitHub's PR page stays the place for formal reviews), and any change to the resource guard.

## Consequences

- **Spec.** V0.review-note as a change file when the work starts; `change` gains `notes[]`.
- **Desktop and phone in the same PR**, one component for the note.
- **The survey's other lessons are left to their own decisions:**
  - *Credentials out of the agent's reach* (Coder Agents, E2B, microsandbox, container-use, mngr). The strongest agreement among the platform and sandbox projects, but not among the direct peers: T3 Code, claude-squad, ccmanager and multiclaude all run agents as the user. Decision 0028 Q3 deferred it, and its threat table names a prompt-injected agent with a shell as not closed.
  - *Retry what is safe to replay* (Restate, Temporal, DBOS). It does not apply as first stated: Fyr's durable effects are merge, re-run failed checks, deploy and the owner's scripts, where at-most-once is the right rule, and messages to the agent are already redelivered after a restart.
- **What the survey found Fyr ahead on.** No surveyed open-source project has admission control for agent workloads as its purpose. Others cap concurrency (Gas Town's `max_polecats`, Coder's limit of five agents without a licence) or pause idle hosts (mngr); none learns a job's cost or gates on pressure.

## Open questions for the owner

1. **Scope.** Build the note now, or wait until question 3 is answered?
2. **Delivery.** Does a note wait for the agent's next turn (`next`), or start one (`wake`)? The first never interrupts; the second shortens the loop when the owner is waiting on the fix.
3. **Testing what lands together**, cheapest first:
   - (a) turn on GitHub's "require branches to be up to date" on the base, so a PR must be green on the current base before it merges; Fyr's merge ask already waits for CI on the head, and Rebase already exists;
   - (b) use GitHub's merge queue where the repo has one, with Fyr adding the PR to it (today Fyr asks the agent to ask the owner);
   - (c) a queue in Fyr: order the asks of a batch by `after`, rebase each on the previous landing, wait for CI on the new head, land, and stop at the first failure;
   - (d) nothing: a red main after merge is caught by the main branch's own test run and fixed forward.

## The owner's answers (2026-11-23)

| Q | Asked | Owner |
|---|---|---|
| 1 | Build the note on a diff line now? | **later**: it stays a proposal; GitHub's review comments and Address review cover it for now |
| 2 | Does a note wait for the agent's next turn or start one? | **wait** (`next`), when it is built |
| 3 | How to test what lands together? | **nothing**: a red main is caught by the main branch's own test run and fixed forward |

So this decision changes no code today. When the note is built, it follows row 1 of the Decision table with `next` delivery.
