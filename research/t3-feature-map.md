# Feature map: T3 Code v0.1.7 → own Claude + GitHub workbench

Source read: `pingdotgg/t3code` tag `v0.1.7` (commit 322f654, 2026-11-03), cloned to
`pingdotgg/t3code` at tag `v0.1.7` (clone it to reproduce; paths below are relative to that checkout).
LOC = non-test TypeScript lines. Four read-only code readers produced the facts; I did not run T3 from source.

## In short

T3 Code is about 1.6 M lines. About 190 k of them exist only for other agents (Codex, Cursor, OpenCode, ...) and other forges (GitLab, Bitbucket, ...).
The four things you rely on (attention, snooze/archive, several hosts, server/client) sit in a small, well-defined core.
That core is perhaps 5–10 % of the code (estimate, not measured).

Tags: **CORE** = build in v1. **LATER** = useful, not v1. **DROP** = exists for other providers/forges, or niche.

---

## 1. Attention model (your top requirement)

### 1.1 Facts the server stores per thread
- Run status: `preparing, queued, starting, running, waiting, completed, interrupted, failed, cancelled, rolled_back`; the thread shell adds `idle` (`packages/contracts/src/orchestrationV2.ts:590, :2047`).
- Pending runtime request `{kind, blocking}`: approval, user_input (AskUserQuestion), auth refresh, secret, link card.
- `lastError`, `lastErrorClass` (`usage_limit` → "Limited"), `usageLimitResetAt`.
- `pendingBackgroundTasks`, `blockedOnThreadWait`, `hasActionableProposedPlan`.
- User lifecycle: `archivedAt`, `settledOverride`, `settledAt`, `unsettledAt`, `snoozedUntil`, `snoozedAt`, `lastSnoozeWakeAt`, `pinnedAt`, `pinOrderKey`, `activeOrderKey`, `autoSettleDisabledAt`, `lastVisitedAt`.
- One "shell" row per thread is derived in `ProjectionStore.ts:1501` (a second SQL path at ~`:6700` duplicates it).

### 1.2 Display status (client)
Priority order (`apps/web/src/components/Sidebar.logic.ts:882`):
approval > input > guest request > waiting (blocked on other thread / awaited background work) > working > failed/limited > ready. The row adds "Woke" and "Done".

- Awaited background work (subagents, watched PRs, commands the agent marks awaited) keeps the thread "not done" (`packages/shared/src/orchestrationV2PendingBackgroundWork.ts:89`). A dev server does not.
- Subagent approvals/questions bubble up to the parent thread (`threadShell.ts:335`).
- **Avoid:** T3 has three separate status pipelines (sidebar, legacy pill, mobile push). Build one status function, shared by server and client.

### 1.3 Unread / seen
- Server stores a watermark `lastVisitedAt`; it only moves forward. The client sends `thread.visit` while the thread is open (throttled to once per 10 s while streaming).
- Unseen completion = latest run completed after `lastVisitedAt`. Failures count as unseen even on never-opened threads.
- **Copy:** "Mark unread" = set `lastVisitedAt = latestRunCompletedAt − 1 ms`. No extra flag needed.
- **Copy:** the first real message sets the watermark to now − 1 ms, so imported history is read.
- Rows dim ("recede") when working, waiting, or ready-and-seen. A collapsed project shows one dot for its most urgent state: attention > failed > unread > working.

### 1.4 Notifications
- Client-only on web/desktop (`ThreadNotificationCoordinator.tsx`, 419 LOC). Diff against the previous shell state; fire "input" when `runId:status` changes, "completion" once per `completedAt`. Never on first sight, for archived threads, or for user-stopped runs.
- Focused window on another thread → toast. Unfocused → OS notification. Sounds deduplicated.
- Mobile push goes server → relay (865 LOC). **For you:** a server-side push (ntfy, or Web Push to a PWA) replaces the relay.

