# 0024 Changes v2: one card per piece of work

Date: 2026-11-16 · Status: accepted (owner: "Decision 0024, then slices 1-5") · Mockups: [`design/changes-v2/index.html`](../../design/changes-v2/index.html) · Amends 0018 (+ addendum #35), 0020 (wake rule), V0.59, V0.62, V0.70, V0.84, V0.87, V0.88 · Keeps 0020's foundation and D5

**Context.** The owner, 16 Nov: "Changesets do not work today - that is a fact." Two days of transcripts, the live store and the code show the cause: one piece of work had three identity schemes (the agent's key, a repo-and-branch record, a PR URL), joined by rules that depend on the order things happen. Each fix since V0.70 handled one order. The design below went through four adversarial review rounds (data model, owner UX, agent ergonomics, scope and feasibility, an implementer's read of slices 1–5) before the owner's decisions in section 12.

## In short

Each piece of work gets one card, whatever route it came in by: a pushed branch, a PR, or the agent's own words. Fyr keeps the card's facts current until the work is merged or ended, and then the card leaves the board. Needs you shows only what waits on the owner, and the owner can correct any card in one press.

**Limits.** Fyr still cannot see work on servers, in documents or in designs by itself, so those cards depend on the agent's report. A code change is finished when it is merged: Fyr does not follow Acme work to development or production. In the shared Acme folder, a push with no PR yet can show as "No thread" until a PR or the owner gives it one.

## Evidence (15–16 Nov)

| Symptom | Count | Mechanism today |
|---|---|---|
| Duplicate cards | 12 pairs in 6 threads | A PR from `gh pr create` arrives with no branch (`turn.go:132`); `joinPRWork` skips `code` (`prfold.go:69`) |
| Record loop | 713 records, 714 runs, 714 notices in 30 min | A record with no commits ahead gets no revision, so `endedAt` never matches and every run makes `~N` (`hostchange.go:108,216`); the "merged" notice is `next`, and `next` wakes an idle agent (`feed.go:24`) |
| Wrong PR in the header | 2 threads ("#116 merged", "#102 closed") | `MainChange` = the last PR seen (`observe.go:87`) |
| Stale after merge | ~25 stale items; an owner broadcast to 7 threads | Poller stopped at merge (fixed V0.82); 17 agent corrections refused |
| Stuck "Needs you" | 4 owner complaints | `needs: owner` is a sticky flag only the agent clears |
| Cards that linger | 78 of 102 thread changes landed/live | `landed`/`live` never count as ended (`vocab.go:70`) |
| Upkeep tax | 61 `change_unchanged`, 108 status calls with no phase, ~108 Stop nudges | V0.62 Stop reason on every run |
| Git observer blind | 23 of 52 threads | cwd is the multi-repo Acme folder |
| `change_list` overflow | 3 calls at 83–100k chars | Lists every record, ended ones too |
| Merge dropped 3 commits | 1 | `Merge` passes no SHA to gh (`github.go:112`) |

## Principles

1. **One piece of work, one card.** Find before create.
2. **Fyr owns the facts it can read; the agent owns intent and the facts Fyr cannot read.**
3. **Every card has an exit.** Merged, ended, or "Untouched for 3 days" with one-press Done.
4. **Needs you is computed, and the owner can dismiss every cause.**
5. **The owner can correct what Fyr got wrong, in one press.**
6. **An observation never starts a loop.**

## 1. Identity

### 1.1 Handles

A change holds a set of handles in `change_handles(handle, change_id, released_at)`.

| Handle | Form | Bound while |
|---|---|---|
| PR | `pr:github.com/o/r#123` | always; it resolves to its change also when the change is done |
| Branch | `br:github.com/<head repo>@<branch>` | until the change is merged or abandoned |
| Key | `key:<agent key>` (host-scoped) | until the change is done |

- Repos are normalized (`NormalizeRemote`). A branch handle uses the PR's `headRepository`, or for a push the remote that was pushed to, so fork PRs join. The default branch of that repo is never a handle.
- A push to a squash-merged branch (`v0`) is new work and makes a new card.

### 1.2 One resolver, one attach

`attach(handles, facts, source)` is the only path into the store.

- No bound handle matches: a new change.
- One change matches: handles and facts are added to it.
- An open change and a done change match: never merged. New handles go to the open change; the done change keeps its PR.
- Two open changes match: **merge**. The older id survives; the younger id goes into `change_aliases(old → new)`, and rules, asks, `after` and tool calls resolve through it. Observed facts win over reported ones. The card history says "joined with branch perf/13256-rows 11:12". A thread change and a host record that match: the thread change survives and follows the record, as #35 does today.

A change reference in any tool accepts an id, a key, a PR URL, `owner/repo#N`, `#N` (unique in the thread) or a branch.

After `gh pr create`, Fyr reads the PR's head (`headRepository`, `headRefName`) as a retried effect, so a rate limit delays the join and does not lose it.

### 1.3 What Fyr observes in git

After each run, for the thread's folder and each repo one level below it:

- **Pushes.** `update by push` entries in the reflog of every remote's tracking refs since the run started; fetch entries are skipped. This catches `-u origin HEAD`, `--quiet`, wrapper scripts and `gh pr create`'s own push (tested on git 2.56.0: logged by default in a non-bare clone; shared by all worktrees). A push to an explicit URL that matches no remote leaves no entry.
- **Attribution.** The reflog does not say who pushed. The thread whose run ran `gh pr create` or `pr_link` for the branch owns the card. Before a PR, a push is credited to a thread only when that thread's folder is itself the checkout that has the branch checked out and no other run overlapped the push. Otherwise the card has **No thread**: it shows under its project with "Give to thread…", and a PR or `change_open {branch}` from a thread takes it.
- A PR from thread X on a card owned by another live thread does not move the card. X sees it in `change_list {scope: project}`.
- **No card for a local commit.** Local work shows in the thread's Changes tab diff, as today.
- **Branch-only exits**, on the poll: first `gh pr list --head <branch>` (a PR opened in the GitHub UI joins; a squash-merged PR lands the card). Then, after a fetch of the default branch: `landed` when the head is an ancestor of it; `abandoned` when `git ls-remote` no longer lists a branch that Fyr has seen on the remote.

Fyr reads only refs that moved since the run started. A record is never made when the branch has no commits ahead of its base, or is the default branch.

## 2. Ownership

A change has one owner thread, or none ("No thread").

- The owner is the thread credited in 1.3, or the thread whose `change_open` made the change.
- Handover (0013) moves ownership of open changes to the new thread. The owner can **Move to thread…** and **Give to thread…**. Nothing else moves ownership.
- When a thread is archived, its open asks are void and its open changes get No thread.
- Being in the same folder adds no link. Another thread reads a change through `change_list {scope: project}` and follows it with a `watch` rule.

**The header chip** shows the open change this thread's runs touched last (a handle attached in the run, or the change named in a tool call); when that one is done, the newest open owned change; none when there is no open change. A beam dot shows when another owned change waits on the owner. `MainChange` and `Thread.PR` go; the PR tab shows the PR of the selected change.

## 3. Lifecycle

Phases stay: `working`, `review`, `landed`, `live`, `abandoned`, `reverted`. A change is **done** at:

| Case | Done when |
|---|---|
| Code (Acme and every other repo) | merged into the repo's default branch (`landed`). Stacked PRs: a PR merged into another branch is checked on each poll, and is `landed` when its merge commit is an ancestor of the default branch. |
| Fyr's own repo | `landed`. While a Fyr deploy watch (V0.66) is armed for it, the card stays until `live` or until the watch ends; a failed deploy is a failure (section 5). |
| Not code | `landed` or `live` as reported, or the owner's answer to an `approve` ask |
| Any | `abandoned` (PR closed without merge, agent intent, owner) or `reverted` |

`live` is shown as a fact on a done card when Fyr sees it. It does not keep a card open.

- **Untouched for 3 days:** any open change with no push, PR event, report or owner action for 3 days moves to that group on the board, with one-press **Done**. This covers pushed branches that never get a PR, `change_open {branch}` before a first push, and forgotten documents.
- Reopening a closed PR is seen through Refresh now or `pr_link` (nothing polls an abandoned PR). Its branch handle is attached again through `attach`.

## 4. Facts and who writes them

| Field | Writer |
|---|---|
| PR state, draft, checks, review decision, merge state, auto-merge, head SHA, pushed | observed only |
| `live` for Fyr's repo | observed |
| `live` elsewhere, `abandoned` as intent, `reverted`, label, conditions of other types (apply, health, migration, tests), revisions, evidence | agent (shown "reported") or owner |
| Done, phase override, owner | owner |

- The poller reads a PR until its change is done and its checks are settled. Slice 3 adds `isDraft`, `mergeStateStatus` and `autoMergeRequest` to the fetch (`github.go:43`).
- An agent write to an observed field is not refused. Fyr ignores the value and replies with the observed one: "Fyr reads #13219 from GitHub: merged 10:42."
- The agent's `abandoned` on an open PR is accepted; the card leaves; the reply says "Still open on GitHub. Close it with gh pr close if it is dead."
- An owner override ("marked done by you") blocks later agent writes to that field.
- **Merge binds to a head.** A Merge button and a merge ask carry the head SHA shown when the ask was made. Fyr checks it against the PR's head, then runs `gh pr merge --match-head-commit <sha>`. A moved head returns "The head moved. Look again."

## 5. Needs you

Fyr computes Needs you on the host and sends each change's cause as `need {cause, head}`. The web client only reads it.

A thread is in Needs you when one of its owned, not-done changes has one of these causes:

| Cause | Exact rule | Back after Not now when |
|---|---|---|
| An open ask | an ask from the thread, not answered, declined or void | never; Not now on an ask declines it |
| **Ready**: "Merge" or "Review and merge" | PR open, not draft, checks pass (`none` is not pass), no conflict, `mergeStateStatus` CLEAN or HAS_HOOKS ("Merge") or BLOCKED by a required review only ("Review and merge"), no auto-merge, no run and no task running | the head changes |
| **Stopped on a failure** | a failing fact, and no run in progress 5 min after the last run ended | the head (or, for a non-git change, the newest revision) changes, or a new failure type appears |

- The 5-minute rule is computed from a new `RunEndedAt` and adds `RunEndedAt + 5 min` to `NextCheck`; it needs no new effect.
- A CI failure while the agent works shows as **Agent is fixing CI**, muted, without the beam.
- An ask is void when its change is done, when its thread is archived, or when its precondition is gone.
- Not now is keyed to the head plus the sorted set of failing types (it replaces `fingerprint()` in `park.go`).
- A finished run with an unseen result leaves Needs you and shows as unread in Seen. A run failure stays a cause.
- On the board, a No-thread change with a failing fact is in Needs you, with the same Not now.
- `needs: owner` on the agent tool is removed.

## 6. The owner's corrections

Each card's menu: **Refresh now** (reads GitHub and git now; "checked 3 s ago"), **Done**, **Abandon**, **Move to thread…**, **Give to thread…** (on No thread), **Open PR**. Each is an owner Input with a receipt, on thread changes and on host records (`ReduceRecord` gets an owner path). An override shows on the card.

The board's Needs you has **Merge all green (n)**: one merge per PR (deduplicated), sent one after another, each bound to the head the board showed.

## 7. The agent's tools

Four tools. `change_unchanged` becomes a no-op for one release; `change_status`, `change_revise`, `change_attach` and `pr_link` stay as aliases for one release.

**change_open** `{key?, pr?, branch?, repo?, kind, title, targets?, group?, after?}`
> Give Fyr a card for work it cannot see: a cluster, cloud, DNS or bucket change, a migration or data fix, a NixOS rebuild, another machine, a document, a design. Call it before the first write. Fyr makes cards for your pushes and PRs itself. When infrastructure work is code in a repo, pass branch and repo, so the card is the one the PR joins. The same key, PR or branch returns the same change.

(Until slice 6, `branch` and `repo` are not in the schema, and the sentence about them is left out.)

**change_update** `{change, phase?, label?, conditions?, revision?, evidence?}`
> Tell Fyr what only you know. Send only what changed; send nothing when nothing moved. Fyr reads PR state, checks, reviews, conflicts and the pushed head itself; if you send one of these, Fyr replies with the value it reads. abandoned takes the card off the board.

**change_request** `{change, ask: review|approve|merge|apply|deploy|revert|look, args?, reason}`
> Ask the owner. It returns at once; the answer comes later as a [Fyr] line. merge: Fyr merges the head the owner saw; never run gh pr merge. approve on a document or design finishes it.

**change_list** `{scope?: thread|project, id?, done?}`
> Fyr's facts, one line per change, open changes only by default, at most 4,000 characters. id gives one change of this thread in full.

**System prompt paragraph:**
> Fyr tracks your pushes and PRs itself. Before the first write to something Fyr cannot see (a cluster, cloud resource, database, NixOS host, another machine, a document or a design), open a change with change_open. Report with change_update only when you learn what Fyr cannot read, such as a failed apply, a new version or proof. Ask the owner with change_request. A [Fyr] line that names a next step is work for you.

**Handover and compaction** inject one line per open change (full id, title, phase) into the new context. Compaction uses a `SessionStart` hook with matcher `compact`.

## 8. Notices

`next` no longer wakes an idle thread (`wakes()` reads only `wake`).

| Event | Urgency |
|---|---|
| A failure appears on an owned change: CI failed, conflict, changes requested, a Fyr deploy failed | wake |
| The result of an ask | wake |
| An owner correction, merged, live, done, CI green, approved | next. The text names the next step when there is one: "PR #13245 merged. Done." |
| Fyr made or joined a card | silent |

- Dedupe key: (change, field, head), or (change, field, revision) for a non-git change. It is kept in a bounded `Feed.Said`, which a run does not clear (today's `Feed.Told` is cleared each run).
- **Wake budget:** at most 5 wakes per thread per 10 min from sources other than the owner and the agent (observers and rules). A sent owner message resets it. Further wakes are queued; the time the window frees goes into `NextCheck`, and the clock sends them then.
- A thread that follows another thread's change with `watch` gets what its rule says.

## 9. UI

| Surface | What | Slice |
|---|---|---|
| Inbox row | Line 1: project icon, title, time. Line 2: the activity text, then one change chip and "+N". A Needs you cause adds the beam dot and its button ("Merge", "Answer") | 5 |
| Board | A count line; **Needs you** across threads with Merge all green; one lane per project, folded to one line by default, each row with a phase rail; **Untouched for 3 days**; **Finished since your last visit** (a visit is opening the board; `boardSeenAt` is a host owner preference) with the reason per card | 4 and 5; No thread in 7 |
| Thread Changes tab | Owned changes, open first, each with the rail, the facts and "checked N s ago", asks inline, evidence and the diff folded; then Done here | 4 and 5 |
| Phone | The same order in one column | with each slice |

## 10. Build order

Each slice is one PR that leaves Fyr working and removes a pain on its own. Sizes are estimates.

| # | Spec row | Slice | Removes | Size |
|---|---|---|---|---|
| 1 | V0.95 | Notices: `next` stops waking; the section 8 table; `Feed.Said` dedupe; the wake budget. Records: never made with no commits ahead or on the default branch; the observed head stored on the record. Migration: a load pass deletes `~N` records that are not in flight and that nothing follows; `acme/app#88` by id | the loop | ~200 lines |
| 2 | V0.96 | `change_list` open only (not ended and not landed, or failing), one line each, at most 4,000 chars; handover and compaction lines | overflow | ~120 |
| 3 | V0.97 | Merge bound to the head (`--match-head-commit`, the head fixed at ask time); the Ready rule and the new poller fields; Merge all green | dropped commits | ~180 |
| 4 | V0.98 | `DoneMark{At, By, Reason}` on `core.Change` (both storages share it) and `IsDone()`; `Ended()` keeps its meaning for rules and settle; done leaves the board; Finished since your last visit; Untouched for 3 days; stacked-PR check; Fyr's deploy watch rule. Stopgap for duplicates: a PR from `gh pr create` reads its head on the first poll and joins the open code change on that branch in the same thread. Stopgap header: the newest open change | lingering, 11 of 12 duplicates, wrong header | ~400 |
| 5 | V0.99 | Computed Needs you (section 5) with `RunEndedAt`; Not now by head; owner corrections on both storages; the four-tool surface with aliases and the observed-field reply; remove `needs: owner` and the changes part of the Stop and PostToolUse upkeep (the hooks stay registered for the view and the notices) | stuck Needs you, upkeep tax, refused corrections, broadcasts | ~550 |
| 6 | later | Handles and `attach`; `change_open {branch, repo}`; the PR head read as a retried effect; delete `prfold.go` and `MainChange` | the rest of the duplicates, header for good | ~500 |
| 7 | later | Reflog push observer for repos under the folder; No thread and Give to thread; branch-only exits; replaces branch observation in `hostchange.go` | blind Acme folder | ~400 |

## 11. Migration (data first, then UI)

1. Delete the `~N` loop records and their refs, and `acme/app#88` (slice 1).
2. Mark done every change at or past `landed` (slice 4).
3. Read the head branch of the 48 `pr:` changes; build handles; merge the duplicate pairs (slice 6).

## 12. Owner decisions (16 Nov)

1. Wake for failures and ask results only. 0020's "wake for every notice" is replaced by section 8 and the wake budget.
2. Code changes finish at `landed`, Acme included. No Acme deploy reader. Production is out of v2.
3. "Ready" (green and waiting for the owner's merge or review) is a Needs you cause, with a button bound to the head.
4. Four review rounds, then this record, then slices 1–5 (V0.95–V0.99).

## 13. Not in v2

One host store with owner and follower links (the old slice 8: no evidence row needs it; revisit after slices 1–7 have run for a week), deploy readers for other repos, production, attribution of pushes by `cd`, `git -C` and edit paths, Split, Fyr-run infra applies, health probes for non-git targets, the Bash-pattern infra hint, range-diff, per-file viewed marks.

## Addendum (16 Nov): slice 5's number and its first part

PR #112 (the load indicator) took V0.99. Slice 5 takes the next free SPEC number when it is built (owner, 16 Nov: "Slice 5 moves"). PR #114 built the first part of slice 5's ask rule (section 5): an effect ask is void when its PR is no longer open or its change is abandoned or reverted (`voidAsks` in `internal/core/voidask.go`). Slice 5 adds "its change is done" there once `IsDone()` (V0.98) exists.