### 1.5 Sidebar ordering
Sections: `pinned | active | working | snoozed | settled` (precedence snoozed > settled > pinned > active).
- Working threads fold into a collapsed "Working" shelf. Active threads sort by **return time** = max(created, unsettled, run requested, run completed, moment it stopped working). This is the "in progress sinks, needs-me rises" behaviour you want.
- **Copy:** a 2 s hold after a run stops (`threadInbox.ts:122`), so a run and its continuation do not flicker. Released at once if the thread needs action.
- **Copy:** pinned order uses fractional index keys (`threadSort.ts:165`).
- Settled list pages from the server, 25 at a time.
- **Avoid:** `Sidebar.tsx` 3.6 k + logic 1.4 k + row 1.8 k LOC, mostly drag-and-drop and animation.

### 1.6 Snooze, settle, archive
- **Snooze** `{snoozedUntil}`. Wake is lazy: "is snoozed" = `now < snoozedUntil`, no server timer. Client sets one timer for the next wake and skips wakes missed during laptop sleep.
  - Ends early on: a pending request, a subagent request, a run failure after `snoozedAt`. A completed run or an automatic wake does **not** end it. A real user message clears it.
  - Refused while a request is pending or a message is queued.
  - Presets: 1 h, 3 h, today 18:00, tomorrow 09:00, Monday 09:00 (calendar-day steps, DST-safe). "Snooze until usage limit resets."
  - Woken threads show "Woke" until visited.
- **Settle** ("done"): manual, or auto-settle sweep every minute (`ThreadSettlementService.ts`, 714 LOC): after N days without activity (default 3), or when its PR merged/closed after your last message. Any new message un-settles.
  - **Copy:** the auto-settle command carries a `snapshotAt`; the server rejects it if the thread changed since. No race with a new message.
  - Settling clears pin/snooze, stops PR watches, closes idle terminals.
- **Archive:** manual only, hides the thread, stops watches, suppresses notifications. No auto-archive.
- 5 s undo for settle, snooze, pin, archive.
- **Avoid:** snooze/settle rules exist twice (server and client) with a comment saying they must agree. Put them in one shared module.

### 1.7 Background wakers
- PR watch: poll every 2 min (cheap fingerprint first). Wakes the thread on failed checks, required checks passing, a foreign comment/review, or a new conflict. Stops on merge/close. Caps comment-only wakes at 10 in a row.
- Scheduled tasks (cron, one-off, webhook) start a thread or message a bound one.
- Usage-limit auto-resume after the reset time.

---

## 2. Server/client and several hosts

| Feature | T3 design | Tag |
|---|---|---|
| One server per host | Node `t3` process owns agents, git, PTYs, files, SQLite. One per state dir (SQLite lock). | CORE |
| Thin clients | Web, Electron, Expo mobile. Electron renderer talks to its own bundled server over WS like any client. | CORE (web/PWA only) |
| Protocol | WebSocket, `effect/rpc`, ~210 RPCs. Core: `dispatchCommand` (~60 commands, idempotent `commandId`) + `subscribeShell` (snapshot, then sequenced thread deltas) + `subscribeThread` (snapshot + sequenced events, resume from cursor). | CORE (the command/snapshot/delta/cursor idea; far fewer RPCs) |
| Reconnect | Jittered backoff ≤ 5 min, probe on foreground, mutations never auto-replayed. | CORE |
| Persistence | SQLite, 99 migrations, event-sourced with outbox (`Orchestrator.ts` 14.3 k, `ProjectionStore.ts` 7.3 k). Events pruned after ~1 day; projections are truth. | CORE: SQLite + projections + event table. DROP: decide/outbox/effect-worker split |
| Multi-host view | No aggregator. Client connects to each environment and merges lists; keys are `(environmentId, threadId)`. | CORE |
| Pairing/auth | One-time code → scoped bearer; WS tickets; per-RPC scopes. Also DPoP, MCP OAuth. | CORE: code → bearer. DROP: DPoP, scopes matrix |
| Reachability | Ordered routes: LAN, Tailscale, SSH forward, T3 Connect (Clerk + Cloudflare relay, 21 k + 8 k LOC). | CORE: Tailscale only. DROP: Connect/relay/Clerk |
| SSH launch | Installs/starts remote `t3`, pairs over SSH, `-L` forward (1.8 k LOC). | LATER |
| Peer links | Agents on host A operate threads on host B via MCP OAuth. | LATER |
| Guest share | Read-only HMAC link to one thread. | DROP |
| Launcher/updates | systemd/launchd launcher with trial updates and SQLite snapshot rollback. | DROP (systemd unit / NixOS module) |
| Mobile app | Expo, 148 k LOC, push, widgets, Live Activities. | DROP (responsive PWA + push) |

Docs worth reading: `docs/internals/overview.md`, `remote.md`, `connection-runtime.md`, `environment-auth.md`, `glossary.md`.

---

## 3. Claude integration

| Feature | T3 design | Tag |
|---|---|---|
| Session runtime | Agent SDK `query()` in the server; SDK spawns `claude` CLI. One query lives across turns via an async-iterable prompt queue. Own `spawnClaudeCodeProcess` (process group, stderr tail, thread env var). | CORE |
| Resume / rollback / fork | Claude session id stored per thread; `resume` + `resumeSessionAt`; `forkSession`. | CORE |
| Interrupt / steer | `query.interrupt()`; a message during a run is pushed into the live query. | CORE |
| Model/effort switch | `setModel` + `applyFlagSettings` live; thinking change needs a restart via resume. | CORE |
| Approvals | `canUseTool`: approval request → UI → `runtime-request.respond` → Deferred resolves. "Accept for session" adds rules. | CORE |
| AskUserQuestion | Same path, kind `user_input`, answers returned in `updatedInput`. | CORE |
| Plan mode | `ExitPlanMode` captured as a plan card and denied; "Implement" starts a new turn. | CORE |
| Permission modes | T3 policy matrix mapped onto SDK modes (Codex concepts leak in). | CORE: use SDK modes directly |
| Compaction | `/compact` as a turn; idle compaction just before prompt cache expiry. | LATER (idle compaction is clever) |
| Usage limits | `rate_limit_event` frames → 5 h / weekly windows; auto-resume after reset. | CORE (small) |
| Cost dashboard | Transcript scanning and pricing (3 k LOC). | LATER |
| Checkpoints | Whole working tree → commit under `refs/t3/...` via temp `GIT_INDEX_FILE`; rollback restores files + `resumeSessionAt`. | CORE (or SDK `enableFileCheckpointing`) |
| Injected MCP server | Per-session HTTP MCP with bearer token: delegate/subagents, thread send/read/wait, schedule, secrets, previews, PR link/watch, worktrees, env links (~60 tools, 12.4 k LOC). | CORE: PR link/watch, thread read/send. LATER: delegation, scheduling, secrets |
| Worktrees | cwd = `thread.worktreePath ?? project.workspaceRoot`; launch strategies local / worktree / existing; setup scripts. | CORE |
| Import `~/.claude` history | Does not exist in T3. | New feature if wanted |
| Multi-provider layer | ~165 k LOC (100 k without generated Codex schemas). The Claude adapter is 11.9 k LOC, mostly translation into the generic model. | DROP. Store SDK messages close to native |

---

## 4. GitHub and git

| Feature | Notes | LOC | Tag |
|---|---|---|---|
| Git engine | status, commit, push, branches, worktrees | 8.7 k | CORE (drop driver registry) |
| Live git status | branch, ahead/behind, dirty | 0.8 k | CORE |
| Commit / push / create PR flow | AI commit and PR text, auto-link created PR | 4.1 k + 4.9 k UI | CORE |
| PR ↔ thread linking | several PRs per thread, auto-link on `gh pr create`, badges, settle on merge | 4 k | CORE |
| PR watch | see 1.7 | 2 k | CORE |
| PR detail UI | summary, timeline, code, checks, review form | 18 k | CORE, trimmed hard |
| Diff viewer | turn / branch / commit diffs, line comments sent to agent | 4.5 k | CORE |
| GitHub client | GraphQL + REST, token → `GH_TOKEN` → `gh` CLI | 14.2 k | CORE (or shell out to `gh`) |
| Stacks | merge/rebase stack | 0.8 k | LATER |
| PR review guides | LLM tour of a PR | 2 k | LATER |
| Forge abstraction | `PullRequestProviderApi`, ~40 methods | 4.6 k | DROP |
| Other forges | GitLab, Bitbucket, Azure, Forgejo, GitCafe | 17.7 k | DROP |
| Cross-environment PR routing | | 3 k | DROP |

---

## 5. Rest of the UI

| Feature | Tag |
|---|---|
| Composer: text, @file mentions, slash commands/skills, model/effort picker | CORE (plain textarea; T3's Tiptap editor is 14 k LOC) |
| Attachments (paste/drag, images) | CORE |
| Per-thread drafts, queued follow-ups | CORE |
| Chat timeline: messages, tool groups, plan cards, markdown | CORE |
| Terminal per thread | CORE (xterm.js; T3 uses Ghostty WASM, 11 k LOC) |
| Command palette (threads, projects) | CORE |
| File tree + code/image preview | CORE |
| Project content search | CORE (needed for @mentions) |
| Settings (minimal), light/dark | CORE |
| Browser preview of dev servers + agent automation | LATER (18 k LOC) |
| HTML "visual replies" | LATER |
| Scheduled tasks | LATER |
| Secrets (`request_secret`, 1Password) | LATER |
| Keybinding editor | LATER (hard-code first) |
| Updates, diagnostics | LATER |
| Devices (simulators), MCP Apps, theme editor/import, Office/PDF preview, resource telemetry, onboarding wizard | DROP |

---

## 6. Competitors

Web research on 2026-11-13, mostly from changelogs, release notes and third-party blogs. (unverified) = not confirmed.

| Tool | Attention | Snooze / archive | Hosts / architecture | GitHub | Licence |
|---|---|---|---|---|---|
| **Enjoy / JOY** (Mo Bitar, enjoy.dev) | One Inbox across projects, "Needs you" first; filters Active / Needs you / Working / Completed. Working while background work (CI watcher, deploy) runs. | Snooze until a time or **until the agent finishes**. **The agent can snooze and wake its own thread.** Queue a thread after another one, at a time, or "when ready"; concurrency cap per project. | Several computers, first-class; window names the current one. Browser client (phone too), end-to-end encrypted per the site. | Light: CI watcher, routine per PR. | Closed, $14.99/month |
| **Conductor** | Backlog / in progress / in review / done; mark unread; PR state on each row. | Archive after merge. No snooze. | Mac only; iOS + QR remote control since Oct 2026. | Strong: Checks tab, "forward failing checks to Claude". | Closed. **Bought by Meta 4 Nov 2026**; its cloud product is announced to shut down on 20 Nov 2026. |
| **Claude Code agent view** | Pinned / Ready for review / Needs input / Working / Completed. A Haiku-class model writes the row summary. `claude agents --json`. | Pin, stop, delete. Idle finished sessions deleted after ~1 h. | One machine; Remote Control / Dispatch from the phone. | PR badge coloured by state; web auto-fix of PRs. | Closed |
| **Codex app** | (unverified) | Archive removes the worktree unless it is pinned. | SSH hosts (alpha); phone pairs per host by QR. | Automations. | Closed |
| **Paradev** (was Superset) | Status from **hooks injected into the agent config**; sounds, dock badge. | Pin, bulk actions. | Remote hosts (beta) via relay. | PR state. | MIT (one source) |
| **Paseo** | (not documented) | (not documented) | **One daemon per machine** (WebSocket + MCP); desktop, web, CLI, mobile clients over Tailscale/SSH/optional E2E relay; Hub for several daemons. Closest to your architecture. | PRs, diffs. | Apache-2.0 or AGPL (sources disagree) |
| **Happy** | Push for approvals and completion. | – | E2E relay you can host, QR keys, several machines per phone. | – | Open |
| **Vibe Kanban** | Kanban; In Review → Done on merge. | Columns | Browser **signs each action, host verifies it**. | Review status. | Open; development stopped |

Also: Nimbalyst (session Kanban + iOS), Omnara (**dropped its wrapper because following Claude Code changes cost too much**), Terragon (shut down Feb 2026), Imbue mngr (agents across local/Docker/Modal/SSH), Cursor (push for finished / needs input / ready for review), Jules (CI Fixer).

Ideas taken into section 7: condition-based snooze and queue (Enjoy), agent can snooze itself (Enjoy), PR state as an attention reason (agent view, Conductor), small-model row summary (agent view), daemon per host (Paseo), signed actions (Vibe Kanban), push only for needs-you (Cursor, Happy), archive cleans the worktree only without unpushed work (Codex). Omnara is the warning: use the SDK's typed messages, never parse terminal output.

---

## 7. Core concepts

Nine concepts. Every feature in sections 1–5 and every gap you named (handover, cross-session, consistent UX) is a combination of them. Four are data, three are mechanics, two are derived views.

### 7.1 Data

**Host.** One daemon on one machine. Stable id, owns sessions, worktrees, processes, its SQLite and its event log. Clients connect to several hosts and merge.

**Thread.** One unit of work as you see it. It is a chain of Claude **sessions**: a handover to a fresh context starts a new session in the same thread, so the thread keeps its place, links and history. Threads link to each other by lineage (`forkedFrom`, `spawnedBy`, `handedOffFrom`).

**Ref.** A typed pointer to something outside the conversation that has its own state: `pr:owner/repo#12`, `issue:…`, `branch:repo@name`, `worktree:host:/path`, `file:repo/path`, `ci:run/123`, `thread:host/id`, `person:email`. Refs are global, so two hosts that mention the same PR agree on it.

**Link.** `thread —role→ ref`, role ∈ `serves` (the goal: this thread exists for PR 12), `owns` (exclusive: the branch, the worktree), `touches` (files changed), `waitsOn` (dependency). Links are written by the agent (MCP tool), detected (git status, `gh pr create`), or set by you.

### 7.2 Mechanics

**Event.** Everything that happens is an append-only typed event with a per-host sequence number: SDK messages, approvals, user actions, git changes, PR and CI changes from watchers, time ticks. All state is a projection of events. Clients get snapshot + events after a cursor. The same log answers "what changed since I last looked".

**Condition.** A predicate over events and time, evaluated by the host: `at(time)`, `threadIdle(t)`, `prChecks(ref, done|failed)`, `reviewArrived(ref)`, `merged(ref)`, `contextAbove(t, 70%)`, `idleFor(t, 3d)`, combined with `any`/`all`. A **Rule** is `when Condition then Action`. One evaluator replaces about six separate T3 subsystems.

**Action.** Every operation is defined once: id, typed params, a precondition over the projected state, the effect (a command that writes events), and optional undo. Every surface is generated from this one registry: command palette, row menu, keybinding, bulk select, MCP tool for agents, CLI, phone. An action can then not behave differently in two places, and an agent can do everything you can do (within a permission scope).

### 7.3 Derived

**Attention.** One pure function, shared by host and client: `attention(threadState, links, refStates, now) → {state, reason, urgency, sortKey, section}`. Every badge, sort, notification, push and filter reads its output. Nothing else computes status.

**Brief.** A structured handover document: `goal, state (host, branch, worktree, refs), decisions, done, open, next, asks (what the reader must do), provenance (thread, event range)`. An agent writes it from the events in a range; you can edit it. It is the only way context moves between places. Renderers decide the shape: a one-line row summary, a "where are we" header, a fresh-session prompt, a child-thread brief, Markdown for a PR comment or an email, JSON to import on another host.

### 7.4 Interface sketch

```ts
type HostId = string; type ThreadId = `${HostId}/${string}`;
type Ref = { kind: "pr"|"issue"|"branch"|"worktree"|"file"|"ci"|"thread"|"person"; key: string };
type Link = { thread: ThreadId; role: "serves"|"owns"|"touches"|"waitsOn"; ref: Ref; source: "agent"|"detected"|"user" };

type Event = { host: HostId; seq: number; at: number; thread?: ThreadId; type: string; data: unknown };

type Condition =
  | { at: number } | { threadIdle: ThreadId } | { prChecks: Ref; is: "done"|"failed" }
  | { reviewArrived: Ref } | { merged: Ref } | { contextAbove: ThreadId; ratio: number }
  | { idleFor: ThreadId; ms: number } | { any: Condition[] } | { all: Condition[] };
type Rule = { id: string; when: Condition; then: ActionCall; once: boolean; owner: ThreadId | "user" };

type Action<P> = {
  id: string; params: Schema<P>; scope: "thread"|"threads"|"host"|"global";
  available(state: State, p: P): true | { reason: string };
  run(p: P): Command[];            // writes events
  undo?(p: P): Command[];
};

type Attention = { state: "needsYou"|"review"|"working"|"waiting"|"snoozed"|"done"|"archived";
  reason: string; urgency: number; sortKey: number };
declare function attention(t: ThreadState, links: Link[], refs: RefStates, now: number): Attention;

type Brief = { goal: string; state: { host: HostId; branch?: string; refs: Ref[] };
  decisions: string[]; done: string[]; open: string[]; next: string; asks: string[];
  provenance: { thread: ThreadId; fromSeq: number; toSeq: number } };
type BriefTarget = "rowSummary"|"welcomeBack"|"freshSession"|"childThread"|"otherHost"|"markdown"|"email";
```

### 7.5 Features as combinations

| Feature | Built from |
|---|---|
| Snooze until a time / until the agent finishes / until CI | Action `snooze(Condition)` → Rule `when C then wake` |
| The agent snoozes or wakes itself | Same Action, exposed as an MCP tool |
| Queue: start after another thread | Thread created paused + Rule `when threadIdle(other) then start` |
| PR watch | Link `serves pr` + Rule `when any(prChecks failed, reviewArrived) then sendMessage` |
| Auto-settle | Rule `when any(merged(pr), idleFor 3d) then settle` |
| Scheduled task | Rule `when at(cron) then startThread` |
| Handover at full context | Rule `when contextAbove(t, 0.7) then handover` → Brief → new session in the same thread |
| Spin off a side task | Brief (`childThread`) + Action `startThread` + lineage |
| Move to another host | Brief (`otherHost`) + Ref worktree/branch pushed + Action on the target host |
| Email a conversation | Brief rendered `email`, sent by an Action (you confirm; secrets redacted) |
| "Where are we" when you come back | Brief over events since `lastVisitedAt`, rendered `welcomeBack` |
| Row summary | Brief rendered `rowSummary` (small model, cached per event range) |
| Group by goal / PR | Group threads by Links with role `serves` |
| Conflict warning | Two threads with `owns`/`touches` Links on the same ref |
| Dependency | Link `waitsOn`; Attention shows "waiting on X" |
| Coordinator view | Attention over all hosts, sorted; bulk = Actions with scope `threads` |
| Coordinator agent | A thread whose MCP tools are the Action registry plus an Attention query |
| Consistent UX | Every surface generated from Actions; every status from Attention |
| Undo | `Action.undo` |

### 7.6 Choices to make before building
- **Rule evaluation on the host or on a coordinator.** Rules that cross hosts (thread on A waits on thread on B) need one of them. Proposal: rules run on the host of their owner thread and subscribe to the other host like a client.
- **Brief quality.** An agent-written Brief can drop a constraint. T3 copies raw messages for that reason. Proposal: a Brief also carries references to the event range, so the next session can read the source.
- **Email and other outbound renderers** publish content. They go through an Action that asks you first.

---

## 8. Proposed v1 cut

1. **Host daemon:** Agent SDK sessions, SQLite event log + projections, git and worktrees, PTY, MCP server that exposes the Action registry.
2. **The nine concepts** as one shared package (types, Attention, Condition evaluator, Action registry).
3. **Actions v1:** send, approve/answer, interrupt, snooze(Condition), wake, settle, archive, pin, link, handover, startThread.
4. **Conditions v1:** `at`, `threadIdle`, `prChecks`, `reviewArrived`, `merged`, `idleFor`, `contextAbove`, `any`/`all`.
5. **Brief v1:** `freshSession`, `rowSummary`, `welcomeBack`, `markdown`. Email and other-host later.
6. **GitHub through `gh`:** PR and CI watchers write Ref state events.
7. **Web/PWA client:** host list, one inbox sorted by Attention, thread view, palette generated from Actions. Tailscale, one-time pairing code, Web Push or ntfy for needs-you.
