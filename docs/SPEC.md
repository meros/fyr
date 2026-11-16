# Fyr spec

Version: 0.8 (sections 3, 5 and 7 state the model as built, decision 0020; 0.5 becomes the change log. Earlier: 0.5 v0, review rounds 4 and 5, owner decision D5, R19, a consistency pass)
Owner: single user (the **user**)

## 0. Purpose and scope

Fyr (Swedish for lighthouse) runs Claude coding agents on the user's own machines. It shows the user, in one list, which work and which decisions need them now.

**In scope:**
- Claude Code as the agent.
- GitHub as the forge.
- One user.
- One host in v1. Several hosts in a mesh after v1 (0.4).
- A browser client for desktop and phone, installable as a PWA.

**Out of scope:**
- Other forges and several users.
- A hosted relay and a native mobile app.
- An interactive browser for the user. (The agent's headless `preview` module is post-v1; in v1 agents use their own browser MCP servers, 0.4.)
- Device simulators and theme editing.
- Untrusted third-party modules.
- Automatic placement of threads on hosts. The data model allows it later (D2, 10.4).

### 0.1 Requirements

| Id | Requirement |
|---|---|
| R1 | One list of all threads and decisions on all hosts. What needs the user is at the top; work in progress is lower. |
| R2 | For each thread Fyr detects whether the agent works, waits, asks, failed, is blocked, or is done. |
| R3 | Snooze a thread until a time or until an event. |
| R4 | Settle (mark done) and archive a thread. |
| R5 | Several hosts (post-v1, 0.4). |
| R6 | Server/client: agents run on hosts; clients are thin and can disconnect at any time. |
| R7 | Handover: context moves between sessions, threads, hosts and people through one mechanism. |
| R8 | Cross-thread awareness: group by goal, detect conflicts, show what a thread waits for. |
| R9 | Consistency: one definition of each operation and of status, used on every surface. |
| R10 | GitHub PR and CI state are inputs to status and can trigger work. |
| R11 | Nix-native, not Nix-required. |
| R12 | Calm, simple, professional UI with world-class UX. |
| R13 | CPU and RAM are managed so the machine does not thrash; heavy operations wait in queues. |
| R14 | Small core; features are modules behind a fixed set of extension points. |
| R15 | Sensitive operations need human approval, up to biometric verification. |
| R16 | Software-factory workflows can be built as modules without core changes. |
| R17 | Coordinators: agents that oversee and steer other threads within a grant the user signed. |
| R18 | Work hours and silent hours per scope (a repo, a goal, or all), in Europe/Stockholm time. Outside hours, notifications and optionally background runs wait. Status stays truthful. |
| R19 | A host can later serve several users without a data migration. v1 has one user and behaves as if multi-user did not exist (3.12). |

### 0.2 Grounding: how the owner uses T3 Code today

Measured from the owner's T3 Code database, 3–13 Nov 2026 (11 days):

| Fact | Number | Consequence in this spec |
|---|---|---|
| Top-level threads the owner started | about 18 a day; 27 % started on the phone | The phone is a primary client (U8, U13) |
| Threads running at the same time | median 1, p90 4, peak 13 | The working section shows live one-line rows (U16) |
| Distinct PRs | 369, mostly one PR per root thread | The PR state is part of every row (12.4 `github` row badge) |
| Native Claude subagents | about 90 a day, 8 per parent on average | Subagents are part of a run, not threads (3.3) |
| AskUserQuestion requests | 312, median answer 1.0 min, p90 5 min, often 2–4 questions at once | Questions are the main Decision kind, with a keyboard flow (13.2) |
| Tool approvals and plans | 0 (mode `auto` on 89 % of threads) | `auto` is the default mode; sensitive effects are async (D5, 5.10) |
| Settle | 180 manual, 47 automatic; auto-settle on merge turned off by the owner | Merge does not settle by default (7.6); bulk settle (U11) |
| Snooze | presets 1 h, 3 h, today 18:00, tomorrow 06:00, Monday 06:00 | These are the default presets |
| Pin, plan mode, worktree mode, scheduled tasks, peers | 0 or near 0 | Kept cheap or left to modules |
| Thread visits | about 1,900 a day | Visits need dwell; peek does not count (6.4); switching is instant (U14) |
| Agent use of the browser preview | about 1,300 tool calls in 30+ threads | In v1 agents keep their own browser MCP servers; image results show in the run's filmstrip (13.5). A `preview` module follows after v1 (12.4) |
| Background waits and monitors | about 1,750 calls | Awaited background work keeps a thread `waiting` (3.3) |
| Failures | 3.3 % of runs; classes: auth, API, MCP start-up, missing workspace; no usage limits | Explicit failure classes with a retry (3.3, 6.2) |
| Agent backends | one Claude driver with 3 profiles (two accounts, one DeepSeek endpoint) | Agent profiles (12.4 `claude`) |

### 0.3 Owner decisions
- **D1.** A Decision (approval, question, plan, proposal) is a first-class entity. It has its own id, and has a default and a deadline. Every answer is logged with its context and later outcome. Policy is versioned data.
- **D2.** An entity id does not depend on the host. The host where an entity runs is its residence. A move keeps the entity.
- **D3.** Hosts form a full mesh. Each host replicates the summary stream of every peer. There is no leader.
- **D4.** A Coordinator is its own concept, with a fixed identity and its own UI surface.
- **D5.** Async effects. An effect that Fyr can execute itself (merge, deploy, force push, sending a Document, closing an issue) is not done inside the agent's run. The agent requests it, the request becomes a Decision with the exact call, the run continues or ends, and Fyr executes the call after the user approves. The result returns to the thread as context (5.10).

### 0.4 Release scope

| Part | v1 | After v1 |
|---|---|---|
| Hosts | one host; every entity has `residence`, `epoch`, `eseq` and HLC times from day one | mesh (2), peer replication, moves (10.4), cross-host Conditions, cross-host conflicts |
| Precondition for the mesh | – | a written state machine for residence and move, model-checked in TLA+ (or similar) against E1, I1, I2, I3 and I11, before any implementation |
| Coordinators | – | 12.7 |
| Workflows | – | 14 |
| Approvals | `confirm`; async effects (5.10); actions at level `verified` only through the host CLI (5.4) | `verified` with passkeys; `policy.set`; shadow policy and policy proposals |
| Users | one user; every user-related field (`UserId`, `owner`, `visibility`, `assignee`, views keyed by user) exists from day one with the single user's id | several users, sharing, user policy layers, per-user fairness (3.12) |
| Policy | the static policy file, loaded at start as a `policy` Document (5.4) | `policy.set`, shadow evaluation, `decision.outcome` Observers (v1.1) |
| Browser for agents | the agents' own MCP servers; image results in the filmstrip (13.5) | the `preview` module and the `browser` slot class |
| Handover | manual `thread.handover` (10.3) | automatic handover at a context ratio |
| Budgets | – | thread budgets, `budgetAbove`, the budget Signal and end class `budget` |
| Worktrees | observed when the agent creates them | `worktree.create` and `thread.start(worktree: new)` |
| Modes and plans | modes `ask` and `auto` | mode `plan` and ExitPlanMode Decisions |
| Pin | – | `thread.pin`, `thread.unpin` (`pinned` is always 0 in v1) |
| Hours | the `hours` schema and `push: hold` (12.8) | `ruleRuns: hold` |
| Sending Documents | – | the `email` and `deliver` modules, `document.send` |
| Cut-over | `fyr import-t3`: reads T3 Code's database read-only and adopts its open threads by their Claude session id (title, cwd, PR links) | – |
| Everything else in this spec | as written, unless the text says post-v1 | – |

### 0.5 v0: the first viable version

v0 is the smallest Fyr the owner can work in every day. It is binding for milestone `v0 first viable` (`PLAN.md`). Everything not listed here is as 0.4 says, or later.

**This table is the change log of v0.** Each row is a change as the owner asked for it, in the order it was built, and tests cite rows by ID, so no row is removed. A later row amends an earlier one. The numbered sections state the current rules: a row that starts with → names the section that now states it, and where the row and that section differ, the section is the rule.

| ID | v0 requirement |
|---|---|
| V0.1 | One host, one desktop browser client on the same machine. The daemon listens on 127.0.0.1 only (decision 0002). |
| V0.2 | Transport: `POST /api/command` with a client-made `commandId` (exactly once per id, I7); `GET /api/state` snapshot; `GET /api/stream` Server-Sent Events with thread upserts and events; `GET /api/threads/{id}/events`. Requests with a non-loopback `Host` are refused; commands need the header `X-Fyr: 1`. Every thread DTO carries `rev`, which increases on every change the client may see, also across host restarts; the client keeps the copy with the higher `rev`. `thread.markSeen` takes `upTo`, the `activityAt` the client showed, so a result that arrives after it stays `done` (6.4). `GET /api/threads/{id}/events?before=SEQ` pages back. The state carries `host` (the host name), and a thread carries `branch` (the checkout branch, read with git after each run). |
| V0.3 | Threads run the `claude` CLI over the control protocol (16.2): start, resume by session id, send, steer while running (the CLI queues it), interrupt, change model, AskUserQuestion as an `ask` Decision, other permission requests as an `approval` Decision. |
| V0.4 | Attention as 6.1–6.2 with the states asking, failed, blocked, done, working, seen, snoozed, settled, archived. `waiting` is produced for background tasks only (V0.16); the governor's waits come later. `blocked` comes only from the PR (CI failed, conflict, changes requested). |
| V0.5 | → 5.3. The Actions of 5.3 that v0 needs, in one registry: create, send, answer, interrupt, retry, dismiss failure, settle (13.1 `e` semantics), unsettle, snooze, unsnooze, archive, unarchive, mark seen, mark unseen, rename, change model. Each has `available` with a reason. |
| V0.6 | → 3.3. A host restart never shows a wrong state: a run or a question that was live when the host stopped becomes `failed` with class `restart`; Retry resumes the session. |
| V0.7 | An agent process with no run and no open Decision for 10 minutes is stopped; the next message resumes it (8.3, process stop). |
| V0.8 | → 3.13. PR: a URL printed by `gh pr create` links the PR to the thread; open PRs are polled with `gh` every 2 minutes; badge on rows; settle when merged. |
| V0.9 | T3 adoption (decision 0004): adopt open top-level T3 threads, with history from the Claude transcript. |
| V0.10 | Desktop UI: inbox sections (6.3), thread view with the conversation, collapsed tool calls and subagent count, the docked question card (13.2), a composer that accepts pasted images, the keys of 13.1 that apply, and a new-thread dialog with folder, profile and model. |
| V0.11 | → 3.16. Agent profiles: a profile is a `claude`-compatible binary with a name, a colour and a model list. Profiles come from `~/.config/fyr/config.json`, or from T3's provider instances when that file is missing. |
| V0.12 | → 5.10, 5.12. **Fyr tools for agents (minimal).** Each session gets Fyr's MCP server (16.2 MCP binding): HTTP at `/mcp` on the host, a per-session bearer token passed with `--mcp-config`, revoked when the session ends, and the tools allowed with `--allowedTools mcp__fyr__*`. Tools: `pr_link(url)` links a PR to the thread; `effect_request(action, args, reason)` with the one v0 action `pr.merge {pr, method}` opens an `effect` Decision and returns at once (5.10); `settle_when(condition: done \| merged)` arms the settle. After approval Fyr runs `gh pr merge` and sends the result to the thread as a message (`effect.result`), which starts a turn when the thread is idle. Steering has three layers: (1) tool descriptions and a short Fyr block in `--append-system-prompt`; (2) the tool gate (12.3): a PreToolUse hook on `Bash` denies `gh pr merge` with the reason "Use mcp__fyr__effect_request with action pr.merge"; (3) post-v0, the credential broker removes merge rights from the agent's token. |
| V0.13 | → 5.10. **Improve Fyr from Fyr (self-deploy).** A thread may work on Fyr's own repo. The agent requests `effect_request(action: "fyr.deploy", args: {dir})` (or the user runs the Action `host.deploy`); the user approves with one key, as any effect (5.10). A deploy Job then: (1) builds `fyrd` and the web client from `dir` into `~/.local/share/fyr/builds/<id>/`; (2) runs `make test` there and stops on red; (3) snapshots the database (`VACUUM INTO`); (4) drains: waits until no run is live and no background task runs (V0.16), the requesting thread's included; (5) points the `next` link at the build and exits with code 75. A small, rarely changed launcher (`fyr`) runs the daemon from `current`; on code 75 it promotes `next` to `current` (keeping `previous`) and starts it. If the new daemon does not report ready within 20 s or exits within 60 s, the launcher puts `previous` back and starts it, and the thread gets `failed` with class `deploy` and the log tail. The result of a successful deploy reaches the requesting thread as `effect.result` from the new daemon (pending effects are stored and delivered at start). Sessions resume with `--resume`. The switch leaves the host unreachable for a few seconds: clients retry a command that gets 502, 503, 504 or a network error with the same `commandId` (I7 makes this safe) for up to 20 s, showing "Fyr is restarting", before they report a failure. The client compares the `buildId` in the stream `hello` with its own and reloads itself when they differ, so the desktop UI updates without a manual refresh; the phone PWA does the same (decision 0005), so web-only changes need no APK. Schema changes are additive only, so `previous` can still open the database. |
| V0.14 | **Phone over the tailnet (first cut).** `fyrd` still listens on loopback. Tailscale Serve publishes it on the tailnet (`https://<host>.ts.net:7443`, tailnet only). A request that carries `Tailscale-User-Login` is served only when that login is the owner: `ownerLogin` from the config, else the Tailscale user of this machine (tailscaled LocalAPI). A request without the header must have a loopback `Host`. Commands still need `X-Fyr: 1`. At 760 px or less the client shows one column: the inbox, or the open thread with a back button. The PWA manifest, the bottom bar and Web Push follow in M0. |
| V0.15 | **Installable PWA with push.** The client is a PWA: `/manifest.webmanifest` (name Fyr, standalone, theme from the tokens, 192 and 512 px PNG icons plus a maskable one), a service worker at `/sw.js` that caches the app shell (never `/api`), and an install prompt where the browser offers one. Web Push with VAPID: the host makes a key pair once and keeps it in the database; `GET /api/push/key` returns the public key; `POST /api/push/subscribe {subscription, device}` and `POST /api/push/unsubscribe {endpoint}` (both need `X-Fyr`) store and remove subscriptions per user (R19); `POST /api/push/test` sends a test. `POST /api/push/status {endpoint}` returns `{known}`, so a client whose subscription the host deleted (404 or 410) shows the bell as off. The host pushes when a thread enters `asking` or `failed` (13.1 push rule; batching of `done` and presence come in M4), with payload `{title, body, threadId, state}`; the body starts with the inbox meta word ("2 questions · …", "Approval · …", "Fyr action · …", "Failed · …", design/mobile rule 12) and the thread id as the notification tag, so a newer push replaces the older one for the same thread. A subscription that the push service answers with 404 or 410 is deleted. Tapping a notification opens the thread (`/?open=<id>`), focusing an open Fyr window when there is one. A tap opens its thread even when the app reloads for a new build first (the thread is kept in session storage until it is on screen), and the service worker tells every open Fyr window, so the installed app gets it when a browser tab is open too. (Found live: taps opened the wrong thread.) |
| V0.16 | → 3.3. **Background work is visible.** The `claude` CLI reports native subagents and background tasks as `system` messages (`task_started`, `task_progress`, `task_updated`, `task_notification`, `background_tasks_changed`). Each thread keeps `tasks`: id, description, type, status (`running`, `completed`, `failed`, `stopped`), the last activity line, tool uses, tokens, start and update times. A thread with no live turn and at least one running task is `waiting` (6.2, section Working) with the reason "N agents"; it is never `done` while its agents work. When the process exits or the host restarts, running tasks become `stopped`. The timeline records `task` events (started, ended with the summary); progress updates change the thread only. The client shows the tasks on the row (count and activity) and in the thread (a strip with each task's state and activity). |
| V0.17 | **Phone layout invariants, tested.** On a phone every screen fits the visual viewport: the composer, the bottom bar, sheets and the commit button are fully visible and above the bottom safe-area inset; headers stay below the top inset; nothing is hidden under the keyboard. This holds after the app returns from the background, after rotation, and when the keyboard opens and closes. The layout height follows `visualViewport` (resize, scroll), `pageshow`, `visibilitychange` and `orientationchange`, never a height measured once. An automated layout test in headless Chrome checks the invariants for every phone screen at 360 × 640, 390 × 844 and 430 × 932, with emulated safe-area insets, a simulated keyboard (a smaller visual viewport) and a simulated resume (hidden, resized, visible); `make test` runs it. |
| V0.18 | **Attachments: images, files and photos.** The client uploads each file with `POST /api/attachments` (raw body, `X-Fyr`, the name in `X-Fyr-Filename`, the type in `Content-Type`); the host stores it under `<data>/attachments/<id>/<name>`, at most 25 MB a file, and returns `{id, name, size, mediaType}`. `thread.create` and `thread.send` take `attachments: [id…]`. The host sends each image under 5 MB to the agent as an image block, and lists every attachment in the message text with its absolute path ("Attached: <name> (<type>, <size>) at <path>"), so the agent reads PDFs, logs and other files with its own tools. `GET /api/attachments/<id>` serves the file to the owner. The `user` event records the attachments' ids, names, types and sizes, and the client shows them as chips with image thumbnails. On a phone the composer and the new-thread screen offer Camera (`capture="environment"`), Photos and Files; pasting and dropping still work on the desktop. |
| V0.19 | **In-thread previews (planned): flexible, quick and cheap.** One Fyr tool, `show`, takes exactly one of `path` (a local image), `html` (a page or a fragment) or `url` (rendered as a screenshot), plus `title`, `caption`, `size` (`phone` 390 × 844, `desktop` 1280 × 800, or `W×H`) and `full` (whole page). One tool keeps every session's tool list small. It returns a short text (attachment id, file path, pixel size), never the image itself, so a call costs a few tokens; the agent reads the file only when it wants to look. Screenshots use one long-lived headless Chrome on the host, a new tab per shot, so a shot takes well under a second; Chrome stops after 5 idle minutes. An HTML fragment is wrapped in a page with the Fyr theme variables and light and dark modes; identical HTML is stored once. Each call appends a `preview` event `{kind: image \| html, attachmentId, title, caption, width, height}`; the client renders it inline as a card (image: tap for full screen; HTML: an iframe with `sandbox="allow-scripts"`, no same-origin, served from `/api/previews/{id}` with a strict CSP, height clamped to 2,000 px), and keeps the thread's previews in a filmstrip. Markdown images that point at `/api/attachments/{id}` render inline. Limits: 5 MB an image, 512 kB an HTML page, 10 screenshots a minute a thread. A live `browser` the agent drives stays post-v1. |
| V0.20 | **Push for everything that needs the user.** The host pushes when a thread enters `asking`, `failed`, `blocked` or `done`. `asking` and `failed` push at once; `blocked` and `done` push after 20 s and only if the thread is still in that state, so a turn that ends and starts again does not notify. No push goes out for a thread the user is watching: a client that shows a thread while its page is visible sends `POST /api/presence {threadId}` every 20 s (and `{threadId: ""}` when it stops showing it); a thread counts as watched for 45 s after the last beat. The body starts with the meta word: "Done · <summary>", "Blocked · CI failed". |
| V0.21 | **Calm tool output, details on demand.** The conversation shows what the agent did, not how it called tools. (1) A tool whose result has its own card renders only as that card: `show` as the preview card, `effect_request` as the decision and its result, `pr_link` as the PR chip, `AskUserQuestion` as the question card; their calls never show as tool rows. (2) Plumbing is hidden: `ToolSearch` and other tool-loading calls, `TodoWrite`, `settask`-like shell bookkeeping; a hidden call that failed is shown. (3) Consecutive remaining calls fold into one summary line in plain words, counted by kind: "Read 3 files · ran 2 commands · searched the web · edited 1 file", with the total time and an error mark when one failed. (4) One tap or click opens the group into rows (friendly name, the key input in mono, duration, status); a second tap on a row shows the full input and output, with long output cut at 200 lines behind "Show all". (5) MCP names read as words (`mcp__fyr__show` → Show, `mcp__github__…` → GitHub …). (6) A preference "Show tool details" (off by default, remembered per device) opens every group. (7) The same holds for every other machine line: run ends (time, cost), agent start and finish lines, decision answers, effect results and Fyr's own messages to the agent ("[Fyr] Your effect_request …") show as one quiet line at most, or only as a mark on the turn (a check, a cross, a clock), with the exact data one tap away. The end of a run reads as words, never as a bare symbol: "Finished in 14 min" or "Stopped after 2 min" in small muted text (the cost one tap away); without a duration it says "Finished" or "Stopped"; a failure keeps its line with the reason. The default view reads as a conversation: the user's words, the agent's words, cards that need the user, previews. What is shown is human-readable: a row's label is the agent's own description of the call when there is one (the `description` input that Claude gives Bash and Agent calls, such as "Run the Go tests"), else a sentence built from the tool and its key input ("Read app.tsx", "Searched for \"presence\"", "Edited thread.tsx (+12 −3)"); paths are relative to the thread's folder and shortened in the middle; raw commands, ids and JSON appear only in the opened details. The same applies to the live activity line on inbox rows and in the Agents strip, in the present tense ("Running the Go tests", "Reading app.tsx", "Searching the web"); bookkeeping calls do not change it. Nothing is deleted: everything stays in the log and in "Show tool details". Subagent folding (V0.16) and the long-text rules stay. |
| V0.22 | → 5.12. **Threads for agents (cross-session).** Fyr tools let an agent work with the owner's other threads, as T3's `delegate_task` and `t3_thread_*` did (used 123 times in 11 days, 0.2): `threads {query?, state?}` lists threads (id, title, project, state, PR, age; at most 30, newest first); `thread_read {id, last?}` returns the last turns of a thread as plain text (the user's and the agent's words and one line per tool group, at most 8,000 characters); `thread_send {id, text}` sends a message to another thread as a `user` event marked `from: <thread id>`, which starts or steers its run; `thread_new {cwd, text, title?, model?}` starts a thread and records `parent: <thread id>`. A thread may start at most 5 threads an hour and send at most 30 messages an hour (runaway limits, research/coordinators.md). Messages and threads made by an agent show who made them. An agent can never answer another thread's questions or approvals: those stay with the user (12.3). |
| V0.23 | → 3.15. **Projects: an identity with sources.** *Sources, repo sources and folders on several hosts are replaced by V0.55: a project has one folder.* *Matching, auto projects and the seeding at start are replaced by V0.28; the rest holds.* A project has its own stable id, a name, a colour, an icon and optional defaults (profile, model); it does not depend on a host. It points at its work through sources: `folder {host, path}` (a root folder on one host) and `repo {url}` (a normalised git remote such as `github.com/meros/fyr`); later kinds (an issue tracker, another host's folder) add sources, not new project types. A thread belongs to the project whose folder source on this host is the longest root containing the thread's folder; failing that, to the project with a repo source equal to the folder's `origin` remote. When none matches, Fyr creates a project with a folder source at the folder's git root (for a worktree, the main repository's root; outside git, the folder itself) and, when there is an `origin`, a repo source, named after the folder. Projects are seeded from T3's projects (root, title, icon) so workspace roots such as `~/locked/acme` keep all their repos together; a seed never overwrites a project the user edited. At every start the host seeds first and then re-matches every thread that was not filed by hand, so a thread moves to the best folder project; projects that matching made, that the user never edited, and that have no threads left are removed. A git root equal to the home folder or `/` is not a project root (a dotfiles repository in the home folder must not swallow everything): the thread's folder is used instead. The home folder and `/` themselves are never an auto project: a thread started there has no project, and the client shows "No project". A project that is neither auto, edited nor seeded (made by matching before the auto flag existed) counts as auto at the re-match. The colour comes from a palette of ten hues that work in light and dark and never use the beam amber; by default it is picked from the name. The icon is a monogram (default), an emoji, or one of a curated set of line icons (T3's lucide names map onto it). A project does not need git, and it does not need a source at all: a project with no sources is a label for threads about one subject (for example scratch threads about the household), and the user files threads into it with `thread.setProject {projectId}`; a project set by hand is kept, and folder matching never moves the thread again. `project.update {id, name?, color?, icon?, profile?, model?}`, `project.create {name, sources?}`, `project.addSource {id, source}` and `project.removeSource {id, index}` go through the command path with receipts. A folder source must be a full path (`~` is the home folder); on this host the folder must exist, else the command fails with 400 and names the path, so a typo never makes a project that matches nothing. The host fills in its own name when a folder source has none. After a change to a project's sources the host re-matches threads at once. The project screen adds a source from one field (a path starting with `/` or `~` is a folder, anything else a repo URL) and removes one with its × button. Icons may arrive without `value` (the host omits an empty one); the client then draws the monogram. Every thread DTO carries `projectId`; the state lists projects with their identity, sources and thread counts. The client shows the project's icon and colour on rows, in the thread header and in the new-thread picker, can filter the inbox by project, and has a project screen to edit the identity. |
| V0.24 | **Hardening: the client survives the host, deploys never go backwards.** (1) All host JSON passes one decoder (`web/src/decode.ts`) before the store: every field a screen reads gets its empty value, because the host omits empty fields. A Go test writes the host's real `/api/state` for edge cases (a thread with no project, an icon without `value`, a label project) to `web/test/fixtures/host-state.json`; a web test feeds it, and each variant with one optional field removed, through the store and the pure functions the screens use. (2) A render error stays in its part of the screen: the inbox, the open thread, each conversation message and the phone app each have an error boundary that shows "<part> could not be shown", the message, Try again and Reload. (3) A deploy refuses a checkout whose HEAD is an ancestor of the live build's commit ("older than the live build"), before it builds. Only one deploy runs: a newer one cancels an older one that has not linked `next`; past that point nothing cancels it. |
| V0.25 | **Changes: the branch diff, read-only.** The thread's Changes tab (13.5) shows what its branch changed against its base, so the owner can review an agent's work on desktop and phone without a terminal. v0 uses git directly, no checkpoints (3.10). The host runs `git` in the thread's folder, never through a shell, with a 10 s timeout per call. The base is `origin/HEAD`, else the first of `origin/main`, `origin/master`, `main`, `master` that exists; the diff starts at the merge base of the base and `HEAD` and ends at the working tree, so it holds the branch's commits, staged and unstaged edits, and untracked files that git does not ignore (shown as added). Renames are detected. Without a base the diff starts at `HEAD` (uncommitted work only); in a repository without commits, at the empty tree. `GET /api/threads/{id}/changes` returns `{git: true, root, branch, base, mergeBase, commits, files: [{path, oldPath?, status: added\|modified\|deleted\|renamed, additions, deletions, binary}], truncated}`, files sorted by path, at most 1,000 (then `truncated: true`); paths are relative to the repository root. A folder that is not a git checkout (or is missing) returns `{git: false}`; an unknown thread 404. `?path=<p>` returns `{path, patch, binary, truncated}`: the unified diff of that one file (a renamed file from its old path), cut at 400 KB at a line end with `truncated: true`; a path without changes returns an empty patch. The path is taken literally (no pathspec magic), and an untracked file is read only when git lists it as untracked, so the call never reads outside the repository. The client loads the list when the tab opens and on Refresh (no live updates). It shows one summary line ("4 files · +120 −18 · against origin/main · 3 commits"), then one row per file: a status mark (A, M, D, R), the path shortened in the middle so the file name stays visible, and `+N −M` (or "binary"). A click or tap on a row opens that file's diff below it and loads it once; another closes it. The diff shows hunk headers, old and new line numbers, added lines on a `calm` wash and removed lines on a `signal` wash, in light and dark; long lines scroll sideways inside the diff and never widen the page. A binary file says "Binary file, not shown"; a cut patch says so at its end. Empty states: "No changes against <base>." and "This thread's folder is not a git checkout." The phone shows the same tab at `/?open=<id>&tab=changes`, with 44 px rows. |
| V0.26 | **Calm, steady screens.** (1) The Working section keeps a stable order: rows sort by when the current run started (for a `waiting` thread, the earliest start of its running agents), newest first, then by id; activity and updates never move a row. (2) An opened tool group shows at most the last 3 calls and a "+N more tools" row above them; a click or Enter on that row shows the rest. A call that failed is never hidden behind it. The same holds on the phone and with "Show tool details" on. (3) The desktop thread header has a ⋯ menu with "Move to project…", which opens the project picker (search by name, keyboard: arrows, Enter, Esc) and files the thread with `thread.setProject {projectId}`, as the phone does. |
| V0.27 | **Plain decision cards.** A card for an effect says in words what happens and why, and keeps the technical form behind "Details". The header names the effect ("Update Fyr", "Merge #15"), the heading is one sentence ("Update Fyr to a new version", "Merge pull request #15: <title>"), the agent's reason follows as text, then one or two sentences on what Fyr does (for a deploy: it builds and tests, waits until no thread works, restarts with a few seconds offline, and goes back by itself on failure). The buttons are verbs ("Update Fyr", "Merge", "Not now"). "Details" (closed by default) holds who asked, that the agent does not wait, the action name and its arguments. An unknown effect gets its action name as words and Approve / Not now. The same wording is used on desktop, phone and in the inbox row. |
| V0.28 | → 3.15. **Workspaces and refs; no auto projects (decision 0008, slice 1).** *Workspaces, refs, providers, repo matching, `suggest` and `project.createFrom` are removed by V0.55; matching is by one folder.* A thread runs in a **workspace** `{id, host, path, provider, refs, branch}`, stored in its own table; the thread DTO carries `workspaceId` next to `cwd`. A **ref** is `{kind, key}`: `folder` with key `<host>:<path>`, or `repo` with a normalised remote (`github.com/meros/fyr`). A **provider** identifies a folder, in this order: `scratch` (the folder is inside a scratch root, by default `~/.t3/scratch` and `~/.local/share/fyr/scratch`, set by `scratchRoots` in the config): no refs; `clone` (the folder is in a git checkout): the folder ref of the main repository root, the folder's own folder ref when it differs (a subfolder or a worktree), and the `origin` remote as a repo ref; `folder` (anything else): the folder's own ref. Project sources are refs (the wire form stays `{kind, host, path}` / `{kind, url}`). A source covers a ref of its kind: a folder covers the same folder and every folder below it on the same host, a repo covers an equal key on any host. **Matching is one function:** the project with the most specific cover of any of the workspace's refs, a longer folder before a shorter one, any folder before a repo, ties by project id. The home folder and `/` have no rule of their own: they match only a project that has them as a source. **Fyr never makes a project by matching.** A thread with no project shows "No project". When its workspace has a repo ref, its DTO carries `suggest: {name, sources}` (the name is the repository's name, the sources are the main root folder and the repo), and the thread header (desktop and phone) offers "Make project “<name>”"; `project.createFrom {threadId}` makes that project (counted as edited) and files every thread without a project that it now matches; a thread with no suggestion gets 400. Matching runs when a thread gets its workspace (create, adoption, start for threads from before workspaces) and only if it has no project, and after a change to a project's sources for every thread not filed by hand; it moves a thread to the project it finds and never takes a project away (a thread that nothing matches keeps its project). A thread filed by hand never moves. At start the host gives every thread without a workspace one, clears the auto flag of auto projects that hold threads and deletes auto projects without threads that were never edited (migration; nothing makes new auto projects). The host no longer seeds projects from T3 at start; the projects seeded earlier stay. |
| V0.29 | **Drafts survive.** What the owner typed in a thread's composer is kept per thread outside the screen: a new message, a card that opens or closes, a screen change, a reload after a deploy and a restart of the app keep it. Sending clears it. It lives in the browser's storage on that device (memory only when storage is not allowed); it is never sent to the host before Send. (Found live: the input cleared when a new message arrived.) |
| V0.30 | → 3.15. **Projects: one picker, one create step, threads start from a project.** *`newProject` is `{name, folder}` and the step shows one folder since V0.55; the "Make project" chip is gone.* *The create step's source field, the folder text field of a new thread and the source lines of the project screen are replaced by V0.42 (folders, a main folder, the folder picker, a scratch folder by default); the rest holds.* One set of words: "Project…" files a thread, "New project" makes one, "No project" is a normal state. (1) **The picker.** "Project…" opens one picker from three places: the project chip in the thread header (desktop and phone, filed or not), the thread ⋯ menu (desktop key `p`; phone "Project…"), and the suggestion chip "Make project “<name>”" (V0.28), which opens the picker with its first row focused instead of making the project at once. The picker has a search field (desktop: arrows walk the rows, Enter picks, Esc closes) and these rows: first **New project from this thread**, then **No project**, then the projects by last use; the thread's current choice is marked. Typed text that matches no project turns the first row into New project “<text>”, and Enter in the search then opens it with that name. Picking a project files the thread by hand (`thread.setProject {projectId}`). "No project" unfiles it: `thread.setProject {projectId: ""}` clears the project and counts as filed by hand, so matching leaves the thread without a project. A filed thread's picker ends with "Edit <name>…", which opens the project screen. On the phone the picker is a sheet; the details sheet's Project row opens it too. (2) **Creating is one step.** Only a name is required. The step shows the name field and the sources the project starts with, each with one plain line of what it does and a × to leave it out, and one optional field that adds a source (a path or a repo URL, as on the project screen). The button says "Create and file this thread" from a thread and "Create project" elsewhere. From a thread the step is prefilled from the thread DTO's `newProject {name, sources}`, which the host computes for every thread with a workspace: for a clone with a remote, the suggestion's name and sources (main root folder and repo); otherwise the folder's name with its folder as the source. A scratch workspace, the home folder, `/` and a thread without a workspace give no `newProject`: the name field starts empty and there is no source. A source that an existing project already has is left out of the prefill; when none is left, there is no `newProject`. Creating from a thread sends `project.create {name, sources, threadId}`: the host makes the project and files that thread by hand in one write, then re-matches as after any source change (so other unfiled threads in the same place join it). The separate form with a folder field and a repo field is gone: every "New project" uses this step. (3) **The project screen** is where the rest is edited (name, icon, colour, defaults, sources). Each source shows one plain line: a folder on this host "Threads started in this folder land here", on another host "Threads started in this folder on <host> land here", a repo "Threads in any clone of this repo land here, on any computer"; no source: "No source: you file threads here by hand". (4) **A new thread starts from a project.** The new-thread dialog (desktop) and screen (phone) ask Project first, with the same picker. It starts on the inbox's project filter, else the project used for the last new thread on this device, else the most recently used project. The folder follows from the project's folder on this host and shows as one line with "Change"; a project without a folder on this host shows the folder field, and the thread cannot start until it has a folder. "No project" shows the folder field. "Pick a folder…" in the picker keeps the project empty and shows the folder field. "New project" in this picker makes the project (no thread to file) and picks it. |
| V0.31 | **Fyr's own right-click menu; no browser menu.** A right-click (or a long press on the desktop) never shows the browser's page menu. What it shows depends on where it lands: on a thread row, the thread's menu (Open; Settle or Unsettle; Snooze… or Unsnooze; Mark unseen or Mark seen; Archive or Unarchive; Project…; Stop run while it runs; Copy link), each with its key, unavailable ones disabled with the reason as a tooltip; on selected text, Copy; on a link, Open link and Copy link; on an image, Open image. In a text field the browser's own menu stays, because spelling suggestions and Paste need it. Anywhere else nothing opens. The menu works from the keyboard (arrows, Enter, Escape), stays inside the window and closes on a click outside, a scroll or Escape. On the phone, a long press on the page shows no browser callout or menu (rows and text fields keep their own behaviour). |
| V0.32 | **The installed app feels native on a phone.** The page never bounces and a pull at the top never reloads it; a double tap never zooms; a tap shows no grey flash; a long press on the app's own controls (rows, buttons, tabs, chips) shows no browser callout and selects no text, while text people read (messages, code, questions, details) stays selectable. A press reacts at once (the pressed colour, and a slight press on the main button). Android gives a short vibration when a swipe settles or snoozes, when a message is sent and when a card is approved. A new screen slides in from the right and Back slides it out (no motion with reduced motion); a sheet does not slide the screen. No scrollbars and no focus rings from touch. A screen change, transition included, is done within 800 ms on a phone (checked in the layout test; found live: open and Back took 4 s each). |
| V0.33 | → 3.3. **Message delivery is visible and cancellable (decision 0009).** Every message sent to an agent (by the owner, another thread's agent or Fyr) gets an id, which the host passes to the CLI as the user message's `uuid`. The CLI queues a message that arrives during a turn and takes it in at the next tool boundary, or starts a turn with it when idle, and reports each step as `command_lifecycle`. A message is `sent` (written to the agent, no report yet), `queued` (in the agent's queue, not read), `read` (the agent took it in), `cancelled` (withdrawn before it was read) or `lost` (the agent could not start, the process ended or Fyr restarted before it was read). The `user` event carries `id`; each change is a `message.state {id, state}` event; the thread DTO keeps the unread ones in `outbox: [{id, state}]`. `thread.cancelMessage {messageId}` withdraws a `sent` or `queued` message with the CLI's `cancel_async_message` and answers 200 only when the CLI confirmed; it answers 409 when the message is read, unknown or not in the outbox, or when the agent cannot withdraw it. After a host restart every message still in the outbox becomes `lost`; nothing is resent by itself. Without `command_lifecycle` (an older CLI) a message becomes `read` when its turn ends. **Client, desktop and phone:** a read message stands where the agent read it, so the log reads in the order the agent saw it. Unread messages stand at the end, after a quiet divider "Agent is here", dimmed, each with the word "Waiting" and a Cancel button (a `sent` one says "Sent", without Cancel). The last read message of the owner carries a small muted "Read"; older ones carry nothing. A cancelled message stays where it was cancelled, struck through, with "Cancelled" and "Send again"; a lost one says "Not delivered" with "Send again" (a message from another thread gets no Send again). No undo toast: a cancel is final at the CLI, and Send again is the honest way back. Stop (interrupt) keeps waiting messages: the agent stops its current step and then takes them in a new turn (the owner's choice, 2026-11-14). |
| V0.34 | **Every output copies in one tap; the rail is calm.** Each assistant message has a Copy button (the markdown source); each code block in a message has its own Copy button (the code); an opened tool call has Copy on its input and on its output; a quiet line's full text has Copy. On the desktop the message button shows on hover and on keyboard focus; on the phone it is always there, faint. A copy confirms with "Copied" on the button for about a second. In the rail, a section with no threads cannot be selected (dimmed, no jump, also not with g1–g5), and the host name stays on one line, cut with an ellipsis, the full name in the tooltip. |
| V0.35 | **Tables stay readable at any width.** A table in a message never breaks a word in the middle and never squeezes a column to a few letters. Where the message column is narrower than about 520 px (a phone), each row shows as a small card: every value with its column name above it, in the order of the columns. Where it is wider, the table keeps its columns; a table wider than the column scrolls sideways inside the message, and the page itself never scrolls sideways. (Found live: a commit column wrapped one character per line on the phone.) |
| V0.36 | → 3.4, 3.13. **Dismiss a merged PR; the thread goes on.** `thread.dismissPR` removes a merged or closed PR from a thread (an open PR cannot be dismissed: "The PR is still open"). The thread keeps its conversation and its state; the PR chip and PR tab go away. Fyr remembers the dismissed URL, so polling or a later link never brings it back; a new PR the agent opens attaches as usual. The client offers "Dismiss PR" on the PR chip and PR tab of a merged or closed PR, and in the thread's right-click menu. A thread without a PR shows no PR chip and no PR tab (no "no PR" placeholder); a link or deep link to the PR tab of such a thread opens Conversation. |
| V0.37 | → 3.4, 5.14. **Live views (experiment, decision 0010).** A thread may keep a **live view**: a page pinned above its conversation that shows the state of the agent's work, so the owner does not have to read the thread to know where it stands. (1) **Flag.** `liveView` is a thread field, set with `thread.create {view: true}` or `thread.setView {on}`. With it, the agent's process starts with a prompt block that explains the view and with three Fyr tools; without it the tools are neither listed nor callable. The prompt and the tools are fixed when the process starts, so `thread.setView` ends an idle process at once and a running one after its run (never while it holds unread messages). (2) **Tools.** `view_publish {html \| path, state}` stores a self-contained page (a fragment gets the Fyr theme; at most 512 kB) once by content hash, in the attachment store, with a bridge script first in `<head>`, and sets the full state. `view_state {patch}` applies a JSON Merge Patch (RFC 7396: keys merge, `null` deletes a key, arrays are replaced whole) and fails before any page exists. A state over 16 kB as JSON is refused with the size. `view_unchanged {reason}` records that the view is current. (3) **Events and DTO.** `view.published {pageId, state}`, `view.state {state}` (the full resulting state) and `view.unchanged {reason}`. The thread DTO carries `live: {pageId, state, updatedAt, checkedAt, headline}`; `headline` is `state.headline` when it is a string (first line, at most 160 characters); `checkedAt` moves on every `view_*` call. (4) **Stop hook.** Fyr registers a Stop hook (`callback view`) next to the PreToolUse gate on every `initialize`. At the end of a run it blocks once, with the reason "Update this thread's live view (view_state or view_publish), or call view_unchanged with a reason, then stop.", when all hold: the thread has the flag and its process has the view tools, no `view_*` call happened since the run started, Fyr has not blocked in this run, `stop_hook_active` is false, and no background task runs. Otherwise it allows. The CLI's synthetic user message with the reason is dropped, its `stop-hook-error` notification is ignored, and when the forced turn called only `view_*` tools the thread keeps the summary it had before the block. (5) **Bridge.** The page and the client talk with `postMessage` and MCP Apps names: the client sends `ui/notifications/state {state}` when the page loads, when it reports `ui/notifications/initialized` and on every change; the page reports `ui/notifications/size-changed {height}` and sends a press as `ui/message {role: "user", content: [{type: "text", text}]}`. The client accepts a message only when `event.source` is the view's own iframe, a JSON-RPC 2.0 message with a known method, and a press with 1 to 2,000 characters of text. In the page, `window.fyr.state` is the state, `fyr.onState(fn)` runs on every change, `fyr.send(text)` and an element with `data-send="text"` send a press, and `data-text="a.b"` shows a state field. (6) **A press is a message.** It becomes `thread.send {text, from: "view"}`; the agent reads `[From the live view] <text>`; the log shows a compact chip "View <text>" with the usual delivery line. A press never runs an Action. (7) **Client, desktop and phone.** The view shows above the conversation (Conversation tab), in the previews' sandbox, as tall as the page reports and at most 60 % of the pane, with "Live view", an "updated N min ago" stamp and a collapse control; collapsed it shows the headline in one line (remembered per thread on the device). While a run goes that has not touched the view, the view is dimmed and the stamp adds "agent working". The inbox row shows the headline in place of the last assistant text. "Turn on live view" / "Turn off live view" is in the desktop ⋯ menu and right-click menu, "Live view on/off" in the phone ⋯ sheet, and "Live view" is a checkbox in the new-thread dialog and screen. Not in this slice: structured blocks, live GitHub bindings, state schemas, multi-file pages, templates. Only an agent process started with the live view registers the Stop hook; other threads have no hook question at the end of a turn. Turning the view on or off restarts the process after its run, so the hook follows the flag. A thread with a live view has a View tab first (View · Conversation · Changes · PR) and opens on it; the view fills the tab. The Conversation tab is always there: above the conversation it shows the view as one headline bar, and a tap on it opens View again. An explicit tab choice wins and survives a reload. The text of the turn Fyr forced to update the view shows as one quiet line, and View buttons are refused like any message while a question is open (both the owner's choice). A question or approval card is never hidden behind the View tab: it shows under the view on the desktop and over it on the phone. |
| V0.38 | **A fast client: small transfers, deltas, a free main thread.** (1) Text responses (JSON, the event stream, HTML, JS, CSS) are gzipped when the client accepts it; the stream is flushed after each event. Images and attachments pass unchanged. (2) What the host sends is lean: a tool output over 16 KB (4 KB inside a subagent) is cut to its head with a line that says how much is left, and base64 image data in a message (screenshots in tool results, which the conversation does not draw) is left out with its type and size; the event carries `cut: true`; the store keeps everything. `GET /api/threads/{id}/events/{seq}` returns one event whole, and an opened tool call with a cut output offers "Load the full output". (Found live: opening a busy thread downloaded 2.1 MB uncompressed, 85 % of it base64 screenshots inside tool results; with V0.38 the same load is 73 KB.) (3) Thread updates on the stream are deltas: after the first whole thread, a connection gets `thread.patch {id, rev, base, patch}` (a JSON Merge Patch against the copy it was last sent) whenever that is smaller; a thread goes out at most every 100 ms per connection, the newest version winning. The client applies a patch only when its copy has `rev === base`; otherwise it fetches the thread whole (`GET /api/threads/{id}`) and changes nothing meanwhile. (Measured: a one-field change of a thread with 20 agents was an 8 KB update.) (4) The device keeps a copy (IndexedDB, written when the browser is idle): the last state and the newest 400 events of each opened thread. At start the app draws the stored state at once, offline too, and asks `GET /api/state?since=<rev>` for only the threads changed since (the reply has `partial: true`); opening a thread draws its stored events and fetches only newer ones. When more than one page is new, the stored events are dropped for the fresh page. Without IndexedDB everything comes from the network as before. (5) The main thread stays free: stream messages are applied together once per frame (a short timer while hidden), a message's markdown renders once, conversation items outside the screen skip layout and paint (`content-visibility`), and following new content at the bottom never reads the layout first. Budget, checked in the layout test at 4× CPU throttling while 300 thread updates and 300 messages stream in over 3 s: no main-thread task over 100 ms and under 250 ms of long tasks in all (before: 10 long tasks, 523 ms). |
| V0.39 | **An update you can follow.** The deployer reports each phase as it happens (`build`, `test`, `drain` with the ids of the threads it waits for, `switch`, then `done` or `failed` with the reason); the host sends it on the stream as `event: deploy` and puts the latest in the state (a finished one stays two minutes), so a page that reloads still sees how it ended. The client shows a banner with a spinner and a sentence: "Updating Fyr · building the new version · 1 min", "running the tests", "waiting for “<thread>” to finish its turn" or "waiting for 2 threads to finish: …", "Restarting Fyr · this page reloads by itself" (also while the host is away during an update), then "Fyr is updated · build <id>" or, in red, "The update failed: <first line>" with Details. The page reloads by itself onto the new build (V0.13). On a desktop the banner sits at the bottom left; on a phone at the top, below the safe area. *Amended by V0.118*: the banner is an inline status line in the layout, calmer, and never over a control. (The owner found deploys opaque.) |
| V0.40 | **A hideable thread list.** On the desktop the thread list (search, project filter, sections) can be hidden with the List button in the rail or the `\\` key; the open thread then takes its room. The owner's choice is kept on the device. Without a choice, a window narrower than 1100 px starts with the list hidden. With no thread open the list always shows, and jumping to a section from the rail (or g1–g5) shows it again. (Found live: in a narrow window the list pushed the thread off the screen.) |
| V0.41 | **Visual pacing: the outcome stands out, the work is quiet.** Each conversation item is an outcome (the owner's messages, the agent's last message of a run or the newest one while it runs, previews, a failed run, effect or agent), a step (earlier agent messages, tool groups, successful effects, answers, notes) or a run's end. Outcomes read at full weight with room before them; steps are muted, smaller, borderless and close together; consecutive steps fold into one line ("Read 3 files · ran the tests and 1 more · 9 steps · 1m 15s") that opens to every step in order, with a failed call's mark kept on the line. Not folded: a single step, system lines only, the newest steps of a run still going, and everything with tool details on. The owner's messages are pale tinted bubbles, not dark ones. (The owner found every paragraph, tool card and system line equally loud; decision 0011.) |
| V0.42 | → 3.15. **Projects with folders, a main folder, an automatic repo line and a host folder picker (decision 0012).** *Several folders, the main folder, `project.setMain` and the repo line are removed by V0.55: a project has one folder; the folder picker and the scratch folder stay.* *Amends V0.23, V0.28 and V0.30.* (1) **Folders and the main folder.** A project's folder sources keep their order; one folder per host is **main** (`main: true` on the source; without one, the first folder on that host). `project.setMain {id, index}` marks a folder source main and clears the mark on the project's other folders on that host; a repo source or a bad index is 400. The state's `cwd` of a project is its main folder on this host, and a new thread in the project starts there. (2) **The repo line is automatic.** The host reads the `origin` remote of each folder of each project on this host (normalised, cached, read again after a change to a project's folders and once at start) and the project in the state and on the stream carries `repos: ["github.com/meros/fyr", …]`: read-only, deduplicated, including repo sources made by hand earlier. A derived repo counts in matching like a repo source (V0.28): clones of the repository on any host land in the project; a folder still wins over a repo. The client has no repo field anymore; it shows "Also matches clones of <repo>". The word "source" is gone from the UI: the project screen lists **Folders** ("Threads started in these folders land here."), each with a star (filled: "Main folder: new threads start here."; empty: make it main) and ×, and "Add folder…" opens the folder picker. A project without folders says "No folder: you file threads here by hand." (3) **No project, no folder: a scratch folder.** `thread.create` takes `projectId?`; it files the thread there by hand in the same write. Without `cwd` the thread starts in the project's main folder on this host; with neither a `cwd` nor a project folder here the host makes `~/.local/share/fyr/scratch/<YYYYMMDD-HHMMSS>-<slug>` (slug: the first words of the text, lower case, letters and digits joined by `-`, at most 40 characters, else `thread`) and the thread has "No project" unless a project was given. Fyr's scratch folder is always a scratch root. The new-thread screen shows "A fresh scratch folder" when nothing is picked. (4) **The host folder picker.** One picker (a dialog on the desktop, a sheet on the phone) with one field: a word searches every section, a path (`~/…` or `/…`) browses there and completes. Sections: **This project** (the project's other folders, from "Change" in a new thread), **Recent** (distinct folders of threads, newest first), **Repos found** (git repositories found by a scan of the project folders on this host and the configured `scanRoots`, two levels deep: a folder with `.git` counts and is not descended into; hidden folders and `node_modules` are skipped; cached for five minutes and refreshed in the background), **Browse** (the subfolders of a folder starting at `~`, breadcrumbs back up, "Use this folder") and **New folder** (a name without `/`, not empty, not taken, made in the folder being browsed). Read-only host calls: `GET /api/fs/list?path=`, `GET /api/fs/recent`, `GET /api/fs/repos`, and `POST /api/fs/mkdir {parent, name}` (X-Fyr). Every path resolves through symlinks and must stay inside the owner's home folder, else 400; listing skips links that lead outside. The picker serves "Change" in the new thread (project folders first), "Add folder…" on the project screen and in the create step. (5) **The create step** lists the folders the project starts with, each "Threads started in <folder> land in this project.", "Add folder…", and the derived repo line; without folders: "No folder: you file threads here by hand." The config option `scanRoots` (`services.fyr.scanRoots`, a list of strings, default empty, `~` expanded) sets the extra scan roots. (The owner did not understand how folders take part in the picker; decision 0012.) |
| V0.43 | → 5.10. **The PR tab: read a PR, act on it, hand it to the agent.** (1) **Detail.** `GET /api/threads/{id}/pr` returns the thread's PR as the host sees it with `gh`: number, URL, repo, title, body (markdown), state, draft, author, head and base branch, additions, deletions and changed files, labels, `mergeable` and `mergeState` (GitHub's merge state status), the review decision, the latest review per reviewer, every check (name, workflow, `state` fail\|running\|pass\|skipped, start and end time, details URL; a CheckRun and a commit status map to the same shape), and the unresolved review threads (path, line, outdated, comments with author, text and time; the first 50 threads). The host caches it 30 s per PR; `?fresh=1` fetches again. A failure (gh not signed in, no network) is `200 {error}` so the tab can say it; a thread without a PR is `200 {error}` too, an unknown thread 404. The PR snapshot the poller keeps in the thread (V0.8) stays as it is. (2) **Merge and re-run** are owner commands on the command path, with receipts: `pr.merge {method: squash\|merge\|rebase}` (default squash) and `pr.rerunFailed`. Both are unavailable without an open PR; Merge also with a merge conflict or requested changes. The host merges with the same code as the `pr.merge` effect, after reading the PR again: it refuses a closed or draft PR and a merge state of conflicts, blocked or behind, with the reason. Re-run reruns, with `gh run rerun <id> --failed`, each failed, cancelled or timed-out workflow run of the PR's head commit, and nothing else. The result is an `effect.result` line in the thread (`by: owner`), and, like every `fyr` action's result (V0.60 (4), #31), it is sent to the agent as a `[Fyr]` message. One guard decides whether a merge can run, whoever starts it (this command, a change's Merge, an agent's `effect_request` or `change_request`); it reads the PR's change first and `thread.pr` only when no change has the PR. The PreToolUse gate still stops the agent from merging (V0.12). (3) **Ask the agent.** A failed check offers "Ask the agent to fix this check": it sends the owner's message "The check <name> failed: <details URL>. Find the cause and fix it.". Unresolved review threads offer "Ask the agent to address N review comments": one message that lists each as `path:line` and its first comment, each comment cut at 300 characters. Both go through `thread.send` and are unavailable while `thread.send` is. (4) **The tab**, desktop and phone, important first: the header (title, `#number`, a state pill, Draft, head → base, author, `+N −M` and the file count, which opens the Changes tab); one merge status line in plain words ("Ready to merge", "Blocked: 1 failing check", "Conflicts with main", "Review required", "Changes requested", "Behind main", "Checks running", "Merged", "Closed"); the actions (Merge…, Re-run failed, Open on GitHub; Dismiss PR once merged or closed, V0.36); checks with the failed ones first, then the running ones, and the passed and skipped ones folded into one line ("12 passed ▸"); reviews; the unresolved threads folded per file, the comment text readable, each with a link to GitHub; labels; the description as markdown, folded after about 12 lines. Merge… opens a small dialog: method (Squash, the default; Merge commit; Rebase) and one confirm button that names the result ("Squash and merge #88"). Merge is disabled, with the reason as its label or title, while the status line is not Ready to merge, or the PR is not open. Loading and error states read in plain words; Refresh fetches fresh, and "updated N s ago" says how old the data is. The phone uses 44 px targets and keeps the actions and the dialog inside the safe areas. The PR chip on the inbox row and in the thread header uses the same words: its label is short ("#88 failing", "#88 conflicts", "#88 review", "#88 ready", "#88 running", "#88 merged"), its title the status line. |
| V0.44 | → 3.16. **Provider quota where a profile is chosen.** The host reads how much of each profile's provider allowance is left, with the logins stored on the host, and the client shows it calmly. (1) **Kinds.** Each profile has a quota kind: `anthropic-oauth` (a Claude subscription: the CLI's OAuth login in `<configDir>/.credentials.json`, field `claudeAiOauth.accessToken`; `configDir` defaults to the profile's `CLAUDE_CONFIG_DIR`, else `~/.claude`; `GET https://api.anthropic.com/api/oauth/usage` with `anthropic-beta: oauth-2025-04-20` gives the windows "5 h" and "7 d", each with used % and reset time), `deepseek` (`GET <baseURL>/user/balance`, default `https://api.deepseek.com`), `openrouter` (`GET <baseURL>/key`, default `https://openrouter.ai/api/v1`: the key's remaining limit, else what it has spent) or `none`. The two key kinds read an API key from `keyFile` (a path, `~` expanded) or `keyEnv` (an environment variable, looked up in the profile's `env` and then the daemon's; default `DEEPSEEK_API_KEY`, `OPENROUTER_API_KEY`). The kind and its fields come from the config's `quota` map by profile id, else the profile's own `quota`; without either, a profile whose binary is `claude` and whose env sets no `ANTHROPIC_BASE_URL` is `anthropic-oauth`, every other profile `none`. NixOS: `services.fyr.quota.<profileId> = {kind; configDir; keyEnv; keyFile; baseURL}`. (2) **Reading.** `GET /api/quota` reads providers whose data is older than 5 minutes and answers with every profile that is not `none`. A rate limit (HTTP 429 or a `rate_limit_error`) waits 5, 10, 20, then 30 minutes before the next try; the data from before stays, marked stale, with a note. An expired or rejected Claude login says "Login expired; run this profile once" (the host never refreshes a token). Between reads, the CLI's `rate_limit_event` on a running session of an `anthropic-oauth` profile updates its windows for free. A token or key never appears in a log line, an error or a reply. (3) **The client** shows one quiet line per profile, "Claude 5 h 34 % · 7 d 61 %" or "DeepSeek ¥12.40 left", under the profile picker of the new-thread dialog (desktop) and screen (phone), and the busiest window per profile in the rail's footer (desktop). A window at 80 % or more is coloured; nothing else is. Hover (desktop) or tap (phone) shows the reset times in local time, when the data was read, and the stale or error note. (The owner switched profiles without knowing which account had room left.) The usage reply is read loosely: only `five_hour` and `seven_day` are decoded, other fields are ignored. A stream event updates windows but never clears an API error. (Found live: the 5 h window was missing because a strict decode failed and a stream event hid the error.) *Amended by V0.122*: a token account is never read from the usage endpoint; a refused or rate-limited read keeps its wait and data across a restart. |
| V0.45 | → 3.3. **Hand a thread over to another profile.** Only the owner starts it (thread ⋯ menu, right-click menu, phone ⋯ sheet); never quota, errors or agents. The thread keeps its history, project and folder; a divider in the conversation says "Handed over from Claude to Claude (acme) · 17:20" and how the context went over, and later runs use the new profile and its model. Only to another profile, not while a run is live, a question is open or the thread is archived. The context goes over by the first rung that works: (1) **resume**: the target runs the same CLI (claude), so the session transcript (`projects/<dir>/<session>.jsonl` and the `<session>/` folder) is copied into the target's config dir (none to copy when both share one) and the target starts with `--resume`; (2) **adopt**: Fyr writes the conversation as Markdown to a file and the target starts a new session whose first message points to it; (3) **brief**: one turn on the source profile writes a handover (goal, done, state, open items, how to continue, branch and PR), Fyr saves it to a file and the target starts from it. A rung that fails (no transcript, copy error, resume error) falls to the next, and the divider names why. If the target cannot start, the thread stays on its profile and the failure shows. The dialog shows which rung will run before the owner confirms. (Decision 0013.) |
| V0.46 | → 3.15. **Quiet hours per project (decision 0014).** (1) **The setting.** A project may carry `quiet: [{from: "17:00", to: "06:00", days?: ["mon"…"sun"]}, …]`, a list of windows (at most 14; decision 0014 addendum): times are `HH:MM` in the host's local time zone; a window whose `to` is not after `from` crosses midnight (`from` equal to `to` is a whole day, which the client's "All day" writes as `00:00`–`00:00`); `days` are the days a window *starts* on, every day when absent or empty, so Fri 17:00–06:00 runs into Saturday morning. The project is quiet while any window holds. `project.update {quiet}` sets the list; `quiet: null` or `[]` clears it; a bad time or day in any window is 400 and names the window. The single object stored before the addendum (`quiet: {from, to, days?}`) still reads, as a list of one, and is written back as a list. (2) **Quiet now.** The host computes, per thread whose project is quiet at this moment, `quietUntil` (RFC 3339, the end of the current quiet stretch: windows that overlap it or follow on without a gap join it, so weeknights 17:00–06:00 Sun–Fri and an all-day Saturday and Sunday give Friday 17:00 to Monday 06:00; at most a week ahead) in the thread DTO, and the same `quietUntil` on the project; both are absent otherwise. The host sends the changed threads and the project on the stream when a window starts or ends (its tick), and when `quiet` changes. (3) **No notifications.** No Web Push for a thread while it carries `quietUntil`, including a done or blocked push whose 20 s wait ends inside the window. Nothing is queued: when the window ends no push is sent for what happened during it. (4) **Hidden from the inbox.** While quiet, the project's threads are left out of the inbox sections on desktop and phone, of the rail and section counts, of the "Needs you" count in the title and of the next-thread order. Picking the project in the project filter shows them, and the filter chip (desktop) or the filter line (phone) says "Quiet until 06:00". Search (desktop and phone) still finds them. The client re-renders at the nearest `quietUntil` with one timer, so the threads reappear when the window ends, without any notification ("back silently"). (5) **The project screen** has "Quiet hours": one row per window, each with from and to in 24-hour time whatever the browser's locale (hour and minute pickers, not the native time field), an "All day" toggle, the seven day toggles (Mon–Sun) and × to remove the row; below the rows "+ Add quiet hours" (17:00–06:00, every day) and two presets, "Weeknights 17:00–06:00" (Sun–Fri starts, so with the weekend it reaches Monday 06:00) and "All weekend" (Sat–Sun, all day), each shown only while the list does not already hold it. No rows is off ("Off: threads show and notify as usual."). One line describes all rows in plain words: "Threads hide and send no notifications weeknights 17:00–06:00 and all weekend." (a lone every-day window keeps "from 17:00 to 06:00"), and "Quiet until Mon 06:00." while quiet. (6) An open thread of a quiet project works as always: the live view, decision cards and message delivery are unchanged. (The owner: "Add per project quiet hours! Hide threads by default and dont notify"; and, for several windows, "add option to have more quiet hours, like all weekend. Maybe multiple rows?") |
| V0.47 | **Previews run real pages: folders, CDN libraries and 3D.** *Amends V0.19 and V0.37.* (The owner: "I want the inline embedded viewer to be able to handle real html with things like 3d".) (1) **A folder preview.** `show {path}` takes a folder (its `index.html`, or the optional `entry`, a relative path, is the page) or an `.html` file (the page; the files next to it come along). Fyr copies the folder once: regular files only, at most 500 files and 50 MB in all, skipping names that start with `.` and `node_modules`; a link that points outside the folder is skipped and named in the reply, an `entry` with `..` or an absolute path is refused. An `.html` file whose folder is over the limits is shown alone, and the reply says so. Identical folders are stored once (a hash over the paths and contents). The preview event is the same `{kind: "html", attachmentId, …}`. (2) **Serving.** `GET /api/previews/{id}/{path}` serves the folder's files with their media types (`.glb` `model/gltf-binary`, `.gltf` `model/gltf+json`, `.wasm` `application/wasm`, `.hdr` `image/vnd.radiance`, `.ktx2` `image/ktx2`, `.js` and `.mjs` `text/javascript`, images, fonts, audio and video); `GET /api/previews/{id}` of a folder redirects to its entry, so relative URLs resolve. A path with `..`, a missing file or a file of a single-page preview is 404. Every file is served with the preview CSP and `Access-Control-Allow-Origin: *` (the page has no origin, so its own module scripts and `fetch` of its models are cross-origin; the 80-bit preview id is the only key, as before). (3) **The CSP** (single pages, folders and live views alike) keeps `sandbox allow-scripts` without `allow-same-origin`: the page never has Fyr's origin and cannot call the API. It allows the preview's own folder (`http(s)://<host>/api/previews/{id}/`, an absolute prefix, because `'self'` means nothing to a page without an origin) and six CDNs: `cdnjs.cloudflare.com`, `cdn.jsdelivr.net`, `unpkg.com`, `esm.sh`, `ajax.googleapis.com` (model-viewer) and `www.gstatic.com` (the Draco and Basis decoders three.js and model-viewer load by default): scripts and modules with `'unsafe-inline'`, `'unsafe-eval'` (no more than inline scripts already allow), `'wasm-unsafe-eval'` and `blob:`; workers from the folder and `blob:`; `fetch` to the folder, the CDNs, `data:` and `blob:`; images, media and fonts from the folder, the CDNs, `data:` and `blob:`. Everything else (other hosts, frames, forms, top navigation) stays closed. (4) **The frame.** The card and the full-screen viewer embed `allow="fullscreen; xr-spatial-tracking; accelerometer; gyroscope"`; without a `size` an HTML card starts at 16:10 of its width, and a page may report its height with `ui/notifications/size-changed` like a live view (at most 600 px at first, 2,000 px with Show all). A Full screen button (card and viewer, where the browser supports it) puts the frame itself full screen. Frames are `touch-action: pan-x pan-y`: a pinch that starts on a preview never zooms the app, while a page's own canvas (`touch-action: none`, as three.js and model-viewer set it) gets drag and pinch. (5) **Agents** learn from the `show` description and the Fyr prompt that a folder or a page that loads three.js or model-viewer from the CDNs works, with a short example. Not in this slice: CDNs beyond the six, network access to other hosts, WebXR sessions. |
| V0.48 | **Settled closes by itself.** Opened from its header in the list, the Settled section closes again after 30 s; a small clock in its header runs down, and pointing at or using the section restarts the time. It stays open while Settled is the selected section in the rail (a settled thread selected, or chosen with the rail or g5). (Owner: "Show them 30 seconds (with a little timeout clock when expanded) then hide again ... unless settled is selected in the left hand menu".) |
| V0.49 | **Talk on the View tab.** A message typed while the View tab is open is sent with `from: "view-typed"`; the agent reads it as "[Typed on the live view tab] …" and the view prompt asks it to answer such a message first in one or two plain, spoken sentences, then do the work and update the view. The View tab shows a small chat above the composer: your last message typed there with its state (Sent, Waiting, Read) and the agent's first spoken reply after it, or its current step while it works; a tap opens Conversation. In Conversation the message is a normal bubble labelled "from the View tab". The delivery state also follows the thread's `outbox`: a message the host no longer holds never shows as Waiting, even when its read event did not reach this device (found live: two answered messages stayed "Waiting" on the phone and could not be cancelled). In Conversation, a typed message and a press show as the same bubble with the same "View" tag (owner: "Make these more alike"). |
| V0.50 | → 3.16. **Providers (decision 0015, slice 1).** (1) **The model.** A provider is a CLI adapter + an auth + an endpoint + the models it offers, kept per host in Fyr's database (`providers` table, additive). Auth is data the adapter applies: `{kind: cli-login | api-key | none, configDir?, secret?}`; an endpoint is `{baseURL?, modelEnv?}`; each model is `{id, name?, efforts?}` (the effort levels it takes; none for e.g. DeepSeek). The UI calls a cli-login a "Claude account" and an api-key an "Endpoint". (2) **The CLI adapter** (`internal/cli`) is what Fyr needs from an agent CLI: launch (auth, endpoint, model, effort, session, resume, permission mode, MCP config, prompt), the session protocol (send, interrupt, set model, cancel a queued message, permission and hook callbacks, the stream of messages, which is Fyr's observation format), where sessions live (handover rung 1, only between providers on one adapter), whether a login exists, and its capabilities (auth kinds, efforts, its own model list). The only adapter is `claude`: it starts the plain `claude` binary on PATH, never a wrapper, and sets `CLAUDE_CONFIG_DIR` for a config folder other than `~/.claude`, `ANTHROPIC_BASE_URL` + `ANTHROPIC_AUTH_TOKEN` (and an empty `ANTHROPIC_API_KEY`) for an endpoint, `ANTHROPIC_API_KEY` for a key without an endpoint, `CLAUDE_CODE_OAUTH_TOKEN` for an account that stores a login token, the endpoint's model variables, and `--effort`. An endpoint without a key does not start ("DeepSeek needs a key: add it in Settings → Providers"). (3) **Secrets** (`internal/secrets`): one store by name behind a swappable backend; today one file per secret under `~/.local/share/fyr/secrets/<name>` (a provider's is `<id>.key`), mode 0600 in a 0700 folder, written atomically. A value never appears in the database, a DTO, a reply, a log line or an error; clients see `keySet` and the last 4 characters (none for a value under 12). An auto-imported provider may instead point at a key where it was found (`env:NAME` in fyrd's environment or `file:/path`) until the owner stores one. (4) **Model and effort.** A thread carries provider + model + `effort`; `thread.create {effort}` and `thread.setModel {model?, effort?}` set it; empty means the provider's `defaultEffort` when the model takes it. The CLI takes effort at start, so a change ends an idle process (or the process after its run) and the next message resumes with it. The effort picker shows only when the model has efforts. (5) **Quota** (V0.44) is keyed by the auth identity (the login folder, or the key and endpoint), so two providers on one account share one entry and one read; `GET /api/quota` still answers per provider. The kind follows the auth: a cli-login is `anthropic-oauth` in its folder, an endpoint at openrouter.ai `openrouter`, at deepseek.com `deepseek` (read with the provider's key), else `none`; `quotaKind` overrides. (6) **Detection ("Found on this host")**, read-only at runtime, never running a program: today's profiles (the config's, or the ones seeded from T3's settings) with their ids, names, models and quota; a wrapper on PATH is *read* (a text script up to 256 KB) for `CLAUDE_CONFIG_DIR`, `ANTHROPIC_BASE_URL` and model variables, and for a secret only where it comes from (`$VAR`, `$(cat /path)`); a secret written into the wrapper is dropped and the find says so; the DeepSeek or OpenRouter endpoint is assumed from the quota kind or the name when the wrapper says nothing; `~/.claude` and `~/.claude-*` with a `.credentials.json` (existence only); `OPENROUTER_API_KEY` and `DEEPSEEK_API_KEY` in fyrd's environment (by name). Import on the owner's click copies a found key into the secret store. (7) **Cut-over.** On the first start with providers and none stored, fyrd imports each find whose id is one of today's profiles (`claudeAlt`, `claudeAcme`, `claudeDeepseek` keep their ids, so threads, handover and quota keep working), marked imported; found keys stay where they were found (no copy without a click). It runs once (`providers.cutover` in kv). From then on the providers are the profiles and the config's or T3's profiles only feed detection. (8) **Status**: ready; needs a login (an account whose wrapper logged in with `CLAUDE_CODE_OAUTH_TOKEN` is a *token login*: its folder's login, often the main account's `~/.claude`, never counts and it does not start without its token, "This account logs in with a token. Run `claude setup-token` while logged in to it and paste the token as its key."; any other Claude account whose folder has no login and no stored token: "Log in once in a terminal on this host: run `CLAUDE_CONFIG_DIR=~/.claude-x claude` and type /login. Or paste a token from `claude setup-token` as its key."); needs a key (an endpoint without one: paste it). No login flow in this slice. (9) **Settings.** A gear at the bottom of the desktop rail and "Settings" in the phone menu open Settings: Providers (each with name, kind, models as an editable list of ids, default model and effort, quota line, status and key field; "Found on this host" with Import; "Add provider": a Claude account with a folder from the host folder picker, or an endpoint with name, base URL, key and model ids), Hosts (this host), Notifications (the push control), Appearance (placeholder). Phone: 44 px targets, inside the safe areas. (Owner: "A provider abstracts away auth mechanism and cli handling mechanism. Keys should be stored safely in a reused way. Models and effort is chosen separately.") **Token accounts (found live 14 Nov).** An account that logs in with a token has the token as its quota identity and reads usage with that token only; without it, it shows no numbers (never the folder's login). A provider that cannot run (no token or key yet) carries `unavailable` (the reason) in the state; the new-thread picker shows it disabled ("needs setup") and Hand over does not offer it. Settings shows "token login" instead of a folder. *Amended by V0.122*: a token account's quota comes only from its sessions' `rate_limit_event`. |
| V0.51 | → 3.17. **Fronts and hosts, slice 1: one machine, two roles (decision 0016).** (1) **Roles.** `fyrd` runs as `host` (runs threads; serves the host protocol and `/mcp`, never the web client), `front` (serves the web client and the browser API; holds no threads) or `both` (the default, so an unchanged config keeps working). A front is configured with a list of hosts only (`hosts: [{name, url}]`, flag `-hosts name=url,…`). In `both` the front reaches the local host over a real loopback link (an ephemeral `127.0.0.1` port) through the host protocol, the same client it uses for a remote host; it never calls the engine or the store. `/mcp` on the main port goes to the local host. (2) **The host protocol** is the v0 API (docs/API-v0.md) without the web client: `GET /api/state` (snapshot, `?since=REV`), `GET /api/stream` (SSE: `hello`, `thread`, `thread.patch`, `event`, `project`, `deploy`), `POST /api/command`, `GET /api/threads/{id}` and its `events`, `events/{seq}`, `changes`, `pr`, `handover`, the attachments and previews, and the host-wide calls (folders, providers, quota, T3, push, presence). The browser API does not change. (3) **Ids and routing.** Thread ids stay unique; the front keeps an id-to-host map and sends each command and each thread call to the thread's host; a call without a thread goes to `args.host` (or `?host=`) when given, a project command to the project's host, anything else to the first host in the list. The front stamps every thread and project with `host` (the host's name). Its revs are the host's, raised by one when only the front's stamp changes, so the client's keep-the-higher-rev rule (V0.2) and `?since=` (V0.38) hold. (4) **An offline host.** When the link to a host drops, the front keeps that host's last threads and marks each with `hostOffline` (RFC 3339, since when); commands and thread calls for them get `503 {error: "<host> is offline since HH:MM; its threads are read-only"}`. When the link returns the front reloads the host's state and clears the mark. The state carries `hosts: [{name, online, since, error?}]`; Settings → Hosts lists them, and an offline thread's header says "<host> offline since HH:MM · read-only". (5) **A remote host's guard.** A host may also listen on a tailnet address (`hostListen`); there it serves only peers whose Tailscale identity (LocalAPI whois of the connection) is the owner's login, and ignores `Tailscale-User-Login` headers. The loopback guard (V0.14) is unchanged. Not in this slice: a second machine, a front's push and presence (they stay on the host), the cache on disk across a front restart. |
| V0.52 | → 5.14. **The live view stays current during a turn.** (The owner: "make sure that the agent is always timely in updating it".) A process with the live view also registers a PostToolUse hook. After a tool call, when the run has gone `ReminderAfter` (4 min) since the latest of its start, the last view_* call and the last reminder, Fyr adds a one-line reminder to the agent's context: update the headline and the items that moved, or call view_unchanged. At most one reminder per 4 min; none without a live view or outside a run. The view prompt asks for updates when an item starts or finishes, not only at the end of the turn. The Stop hook (V0.37) stays. Both are notices of the agent feed (decision 0020, #47). Cost is measured in issue #17. |
| V0.53 | → 3.16. **Login through Fyr (decision 0015, the login row).** *Amends V0.50 (8): "No login flow in this slice".* From Settings → Providers the owner logs a Claude account in without a terminal and without copying a token. (1) **Start.** A Claude account shows **Log in** when it needs a login (**Get a new token** or **Log in again** in its details otherwise); `provider.login {id}` starts the CLI's own login on the host. Only one login runs per provider (a second start gets 409 "A login for X is already running"); an endpoint has none. (2) **Two kinds.** A token account (V0.50 *token login*) runs `claude setup-token` on a pseudo-terminal (it draws only in a terminal) in a private throwaway folder (0700, removed afterwards), never in the account's folder, which may hold another account's login; the token it prints goes straight into the secret store as the provider's key, and the process is ended. Any other Claude account runs `claude auth login --claudeai` on pipes with `CLAUDE_CONFIG_DIR` set to its own folder (made 0700 when missing; none for `~/.claude`), so the CLI keeps and refreshes its own login there; it is done when the CLI exits 0 and the folder has a login. Both run the plain `claude` on PATH with fyrd's environment minus `CLAUDE_CONFIG_DIR`, `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_*` and the display, with `BROWSER=true` so nothing opens on the host. (3) **The page.** `GET /api/providers` carries `canLogin` and `login: {kind, state, url?, message?, expires}`; states `starting`, `waiting` (the sign-in link is shown), `checking` (a code was sent), `done`, `failed`, `cancelled`, `timeout`. The row shows **Open the sign-in page** (the CLI's link, opened in a new tab), a field for the code the sign-in page shows and **Send code** (`provider.loginCode {id, code}`, written to the CLI's input), the time it ends, and **Cancel** (`provider.loginCancel {id}`). A wrong code shows the CLI's error line; setup-token then starts over on Enter, which Fyr presses, and the new link replaces the old one; `auth login` ends (failed, **Try again**). The client polls every 1.5 s while a login runs. A finished login stays visible for 5 min. (4) **Limits.** A login ends after 10 min (`timeout`, "The login timed out after 10 minutes. Start it again."); Cancel and the timeout kill the CLI's process group; removing the provider cancels its login. (5) **Secrets.** The CLI's output is read in memory only and cleared when the login ends: never logged, stored or sent; a client sees only the link and an error line with anything token-like removed. The code is never logged or echoed back. fyrd logs only "login for <id> started/<state>". The status then follows V0.50: a token account is Ready once its key is set, a folder account once its folder has a login. (6) **Fallback.** The V0.50 texts stay as the second way (paste a token from `claude setup-token`; or run the CLI in a terminal). Phone: every target 44 px. Not verified with a real account in tests: a fake CLI (link, code, token) drives the flow; a `live`-tagged test runs the real `claude` in a throwaway HOME up to a refused made-up code. |
| V0.54 | → 3.17. **Fronts and hosts, slice 2: a second machine (decision 0016).** (1) **Push on the front.** The front holds the device data: push keys and subscriptions (in its machine's database; in `both` the same rows the host kept, so subscribed devices keep working) and presence (`POST /api/presence` stays on the front). It follows each host's stream and applies V0.20's plan to every thread of every host: asking and failed push at once, done and blocked after 20 s if still true, never for a thread a device of this front watches or in quiet hours (V0.46); a change the front missed (an outage, its own restart) pushes when it sees the host again. Hosts send no push. Each front pushes to its own devices: two fronts are two sets of devices; watching a thread on one front does not quiet the other. (2) **The cache on disk.** The front writes the last state of each host (threads as the host sent them and as published, projects, profiles, deploy) to `<data>/front/<host>.json`, at most every 2 s and when it stops. After a restart a host it cannot reach shows from the cache at once: its threads marked `hostOffline` (since the front last had it online) and read-only, until the link returns. (3) **Files by host.** An upload goes to `?host=` or `?thread=` (the thread's host; a new thread's project's host), else the first host; an attachment or preview read goes to `?host=` / `?thread=`, else the host the front learned for that id (its upload, or the preview page whose files these are), else the first host. No host is tried in turn. The client sends the thread's host with every attachment, preview and markdown image URL and with each composer's uploads. (4) **State per host.** `hosts[]` adds `profiles` (that host's, decision 0015) and `deploy`; the top-level `profiles` is the merge by id (the first host's wins), so every thread finds its profile; the new-thread dialogs list the project's host's profiles and send `projectId`, which the front routes to the project's host. `actions` are the first host's. `GET /api/quota` merges every online host's (`?host=` for one). (5) **Deploy per host.** The drain and the switch stay on the host (V0.24). A host's `deploy` frames reach the browsers only from the front's local host (its own update, the banner); another host's update shows on its Settings → Hosts row ("online · updating Fyr") and as an offline host while it restarts; nothing else changes on the other front. (6) **Phone.** With more than one host a row names its host and the thread header carries a host chip; a thread on an offline host says "<host> offline" in its row and "<host> offline since HH:MM · read-only" under the header chips, with one host too. (7) **NixOS.** `hostListen` takes `tailnet:PORT` (fyrd reads this machine's tailnet IPv4 from tailscaled; until it is up fyrd exits and the unit restarts it); `hostPort` sets it and opens the port on tailscale0 only; the unit starts after tailscaled when it listens there; `role front` needs hosts and `tailnet.enable` needs a front. docs/API-v0.md has the two machines' config. |
| V0.55 | → 3.15. **A project is one folder (decision 0017).** *Amends V0.23, V0.28, V0.30 and V0.42.* (1) **The model.** A project is `{id, name, color, icon, folder?, profile?, model?, quiet?}`: a name, a colour and one folder on its host (a full path, `~` expanded, that must exist there, else 400 naming the path), or no folder (threads are filed in it by hand). With decision 0016 the project lives on the host of its folder; the front stamps `host` on it. The defaults (profile, model) and quiet hours (V0.46) stay. Removed: `sources` (folder and repo sources), the main folder (`main`, `project.setMain`), `repos` and the automatic repo line, repo matching across hosts, refs and covers, the workspace records and their providers (`workspaceId` on threads and the `workspaces` table), `suggest` and `project.createFrom`, `project.addSource` and `project.removeSource`. (2) **Which threads land in it.** A thread not filed by hand lands in the project whose folder is the thread's folder or contains it; the longest folder wins, ties by project id. Scratch folders, the home folder and `/` have no rule of their own. Matching runs when a thread is made or adopted, at start, and for every thread not filed by hand after `project.create` or a change of a project's folder; it moves a thread to the project it finds and never takes a project away. A thread filed by hand (`thread.setProject`, including `""` for No project, `project.create {threadId}`, `thread.create {projectId}`) keeps its project: nothing matches it again. (3) **Commands.** `project.create {name, folder?, threadId?}`; `project.update {id, …, folder?}` sets the folder (`""` clears it) and then matches. A new thread in the project without `cwd` starts in its folder. (4) **New project from this thread.** The thread DTO's `newProject` is `{name, folder}`: the thread's folder and its last path element; absent for a scratch folder, the home folder, `/`, and a folder a project already has. (5) **Migration at start.** A project stored with `sources` keeps one folder: the one marked main on this host, else its first folder on this host, else its first folder on any host; every other folder and every repo source is dropped, and the host logs each drop with the project's name. Threads keep their projects. The `workspaces` table is dropped. (6) **The project screen** (desktop and phone) shows Name, Folder (the short path with "Threads started in this folder or below land here." and "Change…" through the host folder picker; without a folder "No folder: you file threads here by hand." and "Choose folder…"), Quiet hours, Colour, Icon and the defaults. The create step shows Name and Folder the same way. The "Make project “<name>”" chip and menu item are gone; "Project…" stays. The repo of a project, when a screen needs it later (the project's PR page), is read from the folder's `origin` then, not stored. Archive (decision 0008) is not built yet. (The owner: "Maybe we are overthinking it?") |
| V0.56 | **No live view line in Conversation.** *Amends V0.37.* The headline bar above the conversation that opened the live view is gone (owner: "Remove the live view line in conversation... We have the tab for it"). The View tab is the way in; its headline still shows in the inbox row. |
| V0.57 | **The View-tab chat can be dismissed.** *Amends V0.49.* A round × beside the chat hides it (owner: "I want the bubble conversation in view to be dismissable"). It stays hidden for that message, per thread and device (local storage), and shows again when a new message is typed on the View tab. 44 px target on touch screens. |
| V0.58 | → 3.15. **Projects in Settings, and Delete project.** *Amends V0.50 and V0.55.* (owner: "a dedicated settings page for projects"; before this the project screen opened only from a filed thread's picker.) (1) **Projects in Settings.** Settings (the desktop dialog and the phone screen) has a Projects section after Providers. It lists every project by name, each row with its icon, its folder (the short path, or "No folder: filed by hand") and its thread count. A row opens the project screen (V0.55 (6)). On the desktop, closing the project screen opened from Settings returns to Settings; on the phone, Back does. "New project" opens the create step (V0.30) in the section, without a thread; after it the new project is in the list. No projects: "No projects yet." (2) **Delete project.** The project screen ends with "Delete project…". It asks once: "Delete <name>? Its N threads stay and land in a project whose folder covers them, or in No project. The folder on disk is not touched." `project.delete {id}` removes the project (an unknown id is 404). Every thread in it loses the project and the filed-by-hand mark, then matching runs (V0.55 (2)), so a thread may land in a project whose folder contains its folder. The stream sends `event: project` with `{id, deleted: true}`; a front removes the project from its list and passes the event on; the client removes the project. The device that deleted it closes the project screen (back to Settings when it came from there); on another device an open desktop project screen closes, and the phone's says "This project is not here anymore." There is no undo. Archive (decision 0008) is still not built. |
| V0.59 | → 3.13, 3.14, 5.12. **Changes are records (decision 0018).** *Amends V0.8, V0.12, V0.25, V0.36.* (1) **The record.** A thread holds `changes`: `{id, key, kind, title, targets[], branch?, revisions[], phase, label?, conditions[], evidence[], group?, after[], needs?, phaseBy, createdAt, updatedAt}`. Kinds: code, infra, host, data, doc, design, other. Phases: `working`, `review`, `landed`, `live`, ends `abandoned`, `reverted`. A condition is `{type, status: ok\|fail\|pending, reason?, source: agent\|observed\|owner, at}`; evidence is `{kind, url?, attachment?, title?}`; a revision is `{id, summary?, url?, by, at}`, the 20 newest kept. At most 50 changes per thread; an ended change older than 30 days is dropped first. (2) **Facts beat claims.** A phase or condition with source `observed` is not changed by the agent; the tool says so. The agent never sets a condition of type `review` or `approval` to ok. (3) **Observed: git.** After each run the host reads the thread folder's branch, base, commits ahead of the base, unpushed commits (ahead of the upstream, or all when there is none) and changed files. When any is non-zero on a branch, the thread has a `code` change for that branch (`working`) with the observed conditions `pushed` (ok when nothing is unpushed) and `clean` (ok when no file is changed). *#35:* that change is a host record, one per repo and branch on the host, read also from each change's `workspace` (decision 0019); every thread in its folder shows it, the Changes board once, and a code change of the thread on the same branch follows it (decision 0018, addendum). It lands when the branch is merged into its base and is abandoned when the branch is gone without a merge. (4) **Observed: GitHub.** Every PR of the thread is a `code` change with the PR as evidence, found by its URL, else by its head branch. Open is `review`, merged `landed`, closed `abandoned`; conditions `checks`, `review` and `conflict` are observed. The poller reads the PR of every code change not ended, not only `pr`. `thread.pr` stays the main PR (the PR tab, the merge gate). (5) **The agent's tools.** `change_open {key, kind, title, targets?, branch?, group?, after?, revision?, pr?}` returns the change with that key or makes it (idempotent). `change_revise {change, revision}` adds a revision (the same id twice is one) and sets the observed and reported conditions to pending. `change_status {change, phase?, label?, conditions?, needs?}`. `change_attach {change, evidence[]}`. `change_request {change, ask: review\|approve\|merge\|apply\|deploy\|revert, args?, reason?}` opens a Decision; `merge` with a PR runs `pr.merge` and `deploy` with `{dir}` runs `fyr.deploy`, every other ask reports the owner's answer back to the agent. `change_list {scope?: thread\|project, id?}` returns changes with their facts. `change` is an id or a key. `pr_link` opens a code change with the PR; `effect_request` stays. Each call adds a `change` event that the conversation shows as one line. (6) **Attention.** After the PR's own reasons, a change not ended blocks the thread when a condition fails ("<title>: <reason>") or it needs the owner ("<title>: needs you"). (7) **Settle.** `settle_when {condition: "change", change, phase}` settles the thread when the change reaches that phase (`landed` also counts as reached by `live`). (8) **The Changes tab** lists the changes above the branch diff, newest first: kind, title, phase rail (working · review · landed · live, or the end), "reported" when the agent set the phase, label, the failing and pending conditions, targets, evidence links and the newest revision's summary. The inbox chip shows the most urgent change that is not a PR when the thread has no open PR. |
| V0.60 | → 5.13. **Actions: one model for changes, the thread and the live view, scripted and agent-driven (decision 0018).** *Amends V0.37 and V0.59.* (owner: "Changes should include action. Scripted and llm-driven." and "Dry/align the actions system with views and potentially other things.") (1) **One model.** An action (an *offer*) is `{id, on?, label, run: fyr\|script\|agent, effect?, argv?, cwd?, timeoutSec?, prompt?, in?: this\|new, when?, revision?, onOk?, primary?, by: agent\|observed, digest}`. Its holder is a change, the thread (`on: "thread"`) or the thread's live view (`on: "view"`); each holds one `{offers, runs}` (#31): `changes[].offers` and `changes[].runs`, the thread's `offers` and `runs`, the view's `live.offers` and `live.runs`. At most 12 per holder, the 10 newest runs kept: `{action, label, by, at, running, ok, exit, output (the last 4 KB), endedAt}`. `when`, `revision` (`bind`) and `onOk` exist only on a change; elsewhere they are 400. (2) **Three ways to run.** `fyr`: a built-in Fyr effect (`pr.merge`, `pr.rerunFailed`), only from Fyr. `script`: Fyr runs `argv` exactly, without a shell, in `cwd` (default the thread's folder), with a time limit (default 600 s, at most 3600 s). `agent`: the press sends `prompt` to the thread's agent (`in: this`) or starts a new thread with it in the same folder and project (`in: new`). (3) **Only the owner runs an action.** No agent tool runs one. The press is the command `action.run {threadId, on, action, digest}` (`on`: a change's id or key, `thread`, `view`); `digest` is the fingerprint of every field the owner saw, and a different one is 409 ("The action changed since you saw it"). A change's action is available in the phases of `when` (default every phase not ended) and, when bound, only while its revision is the newest, like a saved Terraform plan; no action while it runs. The client shows a script's `argv` and folder, and a `fyr` action's effect, in a confirmation before it sends the press; an agent action is sent at once. (4) **Results.** On a change, a script that exits 0 sets the condition `action:<id>` ok and moves the change to `onOk`; one that fails sets it to fail ("<label> failed (exit N)"), which blocks the thread (V0.59 (6)). Elsewhere a failed script moves `activityAt`. Every press and result is an `action` event, shown as one conversation line with the output folded; the result of a `fyr` or `script` action is also told to the thread's agent as a `[Fyr]` notice of the agent feed (decision 0020, #47): one sentence with the output's first line, the rest in `fyr_state`, whoever started it; on an archived or settled thread it waits until the thread is revived. (5) **The agent declares** with `offer_action {on, id, label, run, argv?, cwd?, timeoutSec?, bind?, onOk?, prompt?, in?, when?, primary?, remove?}`; ids starting with `fyr.` are Fyr's. A view action needs the live view on, and the view's actions are removed when the owner turns it off. (6) **The live view runs its actions.** The view bridge adds `fyr.run(id)` and `data-action="id"` next to `fyr.send` and `data-send`; they post `ui/action {action}`, and the client presses the view's declared action with that id (with its digest), so a view page cannot run anything the agent did not declare. `data-send` stays: it sends the owner's message. (7) **Built in from observed state** on a code change with a PR in `review`: Merge (`fyr`, primary when nothing fails), Re-run failed checks (`fyr`) and Fix CI (`agent`, primary) while checks fail, Address review (`agent`) while changes are requested, Rebase (`agent`) on a conflict. They follow each poll. (8) **Where they show.** A change's actions on its card in the Changes tab, primary first, agent actions marked ✦, the newest run of each with its output folded; the thread's actions in the thread header above the tabs (desktop and phone); the view's inside the view page. |
| V0.61 | **New project by name, never an existing one by accident.** *Amends V0.30 (1).* (owner: "Making a new project from the new thread button doesn't really work. I still end up in an existing project.") In a project list with a New project row (the new-thread picker, desktop and phone, and the thread's picker): typed text that is not exactly a project's name (any case, spaces trimmed) turns the first row into New project “<text>”, also when other projects match; Enter then opens New project with that name. Enter picks an existing project only when the text is its exact name. Arrow down and Enter, or a click, still pick any match. Without a New project row (the inbox filter) Enter picks the first match as before. |
| V0.62 | → 5.14. **Changes are kept current, like the live view.** *Amends V0.37, V0.52, V0.59.* (owner: "Changes needs to be kept up to date, just like view.") (1) **Which changes.** The agent's open changes: not ended, and the phase not set by an observer. Changes Fyr observes (git, GitHub) keep themselves current and never count. (2) **Upkeep.** Every change tool call that succeeds (`change_open`, `change_revise`, `change_status`, `change_attach`, `change_request`, `offer_action` on a change) and the new `change_unchanged {reason}` set the thread's `changesCheckedAt`. The changes are stale when the thread has open agent changes and `changesCheckedAt` is before the run's start. (3) **End of a run.** The Stop hook of V0.37 checks the view and the changes together and blocks once per run when either is stale, with one reason that names what to update ("… or call change_unchanged with a reason, then stop"); the same rules apply (not while background tasks run, not in a forced turn, once per run). A forced turn that only called view tools or `change_status`, `change_revise`, `change_attach`, `change_unchanged`, `change_list` keeps the run's own summary. (4) **Mid-turn.** The reminder of V0.52 also goes out for stale changes after 4 minutes without a change tool call, on its own clock. (5) The Stop and PostToolUse hooks are registered for every process with Fyr's tools, not only threads with a live view. |
| V0.63 | **New thread: the chip shows the project being made.** *Amends V0.30 (4), V0.61.* (owner: "När man gör new project i create threads vill jag inte att projektväljaren ska visa det gamla projektet tills man trycker create, utan new.") While the New project step is open in the new-thread dialog (desktop) and screen (phone), the project chip shows the project being made: a + icon, the name as it is typed (empty: "New project") and "New · <folder>" or "New · no folder"; the folder line follows its folder. Start with the step open makes the project first and then starts the thread in it, so the thread never lands in the project picked before; an empty name is "A project needs a name." Cancel in the step, or opening the picker again, shows the project picked before. |
| V0.64 | **The Changes board across threads (decision 0018).** *Amends V0.59.* (owner chose "Changes board across threads" from the options.) (1) **One screen** with every change of every thread that is not archived (settled threads included): a dialog opened from a Changes button in the desktop rail, above Settings, and a screen on the phone from the More menu (URL `?changes`). (2) **Columns by stage:** Needs you (a change not ended with a failed condition or `needs: owner`, whatever its phase), Working, Review, Landed, Live; Ended (abandoned, reverted) only with "Show ended". Newest first in a column. Only stages with changes show as columns; one line above them gives the count of every stage. (3) **Groups and stacks.** Changes with the same `group` stay together under the group's name; inside a group a change comes after the changes it builds on (`after`). (4) **Filters:** a search over the change's title, key, targets and group and the thread's title; a chip per project that has changes, and All projects. (5) **The card** is the Changes tab's (V0.59 (8), V0.60 (8)) with a line naming its thread, which opens the thread; its actions run on that thread. (6) The rail button and the menu item show how many changes need the owner. The board reads the threads the client has; it adds no host call. |
| V0.65 | → 3.4. **Changes: what changed since you looked.** *Amends V0.25, V0.59.* (owner: "Git view: what changed since I looked") (1) **The look.** `GET /api/threads/{id}/changes` adds `head` (the commit, "" without commits) and per file `blob`: the git blob id of its content now ("" when it is deleted or not a file), with uncommitted and untracked files hashed by `git hash-object` without writing them. When the owner leaves the Changes tab (another tab, another thread, or the app goes to the background) after it loaded, the client sends `thread.changesSeen {head, files: {path: blob}}` with the whole-branch list it showed last. It is kept in the owner's View (R19), not on the thread, and is not sent in the thread DTO; Refresh inside the tab keeps the look it opened with. (2) **New since you looked.** With a stored look the reply adds `since: {head, at, mode, commits, files[], rangeDiff?, rangeDiffTruncated?}`. A file is new when its blob differs from the one seen, also when it is new to the branch or no longer changed; uncommitted edits count when their content differs from what was seen. `mode` is `ancestor` when the seen head is an ancestor of `HEAD` (`commits` counts the commits since), `rebased` when it is not (`rangeDiff` is `git range-diff <base> <seenHead> HEAD`, without a base `<seenHead>...HEAD`, cut at 400 KB), and `missing` when the seen commit no longer exists. The new files carry status and +/− from the seen head to the working tree (`missing`: their whole-branch values), sorted by path. `?path=<p>&since=<head>` returns that file's diff from the seen head to the working tree, an untracked file as added; `since` is a hex commit id, else 400. The tab shows "New since you looked · N" and "Whole branch" as two choices, the first chosen when N > 0; a `rebased` look also lists the commits as range-diff shows them (kept, changed, added, dropped), each changed one with its interdiff folded. Without a look, or with nothing new, the tab shows the whole branch only. (3) **Viewed.** Each file row has a Viewed checkbox (44 px on the phone). `thread.fileViewed {path, blob, viewed}` stores the mark with the file's blob; a file counts as viewed only while its blob equals the mark, so the mark resets by itself when the content changes. The reply's files carry `viewed`. The tab says "N of M viewed" for the list shown. A look drops the marks of files no longer in the branch; a look and the marks hold at most 1,000 files each (more is 400). (4) **What this version changes.** When the thread has a `code` change (V0.59) whose branch is the diff's branch, the 3 newest revisions with a summary show above the file list, newest first, with a short id and their age. |
| V0.66 | → 3.13, 7.6. **Merged to live: GitHub Deployments, and CI on the diff lines.** *Amends V0.59 (4) and (8), V0.60 (7); builds the two items decision 0018 left out.* (1) **The merge commit.** The poller's `gh pr view` also reads `headRefOid`, `mergeCommit` and `mergedAt`. When it sees a change's PR merged and the change is not `live`, the change keeps `deployWatch {repo, sha, mergedAt}`. (2) **Deployments.** On the same 2-minute poll, Fyr reads GitHub Deployments of each watched merge commit: the first time the repo's newest deployments (`gh api repos/{o}/{r}/deployments?per_page=30`), then that commit's (`…/deployments?sha=<sha>`), and the newest status of the newest deployment per environment (`…/deployments/{id}/statuses?per_page=1`). A repo with no deployment at all ends the watch on that first call, and nothing changes: one call per merged PR. An environment is production when the deployment says `production_environment`, or its name is production or prod. (3) **Production.** `success` (or `inactive`, a later deploy replaced it) moves the change to `live` (observed), adds the environment URL (else the log URL) as evidence `deployment`, and sets the observed condition `deploy` ok. `failure` or `error` sets `deploy` fail "deploy to production failed", adds the log as evidence `deploy-log`, moves `activityAt`, and so blocks the thread (V0.59 (6)). Anything else (queued, pending, in progress, no status yet) is `deploy` pending "deploying to production". (4) **Other environments** are conditions `deploy:<environment>` with the same ok, fail and pending; they never move the phase. (5) **The watch ends** when the change is live, when production failed, when the change ends, 24 h after the merge when the commit has no deployment, and 7 days after the merge otherwise. (6) **Built-in action.** A landed change whose `deploy` failed offers "Investigate the failed deploy" (`agent`, `in: this`, primary), with the environment and the log in its prompt (V0.60 (7)). (7) **CI on the diff lines.** While a code change's PR is open and its checks fail, the poller reads the head commit's check runs (`gh api repos/{o}/{r}/commits/<sha>/check-runs`) and the annotations of each failed run (`…/check-runs/{id}/annotations`), at most 10 runs and 50 annotations, once per head commit and number of failed checks. The change carries them as `annotations [{path, line, endLine?, level: failure\|warning\|notice, message, title?, check}]`; they are cleared when the checks stop failing or the PR is not open. (8) **In the Changes tab.** A file row shows the number of annotations on that file. The open file's diff shows each annotation under the new-side line it names ("<check>: <message>", with the title first when there is one; failures before warnings and notices); an annotation whose line is not in the diff shows above the first hunk with "line N". |
| V0.67 | **One snooze list.** *Amends 13.1 `s`.* (owner chose the phone's list for both, 2026-11-15.) Desktop and phone offer the same snooze times, from one function, in this order: In 1 hour, In 3 hours, This evening (18:00, only before 17:00), Tomorrow morning (06:00), Monday morning (06:00; a week ahead on a Monday). Each shows its time ("15:41", "today 18:00", "Sun 06:00"). On the desktop, keys `1`–`5` pick them in the order shown, so without This evening `4` is the last. |
| V0.68 | → 5.13. **Confirmations are Fyr's own.** *Amends V0.58 (2) and V0.60 (3).* (owner, with a phone screenshot of a script action: "I want these dialogs to be nicer.") (1) **One confirmation.** Every confirmation in the client is Fyr's own: a bottom sheet on the phone, a dialog on the desktop. The client never calls the browser's `confirm()`, `alert()` or `prompt()`. It asks before a script or `fyr` action (V0.60 (3)), Delete project (V0.58 (2)) and Remove provider. (2) **Title.** The action's label ("Set the Acme token", "Merge"), or the question for a destructive action ("Delete Garden?"). (3) **A script** shows its command in monospace. An absolute path in it is shortened: the home folder as `~`, a path inside a scratch folder as `…/<scratch folder>/<rest>`; every other argument is shown as written, quoted as a shell would need it. Under it, one muted line names the folder ("in ~/…/scratch/<name>"; a folder outside a scratch folder with `~` only). A tap on the command (a hover on the desktop) shows the full command and folder; Copy puts the full command on the clipboard. A bound action adds "Only on revision <id>". (4) **A `fyr` action** says in words what happens, as the plain decision cards do (V0.27): "Fyr merges the pull request on GitHub. The agent gets the result." (5) **A destructive action** (Delete project, Remove provider) states its consequence (V0.58 (2)'s words) and its primary button is red. (6) **Buttons.** The primary button names the action ("Run", "Merge", "Delete project"), never "OK"; Cancel is beside it. Focus starts on the primary button. Cancel and Escape close it, and on the phone so does a tap outside; Enter confirms on the desktop. While it is open, the inbox keys do nothing. (7) **Fits.** The sheet fits the phone sizes of V0.17 above the bottom inset, its buttons at least 44 px tall; a long command wraps inside it. (8) **The live view asks too.** An action the view's page runs (`fyr.run(id)`, `data-action`) goes through the same confirmation as its button; an agent action is sent at once. A page therefore cannot run a script or a Fyr effect without the owner's press. |
| V0.69 | → 5.2. **The agent names its threads.** *Amends V0.5 (rename), V0.22.* (owner: "Add tools to rename threads".) (1) The Fyr tool `thread_rename {title, id?}` renames this thread, or a thread this thread started (`id`, its `parent` is this thread); any other thread is refused ("you can rename this thread or a thread it started"). (2) A title is one line: whitespace and line breaks fold to single spaces, it is trimmed, it is not empty, and it has at most 80 characters. (3) The thread keeps `titleBy` (owner or agent). A name the owner gave (`thread.rename`) is kept: the agent's rename is refused with "the owner named this thread; ask them to rename it" (a row of the field rights, decision 0020). (4) A rename is the event `thread.renamed {title, by}`; the same title again changes nothing. |
| V0.70 | → 3.13. **A landed change is history; one change per branch.** *Amends V0.59 (1) and (6), V0.43 (2).* (owner: "Why do threads often end up in errors here?", 15 Nov: a thread stayed Blocked after its PR merged, because the agent had reported `claude-review` failed on its own copy of the change.) (1) **Landed.** A change keeps `landedAt`, set when it first reaches landed and cleared when it goes back to working or review. A failed condition blocks a landed or live change only when it was recorded at or after `landedAt`: in the same call as the landing (a health check after an apply) or later (a deploy that failed). A failure recorded before the landing is shown as history and blocks nothing. `needs: owner` still blocks. A stored landed change without `landedAt` takes its last update. (2) **One change per branch.** `change_open` for a `code` change with a branch, whose key is new, joins the thread's code change on that branch that is in flight (working or review) in the same repo (the same `repo:` target; when either has none, the branch decides). The new key becomes an alias of that change: every tool finds it by either key, and nothing is made twice. (3) **The PR tab after a merge.** On a merged or closed PR, failed checks show muted, under "Checks at merge", with no "Ask the agent to fix this check". |
| V0.71 | → 3.16, 5.13. **A thread another thread starts follows it; a press counts once.** *Amends V0.22, V0.60 (2) and (3).* (owner: "When a acme thread asked for a new thread, the provider wasn't specified and I think it chose wrong. It should probably follow parent unless specified." The live case, 15 Nov: two presses of "Pick up #…" on a claudeAcme thread's live view started four threads, all on `claude` with Sonnet.) (1) A thread that `thread_new` or an agent action with `in: "new"` starts takes the starting thread's provider, model and effort unless the call names them, and has it as `parent`. (2) Every press runs until its result lands, agent actions included: an agent action to this thread ends when the message is sent, one with `in: "new"` when the thread exists ("Started thread <id>." is its result, and the pressing thread's agent hears it); a second press while it runs is "It is running". (3) A press of the same action within 2 s of the last one is the same press and is refused ("It was just pressed"), whatever sent it (a double tap, a view page that calls `fyr.run` twice). |
| V0.72 | → 3.13, 5.2, 5.3. **A PR that does not exist ends its change; the owner ends any change.** *Amends V0.59 (4), V0.60 (7), V0.66, V0.70.* (owner: "both fixes". The live case, 15 Nov: a test run printed the fixture URL `https://github.com/acme/app/pull/88`, Fyr linked it to a thread, and the change "acme/app#88" stayed in `review` with a Merge button; Merge failed with "GraphQL: Could not resolve to a Repository with the name 'acme/app'", the poller dropped that error every 2 minutes, and the owner had no way to end an observed change.) (1) **Does not exist.** When `gh pr view` answers that the PR's repository or the PR does not exist (GraphQL "Could not resolve to a Repository" or "Could not resolve to a PullRequest", or HTTP 404), the poller reports the PR with state `MISSING` as an observed fact (`pr.observed`). The change moves to `abandoned` (observed) with the label "the PR does not exist on GitHub"; it is no longer in flight, so nothing polls its URL, the built-in actions go (Merge among them) and the PR effects' guard says "The PR is not open". The agent hears "PR #88: does not exist on GitHub." No change is made for a missing PR that no change has. (2) **Two answers.** The poller reports it at the second such answer that comes at least 1 minute after the first, so a poll later: the text is unambiguous for this token, but a PR GitHub has only just made may not resolve on a kick right after it opened, and one poll costs 2 minutes. An answer in between starts the count again. A transient error (network, rate limit, 5xx, a failed login, a time-out) changes nothing. A repository the token can no longer read also answers "Could not resolve"; the owner sees the label and the agent can open a new change. (3) **End this change.** The owner command `change.end {change, phase?, reason?}` ends any change of the thread that has not ended, an observed one too (the owner decides): `phase` is `abandoned` (the default) or `reverted`; the reason (one line, at most 200 characters) becomes the label. The change's actions go, its PR is no longer polled, and the agent hears "The owner ended PR #88 as abandoned: <reason>." The agent cannot send it, and the field rights keep the owner's end against the agent and against later facts (a link or a digest of the same PR does not reopen it). More work is a new change with another key (V0.70 rules). (4) **Where.** A small text button "End this change" on every change card that has not ended, in the Changes tab and on the Changes board (not on a host record), 44 px on the phone. It asks in Fyr's own confirmation (V0.68): title "End this change?", the text "Fyr stops watching its PR and removes its actions. The agent is told; more work on it becomes a new change." ("watching it" without a PR), a choice Abandoned or Reverted, an optional reason, and the button "End change". The PR chip of a missing PR says "missing" ("Does not exist on GitHub"), and Dismiss PR is offered as for a closed one. |
| V0.73 | → 5.14. **A live change needs no upkeep.** *Amends V0.62 (1).* (owner chose "Live needs no upkeep", 15 Nov, after ending four finished deploy cards by hand to stop the end-of-run reminder.) The upkeep rules (the Stop reason and the mid-run reminder) count the agent's changes that are not ended and not live. A live change counts again only while something about it fails after its landing (V0.70 (1): a deploy or a health check), because then the agent has work on it. A live change stays on the board as done. |
| V0.74 | → 3.15. **A quiet project stays out of New thread; a 30 s hold shows it for 30 minutes.** *Amends V0.46 (4).* (owner: "For snoozed projects, hide in create BUT also have a temporary 'enable for 30 minutes' that unhides. This toggle should be annoying. Hold a touch area for 30 seconds.", 15 Nov.) (1) **Hidden.** While a project is quiet (V0.46 (2): it carries `quietUntil`), the project list of New thread (desktop dialog and phone screen) leaves it out, its search does not find it, and the form never starts on it (the project filter or the remembered project picks it only when it is not quiet). The project the form already has picked stays in the list when its window starts. (2) **The hold.** Below the list, while any project is quiet, one row says how many quiet projects are hidden and "Hold here for 30 s to show them for 30 minutes". The owner keeps a finger, or the mouse button, on the row for 30 s; a fill and a countdown show the progress. Lifting the finger, moving off the row or leaving the list before 30 s resets it to zero. There is no key and no tap for it, on purpose. (3) **Shown.** After the hold, quiet projects are in the list for 30 minutes on that device (browser storage), and the row says "Quiet projects show until 21:14" with "Hide" to end it at once. When the 30 minutes end, the list hides them again without a reload. The inbox, the counts and the notifications keep V0.46; only New thread changes. |
| V0.75 | → 5.13, 13.5. **A used action says so where it was pressed; the filmstrip scrolls with the conversation.** *Amends V0.60 (4) and 13.5.* (1) **The result on the button.** An action carries its newest run: `fresh` (never run), `running…`, `✓ <label> · HH:MM` after a run that succeeded, or `✕ <label> · failed (exit N)`. A used action loses its primary weight and turns muted, and it stays pressable, because a second run is legitimate; its tooltip says what it does and how the last run ended. A failure keeps its weight and takes the signal colour, and only a failure keeps a line of its own under the bar. The last run's output is one control away, not a row. (2) **The filmstrip is inside the conversation.** The thread's previews scroll with its messages instead of holding a band under the tab row, so a thread without previews has no band at all; on the phone the strip is the same one line it was (13.5). (3) **The header's gaps.** The offers block takes 12 px from the chips row and 6 px between a bar and its lines, and no block adds a fixed gap under the tab row (which is what left an empty 66 px band). |
| V0.76 | → 3.13, decision 0019. **A branch behind its base is not merged into it; the facts say where the base is.** *Amends V0.59 (3).* (owner, 15 Nov: "changes show nothing now", issue 51.) (1) **The guard.** A host record's branch ends as merged only when it is ahead of its base and its head is an ancestor of it. A branch that is behind or level with its base has nothing of its own to merge, and makes no record: a checkout on `main` that is behind `origin/main` is not a merge, and neither is `main` at the head. `Git.End` reads `rev-list --count <base>..<branch>` and answers `merged: false` when it is 0, in front of the ancestor test. (2) **The base's head.** `GitFacts` carries `BaseHead`, where the base points (`origin/main`). A record that has nothing ahead keeps its place in flight and takes the next revision when its branch catches up, so the record of a branch that keeps working shows each landed commit instead of one revision and a landing at the first look. |
| V0.77 | → 5.2, the provider screen. **A provider card says each thing once, and its badge tells the truth.** (owner, 15 Nov, with the `Claude (acme)` card: "Rate limited by the provider; trying again later · Rate limited by the provider; trying again later · Next try 21:31", with the badge reading Ready; "Issue and fix: Handle this better".) (1) **One statement.** The card draws the error once: in the line when the account has no data, in the detail when it has, and never in both. The next try is one line under it. (2) **The badge.** Ready means nothing is wrong. A card whose quota failed says Rate limited or Login expired, one whose data is older than its last good read says Stale, one never read says Not read yet. The status of the account (V0.50) still decides whether it needs a key or a login, and wins when it is not ready. (3) **The reason, in words the owner acts on.** A rate limit says when the next try is; an expired login says to paste a new token or run the profile, never "run this profile once" to a token account (issue 65). |
| V0.78 | → V0.64, the Changes board. **Settled work leaves the board.** (owner, 15 Nov, with the board full of landed and live cards from finished threads: "File issue: hide settled", issue 54.) (1) The board leaves out the changes of a thread that is settled or snoozed (archived threads were already out). (2) **Except what needs the owner:** a change of such a thread that is in Needs you (a failed condition, or it waits for the owner) stays, because V0.59 (6) blocks the thread on it; the badge counts it. (3) A host record shown by an open thread stays, under that thread. (4) A **Show settled** tick beside Show ended brings them all back; it is off by default and remembered per device. The conversation's own change list (V0.59 (8)) is unaffected. |
| V0.79 | → 5.2, 13. **Start a skill or a slash command from the composer.** (owner, 16 Nov: "manual activation of agent skills (with autocomplete)", issue 70.) (1) **The list.** The CLI answers `initialize` with `commands` (name, description, argument hint): its built-in commands, the custom commands and the skills of the folder and its plugins. The host keeps the list of each session's answer for its thread, and the newest list per provider and folder (stored, so it survives a restart). Commands that need the interactive terminal are left out: `clear`, `login`, `logout`, `config`, `exit`, `quit`, `help`, `resume`, `theme`, `vim`, `terminal-setup`, `ide`, `doctor`, `status`, `statusline`, `permissions`, `model`, `mcp`, `agents`, `hooks`, `memory`, `export`, `bug`, `feedback`, `upgrade`, `release-notes`, `privacy-settings`, `install-github-app`, `migrate-installer`, `rewind`, `plugin`, `plugins`, `login-method`, `add-dir`, `output-style`, `fast`, `usage`. (2) **Served.** `GET /api/threads/{id}/commands` answers `{commands, from}`: the thread's own list (`from: "thread"`), else the newest list seen on the host for the thread's provider and folder (`"folder"`), else none (`""`). `GET /api/commands?cwd=&profile=` answers the newest list for a provider and folder, for New thread. Neither is in the thread DTO. (3) **Autocomplete.** Typing `/` at the start of the composer (desktop, phone and New thread) opens a list above it of the commands whose name starts with the typed text, then those that contain it, at most 8, each with its description. Arrow keys move, Tab or Enter picks, Escape closes; a tap picks. A typed space or a text that no command matches closes it. (4) **Picked.** A pick fills `/name ` and, while nothing follows the name, the composer shows the command and its argument hint in a muted line above the field (`/review <pr-number>`), since a field with text shows no placeholder. A send sends the text unchanged, so the CLI runs the command or the skill. |
| V0.80 | → 8, 12.6, 13. **Each thread in its own cgroup under one budget for agent work; the client shows the load.** (owner, 16 Nov: "I think the most important next thing to fix is the computer resource management", issue 74; load 81 on 8 cores with every agent, test and container in the daemon's cgroup.) (1) **Split.** The unit delegates `cpu memory` (`Delegate=`). Before it starts anything, fyrd moves every process of the unit's cgroup into `server/` (cpu.weight 1000, memory.low from the module), then enables `+cpu +memory` for the unit's children. (2) **Per thread.** Every agent CLI starts directly in `agents/<thread id>/` (clone3 `CLONE_INTO_CGROUP`, no sweep), so everything it starts is there too; the deploy's build and test run in `agents/self-deploy/`. A thread's cgroup is removed when its process ends and it is empty. (3) **Budget.** `agents/` carries cpu.weight 100, cpu.max (4 cores), memory.high, memory.max and memory.swap.max from module options, with T3's defaults. `agents-lock-cap` (the machine's QoS) may rewrite `agents/cpu.max` at `/sys/fs/cgroup/system.slice/fyr.service/agents/cpu.max`. (4) **Off.** Without the module's cgroup settings, or without a writable delegated cgroup v2 with `cpu` and `memory` (dev runs, tests, other systems), fyrd runs every process as before and says why once in the log. (5) **Load.** `GET /api/load` answers each host's load average, cores, CPU pressure (`/proc/pressure/cpu` some avg10) and, with cgroups, the agents' total and each thread cgroup's CPU (cores, from the `cpu.stat` usage_usec between two reads) and `memory.current`; the front merges its hosts. The host reads them only when asked, at most every 2 s. The client asks every 5 s while the page is visible. (6) **Shown.** The desktop rail shows the load average above the quota, coloured when the 1-minute load passes the cores or CPU pressure passes 40 %; the tooltip has the three averages, the pressure and the agents' share. A working thread's row (desktop and phone) adds its CPU ("1.6 cores"), coloured from 2 cores. |
| V0.81 | → V0.13, V0.39. **A deploy that changes only the web client switches without a restart.** (owner, 16 Nov: "Wouldn't it be possible to switch/update the frontend/backend WITHOUT stopping the claude sessions?", issue 75.) (1) **Same fyrd.** Before it builds, the deployer hashes the sources that go into `fyrd` (SHA-256 over the path and content of every file of the checkout except hidden entries, symlinks, `node_modules`, `bin`, `docs`, `research`, `design`, `spikes`, `testdata`, `*_test.go`, `*.md`, and everything under `web/` but its `*.go` files) and writes the sum to the build's `server.sum`. The running daemon's sum is the `server.sum` beside its own executable; a daemon without one (the seed, a build from before V0.81, `fyr install`) always takes the full path. (2) **Client-only path.** When the sums are equal it still builds and runs `make test` (red stops it as before), copies the built client (`web/dist`) into the build as `web/`, and then, with no snapshot, no drain and no restart: points `previous` at the build `current` named and `current` at the new build (so a later restart runs it, and the launcher's rollback has `previous`), makes the front serve the new client and report the new build's id (`fyrd version`) as `buildId`, and closes every browser stream, so each open page reconnects, sees the new `buildId` in `hello` and reloads (V0.13). Running threads are not touched. The deploy's result reaches the requesting thread at once ("Fyr now runs build <id>; only the web client changed, so nothing restarted"). (3) **Failure.** A switch that fails (the built client cannot be copied or has no `index.html`, a link cannot be written, the front refuses it) puts the links back, keeps serving the old client and reports `failed`; nothing restarts. (4) **Banner.** The phases are `build`, `test`, `client` ("Updating the web client · this page reloads by itself"), then `done` ("Fyr is updated · build <id>") or `failed`; no drain and no restart phase. (5) Any other difference runs the full path of V0.13 unchanged. |
| V0.82 | → V0.59 (4), V0.66, V0.13. **Change records do not go stale after a merge.** *Amends V0.59 (4) and V0.66.* (owner, 16 Nov: "Changes are still not kept in sync... They are super annoying if they linger", issue 83.) (1) **Checks after the merge.** The poller also reads the PR of a `landed` or `live` code change while its snapshot is not final, for at most 24 hours after the merge (`mergedAt`, else when the change landed); then it stops. Not final: the PR's state is not yet merged or closed, or its checks are `pending` or `none` (none read yet). Each read replaces the PR snapshot (state, checks, merge commit); the phase does not move back, and a `live` change stays `live`. A merged PR whose deploy watch (V0.66) a repo without deployments ended does not arm it again on a later read. (2) **A self-deploy makes changes live.** Before it builds, the deployer records the checkout's `HEAD` and its `origin` remote, and stores them with the pending deploy, so the success report of the new daemon (V0.13) has them; the client-only path (V0.81) uses them when it succeeds. On success, every `landed` code change of any thread whose targets name that repo and whose PR has a merge commit that is an ancestor of the built commit (`git merge-base --is-ancestor <merge> <HEAD>` in the checkout) moves to `live` (observed), with the observed condition `deploy` ok and the build id as evidence `build`. A change whose merge commit is not in the build, or that the owner ended, stays as it is. The move goes through the Input path as an observation of the deploy observer (`deploy.self`). |
| V0.83 | → V0.13, V0.39. **A deploy that waits for threads can be forced; the cut turns resume.** (owner, 16 Nov, issue 76, decision 0022: keep the drain and add a way out.) (1) **Button.** While a deploy is in phase `drain` with at least one blocking thread and the host is live, the update banner has **Restart now**, on the desktop and on the phone (44 px target there). Every other phase, and a drain with no blocking thread, shows no button. (2) **Confirmation.** A press opens Fyr's confirmation (V0.68): "Restart Fyr now?", a text that names the threads whose turns end ("“<thread>” ends its turn" or "These N threads end their turns: …") and says each gets the message “continue” after the restart, a red primary **Restart now**, and Cancel, which sends nothing. (3) **Command.** Restart now sends `host.deployNow` (no `threadId`), an owner-only host kind (an agent cannot send it) whose reply waits for the deployer (Await, 5.1): `409` "No update is waiting for running threads" outside a drain. (4) **Switch.** The deployer's drain loop stops at once: it stores the threads running at that moment (the requester's own thread included when it runs) as `cut` in `pending_deploy`, links `next` and exits with code 75 as V0.13 (5) says; the running processes end by the normal stop (SIGINT to the process group). A drain that ends by itself cuts nothing. (5) **Continue.** When the new daemon is up (where it reports the pending deploy, also after a rollback), each cut thread gets the text "continue" as the owner's message through the one send path, so it shows in the conversation and starts a run that resumes the session (`--resume`). Its command id is `deploy-<build>-continue-<thread>`, so a second start sends nothing twice (I7). A thread settled or archived since gets nothing, and one that cannot take a message (an open question, a handover) keeps waiting for the owner. (6) The client-only path (V0.81) has no drain and no button. |
| V0.84 | → V0.59 (6), V0.64, V0.78, 5.2. **Not now: the owner parks a change that needs them until something on it changes.** *Amends V0.59 (6) and V0.64.* (owner, 16 Nov, issue 93, about a coordinator thread in Needs you on a change whose `needs: owner` only the agent could clear: "Why does it say it needs me?", "what about the stuck needs you?".) (1) **Command.** `change.park {change}` is an owner-only command (an agent's call is refused, like `change.end`). It applies to a change of the thread that has not ended and blocks (V0.59 (6): a failed condition, or `needs: owner`), and stores `parked {at, seen}` on the change: the time and a fingerprint of what the owner saw (needs, label, phase, the failed and pending conditions with their reasons, the newest revision id). `change.unpark {change}` is its Undo. (2) **Not blocking.** A parked change whose fingerprint still matches does not block its thread and is not in Needs you: not in the thread's attention (the inbox section), not in the board's Needs you column and not in the needs count; on the board it sits in its phase column. Another change of the thread that blocks still blocks. (3) **Expiry.** Any change to the fingerprinted fields (a new reason or label, a new or changed condition, a new phase, a new revision) makes the change block again by itself; Fyr clears `parked` at that reduce, so the agent and the client see it is back. The same report again is no new fact. (4) **Card.** A change card that blocks shows **Not now** next to End this change (44 px on the phone, inside the card); a parked card shows a quiet line "Not now · hh:mm" with **Undo**. (5) **The agent.** The agent is told when the owner parks or unparks a change, and `fyr_state` and `change_list` say the owner parked it, so it stops asking. A host record (an observed branch shown by several threads) is not the thread's to park. |
| V0.85 | → V0.80, V0.20, V0.54. **The thread the owner has open gets a higher CPU weight.** (issue 78, the part of issue 74 that V0.80 left: the work being watched goes first under contention.) (1) **Forward.** The front keeps presence (V0.20, V0.54). On every beat that names a thread it sends `POST /api/threads/{id}/focus {device}` to that thread's host, the same per-host forwarding as every thread route, in role both too. When a device's beat names another thread or none, the front sends `{device, off: true}` for the thread it showed before. The send is in the background with a 5 s timeout and is skipped for an offline host; the beat's answer does not wait for it. (2) **Weight.** The host keeps, per device, the thread it focuses and when. A thread focused by any device within 45 s (`notify.Watch`, the same window that keeps it watched) gets `agents/<id>/cpu.weight` 300; every other thread cgroup has 100. A thread whose cgroup is made while it is focused starts at 300. (3) **Expiry.** The weight goes back to 100 when the last device's beat for the thread is older than 45 s (the host checks every 5 s and on each focus), or at once on `off`. (4) **Off.** Without cgroups (a nil tree, V0.80 (4)) the route answers 204 and changes nothing. |
| V0.86 | → V0.80, V0.13, 16.2. **Slots for heavy jobs: at most N test suites, builds or e2e runs at once across a host's threads.** (owner, 16 Nov: "Fair CPU share, Slots for heavy jobs, Show the load", issue 79: eight threads that each start a full suite share four cores and all run slowly.) (1) **Pool.** Each host has one pool of N slots, `slots` in config.json, default 2 when absent or negative. `slots: 0` turns slots off: every `slot_acquire` answers at once with "no limit", nothing queues, the deploy's `make test` takes no slot, and the rail shows no slots line. Every kind (`test`, `build`, `e2e`) takes from the same pool. A thread holds at most one slot. The queue is first come, first served; a priority waiter goes ahead of every waiter without priority and behind earlier priority waiters; nobody takes a slot from its holder. (2) **Tools.** `slot_acquire {kind}` answers at once and never blocks a tool call: either the thread now holds the slot ("You hold a test slot. Run the job now, then call slot_release."), or it is queued with its position ("Queued for a test slot: 2 ahead. Do not start the job; Fyr tells you in a [Fyr] message when the slot is yours."). A second call answers the same way and does not queue twice. `slot_release` gives the slot back or leaves the queue. When a queued thread gets the slot, Fyr tells its agent with a notice that wakes it ("A test slot is yours now: run the job, then call slot_release."); the thread records event `slot` with the kind. (3) **Release.** A held slot is released when the thread's run ends, however it ends. A place in the queue survives the run's end, since the agent was told to end its turn while it waits. A held slot of a thread with no live run for 2 minutes after the grant is released (the grant's wake did not start a run), and a thread that no longer exists leaves the pool; the engine's 10 s tick checks. **Restart.** The holders and the queue (not the deploy's priority wait, which ends with its daemon) are stored in the store's KV table under `slots`, written on every change and loaded at start. After a restart a holder without a live run follows the 2-minute rule, and a queued thread keeps its place and gets the grant notice when its turn comes. (4) **Deploy.** The self-deploy's `make test` waits for a slot with priority, holds it while the tests run and releases it after; the wait ends when the deploy is replaced. Its owner in the pool is `self-deploy`. The build does not take a slot. (5) **Prompt.** The prompt Fyr gives every agent says to call `slot_acquire` before a full test suite, a build or an e2e run and `slot_release` after, to run the job only while holding the slot, and that the slot goes back when the run ends. (6) **Shown.** `GET /api/load` adds per host `slots {n, held: [{owner, kind, since}], waiting: [{owner, kind, priority}]}`, with cgroups or without, and leaves it out while slots are off. A thread's row (desktop and phone) shows "waiting for a test slot (2 ahead)" while queued and "test slot" while it holds one. The desktop rail adds "1/2 slots" under the load figure, coloured while anyone waits; the tooltip says how many hold and wait. |
| V0.87 | → V0.59 (3), (4), V0.70 (2), V0.76, V0.64, decision 0018 addendum. **One work item, one card: a change on a branch follows its record, and a PR joins the change of its work.** *Amends V0.59 (3) and (4).* (owner, 15 Nov, issue 69: "auto-changes often get duplicated"; the Changes tab showed `pr:meros/fyr#67` landed and the agent's `fyr:decision-0021` in review with the same PR.) (1) **Follow.** A change of the thread of any kind with a branch, not only `code`, follows the host record of its repo and branch (`observed` is the record's id), and the thread shows it instead of the record. It takes the record's workspace, `pushed` and `clean`, and the end of the branch as before, and shows the record's revisions until the agent writes one of its own; after that it adds the record's newest. (2) **A PR joins the change of its work.** A PR found by its head branch attaches to the thread's change on that branch of any kind that has no PR, the one that follows a record first. When the agent gives a change a PR (`change_open {pr}`, `change_attach` with `pr` evidence) that a change Fyr made for it (key `pr:<repo>#<n>`) already holds, or opens a change that is not `code` on the branch of such a change in flight, Fyr's change folds into the agent's: its key becomes an alias, and its PR snapshot, observed phase and conditions, evidence, revisions, actions and Rules move over; the thread's main change follows. The next PR poll folds two changes that already hold one PR the same way. (3) **The client** shows one card per change id in a thread's Changes tab, the newest, and one card on the board for a host record and the change that follows it. |
| V0.88 | → V0.59 (3), V0.76, V0.87. **A merged branch that stays checked out makes no new record.** *Amends V0.59 (3).* (owner, 16 Nov: "Some threads gets bombarded by notices... Fix that bug". The live case: the Acme checkout stayed on a branch whose PR was squash-merged. After each run the observer saw the branch 1 commit ahead of `origin/HEAD`, found no record in flight and made a new one (`~2`, `~3`, …); the merged PR ended it at once; "Branch …: merged." woke the agent, and its run ended with the next observation: 712 records and 714 notices in 3 hours, a run every 2 s.) (1) **Same head, same work.** When a branch has no record in flight, the observer makes a new one only when no ended record of that repo and branch has the checkout's head as its newest revision. A merged branch that is still checked out, with or without changed files, is the work that ended; a new commit on it is new work and gets a new record. (2) **A merged PR ends only its own head.** A merged PR on the record's branch ends a record as merged only when the PR's head commit is the record's newest revision, or when either is unknown. New commits on a branch after its PR merged are not that PR's merge. |
| V0.89 | → 5.10 (`owner`, Loop guard), V0.72, 13.1 `e`. **The owner's settle never wakes the agent.** *Amends 5.10.* (owner, 16 Nov: "When pressing settle, a thread often goes back to working and answers \"ok i will settle when done!\"".) The cause: Settle on an idle thread with an open PR sets *settle when merged*, which leaves the thread awake, and the `owner` notice "The owner set this thread: settles when its PR merges." is `next`, which wakes an idle agent (owner, 2026-11-15). The agent then starts a turn only to acknowledge it. (1) An `owner:settle` notice whose new state is a settle (settled, settles when its PR merges, settles when its condition holds) is `self`: it never wakes the agent and goes out with the next tool call or delivery, as the loop guard does. (2) Unsettle still wakes: when that notice merges into a queued settle notice, the merged notice is not `self` (Coalescing). |
| V0.90 | → V0.13 (4), V0.6, V0.83, 16.2. **The agent processes outlive fyrd: a deploy or a crash does not stop a turn.** *Amends V0.13 (4), V0.6 and V0.83.* (owner, 16 Nov, decision 0023: "Build the Claude in long living stable process, to enable full reload and crashes without stopping progress"; "no duplicate process, always find the old running process, timeout if abandoned for an hour"; "All smarts is in the daemon".) (1) **Runner.** The launcher runs a runner on a Unix socket (mode 0600, `<data>/runner.sock`, or a short path in the runtime folder when that one is too long) and passes it to fyrd as `FYR_RUNNER`. fyrd starts every agent process through it with the thread id as the key. The runner keeps at most one live process per key: a second spawn for a key fails with the id of the one that runs, and fyrd links to that one. (2) **No smarts.** The runner numbers each stdout line and stores it until fyrd acks it; it writes what fyrd sends, closes stdin, kills, lists and forgets on request, and never reads a line's content. An ack names the last line fyrd is done with and the lines up to it that the runner keeps; fyrd keeps every request of the CLI that it has not answered. (3) **Adoption.** At start fyrd lists the runner's processes (retried for 10 s) before the restart repair. A thread with a live process keeps its run, its unread messages and its questions (V0.6 does not apply to it; a handover in flight still fails). fyrd adopts one process per thread (a live one before an exited one, then the newest) and ends the others, before it resumes effects or serves. The adopted process gets the stored lines first, then the new ones; its MCP token, effort and flags come from the note fyrd gave at the spawn. (4) **Questions.** A Decision's id derives from the process and the CLI's request id, so a request that comes again finds its Decision and opens no second one. An answer given before the request comes again waits for it. A question the CLI withdrew while no fyrd listened is withdrawn when the turn ends. (5) **Abandoned.** A process with no fyrd linked for 1 hour gets stdin closed and is killed 10 s later; an exited process that no fyrd collected within 1 hour is dropped. A linked process is never ended by the runner. (6) **Deploy.** With a runner, a deploy does not drain: it switches with threads running, cuts no turn, shows no drain phase and no Restart now. Without one (no launcher, or a runner with another protocol version, which fyrd logs) the agents are fyrd's children and V0.13 (4) and V0.83 apply. (7) **Shutdown.** fyrd's shutdown lets go of each process and ends none; the launcher's shutdown ends them all (stdin closes, a kill after 5 s). |
| V0.91 | → V0.4, #56. **An inbox row puts the project first and the chips under the title.** (owner, 16 Nov: "Always show project icon/name first here, make sure all chips etc are placed under or something... We need to fit everything... Relayout!") On the desktop list, a row in every section has the same order. (1) **First line:** the project tag (icon and name, at most 45 % of the line) and then the title, cut with an ellipsis; the age at the end. Nothing else shares this line. (2) **Second line:** Needs you shows the state word, "from <thread>" and the row's details; the other sections show "from <thread>", what the agent does (working) or the snooze end (snoozed), then the CPU and the slot. A row with nothing for the second line has none. (3) **Chips:** the PR or change chip and the subagent count sit under the title, never beside it: on a third line in Needs you, at the end of the second line elsewhere (at its start when the line has no text). "0 sub" is not shown. |
| V0.92 | → V0.91. **The desktop rail shows the Fyr mark in place of the name.** (owner, 16 Nov: "replace with a logo", about the "Fyr" text at the top of the rail.) The rail's top shows the app icon (`/icons/icon-192.png`, the lighthouse mark of `web/scripts/icons.ts`) at 32 × 32 px, centred, with the accessible name "Fyr". The phone header is unchanged. |
| V0.93 | → V0.92. **The sidebar logo is a rendered isometric lighthouse.** *Amends V0.92.* (owner, 16 Nov: "replaced by a rendered isometric lighthouse, properly sized/transparent etc. True 3d rendering"; "This is good! Use it", version 4 of four.) The rail's top shows `/logo.png`, a 128 px PNG with a transparent background rendered with Blender Cycles through a true isometric orthographic camera: a white tower with two slate bands on a rock base, a dark gallery and dome, an amber lamp, and two amber beams that fade outward. It shows at 32 × 32 px, with the accessible name "Fyr", and is precached by the service worker. The scene script and the 512 px master are in `design/logo/`. The app icon and the phone header are unchanged. |
| V0.95 | → 0020 (feed), V0.88, V0.89, decision 0024. **An observation never starts a loop: `next` does not wake, a fact is said once per head, observers wake a thread at most 5 times in 10 minutes, and a branch with nothing ahead makes no record.** *Amends 0020 (the wake rule), V0.59 (3), V0.88 (1) and V0.89 (2).* (owner, 16 Nov, decision 0024 sections 8 and 11, slice 1: one thread got 713 records, 714 runs and 714 notices in 30 minutes. A squash-merged branch stayed checked out with changed files and nothing ahead of its base; each run made a record `~N` with no revision, so V0.88 (1) never matched it; the merged PR ended it at once, and the `next` notice "Branch …: merged." woke the agent.) (1) **Wake.** Only a `wake` notice starts a turn of an idle agent; a `next` notice goes out with the next turn (the pending ones go before the words of the message that starts it, and are taken then, so a tool call does not repeat them) or at the next tool call. Wake: a failure appears on a change (CI failed, conflicting, changes requested, a deploy failed), the result of an ask, an effect or an action, the agent's watch that fired, a slot granted. Next: the owner's corrections and settings (unsettle too, which amends V0.89 (2)), merged, live, closed, gone, ended, CI passed, approved. Silent: Fyr made or joined a card. (2) **Said once per head.** A fact notice about a change is keyed (change, field, head); the head is the PR's head commit, else the change's newest revision, else the head the observer stored on the record. The thread's `Feed.Said` keeps the last state said per key, at most 200 keys, and a run does not clear it. The same state on the same head again is dropped; a new head is a new notice. (3) **Wake budget.** Notices from observers and Rules (facts, records, watches) wake a thread at most 5 times per 10 minutes. A further wake stays queued and is taken at the next tool call or turn; when the oldest of the 5 leaves the window, that time is in the thread's NextCheck, and the clock delivers the queued wake then. A message the owner sends resets the budget. The owner's and the agent's own Inputs, results and slots do not count. (4) **Records.** The git observer makes a record only for a branch with commits ahead of its base that is not the repo's default branch (`origin/HEAD`, else the base's branch); changed files or unpushed commits alone make none (amends V0.59 (3)). The record's workspace stores the head the observer read, so V0.88 (1) finds an ended record by that head even without a revision, and V0.88 (2) compares a merged PR's head with it. A branch whose last record ended at an unknown head makes no new record until it has a head. (5) **Migration.** At load, Fyr deletes the records with a key `git:<repo>#<branch>~N` that are not in flight and that no thread change follows, with the threads' references to them, and removes the change of `https://github.com/acme/app/pull/88` (a test fixture that leaked into live threads) from every thread. Both are idempotent. |
| V0.96 | → V0.12 (tools), 13.5, decision 0024. **`change_list` lists open changes, one line each, in at most 4,000 characters; handover and compaction carry those lines.** *Amends V0.12 (tools) and V0.59 (5).* (decision 0024 section 7 and slice 2: on 15 and 16 Nov `change_list` returned 83,000 to 100,000 characters three times and `fyr_state` 115,000, each failed call cost about 25,000 tokens, and after a handover or a compaction agents lost their change ids.) (1) **Open.** A change is open when it has not ended (abandoned or reverted) and is not at landed or live. A landed or live change with a failed condition counts as open. (2) **change_list `{scope?, id?, done?}`.** By default it lists the open changes, one line each: the full id, the PR as `owner/repo#N` (else the key), the title in quotes, the phase, and the failed or pending conditions in a few words (`checks failed`, `review pending`), with "needs the owner" and "parked" when they hold. A first line says how many changes are not shown because they are done. `done: true` lists every change. `scope: project` uses the same lines, each with the other thread's id. The whole reply is at most 4,000 characters; when lines are cut it ends with "+N more; pass id for one change". `id` returns that one change of this thread in full, as JSON. (3) **fyr_state.** Its changes part (`about` empty or `changes`) shows only the open changes, in its own form, within the same 4,000 characters and with the same tail; a line says how many done changes are left out. `about: <id>` still shows one change in full. (4) **Handover.** The first message to the new agent (rungs 2 and 3) ends with one line per open change of the thread: full id, title and phase. (5) **Compaction.** A process with Fyr's tools registers a `SessionStart` hook with matcher `compact`; it returns the same lines as `additionalContext`, or nothing when the thread has no open change. |
| V0.97 | → V0.43, 5.10, decision 0024. **A merge is bound to the head the owner saw, and the board merges every green PR at once.** *Amends V0.43.* (owner, 16 Nov, decision 0024 slice 3: on 15 Nov Fyr's Merge merged an old head and dropped 3 commits.) (1) **Head.** Every merge carries the head commit: the PR tab's Merge… sends the head it shows (from the PR detail, else the PR chip), a change's Merge button sends its PR's head, and an agent's merge ask (`change_request {ask: merge}` or `effect_request pr.merge`) gets the head Fyr reads when the ask is made, unless the agent names one; the head is stored in the decision's args and shown on its card ("Merge a1b2c3d"). Fyr refuses a head it no longer reads with "The head moved. Look again." when the button is pressed or the ask approved, then reads the PR again and refuses the same way before gh runs; otherwise it runs `gh pr merge --<method> --match-head-commit <sha>`, so GitHub refuses a head that moves in between. A merge without a head is refused. Each Merge button shows the short SHA ("Merge a1b2c3d", "Merge a1b2c3d…" on the PR tab). (2) **Poller.** The PR read adds `isDraft`, `mergeStateStatus` and `autoMergeRequest`; the PR snapshot carries `draft`, `mergeState` and `autoMerge`. (3) **Ready.** `core.Ready(pr, threadRunning)` is "Merge" when the PR is open, not a draft, its checks pass (none is not pass), it has no conflict, `mergeStateStatus` is CLEAN or HAS_HOOKS, no auto-merge is set and the thread has no run and no task running; "Review and merge" when all of that holds except that the state is BLOCKED and the review decision is REVIEW_REQUIRED; empty otherwise. Each change in flight carries it in the thread DTO as `ready` (derived, not stored). (4) **Merge all green (n).** The Changes board shows the button when n changes it lists have `ready` "Merge", a PR head and a Merge action. After one confirmation the client presses each one's Merge, one PR once (the first card of a PR in board order), one after another, each bound to the head the board showed. A refusal shows under its card ("Not merged: ...") and the rest go on. |
| V0.98 | → V0.59, V0.64, V0.70, V0.87, decision 0024 (slice 4). **A finished change leaves the board.** *Amends V0.59 (3), (4) and (8), V0.64, V0.70 (2) and V0.87 (2).* (owner, 16 Nov: "Changesets do not work today"; 78 of 102 thread changes sat landed or live on the board, 12 duplicate pairs, 2 headers showed another PR.) (1) **Done.** A change, of a thread or a host record, carries `done {at, by, reason}` once it is finished; `ended` (abandoned, reverted) keeps its meaning for rules and settle. Fyr marks it at the reduce that finishes it: code at `landed` (reason `merged` for a merged PR); a PR merged into a branch that is not the repo's default branch (stacked) when its merge commit is an ancestor of the default branch, which the poller reads on each poll through GitHub's compare (when that read fails: when the change of its base branch is done); a change whose deploy watch (V0.66) is armed when it is `live` or the watch ends; not code at `landed` or `live` as reported; any change at `abandoned` or `reverted`. A change that goes back in flight (a reopened PR) is no longer done, unless the owner marked it. (2) **Out of sight.** The board and the thread's open list leave done changes out; a done change that still blocks (V0.70) stays in Needs you. Nothing polls the PR of a done change once its checks settle (V0.82), and the thread keeps a done change 30 days. (3) **Finished since your last visit.** The board shows a folded group "Finished since your last visit · N": the changes done after the board was last opened, newest first, each with its reason ("merged 10:42", "abandoned by you", "marked done by you"). Opening the board is a visit; the host stores the time as the owner's `boardSeenAt` and returns the one before. The thread's Changes tab lists its done changes under "Done here", folded. (4) **Untouched for 3 days.** An open change that does not need the owner, with no push, PR event, report or owner action for 3 days (`touchedAt`, moved when any fact of the change moves; a poll that reads the same PR again is no touch), sits in the board's group "Untouched for 3 days" with **Done**. Done is the owner's `change.done {change}` with a receipt, on a thread change and on a host record (the record's reduce takes this one owner Input); the card says "marked done by you". (5) **One card for a `gh pr create` PR.** A PR found in the agent's output has no branch, so Fyr makes `pr:<repo>#<n>`; the first poll reads its head branch, and that change folds (V0.87 (2)) into the thread's open change on that branch in the same repo, of any kind, that has no PR. A PR found by its branch also matches the repo. (6) **The header** shows the PR of the thread's newest open change that has one, never a done one, and nothing when none is open. (7) **Migration**, once on load: in each thread a `pr:` change and an open change on the same repo and branch fold into one, then every change at or past `landed` is marked done (reason "landed before v2") unless its deploy watch is armed, and every ended change and every landed host record is marked done. |
| V0.99 | → V0.80 (6), V0.86 (6). **The rail shows the load as a ring and the slots as pips.** *Amends V0.80 (6) and V0.86 (6).* (owner, 16 Nov: the rail footer read as plain text, a red "10", "load", "2/2 slots"; the owner wants a nicer indicator that reads at a glance.) (1) **Ring.** Each host's 1-minute load is a 30 px ring with the figure inside. The arc is the load over the cores, full past the cores. Its colour is calm under 70 % of the cores, amber from 70 % up to the cores, and the hot colour (with the figure) when V0.80's rule holds (load past the cores or CPU pressure at 40 %). (2) **Pips.** Under the ring, one pip per slot: filled while a thread holds it, empty while free. While anyone waits, the filled pips take the hot colour and "+n" follows with the number waiting. Above 6 slots the rail writes "3/8" in place of the pips. (3) **Unchanged.** The tooltip keeps the exact values (the three averages, the pressure, the agents' share, the slots in use and waiting); the screen reader gets one label with the load, the cores and the slots. The phone shows no machine load (only each thread's CPU and slot in its row), so it does not change. |
| V0.100 | → V0.91, V0.17, V0.23. **The phone rows are the desktop rows, and the phone shows the project filter as chips; the thread header names the provider.** (owner, 16 Nov: "Keep these rows in sync with Desktop layout wise"; "Also filter on project for mobile"; "keep desktop and mobile in sync except for explicit changes that are there for a reason"; "Show the provider!") (1) **One row.** Both lists render the same row (`web/src/row-ui.tsx`) in the V0.91 shape: project tag and title first, the age at the end, the details on the second line, the chips under the title. The phone sets only larger sizes for touch. It differs in three ways, each for a reason: it names no keys (no keyboard), "from <thread>" is not a link (a tap opens the row), and it names the host before the details when the front has more than one host (V0.54). (2) **Project filter.** The phone shows the desktop chip row of projects under the section strip, with 32 px chips; a tap filters, the × clears, and a quiet project picked shows its note in its chip. It replaces the "Only <project>" line. The menu item "Filter by project…" stays. (3) **Provider.** The desktop thread header shows a chip with the provider's colour dot and name, before the model; its tooltip gives the reason when the provider cannot run. The phone already shows it in the thread details. |
| V0.101 | → V0.59, V0.62, V0.84, V0.91, 6.2, decision 0024 (slice 5; V0.99 and V0.100 were taken). **Needs you is computed on the host; the agent reports only what Fyr cannot read.** *Amends V0.59 (5) and (6), V0.62, V0.84 (1) and (3), V0.91 and 6.2.* (owner, 16 Nov, decision 0024 slice 5: 4 complaints of a stuck Needs you; 61 `change_unchanged` calls and about 108 Stop reminders of pure upkeep; 17 agent corrections refused; an owner broadcast to 7 threads to fix records.) (1) **Computed Needs you.** Each change a thread shows that is not done or ended carries `need {cause, head, text, ask, action, muted}` in the thread DTO, never stored; the web client only reads it and derives nothing from conditions. The causes: an open ask of the thread (a `change_request` on the change, or an `effect_request` on its PR) that is not answered, declined or void; **Ready** by V0.97's rule (`Merge`, `Review and merge`), with the PR's head; **Stopped on a failure**: a failed condition and no run or task in progress `StopAfter` (5 min) after the last run ended. The thread stores `runEndedAt` when a run ends and `stopped`, the 5-minute rule at the last reduce; `NextCheck` adds `runEndedAt + 5 min` while a failure waits, so the clock's reduce turns it on, with no new effect. A failure while a run goes is `fixing` ("Agent is fixing CI"), muted, never in Needs you. A done or ended change, and every change of an archived thread, is never a cause. Attention: a cause puts the thread in Needs you as blocked; a finished run whose result the owner has not seen is `done` in Seen (unread), not in Needs you; a run failure stays in Needs you. Asks are void (V0.100's `voidAsks`, extended) when their change is done (a deploy ask stays), their PR is no longer open, or the thread is archived; an agent's open ask no longer blocks Archive. (2) **Not now by head.** A park holds while the change's head (the PR's head, else its newest revision) and the sorted set of its failing condition types are as the owner saw them; a new reason, label or phase does not bring it back. Not now applies to Ready and a failure; on an ask it is the decline (`thread.answer {allow: false}`). The card says "back when the head changes". (3) **Owner corrections**, each an owner Input with a receipt: **Refresh now** (`change.refresh {change}`, on a thread change or a host record the thread shows) reads GitHub (the poller's kick) and git (the observer) now, and the card says "checked N s ago" from the PR's `checkedAt`; **Abandon** (`change.end`) on a thread change and, through the new `record.end` owner Input of `ReduceRecord`, on a host record (ended by the owner and done); **Done** (V0.98). An owner end or Done refuses a later agent write to the phase. (4) **Four agent tools** with the descriptions of decision 0024 section 7: `change_open` (key optional with pr), `change_update {change, phase?, label?, conditions?, revision?, evidence?}`, `change_request`, `change_list`. `change_status`, `change_revise`, `change_attach` and `pr_link` are aliases for one release, callable and not listed (`mcp.Tool.Hidden`); `change_unchanged` is a no-op. `needs` is gone from the tools and the model. A change reference is an id, a key, a PR URL, `owner/repo#N`, or `#N` when one change of the thread has that PR. An agent value for a field Fyr observes is ignored, not refused, and the reply names what Fyr reads ("Fyr reads #13219 from GitHub: merged 10:42."); `abandoned` on a PR GitHub shows open is taken, the card leaves, and a later poll of the open PR does not bring it back ("Still open on GitHub. Close it with gh pr close if it is dead."). The system prompt carries section 7's paragraph. (5) **No change upkeep.** The changes parts of the Stop reason and the PostToolUse reminder (V0.62) go; the hooks stay registered for the live view's upkeep and the notices, and the runner meta keeps `changeTools`. (6) **Inbox row** (V0.91's shape): a thread in Needs you whose change has a cause shows the beam dot and its button after the change chip: "Merge a1b2c3d", bound to the head (V0.97), for Ready; "Answer" for an ask, which opens the thread. The thread's Changes tab shows the cause inline on the card: the ask with its answer ("Merge a1b2c3d" for a merge ask, else Yes) and Not now; Ready with Merge bound to the head and Not now; a failure with Not now; fixing muted without the beam. |
| V0.102 | → V0.97, V0.43, 5.10. **In a repo the owner opted in, Fyr approves an agent's merge ask itself once the checks pass on the head the ask named.** (owner, 16 Nov: "allow auto-approve for merge in specific repos".) (1) **Setting.** Per GitHub repo (`owner/name`, compared in lower case), stored on the host (kv `autoMerge.repos`). Only the owner changes it: the host command `host.autoMerge {repo, on}` (owner only, with a receipt). No agent tool sends it. Settings → Auto-merge lists the repos of the threads' PRs and the repos turned on, each with an On/Off button, and a field to turn on a repo by name; `GET /api/autoMerge` serves the list. (2) **Approval.** After every Input of a thread, Fyr looks at its open merge asks (`change_request {ask: merge}` or `effect_request pr.merge`). An ask whose PR is in a repo turned on is approved when the PR's head is the head the ask named and `Ready` (V0.97 (3)) is "Merge": open, not a draft, checks pass, no conflict, merge state CLEAN or HAS_HOOKS, no GitHub auto-merge, and no run or task running in the thread. The approval is an observer's Input (`merge.autoApproved`); neither the agent nor a client can send it. It closes the ask and runs the same merge as the owner's yes, bound to the ask's head: V0.97 (1) holds, and a push after the ask leaves the ask open for the owner, who gets "The head moved. Look again." on a press. Turning a repo on approves the asks that already wait in it. A repo that is off waits for the owner as before. (3) **Shown.** The thread shows the ask closed as "Auto-approved: <repo> merges without a press (head a1b2c3d)." and the result starts with "Auto-approved by the auto-merge setting of <repo>."; the agent hears that result. The change keeps `autoApproved {repo, head, at}`, and its card on the board and in the thread says "Merge auto-approved by the repo setting (<repo>, a1b2c3d)". |
| V0.103 | → V0.93. **The phone's inbox header shows the logo.** *Amends V0.93* ("the phone header is unchanged"). (owner, 16 Nov: "Why doesn't mobile show the fyr icon?"; the rule that desktop and phone stay in sync.) The phone's inbox header shows the same `/logo.png` as the rail, at 28 × 28 px, before the name "Fyr". The rail has no room for the name; the phone header has, so the name stays, and the image has an empty accessible name there so a screen reader says "Fyr" once. The rail and the phone share one component. The thread, search and selection headers on the phone do not show the logo: the desktop shows it only in the rail, never in a thread header. The touch targets in the header do not change. |
| V0.104 | → V0.100, 13.1, 7.5. **The owner pins a thread; a pinned thread sorts above every other thread, and snooze, settle and archive remove the pin.** (owner, 16 Nov: "New feature: pin thread (like this coordinator thread). Place pinned on the top. Snooze/Settle removes pin".) (1) **Actions.** `thread.pin` and `thread.unpin` are owner actions on the one path, with a receipt; an agent cannot send them. The thread stores `pinnedAt`, and the DTO carries it. Pin is unavailable on a thread that is pinned, settled, snoozed or archived; Unpin on a thread that is not pinned. (2) **Order.** Both lists draw a Pinned block above every section, always open, when a thread is pinned. Its rows are in the normal order: the section order, then the section's own order. A pinned row keeps the row shape of its section (V0.91), shows a pin mark before the project, and shows once (not again in its section). The rail, the phone strip and the badges still count it in its section, because its attention did not change. j/k start at the Pinned block. (3) **Removal.** A snooze removes the pin. A settle removes it when the thread settles: at once, or when a deferred settle (when done, when merged, the agent's settle_when) fires; until then the pin stays. Archive removes it too. Unsnooze, unsettle, unarchive and a new run do not restore it. (4) **Controls.** Desktop: Pin or Unpin in the row's right-click menu, and a pin toggle left of Snooze in the thread header. Phone: Pin or Unpin in the thread's ⋯ sheet (the phone has no right-click and no room in its header, so the sheet holds it). (5) **Agents.** The `threads` tool writes "pinned" after the id of a pinned thread. |
| V0.105 | → V0.98 (1), V0.95 (4), V0.66, V0.82, decision 0024 (3). **A merged change is done in every repo, and a record on the default branch is done.** *Amends V0.98 (1) and decision 0024 (3), the row for Fyr's own repo.* (Live data after the V0.101 deploy, 16 Nov: `hosting:p15-buckets`, merged as AcmeHosting#1547, stayed on the board because AcmeHosting has GitHub deployments and its V0.66 deploy watch held the change open, against the owner's decision that code is done at merged; a record `git:github.com/meros/fyr#main~2` from before V0.95 stayed in flight and the observer kept it.) (1) **Merged is done.** A change whose phase reached `landed` is done; an armed deploy watch no longer holds it. The deploy watch (V0.66) and a self-deploy (V0.82) still move it to `live`, shown as a fact on the done card. (2) **The default branch.** When the git observer reads a repo's default branch and an open record of that branch exists, the record is done with the reason "default branch"; the load pass does the same for an open record whose branch is the default one (by its workspace base, else main or master). A feature branch is untouched. Tests: `TestIsDoneByKindAndPhase`, `TestRecordOnDefaultBranchIsDone`. |
| V0.106 | → V0.104 (3), 7.4. **A pinned thread never settles by itself; only the owner's Settle settles it.** *Amends V0.104 (3).* (owner, 16 Nov: "Pinned should never auto-settle".) (1) **Waits.** A settle Rule that the owner did not arm (the agent's `settle_when` done, merged or change; the agent's `watch` with then "settle"; any settle Rule Fyr arms) is suspended with the reason `pinned` while the thread is pinned. It stays recorded and does not fire. (2) **Unpin drops them.** Unpin disarms those Rules; they do not fire after it, also when their condition holds by then. (3) **The owner's Settle.** The owner's Settle (now, or when done or when merged, which the owner armed) still settles a pinned thread and removes the pin, as in V0.104. Settle stays available on a pinned thread that has only the agent's "settle when done"; the press replaces it with the owner's. (4) **The agent is told.** On a pinned thread, the reply to `settle_when` and to `watch` with then "settle" adds: "The owner pinned this thread: it does not settle while pinned, and unpin drops this condition." (5) **Unchanged.** Snooze and archive still remove the pin. |
| V0.107 | → V0.37 (1), V0.52. **Every agent process lists the view tools, so a view the owner turns on during a run is filled in that run.** *Amends V0.37 (1) and (4).* (Bug from the Acme coordinator thread, 16 Nov: the owner turned the view on while a long run went; `fyr_state` said "view: on, no page yet", the running process had no `view_*` tools, and the notice said "it takes effect with your next process", so the view stayed empty with nothing shown to the owner.) (1) **Tools.** Fyr's MCP server lists `view_publish`, `view_state` and `view_unchanged` for every thread. The CLI reads the tool list once, when the process starts; Fyr's MCP transport answers JSON over POST only and has no stream for `notifications/tools/list_changed`. While the view is off, a call fails with "the live view is off in this thread; only the owner turns it on, and Fyr tells you when they do". (2) **Notice.** When the owner turns the view on, the agent's notice asks it to publish now with `view_publish {html or path, state}`, says what the view is and that `state.headline` is its status line, and asks for `view_state` as items move. (3) **Upkeep.** Every process with Fyr's MCP tools counts as having the view tools, so the Stop hook (V0.37 (4)) and the reminder (V0.52) apply in the run where the owner turned the view on. The view prompt still comes with the next process, which starts after the run as before. Tests: `TestViewToolsOnlyWithFlag`, `TestViewOnNoticeAsksForPublish`. |
| V0.108 | → V0.69, 5.10, V0.104. **Regenerate title: Fyr writes a new thread title from the conversation so far, on the owner's press or the agent's request.** (owner, 16 Nov: "Add regenerate title - on demand from UI or automatically through tools if agent wants"; V0.107 was taken by #123.) Today a thread's first title is the first line of its first message (`TitleFrom`, cut to 80 characters) or the `title` that `thread_new` gave; after that only `thread.rename` (owner) and `thread_rename` (agent, V0.69) change it, and a name the owner gave is kept against the agent. A thread that drifted from its first message keeps a name that says what it was. (1) **Owner.** `thread.regenerateTitle` is an owner action with a receipt; an agent cannot send it. It returns the effect `title`; the engine makes the title in the background and lands it as the observer Input `title.made`. The new title replaces any title, also one the owner typed, because the owner asked for it, and it is stored as the owner's (`titleBy: owner`), so the agent's `thread_rename` keeps it after. The thread log gets `thread.renamed {by: owner, regenerated: true}`. Desktop: "Regenerate title" in the thread header's ⋯ menu and in the row's right-click menu. Phone: "Regenerate title" in the thread's ⋯ sheet (the phone has no right-click and no ⋯ menu in its header, so the sheet holds it). Both send the same command through one function (`regenerateTitle` in `web/src/commands.ts`) and show the toast "Fyr is writing a new title"; the title changes when the host answers. (2) **Agent.** `thread_rename {regenerate: true}` (no `title`) on this thread or a thread it started makes the title in the tool call and applies it as the agent's rename. A name the owner gave is kept: the tool refuses with the V0.69 reason before any model call. (3) **How the title is made.** One `claude -p` run of the thread's own provider (its auth, key and endpoint), in the system temp folder so no project instructions load, with `--tools ""`, `--strict-mcp-config`, `--setting-sources ""`, `--no-session-persistence` and a short system prompt that replaces Claude Code's (`--bare` is not used: it never reads an OAuth login). The model is the provider's Haiku model (`claude-haiku-5-5` on a Claude login), else the model its endpoint sets in `ANTHROPIC_DEFAULT_HAIKU_MODEL`, else the thread's model (*amended by V0.115*: the System agents setting picks the provider and the model, and there is no fallback to the thread's model). The prompt is the conversation: the owner's words and the agent's text, no tool calls; a long one keeps its first 2 000 characters (why the thread started) and its newest 10 000 (what it is about now). Fyr keeps the first line of the answer, drops quotes, Markdown emphasis, a "Title:" prefix and a final full stop, and cuts it to 80 characters. At most one call per thread runs at a time; a call stops after 90 s. (4) **Cost.** Measured on a short conversation: 945 input and 15 output tokens, $0.0002 at list price, about 2 s. A full 12 000-character prompt is about 3 000 to 4 000 tokens, so about $0.001 a call (estimated from the measured rate). A Claude login spends it from the subscription's allowance. Fyr never calls on its own: only a press or an agent's request starts a call. (5) **Failure.** When the call fails (no login or key, no answer in 90 s, an empty answer, a thread with no delivered message yet), the title stays. The owner's thread shows the failed line "Fyr could not regenerate the title: <reason>. The title stays."; the agent's tool returns the same line as its error. Tests: `TestRegenerateTitleIsTheOwners`, `TestOwnerRegeneratedTitleReplacesTheOwnersName`, `TestFailedTitleKeepsTheTitle`, `TestCleanTitle`, `TestOwnerRegeneratesTheTitle`, `TestFailedRegenerateKeepsTheTitle`, `TestAgentRegeneratesTheTitle`, `TestHaikuModel` (was `TestTitleModel`; V0.115), `TestPrintArgs`, `TestPrint`, the layout test's V0.108 block. |
| V0.109 | → 6.1, V0.48, V0.104. **The marked row in the desktop thread list is the open thread, whatever path opened it, and the row is in view.** (owner, 16 Nov: "The selected thread in the list of threads MUST follow the one on the right-hand side. Sometimes when jumping to a convo it doesn't follow.") (1) **One source.** The list keeps no cursor of its own. The marked row is the open thread; with no thread open, the last thread opened stays marked, and j/k start there. (2) **Paths.** A row click, a link from another thread ("From thread", "Started by"), a notification, `?open=`, a new thread and a reload open the thread, so the row is marked. j/k and g1–g5 open the row they move to when a thread is open (a peek stays a peek); with nothing open they only mark it. Settle, snooze and archive from the list open the next row (else the one before); from the thread pane the thread stays open. (3) **Drawn and in view.** When the open thread's row is in a closed section, that section opens; past the section's row limit, the section shows all its rows. This holds when the row moves to another section while it is open (Working → Settled). Settled opened this way closes 30 s after the owner leaves it (V0.48). The row scrolls into view when it changes, moves to another section or the Pinned block, or is drawn again. (4) **Hidden rows.** When search, the project filter or a quiet project hides the open thread's row, no row is marked and the filter stays as the owner set it. (5) **Phone.** The phone shows the inbox or one thread, never both, so it marks no row. Tests: `test/selection.test.ts`; layout e2e "V0.109 desktop" (a "From thread" link, `?open=` of a Settled thread, k in the list). |
| V0.110 | → V0.15, V0.92, V0.93. **The favicon, the PWA icons, the Apple touch icon and the push badge are the rendered lighthouse.** (owner, 16 Nov: "Why is the favicon/PWA icon still the old lighthouse?"; V0.93 changed only the rail logo.) (1) `design/logo/icons.sh` cuts every icon from `design/logo/lighthouse-512.png` once, and they are committed in `web/assets/icons/`; the build copies them and draws nothing. (2) The "any" icons (192, 512) and the favicon (32) are the render on transparent. The maskable icon (512) and the Apple touch icon (180) are the render at 80 % (the maskable safe zone) on a full-bleed `--night` tile (`#14212d`), because the white tower and the amber beams need a dark ground. The push badge (96) is a white silhouette from the render's alpha. (3) The icons live at `/icons/lighthouse/` (`web/src/icon-paths.ts`): the service worker serves `/icons/` cache-first as immutable, so new icons take a new folder. (4) The hand-drawn mark and its PNG encoder (`web/scripts/icons.ts`) are removed. An installed PWA can keep its old home-screen icon until the owner reinstalls it. |
| V0.111 | → V0.102, V0.13, V0.90, 5.10. **Auto-deploy: Fyr approves an agent's deploy ask itself when the checkout is clean and its commit is on origin's default branch; the setting is on by default.** (owner, 16 Nov: "Always allow fyr update... It's a very benign thing now".) Since V0.90 a deploy cuts no turn, and a failed build or test switches nothing. (1) **Setting.** One host setting, not per checkout, stored in kv `autoDeploy` (`on`, `off`; empty is on, the owner's default). Only the owner changes it: the host command `host.autoDeploy {on}` (owner only, with a receipt); no agent tool sends it. Settings → Auto-deploy has an On/Off button on desktop and phone (the same component); `GET /api/autoDeploy` serves `{on}`. (2) **Gate.** After every Input of a thread, Fyr looks at its open `fyr.deploy` asks (`change_request {ask: deploy}` or `effect_request fyr.deploy`). For each ask it checks the checkout once: `git status --porcelain` is empty (no uncommitted or untracked file, which the build would include), and HEAD is the commit `git ls-remote origin HEAD` reports or an ancestor of it. Code that passes is code a person already merged, so the press adds nothing. An ask that fails the gate stays open for the owner, and the thread shows "Not auto-approved: <why>. The deploy waits for your press." (3) **Approval.** The approval is an observer's Input (`deploy.autoApproved`); neither the agent nor a client can send it. It closes the ask as "Auto-approved: Fyr deploys a1b2c3d without a press." and runs the same deploy as the owner's yes, bound to the commit the gate read (`args.commit`). The build, the tests, the older-than-live check (V0.24), the rollback and the reports are as before. (4) **One at a time.** While a deploy builds, tests, drains, or waits out its probation (`pending_deploy`), Fyr approves no other deploy ask: the thread shows "Another Fyr deploy runs; Fyr approves this one when it ends." once, and the ask is approved when the running deploy ends (a failure, or the new build's probation). A deploy the owner presses still replaces one that has not switched, as before. (5) **The commit.** A deploy builds the checkout's HEAD when it starts. An auto-approved deploy refuses to build when HEAD moved after the gate: "The checkout moved from a1b2c3d to e4f5a6b after the approval; nothing was built. Ask again." An ask the owner presses builds the HEAD at the press; the tool description tells the agent to pull before it asks and to leave the checkout until the result. (6) **Shown.** The result starts with "Auto-approved by the auto-deploy setting (a1b2c3d)."; the agent hears that result. The change keeps `autoApproved {deploy: true, head, at}`, and its card says "Deploy auto-approved by the setting (a1b2c3d)". Limit: the gate trusts the checkout's `origin` remote; an agent that points `origin` at another repo passes it. Tests: `TestAutoDeployApprovesTheAskAndBindsTheCommit`, `TestOnlyAnObserverAutoDeploys`, `TestAutoDeployHeldAsk`, `TestAutoDeploySettingIsTheOwners`, `TestAutoDeployByTheSetting`, `TestAutoDeployOneAtATime`, `TestDeployRefusesAMovedCommit`, `TestDeployBusy`, `TestGate`, `web/test/automerge.test.ts`, the layout test (the section on desktop, the 44 px toggle on the phone). |
| V0.112 | → V0.43, V0.97, V0.102, 5.10. **A merge waits for CI: no check reported on a repo with CI is pending, never green.** *Amends V0.43 (2) and (4), V0.97 (3).* (Bug from the coordinator thread, 16 Nov: Fyr merged meros/fyr#126 at 16:05:33 on the owner's yes to the agent's merge ask, 8 s after the ask; the PR's check list was empty, and its pull_request run started 18 s after the merge. Auto-merge was off for meros/fyr. The guard of `pr.merge` read no checks, and the host's last read refused failed checks only on a BLOCKED merge state, which an unprotected repo never has.) (1) **One summary.** `github.Summarize` is the one place a PR's check list becomes its checks, for the poller and for the PR detail: `fail` when a check failed, `pending` while one runs (queued or in progress, or a commit status PENDING or EXPECTED), `pass` when at least one check reported and none failed or runs (skipped counts as reported). An empty list on an open PR is `pending` on a repo with CI, because GitHub lists a run only after it is queued; it is `none` only on a repo with no CI. The PR detail carries the summary as `checked`. (2) **A repo with CI.** A repo has CI when a check reported on the head of any of its 20 newest PRs (one GraphQL read), or on any PR of it that Fyr read since the host started. A yes is kept for the life of the host process; a no is read again after 10 minutes. A read that fails counts as CI, so a merge waits and never runs on a guess. A new repo whose first PR adds its first workflow therefore reads as no CI until a check reports on a PR; GitHub branch rules still apply. (3) **The gate.** A `FyrEffect` has a `Gate` beside its `Guard`: what must also hold when the owner's yes, the owner's press or Fyr's auto-approval runs it, but not when the agent asks for it. The merge's Gate refuses while Fyr reads the PR's checks as `pending`: "CI has not passed on a1b2c3d yet: Merge waits for the checks". The agent may still ask for a merge while CI runs; the owner's yes on that ask is refused with that reason and the ask stays open. Before gh runs, the host reads the PR again and refuses the same way ("No check has reported on a1b2c3d yet: Merge waits for CI", or "CI has not passed on a1b2c3d yet" while a check runs), whatever GitHub's merge state is. A repo with no CI (`none`) does not wait. Failed checks keep V0.43's rule: a failing check that GitHub does not require (UNSTABLE) does not block, and BLOCKED with a failing check refuses. Auto-merge (V0.102) needs `pass` through Ready, as before. (4) **Shown.** A merge ask whose yes waits keeps its card and its place in Needs you (an open ask always is, 7.4); its `need` carries `wait`, the line adds it after the reason, and "Merge a1b2c3d" is disabled with the reason as its title. The PR tab's status line is "Checks running: N" or "No check has reported yet", and Merge… is disabled, on any merge state; the PR chip's "Checks running" no longer allows Merge. Desktop and phone share the card (`NeedLine`) and the tab. Tests: `TestOwnerMergeWaitsForPendingChecks`, `TestOwnerMergeOnNoCIOrFailedChecks`, `TestMergeCheckedWaitsForCI`, `TestFetchEmptyChecksByRepoCI`, `TestMergeBlock`, `test/merge-wait.test.ts`. |
| V0.113 | → V0.103, V0.93. **The phone's inbox header shows the logo alone, without the word "Fyr".** *Amends V0.103* ("before the name \"Fyr\"", "the name stays"). (owner, 16 Nov: "Remove the name \"Fyr\" from the mobile header".) The phone's inbox header shows the 28 × 28 px `/logo.png` and no visible name, as the desktop rail has since V0.93; the desktop does not change. The image's alt is "Fyr", so a screen reader still says "Fyr" once. The connection dot and host name follow the logo; the touch targets in the header do not change. Test: the layout test (the logo is there with alt "Fyr", and the header's brand has no text "Fyr"). |
| V0.114 | → V0.46, V0.74, V0.100, 3.15. **Every way into a quiet project goes through the same 30 s hold; one unlock opens them all.** *Amends V0.46 (4) ("Picking the project in the project filter shows them", "Search (desktop and phone) still finds them") and V0.74 (3) ("only New thread changes").* (owner, 16 Nov: "Hey, it was super easy to open acme during quiet hours. I wanted a cumbersome process.") (1) **One unlock.** V0.74's hold (30 s, a finger or the mouse button, no key and no tap) and its 30 minutes on that device (browser storage) now open every path below at once, and "Hide" in New thread shuts them all; another tab of the same browser follows. The unquiet inbox, its counts, the title's Needs you count and the notifications keep V0.46 (2)–(4) during the unlock: the unlock opens paths, it does not end the window. (2) **The project filter** (the desktop chip, the phone chips): picking a quiet project shows none of its threads; under the chips a box says "<project> is quiet until 06:00." and "Its threads are hidden in its quiet hours.", with the hold row ("Hold here for 30 s to open quiet projects for 30 minutes."). After the hold its threads show, as V0.46 (4) had them. (3) **Search** (desktop ⌘K, phone): a quiet project's threads are not found; when a search would have found any, the same box says "Threads of quiet projects are left out of search." with the hold. (4) **A quiet thread opened any other way** (a "From thread" or "Started by" link, a [Fyr] notice, a board card or Needs you, `?open=`, back and forward, a push tap) shows, in place of the thread (desktop: the thread pane; phone: a screen titled "Quiet hours" with Back), "<project> is quiet until 06:00." and "This thread is in its quiet hours." with the hold; the hold opens it where it stands. Behind the gate the thread is not loaded, not marked seen and not reported as watched (presence). (5) **The open thread stays open.** A thread opened while its project was not quiet, or during the unlock, keeps working when the window starts or the unlock ends (V0.46 (6)); leaving it and coming back (another thread, the inbox, back and forward) goes through the gate again. (6) **Not gated, on purpose:** j/k and g1–g5 walk the inbox order, which holds no quiet thread (V0.46 (4)); the board shows a quiet project's cards, but opening one goes through (4); no push is sent in quiet hours (V0.46 (3)), and a push tapped from before the window goes through (4); the project screen (quiet hours, name, folder) stays open so the owner can change the hours, and lists no threads; the agents' `threads` tool is not the owner's way in and lists every thread as before. Tests: `web/test/quiet-gate.test.ts` (one test per path) and the layout test (the phone chip shows the gate, not the thread, without the unlock; with it, the thread; `?open=` to a quiet thread shows the gate). |
| V0.115 | → V0.108, V0.50, V0.102, decision 0016. **System agents: the model calls Fyr starts itself run on the provider and model the owner picks in Settings; the default is Haiku on the host's default provider.** *Amends V0.108 (3) and (4).* (owner, 16 Nov: "Vi bör ha en inställning för dessa system-agenter... Billig typ haiku, men det ska vara valbart host/provider/modell"; the coordinator: "Align", so the title follows the setting.) A system agent is a call Fyr makes on its own behalf, not an owner's thread. There are two jobs: `title` (Regenerate title, V0.108) and `setup` (the project setup run, which sets the icon, colour and name). Before this row the only one was the title call, and it ran on the thread's own provider. (1) **Setting.** It is stored per host (kv `systemAgents`), because a provider and its keys never leave their host (decision 0016): a host's system agents run on that host with its providers. The setting has a default `{profile, model}` and an optional override per job. No provider means the host's default provider (its first). No model means that provider's Haiku model: one in its model list, else the model its endpoint sets in `ANTHROPIC_DEFAULT_HAIKU_MODEL`; on a Claude login that is `claude-haiku-5-5`. An override that names a provider is taken whole: its model, or that provider's Haiku. An override that names only a model runs it on the default's provider. Only the owner changes the setting, with the host command `host.systemAgents {job?, profile, model}` or `{job, clear: true}` (owner only, with a receipt); a provider the host does not have is refused. No agent tool sends it. `GET /api/systemAgents?host=` serves the setting, the host's providers with their models and Haiku model, and, for each job, the provider and model it runs on now or why it cannot run. (2) **Settings → System agents**, on desktop and phone (the shared Settings body). When the front has more than one host, a Host picker comes first. Then come a Provider and a Model picker for all jobs, and for each job ("Thread titles", "Project setup") a picker that reads "Same as above" or names its own provider, with a Model picker once it has one. Under each job a line reads "Runs on Claude · claude-haiku-5-5", or gives the reason it cannot run, in the error colour. A provider that cannot run is listed with its reason. On the phone the pickers are 44 px. (3) **One interface.** Every system job reads the setting through `(*agent.Manager).SystemAgent(job)`, which returns `{Host, Profile, Model}` or an error. A print job runs with `SystemPrint(ctx, job, system, prompt)`, the V0.108 launch: in the system temp folder, with a system prompt that replaces Claude Code's. No model id is hardcoded in a system job. The title call (V0.108 (3)) now runs on the setting, no longer on the thread's provider. (4) **Failure is visible.** If the provider is missing, cannot run (no login or key), or has no Haiku model while no model is set, the job fails with a reason that names where to fix it ("Z.ai has no Haiku model: pick a model in Settings → System agents"). A title then stays and the V0.108 (5) failed line shows. A job never falls back to another provider, to the thread's model or to a bigger model. *Amends V0.108 (3)*, which fell back to the thread's model. *Cost (amends V0.108 (4))*: the default is Haiku, so a title costs as before (about $0.001 a call); a bigger model the owner picks costs more. Tests: `TestSystemAgentsSetIsTheOwners`, `TestSystemAgentsFor`, `TestSystemAgentsSetting`, `TestSystemAgent`, `TestSystemAgentFailsVisibly`, `TestHaikuModel` (replaces `TestTitleModel`), `web/test/system-agents.test.ts`, and the layout test's V0.115 checks (desktop and phone). |
| V0.116 | → 3.15, V0.23, V0.55, V0.69. **Project setup: an agent fills in a project's image icon, colour and name; the owner's values are kept.** *Amends V0.23 ("the icon is a monogram, an emoji or a line icon"): a project may also have an image, and the glyph is its fallback.* (owner, 16 Nov: "real images as the project icon, set automatically by an agent, from the project's assets or online"; and "project setup in general should be more automatic in the same way".) (1) **The image icon.** A project may carry `image`, the version of its stored icon. The host keeps it under its data folder (`project-icons/<id>/64.png` and `128.png`) and serves it at `GET /api/projects/<id>/icon?size=64\|128&v=<image>` (PNG, `nosniff`, kept by the browser when `v` is given); a front sends `/api/projects/<id>/…` to the project's host. Fyr reads PNG, JPEG and GIF only, at most 2 MB and 4096 × 4096 pixels; a picture up to 5:4 is cut square in the middle, a wider or taller one (a wordmark) is padded with transparency; it is scaled to 64 and 128 px and written again as PNG, which drops every metadata chunk. Every place that draws the project's icon (the shared `ProjectIcon`: the project chip, the row tag, the filter, the thread header, the project screen, Settings → Projects, New thread, the board; desktop and phone) draws the image when there is one and the glyph in the project colour when there is none or it does not load. (2) **Who set it.** A project carries `by: {name, color, image}`: `owner`, `agent`, or `""` for a placeholder. A name equal to the folder's last element and the colour picked from the name are placeholders; for a project from before V0.116 that is derived the same way. A value the owner changes (`project.update`, the upload, Remove image) is the owner's; saving an unchanged value does not change who set it. Three rows in the rights table (`project.name`, `project.color`, `project.image`) keep the owner's value against an agent, as `thread_rename` keeps the owner's title (V0.69): the agent may fill a placeholder and change its own values, never the owner's. An image the owner removed is the owner's choice of no image: the agent does not put one back. (3) **The owner.** The project screen (the shared `ProjectEditor`, desktop and phone) has an Image field with Upload image… (Replace image… when there is one; the browser checks PNG, JPEG or GIF and 2 MB first, the host again: `POST /api/projects/<id>/icon`, X-Fyr, the body is the picture), Remove image (`project.update {id, image: ""}`, which also deletes the files), and Set up automatically (only with a folder). The field under it is "Icon without the image" when there is an image. Name, Colour and Image show "Set by agent" while the agent's value stands. The setup line says "Set up automatically: an agent filled in what you had not set.", or "Automatic setup could not start: <why>". On the phone the image buttons are 44 px high (touch). (4) **The agent's tool.** `project_setup {project?, icon?, name?, color?}` (`project`: an id or a name, default the calling thread's project; `icon`: a full path or an http(s) URL). It is one tool for every field the setup fills, so a later field is a new argument, not a new mechanism. Fyr reads the file or fetches the URL (http and https only, at most 3 redirects, 20 s, 2 MB; every connection, after each redirect too, must go to a public address: loopback, private, link-local, CGNAT and Tailscale addresses are refused), checks and stores it as in (1), and without `color` takes the colour from the icon: the most common hue among its opaque, coloured pixels, mapped to the nearest palette hue (an icon with too little colour gives slate). The Input `project.setup` (agent only) fills each field the owner has not set, marks it the agent's, and the reply names what it set and what it kept ("Kept the name: the owner set it."). (5) **The setup run.** `project.setupRun {id}` (the owner's command, or the host itself) starts a thread titled "Set up <name>" in the project's folder, filed in it, with a fixed brief: look in the folder for the project's own image (favicon, apple-touch-icon, logo files, `public/`, `static/`, `assets/`, the web manifest's icons, the README's image; a square mark over a wordmark); if there is none, fetch only the project's own branding online (its website's icon, or its GitHub owner's avatar from the folder's remote), nothing whose licence does not allow it; convert SVG, WebP or ICO to PNG outside the folder; give the real name (package.json, go.mod, Cargo.toml, pyproject.toml, the README title, the repo name) only when the name is the folder's; ask the owner nothing, change no files, and end with one line. The thread runs on the system agent of the job `setup` (V0.115: Settings → System agents, "Project setup"; by default the host's default provider with its Haiku), never a hardcoded model. The project records the run as `setup: {thread, at}`; a system agent that cannot run, or a thread that cannot start, is recorded as `setup: {at, error}` and answered 409, and the project screen shows it. A project without a folder is 400. (6) **Automatic.** A project made with a folder gets one setup run at once. Each project from before with a folder, no image and no run gets one, once, a minute after the host starts and 30 s apart; a run that started (it has a thread) is never repeated by itself, also when the agent set nothing: the owner presses Set up automatically. *Amended by V0.117*: a project whose run never started (setup.error, no thread) gets another try at the next start. Online fetching needs no yes per project (the owner's choice, 16 Nov: own branding only). (7) **Not built, for later fields of the same tool:** the default provider and model, quiet hours, the repo link, a short description, the project's website. Tests: `internal/projicon` (types, size and pixel limits, square and scale, metadata dropped, main colour, the local-network block, redirects), `internal/core` (who set what, placeholders, the owner's kept, the run recorded), `internal/engine/setup_test.go` (the tool from a file and a URL, the owner's upload and remove, the run, AutoSetup on create, the one pass for projects from before, delete removes the files), `internal/server/icon_test.go`, `internal/front/icon_test.go`, `web/test/project-icon.test.ts`, and the layout test (Settings → Projects draws Fyr's image at 28 px and a glyph where an image does not load; Fyr's project screen on the desktop and the phone, its buttons 44 px on the phone). |
| V0.117 | → V0.115, V0.116, V0.108. **On a Claude login the default system agent is claude-haiku-5-5, also when the stored model list has no Haiku; a setup run that never started runs again at the next start.** *Amends V0.115 (1) and V0.116 (6).* (Bug on the live build db1211d, 16 Nov: daemon.log said "Project setup cannot start: Claude has no Haiku model: pick a model in Settings → System agents", with no model set. The host's Claude login stores Opus, Sonnet and Fable only, and V0.115 read the Haiku model from that list alone. Regenerate title failed the same way.) (1) **Haiku.** A provider's Haiku model is one in its model list or its model caps, else the endpoint's `ANTHROPIC_DEFAULT_HAIKU_MODEL`. Else, on a Claude login (the claude CLI with no endpoint and no API key; a login by token included), it is the claude CLI's own Haiku (`claude-haiku-5-5`). An endpoint provider with no Haiku still fails the job visibly (V0.115 (4)). The title job, the setup job and the Settings line read the same rule. (2) **Retry.** A setup run that could not start records its error with no thread. The once-per-project pass at host start (V0.116 (6)) gives such a project another try, and the error goes when the run starts. A run that started is never repeated by itself. The pass runs only at host start, so a failing system agent is tried at most once per restart. Tests: `TestHaikuModelOnAStoredClaudeLogin` (the live provider record), `TestSystemAgentDefaultOnAStoredClaudeLogin`, `TestSetupThatCouldNotStartRunsAgain`. |
| V0.118 | → V0.39, V0.81, V0.83, V0.111. **The update status is a line in the layout and never covers a control; it is calm now that deploys are frequent.** *Amends V0.39 (where the banner sits, how long a success shows), #81 (the fold) and #85 (the dismissed key also takes a success that went by itself).* (owner, 16 Nov: "The updating notification is still in way of important ui elements"; with auto-deploy, V0.111, deploys run several times an hour.) (1) **Place.** One component on both fronts (`DeployBanner`, `web/src/deploy-ui.tsx`). On the desktop it is the first line of the page, above the rail, the list and the thread (`.shell` is a column; the app takes the rest), so it pushes the content down. On the phone it is the first line of the screen, below the top safe area, above the header (`PhoneApp`'s `status`); the screen below it is shorter by its height. It is never `position: fixed` or `absolute`: it covers no composer, send button, rail button, header button, sheet or dialog. Collapsed it is one line (30 px on the desktop, 44 px on the phone, touch), the sentence cut with an ellipsis. (2) **Calm.** `build`, `test`, `drain`, `switch` and "Restarting Fyr · back in a few seconds" show as the line with a spinner; a press on it shows the whole sentence (and a failure's details), a second press makes it one line again. A success ("Fyr is updated · build <id>") shows for 5 s from when the page first sees it and then goes by itself, and only when it ended within the last 60 s (a page opened later shows nothing); once gone it is stored as dismissed (#85), so a reload does not show it again. A failure stays, in red, with **Details** and **×** (Dismiss), until it is dismissed; the dismissed result stays dismissed on this device (#85: its phase and build). **Restart now** (V0.83) is on the line while it applies. The fold of #81 is gone: the line is already small. (3) **Client-only.** An update that changes only the web client (V0.81) shows no status: the page's reload is its sign. The deployer marks its progress `client: true` (the phase `client` and its `done`), and the page shows neither. (4) **Not changed, named:** the desktop toast (bottom centre, 4 s or the undo window) and the phone toast (above the bottom bar or the composer at rest) still float over the content for their few seconds; the key hints bar (shown while the owner holds `?`) covers the bottom 28 px; menus, dialogs, sheets and the preview viewer are overlays the owner opens. The live view (V0.37) and the rail's load ring (V0.99) are in the layout already. Tests: `web/test/deploy.test.ts` (what shows when, the 5 s and 60 s), `TestDeployClientOnly` (the client mark), and the layout test: in each phase (`build`, `test`, `drain`, `switch`, `failed`, `done`) at 1280 × 800, 390 × 844 and 360 × 640, with a thread open and with the details opened, the status is not fixed or absolute, its box overlaps no visible button, field or composer outside it (cut by scrolling ancestors), and it is inside the screen below the top inset; `drain` has Restart now, `failed` has Details and Dismiss, Dismiss hides it, `done` goes by itself, `client` shows none; the press opens and closes the line on both fronts. |
| V0.122 | → V0.44, V0.50, V0.77. **A token account's quota comes from its sessions, never from the usage endpoint; a rate-limit wait survives a restart.** *Amends V0.44 (2) and V0.50 (5).* (owner, 16 Nov: "The quota on the Claude acme rate limits all the time... Figure it out"; Settings → Providers showed "Claude (acme): Rate limited by the provider; trying again later" all the time, and daemon.log had 322 failed reads for it in one day, alternating 429 and "Login expired". The cause, from the Claude CLI's own code: a `claude setup-token` token, which Fyr passes as `CLAUDE_CODE_OAUTH_TOKEN`, carries only the `user:inference` scope, and `GET /api/oauth/usage` needs `user:profile`. The CLI itself treats a 403 from that endpoint as a rate limit. The read could never succeed. The backoff lived in memory, so each deploy reset it and Fyr asked about every 6 minutes.) (1) **A token account is never read.** The host never sends a token account's token to the usage endpoint. Its windows come only from the `rate_limit_event` of its own running sessions (V0.44), and its status carries `source: "session"`. The card shows the windows with their age ("From the last session, 12 min ago"); with none yet it says "usage shows after the next run" and the badge says "After a run". A token account with no token still says "No token yet; paste it in Settings → Providers" (checked locally, no request). (2) **A refused read is logged with its cause.** The log line names the status and the reply's `error.type` and `error.message`, cut to 200 characters, with the token and anything shaped like a key taken out. The owner's status keeps Fyr's own short sentence. (3) **The wait and the data survive a restart.** The host keeps each identity's last status, its next read time and its count of consecutive rate limits in `<data>/quota-state.json` (mode 0600, no secret in it), and loads it at start. A restart during a wait does not read early, and a restarted host continues the 5, 10, 20, 30 min steps where it was. (4) **The badge names its error.** "Rate limited" only for a rate limit; "No token", "Login expired" (a folder login refused or past its expiry) or "Can't read" for the rest. (Checked on this host: the Acme threads hit no inference limit. Every `rate_limit_event` on the account was `status: allowed`, `seven_day` at 67 to 78 %. The `rejected` in those events is `overageStatus` (extra usage is off for the org: `org_level_disabled`), not a limit.) |

Not in v0: phone, tailnet, pairing, push, hours, governor, policy file, async effects, handover, away strip, palette.

## 1. Concepts

The core has ten concepts.

| Concept | Kind | Definition |
|---|---|---|
| **Host** | data | A daemon on one machine. It owns one event log, the entities that reside on it, checkouts and processes. |
| **Entity** | data | A durable thing with an id, a kind, one residence host, a residence epoch, and its own event sequence. |
| **Ref** | data | A typed pointer to a thing that has its own state. Every Entity is also a Ref. |
| **Link** | data | A typed relation from a thread to a Ref. |
| **Document** | data | An immutable, typed, structured record. |
| **Event** | mechanic | An append-only record of one change. All state is a projection of events. |
| **Action** | mechanic | A named operation with typed parameters. It is the only way a principal changes state. |
| **Rule** | mechanic | `when Condition then ActionCall`, owned by an Entity. It is the only way time or outside state causes an Action. |
| **Job** | mechanic | A durable, resumable unit of side-effect work with a resource class. |
| **Attention** | derived | A pure function from an entity's projection to its display state and sort position. |

**Entity kinds.**
- The core defines three built-in kinds:
  - **Thread:** one unit of work.
  - **Decision:** a question to the user that blocks or steers work.
  - **Coordinator:** an agent that oversees threads within a signed grant.
- Modules register more kinds, for example `workflow`, `schedule` and `budget`.

**Conversation.** A component that Thread and Coordinator share. It holds the Agent sessions, the runs, usage, and handover. A **Session** is one agent session. A **Run** is one agent turn.

**Brief.** The Document schema `brief` (3.9).

**Module.** The unit of packaging (12). A module adds behaviour only through extension points.

**User.** A person with an account on a host (3.12). v1 has exactly one.

**View.** The state that belongs to one user's view of one entity: snooze, pin, read watermark, failure acknowledgement (3.12). It is not part of the entity's work state.

**Principal:**
- `user(userId, deviceId)`;
- `agent(entityId)`: the agent of a thread;
- `coordinator(entityId)`;
- `rule(ruleId)`;
- `system(component)`: the core, a Job or a module.

**Lineage root.** For a thread, the first entity in its `lineage` chain (itself if it has none). Quotas (5.2) and mode ceilings (5.2) are kept on the lineage root.

## 2. Architecture

- **Host daemon.** One per machine. It runs the core and the configured modules.
- **Clients.** A client connects to each host separately and shows each entity from its residence. Clients do not use the mesh.
- **Client origin.** The web client is served from one origin, `clientOrigin`, which is the same in every host's config. Hosts accept client connections only from that origin. Passkeys are registered for that origin.
- **Reachability.** Tailscale with HTTPS. The daemon is a tailnet node (16).
- **Host list.**
  - Each device stores `(hostId, url, deviceToken)` for each host.
  - The export contains `hostId` and `url` only. An import requires pairing again.
- **Offline host.**
  - Clients keep its last known state.
  - If no host is reachable, the client shows one banner for all hosts, not a marker per row.
  - Actions on an offline host's entities are unavailable with the reason "host offline". Captures go to the client outbox (13.1 U23).
- **Clocks.**
  - Each host stamps events with a hybrid logical clock (HLC) value `at`. `at` is used for display and for sorting across hosts.
  - Order within one entity uses `eseq` (3.2), never `at`.
  - A Rule's time Conditions use the wall clock of its owner's residence.

**Mesh (D3, post-v1).**
- **Pairing.** Every host pairs with every other host (`host.pairPeer`, 11.3).
- **Replication.** Each host subscribes to the `summary` stream of each peer and keeps a read-only replica of it. The stream holds:
  - the projections of replicated entities (without messages);
  - Links, Ref states and Document headers;
  - `level.evaluated` events;
  - the bodies of `grant` and `policy` Documents, pushed eagerly.
- **Document bodies.** A host fetches other Document bodies on demand with `document.get` from the Document's host.
- **Liveness.** Every peer sends a heartbeat on the summary stream every 10 s. A replica with no heartbeat for 30 s is **stale**. A Condition that depends on a stale replica is `unknown` (7.2).
- **Peer trust.** A peer is trusted to report its own residents' state truthfully. A peer is not trusted to assert a principal: a command from a peer carries the original principal and its proof (a grant signature, an approval proof, or a Job id whose `job.requested` event the receiving host can verify in the sender's replica). A host rejects a `coordinator(id)` command when its replica of the coordinator's residence is stale or the grant is not the head of its `supersedes` chain (K24).
- **Scale.** The target is N ≤ 6 hosts.

## 3. Data model

This section states the model as built. `internal/core` is the reference: when this text and the code disagree, the code is right and this text has a bug. Each subsection is built unless it is marked **Post-v1**. A post-v1 part is designed and kept here, and no code does it yet. Section numbers are stable IDs (`PROCESS.md`), so new subsections come after 3.12.

### Records

The state is a set of typed records (decision 0020, part 1). A record has one owner host (decision 0016): no record has two writers. Every change to a record goes through the one path of 5.1. Messages are in the event log and the conversation. They are not part of a record.

| Record | Holds | Stored | Key | Section |
|---|---|---|---|---|
| Thread | one agent conversation and its state that is not messages | `threads` (JSON) | thread id | 3.4 |
| View | one user's state of one thread | `views` | user and thread id | 3.4 |
| Change | one change a thread makes or asks for | inside its Thread | change id, key | 3.13 |
| Host change record | one observed git branch on the host | `changes` | change id; one per repo and branch | 3.14 |
| Decision | a question or approval for the owner | inside its Thread | decision id | 3.5 |
| Rule | an armed watch | inside its Thread, or the View (snooze) | rule id | 7.1 |
| Effect | work Fyr runs after a reduce | `effects` (durable effects only) | effect id | 5.10 |
| Receipt | the result of one command | `receipts` | command id | 5.1 |
| Project | a name, a colour and one folder | `projects` | project id | 3.15 |
| Provider | an agent CLI, an auth, an endpoint and models | `providers` | provider id | 3.16 |

**Events.** One table `events {seq, at, subject, type, data}` per host. `seq` increases per host; `subject` is the thread id (or the change id of a host record). A reduce appends its events in the same transaction as the record (5.1).

**Stored data keeps loading.** A thread or view stored in an older shape is migrated when it is read (`DecodeThread`, `DecodeView`): the PR on the thread (before #32), the settle fields (before #33), the actions' old places (before #31), `settle.when` and `deployWatch` (before #46), a snooze in `snoozeUntil` (before #46), and a landed change without `landedAt` (V0.70).

**Post-v1.** Coordinator (3.6), Document (3.9), Checkpoint (3.10), User presence per device (3.11), and the entity header with residence and epochs (3.2).

### 3.1 Identifiers

- **Thread id** `t_…`, **decision id** `d_…`, the agent's **watch id** `w_…`: random, made by the host.
- **Change id** `c_` and 12 hex digits of the FNV-1a hash of the change's key (`ChangeID`). A retried call or a second observer with the same key finds the same change.
- **Rule id** names its purpose: `settle:done`, `settle:<change id>`, `deployWatch:<change id>`, `builtins:<change id>`, `snooze`, or a watch id.
- **CommandId**: chosen by the caller. The host keeps one receipt per id (5.1 A4).
- **Effect id**: set by the engine when it stores the reduce that returned the effect. The result comes back with the source `effect:<id>` (5.10).

**Post-v1** (a mesh of hosts, 0.4):
- `HostId`: random 128-bit id.
- `EntityId`, `DocumentId`, `RuleId`, `JobId`: ulid. They are globally unique and do not depend on the host.
- `EventId`: `(HostId, seq)`. `seq` strictly increases per host, with no gaps. An EventId identifies an event. It is never used to order events of different hosts.
- Jobs derive their CommandIds as `H(jobId, step, index)` (8.1). Rule firings use `H(ruleId, firing)` (7.3).

### 3.2 Entity (Post-v1)

Built: a record has one owner host and its host's event log (decision 0016). A thread does not move between hosts; Hand over… stays on one host. The header below is the design for a later mesh, where an entity can move. Decision 0016 replaced the v1.2 mesh with replicas; this header stays as the plan for moves.

```ts
type EntityRef = { kind: string; id: EntityId };
type EntityHeader = {
  ref: EntityRef;
  residence: HostId;       // projected from `entity.resided` events
  epoch: number;           // increases by 1 at each residence change (10.4)
  eseq: number;            // the number of events with this entity as subject; gapless, travels across residences
  fenced: { to: HostId; epoch: number; moveId: string } | null;   // set by `entity.leaving` (10.4)
  replicated: boolean;     // from the kind's registration
  owner: UserId;           // R19; v1: the one user
  visibility: { private: true } | { shared: UserId[] };   // R19; v1: private
};
```

- **Subject.** Every event has at most one subject entity. The subject's `eseq` increases by 1 with each such event, on whichever host the entity resides.
- **Order within an entity.** "After" and "before" for one entity always compare `eseq`. The Rule `armed` snapshot (7.1), the read watermark (6.4) and lineage points all store `(EntityRef, eseq)`.
- **Residence.**
  - Only the residence host appends events with the entity as subject.
  - Commands that target the entity go to its residence. Other hosts reject them with "resides on <host>" (A7).
- **Highest epoch wins.** When two hosts claim residence, every host, peer and client trusts the higher `epoch`. I2 makes two claims at the same epoch impossible.
- **Entity kinds** are registered through the `EntityKind` extension point (12.2). The registration gives the projection reducer, `replicated`, `movable`, whether the kind has a Conversation, and the kind's lifecycle states.
- **Entities as Refs.** Every entity is also a Ref with `{kind: entity kind, key: id}`.

**Invariants**
- **E1.** An entity has exactly one residence at the highest epoch. While a move is in progress, the source host is still the residence, but it is fenced (10.4).
- **I1. Fence quiescence.** After `entity.leaving {epoch: e}` on host A, A appends no event with that subject except `entity.left` or `entity.stayed`. Observers and timers that would write such an event buffer it; the move bundle carries them (10.4). The bundle's final `eseq` equals the last `eseq` that A appended.
- **I2. One residence per epoch.** For each `(entity, epoch)` at most one host ever appends `entity.resided`. `entity.stayed {e}` requires an acknowledged abort from the target, which bars `entity.resided {e+1}` for that `moveId`.

### 3.3 Conversation (component of Thread)

The run state of the thread's agent session. Only observations from the agent write it (source `observer:process`, 5.1). Its fields are at the top level of the stored and the wire JSON.

```ts
type Conversation = {
  running: boolean; runStartedAt?: Time;
  activity?: string;               // the last tool or text, one line, present tense (V0.21)
  subagents?: number;
  summary?: string;                // the last assistant text, one line
  tasks?: Task[];                  // native subagents and background tasks (V0.16)
  failure?: { class: FailureClass; message: string; at: Time };
  runEnds?: number;                // runs that ended; `runEnded` Rules compare with it (7.2)
  outbox?: { id: string; state: "sent" | "queued" }[];   // messages the agent has not read (V0.33)
  handover?: { to: string; model?: string; at: Time };    // a move to another provider that runs (V0.45)
};

type Task = { id: string; description?: string; type?: string;
  status?: "running" | "completed" | "failed" | "stopped"; activity?: string; summary?: string;
  toolUses?: number; tokens?: number; startedAt?: Time; updatedAt: Time };

type FailureClass = "api" | "auth" | "mcp" | "process" | "restart" | "error";
```

- **Failure classes.** The agent manager reads `api`, `auth`, `mcp` and `error` from the CLI's result text; `process` is a process that ended or could not be written to; `restart` is 3.3 Restart. A failed deploy is no failure class: its rollback reaches the thread as the deploy effect's result (5.10).

- **Tasks.** A thread with no live run and at least one running task is `waiting` (6.1). When the process ends or the host restarts, running tasks become `stopped`.
- **Outbox.** A message is `sent` or `queued` while unread. `read`, `cancelled` and `lost` remove it from the outbox; the `message.state` event records them.
- **Restart (V0.6).** The Input `host.restarted` marks every message in the outbox `lost`, ends a handover with an error, and makes a live run or an open question `failed` with class `restart`. Effect Decisions stay open, because they do not need the process (5.10).
- **Handover (V0.45).** While `handover` is set, `thread.send` is unavailable ("Wait for the handover to finish").

**Post-v1** (sessions and runs as lists, usage and budgets, waits from admission and Rules, held inputs). The design:

```ts
type Conversation = {
  profile: string;                 // agent profile (12.4 claude); names its agent kind
  sessions: Session[]; runs: Run[];
  usage: { tokens: number; costUsd: number };
  budget: { tokens: number } | null;
  waits: Wait[];                   // the one source of `waiting` (6.2 row 7)
  heldInputs: HeldInput[];         // user sends held by a running handover (10.3)
  effects: EffectResult[];         // results of async effects not yet seen by the agent (5.10)
};

type Session = { id: string; started: number /*eseq*/; ended: number | null; processAlive: boolean;
  startReason: "new" | "handover" | "fork" | "move"; contextTokens: number; contextLimit: number };

type Run = { id: string; sessionId: string; trigger: "user" | "agent" | "coordinator" | "rule" | "system";
  status: "queued" | "running" | "completed" | "failed" | "interrupted";
  admitted: boolean;                              // projected from `run.admitted` (8.2)
  queued: number; ended: number | null;           // eseq
  lastProgressAt: number | null;                  // HLC of the last tool event or message
  checkpoint: { before: string; after: string | null } | null;   // git tree hashes (3.10)
  end: { class: EndClass; message: string; resetAt?: number } | null;
  subagents: { id: string; description: string; status: "running" | "done" | "failed"; summary: string | null }[];
  background: { id: string; kind: "command" | "monitor"; awaited: boolean; status: "running" | "done" }[] };

type EndClass = "ok" | "usage_limit" | "restart" | "paused" | "stalled" | "user" | "budget" | "moved"
              | "auth" | "api" | "mcp_startup" | "workspace_missing" | "admission_denied" | "error";

type Wait = { reason: string; source: "admission" | "rule" | "background" | "signal"; since: number };
```

- **Waits.** `waits` is the only input for the state `waiting`. It is filled by admission that returned `wait` (8.2), an armed Rule with purpose `wait` (7.5), an awaited background task, and a module Signal with state `waiting`.
- **Run end classes and attention** (I10). This table is total. 6.2 uses it.

| Status | Class | Attention after the run |
|---|---|---|
| completed | ok | done (row 9) |
| failed | auth, api, mcp_startup, workspace_missing, error, admission_denied | failed (row 3) |
| interrupted | user | seen (row 10); the user stopped it |
| interrupted | restart, budget, stalled | failed (row 3) |
| interrupted | usage_limit | waiting (row 7), through the wait Rule that the `claude` module creates at that run end (7.6) |
| interrupted | paused | waiting (row 7) while the governor's wait Rule is armed; failed (row 3) if no wait Rule is armed |
| interrupted | moved | the run is withdrawn and queued again on the target; no attention change |

### 3.4 Thread and View

A Thread is one conversation with one agent session in one folder, on one host. Its parts are components (#33): the Conversation (3.3), the Changes (3.13), the Actions (5.13), the Live view, the Settle and the Rules (7), and the Feed (5.14). It has no workspace, branch or repo field of its own: those belong to its code changes (decision 0019).

```ts
type Thread = Conversation & {
  id: string; title: string;
  titleBy?: "owner" | "agent";     // who named it last; the owner's name is kept (V0.69, 5.2)
  cwd: string;                     // the folder it runs in
  projectId?: string;              // 3.15
  projectManual?: boolean;         // filed by hand: folder matching never moves it (V0.55)
  owner: UserId; visibility: "private";   // R19; v1: the one user
  profile: string;                 // the provider id (3.16); the wire keeps the old name
  model: string; effort?: string;  // "" effort is the provider's default (V0.50)
  sessionId: string;
  branch?: string;                 // the folder's branch, read with git after each run
  createdAt: Time; updatedAt: Time;
  activityAt: Time;                // moves when something happens that the owner should look at
  decisions?: Decision[];          // 3.5
  changes?: Change[];              // the thread's own changes (3.13)
  refs?: RecordRef[];              // the host change records it shows (3.14)
  mainChange?: string;             // the code change whose PR the PR tab shows and the PR commands act on
  changesCheckedAt?: Time;         // the last change tool call (V0.62, 5.14)
  offers?: ActionSpec[]; runs?: ActionRun[];   // the thread's own actions (5.13)
  live?: LiveView;
  settle?: { at?: Time };          // when it settled; settle Rules decide when (7.6)
  rules?: Rule[];                  // 7.1
  feed?: Feed;                     // notices not delivered yet (5.14)
  archivedAt?: Time;
  source: "fyr" | "t3"; parent?: string;   // the thread whose agent started it (V0.22, V0.71)
  t3Id?: string;
};

type LiveView = {
  enabled?: boolean;               // the owner's switch (V0.37)
  pageId?: string; state?: object; // the page and its JSON state, at most 16 kB
  headline?: string;               // state.headline when it is a string
  updatedAt: Time; checkedAt: Time;   // checkedAt moves on every view_* call
  offers?: ActionSpec[]; runs?: ActionRun[];   // the view's actions (5.13); they go when the view is turned off
};

// Per user and thread (R19). v1 has one View per thread.
type View = {
  user: UserId; entity: string;    // the thread id
  seenAt: Time;                    // the read watermark: done while activityAt is after it (6.1)
  rules?: Rule[];                  // the user's Rules: the snooze (7.5)
  changesLook?: { head: string; at: Time; files?: Record<string, string>; viewed?: Record<string, string> };   // V0.65; not in the thread DTO
};
```

- **Lifecycle.** A thread is active, settled (`settle.at` set) or archived (`archivedAt` set). Settle and archive are owner commands (5.3); a settle Rule settles it too (7.6). Any message from the owner revives it: it clears the failure, the settle and the snooze, and counts as seeing the thread.
- **Snooze is a View state.** It is the View's snooze Rule (7.5), so it belongs to one user and never to the thread's work state.
- **Main change.** `mainChange` names the code change with the thread's main PR. `thread.dismissPR` keeps the change and marks it `dismissed`; a dismissed change is never the main change again (V0.36).
- **Asleep.** A settled or archived thread is asleep: its notices wait (5.14) and its agent's watches are suspended (7.4).

**Post-v1** (the entity design): `lineage {kind: fork | spawn, from, atEseq}`, `mode` and `modeCeiling` (I5), `actedOnBy` (a coordinator), and a View with `pinned` and `failureAck`. The invariants of the design, which the built model does not check:

- **L1 (I4).** A View's `snooze` is set if and only if its snooze Rule's status is `armed` or `suspended`. Built: the snooze is the Rule itself, so L1 holds by construction.
- **L2.** A blocking Decision entry (5.6) for a run requires that run to be current. When the run ends, that entry becomes `parked` in the same transaction.
- **L3.** An `archived` thread has no current run, no armed Rules and no `open` Decisions. Agent-principal Actions on an archived thread are unavailable. Its final `run.ended` cancels any Decision or Rule that was created in the archive window.
- **I3. Suspend reasons.** A Rule is `suspended` iff `suspendedBy` is not empty. Built: 7.4.
- **I5. Modes never widen down a lineage.** A thread's `modeCeiling` is at most its lineage parent's effective mode. The effective mode of a run is `min(thread.mode, thread.modeCeiling, ceiling of the triggering principal)`. Only a `user` with `confirm` raises a ceiling (`thread.setMode`). The order is `plan < ask < auto`.

### 3.5 Decision

A Decision is a question or an approval that waits for the owner. It lives in its thread's `decisions`.

```ts
type Decision = {
  id: string; kind: "ask" | "approval" | "effect"; assignee: UserId;
  questions?: Question[];          // ask: AskUserQuestion, with options
  tool?: string; input?: unknown;  // approval: a tool call that waits for permission
  agent?: string;                  // the subagent that asked, when known
  effect?: { id?: string; action: string; args: object; reason?: string };   // effect: Fyr runs it once approved (5.10)
  createdAt: Time;
};
```

- `ask` and `approval` hold the agent's run. They die with the process; after a restart the thread is `failed` with class `restart` and Retry asks again.
- `effect` does not hold the run and survives a restart (5.10).
- Only the owner answers (5.6).

**Post-v1** (D1: a Decision as its own entity, with blockers, merging, defaults and deadlines):

```ts
type Decision = EntityHeader & {
  kind: "approval" | "question" | "plan" | "proposal";
  style: "async" | "blocking";           // 5.10; only approvals can be blocking
  assignee: UserId;                      // R19; v1: the one user. Only the assignee answers (delegation is post-v1); a `verified` proof comes from the answering user's passkey
  blocks: Blocker[];                     // threads, a coordinator, a Job or a module entity
  requestedBy: Principal[];              // the principal chain that led here, shown on the card (K14)
  dedupKey: string;                      // for approvals the core sets it to payloadHash (I8)
  subject: ActionCall | ToolCall | Question[] | PlanBody | ActionCall[];
  payloadHash: string;                   // H(kind, subject)
  level: AssuranceLevel;
  policyVersion: DocumentId;
  default: { answer: unknown; at: number } | null;
  deadline: number;
  status: "open" | "answered" | "defaulted" | "expired" | "cancelled";
};

type Blocker = { entity: EntityRef | null; job: JobId | null; runId: string | null;
  state: "open" | "parked" | "resolved" };
```

- **Residence.** A Decision resides with the host of its first blocker. A move of that entity detaches it (10.4).
- **I12. One blocker per run.** A Blocker with `state: open` blocks at most one run. L2 acts on the Blocker, not on the whole Decision.
- **I8. Answer binding.** An answer or allowance applies only to Decisions whose `payloadHash` equals the hash that was shown and, for `verified`, signed. Merged or grouped approvals have an equal `payloadHash`.

### 3.6 Coordinator (Post-v1, D4)

```ts
type Coordinator = EntityHeader & Conversation & {
  name: string;
  grant: DocumentId;               // Document of schema "grant", signed (5.8)
  plan: DocumentId | null;
  cursors: Record<HostId, number>; // last summary seq digested per host
  status: "running" | "ready" | "stopped" | "failed";   // ready = no current run; a stop the user did not cause is `failed` (K18)
  acknowledged: number;            // eseq up to which the user has seen the coordinator in its pane
};
```

- A Coordinator has a Conversation but no checkout and no lifecycle.
- It is not a row in the inbox, except when its attention state is `asking` or `failed` (6.3). Its own pane is described in 13.4.
- Its tools are only the Actions its grant allows, plus read tools. It has no edit, write or shell tools.

### 3.7 Ref and Link (Post-v1)

Not built. The built Thread field `refs` is a different thing: a reference to a host change record (3.14). What a thread works on is its changes (3.13).

```ts
type Ref = { kind: string; key: string };
type Link = { thread: EntityId; role: "serves" | "owns" | "touches"; ref: Ref; source: Principal | "observed" };
```

| Kind | Key | State |
|---|---|---|
| `checkout` | `${HostId}:${path}` | git module |
| `worktree` | `${HostId}:${path}` | git module |
| `document` | DocumentId | none |
| `checkpoint` | `${repo}:${treeHash}` | none |
| `person` | email | none |
| `sink` | `${moduleId}/${name}` | none |

- **Module Ref kinds.** The `github` module adds `repo`, `pr`, `issue`, `branch` and `file`.
- **Canonical keys.** A Ref key is canonical.
- **Per-host state.** Ref state is a per-host cache. Two hosts can disagree for up to one poll interval.
- **Lineage scope.** A RefKind registration may set `lineageScope: true`. Threads that `serve` the same Ref of such a kind count as one lineage (5.2).

| Link role | Meaning | Source |
|---|---|---|
| `serves` | The thread exists for this goal. | observed, agent, coordinator, user, system |
| `owns` | Exclusive use. | observed: worktree (or the checkout if no worktree), and branch |
| `touches` | Changed this file on this branch. | observed after each run |

### 3.8 Event

Built: an event is `{seq, at, subject, type, data}` (Records, above). A reduce writes the events of its Input; the Input's source says whether it was the owner, the agent, an observer, a Rule or an effect's result (5.1).

**Post-v1** (the entity design):

```ts
type Event = { id: EventId; at: HLC; source: "intent" | "observation";
  subject: EntityRef | null; eseq: number | null;     // set together
  affects: EntityRef[];                               // entities whose projection or attention must be recomputed
  type: string; data: unknown;
  cause: { command: CommandId } | { job: JobId } | { rule: RuleId; firing: number } | null };
```

- **Observed values.** An observation event stores the value it observed. Projections, Rules and Attention are therefore deterministic over the log.
- **Subjects that change residence.** On a move, the new residence copies the entity's earlier events into a read-only `imported_events` table, keyed by EventId and ordered by `eseq`.
- **Retention.** Events are kept forever in v1.

### 3.9 Document (Post-v1)

```ts
type Document = { id: DocumentId; schema: string; host: HostId;
  about: { entities: EntityRef[]; from?: number; to?: number };   // eseq range of about.entities[0]
  body: unknown; createdBy: Principal; supersedes: DocumentId | null;
  signature: WebAuthnAssertion | null };
```

- **Immutability.** A Document is immutable. To change one, create a new Document with `supersedes` set.
- **Id.** `document.create` takes an optional `key`. The Document id is then `H(about.entities[0], schema, key)`, so a retry does not create a second Document (10.3).
- **Replication.** Document headers are replicated. Bodies are fetched on demand, except the bodies of `grant` and `policy` (2).
- **Moves.** A move carries the bodies of the Documents `about` the entity and those given to it (10.4).
- **Schemas.** Modules register schemas.

**Core schemas**

```ts
type Claim = { text: string; evidence: { event: EventId } | { commit: string } | { command: string; exit: number; event: EventId } | "unverified" };

type BriefBody = {
  goal: string;
  state: { residence: HostId; checkout: Ref | null; branch: Ref | null; pushed: boolean; links: Link[] };
  decisions: string[]; done: Claim[]; open: string[]; next: string;
  asks: string[];
  excerpts: { eseq: number; role: "user" | "assistant"; text: string }[];   // max 8, verbatim
};

type DelegationBody = { objective: string; output: { schema: string }; boundaries: string[];
  owns: Ref[]; doneWhen: string[]; budget: { tokens: number } };

type PlanBody = { goal: string; steps: { text: string; status: "todo" | "doing" | "done" | "dropped" }[]; notes: string };

type PolicyBody = { version: number; rules: PolicyRule[]; defaultsByMode: Record<"plan" | "ask" | "auto", AssuranceLevel>; shadow: boolean };
type PolicyRule = { match: Matcher; level: AssuranceLevel; note: string };

type GrantBody = { coordinator: EntityId;
  scope: { repos?: string[]; goals?: Ref[]; all?: true };
  hosts: HostId[]; actions: string[]; modeCeiling: "plan" | "ask" | "auto";
  spawn: { concurrent: number; perHour: number }; budget: { tokensPerDay: number }; expiresAt: number };
```

### 3.10 Checkpoint (Post-v1)
Before and after each run, the `git` module writes the worktree's full tree to `refs/fyr/checkpoints/<entityId>/<runId>/{before,after}`. It uses a temporary index. These checkpoints make `thread.fork` at a run, rollback and replay possible. Built: the Changes tab reads git directly (V0.25, V0.65).

### 3.11 User presence
Built (V0.20, V0.54): a client that shows a thread while its page is visible sends `POST /api/presence {threadId}` every 20 s; the front counts the thread as watched for 45 s and sends no push for it.

**Post-v1** (presence per device):
- **Per device.** Each client reports `user.presence(state: "active" | "inactive" | "away")` for its own device to every connected host. A client reports at most once a minute, and on each change.
- **Projection.** Each host projects the last report of every device, grouped by user.
- **Use.** Sinks receive the presence of every device of the user concerned (12.2). The `push` Sink uses it (6.3). The away strip uses the user's own last `active` time (13.1 U13).

### 3.12 Users, ownership and views (R19)
v1 has one user (`owner`). The fields below exist from day one, filled with that user's id, so a later multi-user host needs no migration.

- **Built.** Every thread has `owner` and `visibility` (`private`). The View is keyed by user and thread (3.4). Decisions have an `assignee`.
- **User identity (post-v1).**
  - `UserId`: ulid. A host stores its users; v1 creates one at first start.
  - Devices, passkeys, push subscriptions and device tokens belong to a user.
  - The principal is `user(userId, deviceId)`. Every event caused by a user records the `userId` in its `cause`.
- **Ownership and visibility.** A user sees an entity when they own it or it is shared with them.
- **Work state and view state.**
  - Work state is on the thread and shared: runs, Decisions, changes, lifecycle (`active`, `settled`, `archived`).
  - View state is per `(userId, threadId)` (the View, 3.4): the read watermark, the snooze, the Changes tab's look and Viewed marks.
- **Attention per user.** `attention(thread, view, now)` takes the user's View (6.1).
- **Decisions.** Only the assignee may answer; delegation to another user is post-v1.
- **Providers and credentials (post-v1 for several users).** Providers, the Claude account, the GitHub identity and the tokens that sessions get belong to a user. A session uses the provider and credentials of the thread's owner.
- **Host roles (post-v1).** `admin` configures modules, the host policy and slots; `user` may add their own policy layer, which can only raise levels. v1: the one user is `admin`.
- **Fairness (post-v1).** The governor counts slots per user within the host limits (8.3).
- **Not covered.** Isolation between users at the OS level (separate Unix users, containers) is an open question (Q8). Until it is solved, a multi-user host trusts all its users at the OS level.

### 3.13 Change

A Change is one change a thread makes or asks for (decision 0018): a git branch, a PR, a server change, a migration, a design or a document. A thread holds at most 50 changes; an ended change older than 30 days goes first.

```ts
type Change = {
  id: string;                      // ChangeID(key) (3.1)
  key: string;                     // the agent's stable name, or Fyr's: "pr:<repo>#<n>", "git:<repo>#<branch>"
  aliases?: string[];              // other keys that find it (V0.70)
  kind: "code" | "infra" | "host" | "data" | "doc" | "design" | "other";
  title: string; targets?: string[];   // "repo:github.com/o/r" names its repo
  branch?: string;
  revisions?: Revision[];          // the 20 newest
  phase: Phase; phaseBy: "agent" | "observed" | "owner";
  label?: string;                  // a kind's own word: "planned", "patch set 3"
  conditions?: Condition[];
  evidence?: { kind: string; url?: string; attachment?: string; title?: string }[];
  group?: string; after?: string[];    // a group of changes; the changes it builds on
  needs?: "owner";
  pr?: PR;                         // the PR snapshot GitHub last reported
  dismissed?: boolean;             // the owner put the merged or closed PR away (V0.36)
  offers?: ActionSpec[]; runs?: ActionRun[];   // its actions (5.13)
  annotations?: Annotation[];      // failed checks' annotations while the PR is open (V0.66)
  landedAt?: Time;                 // when it first reached landed (V0.70)
  workspace?: Workspace;           // where its git facts are read (3.14)
  observed?: string;               // the host record it follows (3.14)
  createdAt: Time; updatedAt: Time;
};

type Phase = "working" | "review" | "landed" | "live" | "abandoned" | "reverted";
type Condition = { type: string; status: "ok" | "fail" | "pending"; reason?: string;
  source: "agent" | "observed" | "owner"; at: Time };
type Revision = { id: string; summary?: string; url?: string; by: string; at: Time };
type PR = { url: string; repo: string; number: number; title?: string;
  state: "OPEN" | "MERGED" | "CLOSED" | "MISSING"; checks: "pass" | "fail" | "pending" | "none";
  review?: string; conflict?: boolean; branch?: string; head?: string;
  mergeSha?: string; mergedAt?: Time; checkedAt?: Time };
```

- **Phases.** `working`, `review`, `landed` and `live` are in order; `abandoned` and `reverted` end a change. A change is *in flight* in working and review. "Reached" compares the order: a live change has reached landed.
- **Opening.** `change_open` with a key the thread has returns that change and fills only empty fields, so a second call never undoes the first. A new `code` change with a branch joins the thread's in-flight code change on that branch in the same repo (the same `repo:` target; when either has none, the branch decides); the new key becomes an alias (V0.70).
- **Revisions.** A new revision sets every condition that the owner did not set back to `pending`: they were about the old revision.
- **Conditions and their sources.** A condition is one fact with the principal that set it. Fyr observes `checks`, `review`, `conflict` (GitHub), `pushed`, `clean` (git), `deploy` and `deploy:<env>` (GitHub Deployments) and `action:<id>` (a script action's result). The agent may report any other type. A value that Fyr observed is kept against a report from the agent ("facts beat claims"), and an agent cannot set `review` or `approval` to ok. Both are rows of the field rights (5.2).
- **The PR.** Every PR of the thread is a `code` change, found by its URL, else by its head branch, else made with the key `pr:<repo>#<n>`. Open is review, merged landed, closed abandoned; a merged change that is live stays live. `MISSING` is Fyr's own state: GitHub answered twice, a poll apart, that the PR or its repository does not exist (GraphQL "Could not resolve to a Repository" or "… to a PullRequest", or HTTP 404); the change is abandoned (observed) with the label "the PR does not exist on GitHub", and no change is made for it. A transient error (network, rate limit, 5xx, login, time-out) changes nothing (V0.72). While the PR is open, `checks`, `review` and `conflict` follow it; when it merges or closes they are dropped. The poller reads the PR of every change in flight (5.10, `pr.observed`).
- **Ending.** An observer ends a change (a PR closed or missing, a branch gone), the agent reports its end, or the owner ends it with `change.end` (5.3): abandoned by default, or reverted, with the reason as its label. An ended change is not polled, offers no actions, and blocks nothing. The owner's end is kept against the agent and the facts (5.2); more work is a new change with another key (V0.72).
- **Blocking.** A change blocks the thread (6.1 blocked) when it is not ended and has a failed condition, or `needs: owner`. On a landed or live change, only a failure recorded at or after `landedAt` blocks: a failure before the landing is history (V0.70). The thread shows the reason of the newest blocking change: "<title>: <reason>".
- **Live.** When a change's PR merges, the deploy watch Rule (7.6) reads GitHub Deployments of the merge commit: production `success` makes the change `live` (observed); `failure` sets `deploy` to fail (V0.66).

### 3.14 Host change records and workspaces

An observed git branch is a host record (#35, decision 0018 addendum): one Change per repo and branch on the host, stored once in the table `changes` with its own events. Agent changes stay in their thread.

```ts
type Workspace = { repo?: string; path?: string; branch?: string; base?: string;
  madeByFyr?: boolean };           // Fyr made the worktree and removes it when the change ends (#44)

type RecordRef = { id: string; repo?: string; branch: string; ended?: boolean };   // a thread's `refs`
```

- **Observed facts.** After each run, the effect `git.observe` reads the thread's folder when it is a checkout, the workspace of each in-flight change, and whether the branch of each in-flight record the thread shows merged or went. Each read is an Input of the record (`git.observed`, `git.ended`) with source `observer:git`.
- **A record.** The first facts of a repo and branch with commits ahead, unpushed commits or changed files make a record (key `git:<repo>#<branch>`, phase working). Its conditions are `pushed` and `clean`, and a revision for the head commit. It lands when the branch is merged into its base (the head is an ancestor of `origin/<base>`, or a PR of the branch merged) and is abandoned when the branch is gone without a merge. New work on the branch after that is a new record (`key~2`).
- **References.** A thread holds a `RecordRef` for each record it shows: the thread whose run observed it and every thread whose folder is in its checkout. A record that moves is sent to each of them as the Input `change.observed`, so each thread gets its own notice (5.14).
- **Following.** The thread's code change on the record's repo and branch follows the record: it keeps its own id, key, PR and actions, and Fyr copies the record's workspace, `pushed`, `clean`, newest revision and end onto it (`observed` names the record). The thread then shows its change instead of the record.
- **Several worktrees per thread.** A worktree belongs to a code change (`workspace`), never to the thread (decision 0019). A thread holds any number of changes, so it holds any number of worktrees. Making worktrees is not built (#44).

### 3.15 Project

A project is a name, a colour and one folder on its host (V0.55, decision 0017), with defaults and quiet hours. It lives on the host of its folder; the front stamps `host` on it.

```ts
type Project = { id: string; name: string; color: string; icon: Icon;
  image?: string;                  // the version of the image icon (V0.116); the icon is then the glyph's fallback
  by?: { name?: string; color?: string; image?: string };   // "owner", "agent" or "" (a placeholder) (V0.116)
  setup?: { thread?: string; at: Time; error?: string };    // the last setup run (V0.116)
  folder?: string;                 // a full path on this host; "" means threads are filed by hand only
  profile?: string; model?: string;    // defaults for a new thread
  quiet?: { from: string; to: string; days?: string[] }[];   // quiet hours (V0.46), at most 14 windows
  edited?: boolean; createdAt: Time };
```

- **Matching.** A thread not filed by hand lands in the project whose folder is the thread's folder or contains it; the longest folder wins, ties by project id. Matching runs when a thread is made or adopted, at start, and after every project Input. It never takes a project away, except from a thread whose project was deleted, which also loses its filed-by-hand mark (V0.58). A thread filed by hand keeps its project. Each thread that matching moves gets the Input `project.matched` (source `observer:host`), which keeps its `updatedAt`.
- **Commands.** `project.create {name, folder?, threadId?}`, `project.update {id, name?, color?, icon?, image?, folder?, profile?, model?, quiet?}` (`image: ""` removes the image icon, V0.116), `project.delete {id}` (V0.58), `project.setupRun {id}` (V0.116); the agent's `project.setup {id, name?, color?, image?}` through the tool `project_setup` (V0.116). They are Input kinds of the project record on the one path (5.1): the engine checks the folder on the host, reduces the project, then reduces the thread Inputs it causes (the thread `project.create` files, as `thread.setProject`; the threads matching moves) and stores all of it with the receipt in one transaction (#48).
- **Setup (V0.116).** An agent run fills the fields the owner has not set (`by`): the image icon, the colour from it, the name. The owner's value is kept (the rights rows `project.name`, `project.color`, `project.image`).
- **Quiet hours.** While a window holds, the thread DTO carries `quietUntil`; the front sends no push for the thread and the client hides it from the inbox (V0.46). Quiet hours are computed, not a Rule (7.6, decision 0020).

### 3.16 Provider

A provider is an agent CLI adapter, an auth, an endpoint and the models it offers, kept per host in the table `providers` (V0.50, decision 0015). The thread's `profile` field holds the provider id.

```ts
type Provider = { id: string; name: string; color?: string;
  cli: string;                     // the adapter: "claude" is the only one
  auth: { kind: "cli-login" | "api-key" | "none"; configDir?: string; secret?: string };
  endpoint: { baseURL?: string; modelEnv?: object };
  keyRef?: string;                 // "env:NAME" or "file:/path": an imported key, until the owner stores one
  models: { id: string; name?: string; efforts?: string[] }[];
  defaultModel?: string; defaultEffort?: string;
  tokenLogin?: boolean;            // a cli-login that logs in with its stored token only
  quotaKind?: string;              // "" picks by auth and endpoint (V0.44)
  imported?: string; createdAt: Time };
```

- **Secrets.** A provider names a secret; the secret store (`internal/secrets`) keeps the value in a file with mode 0600. A value never appears in the database, a DTO, a reply, a log line or an error; clients see `keySet` and the last 4 characters.
- **Quota.** Quota is keyed by the auth identity (the login folder, or the key and endpoint), so two providers on one account share one reading (V0.44, V0.50 (5)).
- **Status.** Ready, needs a login, or needs a key. A provider that cannot run carries `unavailable` with the reason; pickers show it disabled.
- **Login (V0.53).** `provider.login`, `provider.loginCode` and `provider.loginCancel` run the CLI's own login on the host, at most one per provider, for at most 10 minutes.
- **Commands.** `provider.import`, `.create`, `.update`, `.setKey`, `.login`, `.loginCode`, `.loginCancel` and `.delete` are host Input kinds on the one path (5.1): the reduce returns the effect `provider`, which the registry runs before the engine answers, and the engine keeps the receipt (#48).
- **Model and effort.** A thread holds provider, model and effort. A thread that another thread starts takes the starting thread's provider, model and effort unless the call names them (V0.71 (1)).

### 3.17 Host and front

`fyrd` runs as a host, a front, or both (V0.51, decision 0016).

| Role | Does | Keeps |
|---|---|---|
| Host | runs threads; serves the host protocol and `/mcp` | its records: threads, views, changes, projects, providers, keys, effects, receipts |
| Front | serves the web client; follows each host in its list; sends each command to the host that owns the record; sends Web Push | device data: push keys and subscriptions, presence, and a cache of each host's last state (`<data>/front/<host>.json`) |

- **Routing.** The front keeps a map from thread id to host. A thread call goes to the thread's host; a project command to the project's host; a call without either to `args.host` (or `?host=`), else the first host.
- **An offline host.** The front shows the host's threads from its cache, marked `hostOffline`, read-only. Commands for them get 503.
- **Trust.** On the tailnet, a host serves only peers whose Tailscale identity is the owner's login (V0.51 (5)).

## 4. Events

### 4.1 Core event types

| Group | Types |
|---|---|
| Entity | `entity.created`, `entity.leaving`, `entity.resided`, `entity.left`, `entity.stayed`, `entity.moveAborted` |
| Thread | `thread.titled`, `thread.summarized`, `thread.pinned`, `thread.unpinned`, `thread.visited`, `thread.markedUnseen`, `thread.failureAck`, `thread.lifecycle`, `thread.mode` |
| Conversation | `session.started`, `session.ended`, `session.process`, `session.context`, `run.queued`, `run.admitted`, `run.started`, `run.message`, `run.progress`, `run.ended`, `run.withdrawn`, `input.held`, `input.released`, `effect.result`, `effect.delivered`, `wait.added`, `wait.removed`, `usage` |
| Decision | `decision.opened`, `decision.merged`, `decision.blockerParked`, `decision.answered`, `decision.defaulted`, `decision.expired`, `decision.cancelled`, `decision.outcome` |
| Coordinator | `coordinator.digest`, `coordinator.status`, `coordinator.acknowledged` |
| Link, Ref | `link.added`, `link.removed`, `ref.state` |
| Rule | `rule.created`, `rule.fired`, `rule.suspended`, `rule.resumed`, `rule.disarmed` |
| Job | `job.requested`, `job.started`, `job.step`, `job.attempting`, `job.ended`, `job.acknowledged` |
| Document | `document.created` |
| Policy | `level.evaluated` (every gated call: call hash, principal, level, policy version, shadow levels) |
| Host | `host.started`, `peer.state`, `resource.level`, `timer.fired`, `user.presence` |

- **`decision.answered`** holds the answer, the device, `latencyMs`, `policyVersion`, the level and a context snapshot.
- **`decision.outcome`** is written later by Observers. Its values are `merged`, `closed`, `reverted`, `reasked` and `failed`.
- **`rule.fired`** holds the firing number, the CommandId and the outcome (7.3).

**Derived notifications.** These are not events. The host computes them after each commit and passes them to Sinks and clients:
- `attention.entered {entity, section, state}`;
- `decision.opened`;
- `deferred.rejected {action, reason}` (7.5), shown as a toast.

### 4.2 Activity events
`run.queued`, `run.ended`, `decision.opened` and `decision.answered` update `lastActivityAt`. No other event does.

## 5. Actions

This section states how the state changes, as built (decision 0020). One path takes every change: an Input goes to one pure reducer, the engine stores the result in one transaction, and Fyr runs the effects afterwards. Subsections marked **Post-v1** are designed and not built. The owner's commands (5.3), the agent's tools (5.12), the observers, the Rules (7) and the results of effects (5.10) are all Inputs.

### 5.1 Inputs and the one path

```ts
type Input = {
  source: "owner" | "agent" | `observer:${string}` | `rule:${RuleId}` | `effect:${EffectId}`;
  kind: string;                    // an Input kind (below)
  thread: string;                  // the thread it changes; a project kind names its project in args.id
  args?: unknown;                  // a map for commands and tools; a typed value for observers and results
  cmdId?: string;                  // the CommandId: one receipt per id (A4)
  from?: string;                   // agent: the thread whose agent sent it to another thread (5.12)
};

type InputKind = {
  name: string;
  on: "thread" | "project" | "host";                   // the record it changes; host: none (below)
  from: ("owner" | "agent" | "observed" | "rule")[];   // the principals that may send it
  available?: (thread, view, now) => "" | reason;      // the guard
  reduce: (records, input, now) => { events, effects, follow, reply } | Rejection;
  makes?: boolean;                 // it makes its record (a new project, an adopted thread)
  await?: boolean;                 // the reply waits for its effects (below)
};

type Rejection = { code: "conflict" | "badArgs" | "notFound" | "rights" | "unavailable"; reason: string };
```

- **Sources.** `owner` is the owner's command. `agent` is the thread's agent through a Fyr tool. `observer:<name>` is a fact: `process` (the agent's stream), `github`, `deployments`, `git`, `host` (a restart), `clock`. `rule:<id>` is a Rule's firing (7.3). `effect:<id>` is the result of an effect Fyr ran (5.10).
- **Principal.** The source writes as one principal in the field rights (5.2): `owner`, `agent`, `rule`, or `observed` for an observer and an effect's result. An effect's result is a fact Fyr observed of its own work.
- **The reducer.** `Reduce(thread, view, input, now)` is pure. It checks that the source may send the kind, runs the kind's guard, and applies the kind's reducer to copies of the thread (with its changes and Rules) and the owner's View. Then, on the same copies, it runs the Rule scheduler (7.3) and the agent feed's notice rules (5.14). It returns the new records, the events, the effects and the reply, or a Rejection. Nothing is stored on a rejection.
- **The engine's path.** One function takes every Input: replay the receipt when `cmdId` has one; load the records; reduce; store the records, their events, the receipt and the durable effects in one transaction; publish the thread to the clients; start the effects. A thread that the reduce leaves unchanged is not written and keeps its `updatedAt`.
- **Host change records** have their own pure reducer (`ReduceRecord`, 3.14) and the same path: a record that moves becomes one `change.observed` Input per thread that shows it.
- **Projects** have their own pure reducer (`ReduceProject`, 3.15) and the same path: replay the receipt, reduce the project, reduce the thread Inputs it causes (`follow`, and matching), store it all in one transaction, broadcast the project and the threads.
- **Host kinds** change no record of the state: the provider commands (3.16) and `t3.adopt` (V0.9). `ReduceHost` checks the sender and returns the kind's effect.
- **Await.** A kind whose reply needs its effect's outcome (`thread.cancelMessage`, which needs the CLI's answer before it can say the agent never sees the message (V0.33); `t3.adopt`; `provider.*`) is reduced and stored without its receipt; the engine runs its effects in order, then stores the receipt with the outcome. A failure that may pass on a retry (409, 5xx) stores no receipt. One lock covers the receipt check, the work and the receipt.
- **Adopted threads.** T3 adoption sends each thread as the Input `thread.adopted` (source `observer:t3`), which makes the thread with its view and its history at the times it happened, and keeps T3's `updatedAt`.
- **A new feature** is a record field, an Input kind, a reducer case, and perhaps a Condition kind (7.2) and a notice rule (5.14). It never needs a new path for state, effects or telling the agent.

**Rules**
- **A1.** Every event comes from a reduce.
- **A2.** A reduce is pure. All work is an effect (5.10).
- **A3.** Every surface reaches the state through the one path: the client's keys, buttons, menus and bulk selection; the agent's tools; the observers; the Rules; the effect runner. A surface may hide a command. It may not change its behaviour or its availability.
- **A4 (I7). Receipts.** The host keeps one receipt per CommandId, with the terminal result. A retry with the same id gets the stored result and changes nothing. A guard rejection (`unavailable`) stores no receipt, so a retry is tried again.
- **A5.** A rejection returns the reason from the guard or the reducer. Every surface shows that reason. The HTTP status follows the code: `badArgs` 400, `rights` 403, `notFound` 404, `conflict` and `unavailable` 409.
- **A6. Firings in the same transaction.** A Rule's `then` is an Input that the scheduler reduces on the same copies, in the same transaction (7.3). It is checked like any Input: the kind must accept `rule`, and its guard must pass.
- **A7 (Post-v1).** A command whose target is an entity goes to the entity's residence. A host that is not the residence rejects it. A fenced entity (10.4) rejects all commands except the move steps, with the reason "moving". Built: the front sends each command to the host that owns the record (3.17).

**Post-v1** (the Action design this path replaced): `Action<P> {params, target, callers, defaultLevel, effect, restoresVisibility, available, apply}` with nested calls of depth 4, applied in one transaction, and a whole call tree that becomes one approval Decision when any call needs a level above `none` (K13).

### 5.2 Field rights and limits

Each field a reducer writes on behalf of a sender names who may write it (decision 0020 (2)). "Facts beat claims" (decision 0018) and "the agent never marks a review or an approval ok" are rows of one table (`Rights`), not code in each tool.

| Field | Who may write it | Kept against | Refusal |
|---|---|---|---|
| `thread.title` | owner, agent | a title the owner gave is kept against the agent | "the owner named this thread; ask them to rename it" (V0.69) |
| `thread.project` | owner | | "only the owner files this thread in a project" (`projectId` of `thread.create`, `thread.setProject`) |
| `thread.view` | owner | | "only the owner turns the live view of this thread on or off" (`view` of `thread.create`, `thread.setView`) |
| `view.seen` | owner | | an agent's Input leaves the owner's `seenAt` as it was (`thread.send` revives the thread without marking it seen) |
| `change.phase` | owner, agent, observed | a phase Fyr observed is kept against the agent | "Fyr observes the phase of …; a report cannot change it" |
| `change.phase` | owner, agent, observed | the owner's end is kept against the agent and the facts (V0.72) | "the owner ended the phase of …; open a new change with another key" |
| `change.condition` | owner, agent, observed | a condition Fyr observed is kept against the agent | "Fyr observes …; a report cannot change it" |
| `change.condition.review=ok` | owner, observed | | "an agent cannot mark "review" ok; ask the owner with change_request" |
| `change.condition.approval=ok` | owner, observed | | the same for `approval` |

- **Several rows of one field** all apply: a write passes only when every row allows it.
- **Who may send a kind** is the kind's `from` (5.1): the owner's commands accept only the owner, except `thread.create` and `thread.send`, which another thread's agent sends too (5.12); the agent's tools only the agent; facts only observers and effect results; a Rule's `then` only kinds that accept `rule`.
- **Built limits.** At most 50 changes per thread (3.13), 12 actions per holder and 10 runs kept (5.13), 20 watches by the agent per thread (7.1), 8 Rule firings per Input (7.3), 50 queued notices (5.14). A thread's agent may start at most 5 threads an hour and send at most 30 messages an hour to other threads (V0.22).

**Post-v1** (several principals and quotas):
- `coordinator(id)`: an Action only if its valid grant (5.8) allows the Action, every target entity and the host.
- `system(component)`: all Actions. A level above `none` opens a Decision.
- **Modes (I5).** A thread started by an agent, a coordinator, a Rule or a Job gets `modeCeiling = min(parent's effective mode, the requested mode)`.
- **Guards from modules.** A module may contribute Guards (12.2). The guard result of every Action is the Action's own check AND every Guard's check. Guards do not apply to Actions that restore visibility (`thread.wake`, `thread.unsettle`, `thread.unarchive`), so a Guard can never keep a thread hidden (K7).
- **Lineage quotas (I6).** Every thread start, every armed Rule and every token is charged to the lineage root: 10 thread starts per hour, 5 armed Rules created by agents, the root's `budget`.
- **Firing limits.** A Rule fires at most once per 10 s and 100 times per day.
- A thread that a coordinator started cannot create a coordinator.

### 5.3 Owner commands

The owner's commands are the Registry (`Ordered()` gives the list clients show). Each is an Input kind that only the owner sends, with its guard as `Available`. A command that changes only the View still marks the thread updated.

| Command | Args | Unavailable when (reason) | Does |
|---|---|---|---|
| `thread.create` | cwd, text, attachments?, images?, title?, projectId?, profile?, model?, effort?, view?, parent? | | Makes the thread; files it by hand when `projectId` is given; sends the first message. Without `cwd`, the engine uses the project's folder or makes a scratch folder (V0.42). |
| `thread.send` | text, attachments?, images?, from? (the surface: `view`, `view-typed`) | archived; a question is open ("Answer the open question first"); a handover runs | Revives the thread (3.4) and sends the message. |
| `thread.cancelMessage` | messageId | the message was read ("The agent has already read this message") | Withdraws a message the agent has not read; replies after the agent confirmed (V0.33, Await 5.1). |
| `thread.answer` | decision, answers or allow | nothing to answer | Answers a Decision (5.6). |
| `thread.interrupt` | | no run | Stops the run; waiting messages stay queued (V0.33). |
| `thread.retry` | | the last run did not fail; archived | Revives the thread and asks the agent to continue from where it stopped (V0.6). |
| `thread.dismiss` | | no failure | Clears the failure. |
| `thread.dismissPR` | | no PR; the PR is still open | Marks the main change `dismissed` (V0.36). |
| `thread.settle` | now? | already settled; it settles when the run ends; archived | 13.1 `e`: while a run is live, arms "settle when done"; idle with an open main PR, arms "settle when merged"; else settles now. `now: true` (bulk) settles now unless a run is live. |
| `thread.unsettle` | | not settled and no settle Rule | Clears the settle and every settle Rule, the agent's too. |
| `thread.settleWhenMerged` | | no open PR; already set | Arms the settle Rule on the main change reaching landed (13.1 `m`). |
| `thread.snooze`, `thread.unsnooze` | until | settled; not snoozed | Arms or drops the View's snooze Rule (7.5). |
| `thread.archive`, `thread.unarchive` | | already archived; a run is live ("Stop the run first"); not archived | Sets or clears `archivedAt`. |
| `thread.markSeen`, `thread.markUnseen` | upTo? | | Moves the View's `seenAt` (6.4). |
| `thread.setProject` | projectId | | Files the thread by hand; `""` is No project. |
| `thread.rename` | title | | Renames; `titleBy` becomes owner (V0.69). |
| `thread.setView` | on | archived | Turns the live view on or off; off drops the view's actions. |
| `thread.handover` | profile, model? | a handover runs; a run is live; a question is open; a message is unread | Moves the thread to another provider (V0.45). |
| `thread.setModel` | model?, effort? | archived | Changes the model and effort (V0.50). |
| `thread.changesSeen`, `thread.fileViewed` | head, files; path, blob, viewed | | Stores the Changes tab's look and Viewed marks in the View (V0.65). |
| `pr.merge`, `pr.rerunFailed` | method? | the effect's guard on the main PR (5.10) | Runs the Fyr effect on the main PR (V0.43). |
| `change.end` | change, phase? (`abandoned`, the default, or `reverted`), reason? | no change that has not ended ("No change to end"); the change has ended | Ends the change as the owner: its actions go and its PR is no longer polled; the reason is its label (3.13, V0.72). |
| `action.run` | on, action, digest | 5.13 | Presses an action (5.13). |

**Lifecycle transitions (complete)**

| From | To | By |
|---|---|---|
| active | settled | `thread.settle`; a settle Rule's firing (7.6) |
| settled | active | `thread.unsettle`; `thread.send` (revive) |
| active, settled | archived | `thread.archive` |
| archived | active | `thread.unarchive` |

**View snooze transitions (complete)**

| From | To | By |
|---|---|---|
| not snoozed | snoozed | `thread.snooze` (unavailable when settled) |
| snoozed | snoozed | `thread.snooze` (replaces the Rule) |
| snoozed | not snoozed | the snooze Rule's firing at its time; `thread.unsnooze`; `thread.send`; `thread.retry`; `thread.settle` when it settles now; `thread.archive` |

**Post-v1 commands.** `thread.fork`, `thread.setMode`, `thread.snoozeWhenDone`, `thread.pin`, `thread.unpin`, `entity.move`, `entity.import`, `entity.abortMove`, `entity.finishMove`, `entity.cancelMove`, `decision.open` by an agent, `link.add`, `link.remove`, `rule.create` and `rule.disarm` as commands, `document.create`, `document.get`, `document.send`, `checkout.add`, `coordinator.*`, `policy.set`, `user.presence`, `host.pair`, `host.pairPeer`, `host.revokeDevice`, `host.revokePeer`, `host.rename`, `job.acknowledge`. Their design is in REVIEW-LOG and earlier versions of this section (spec 0.7).

### 5.4 Assurance levels (R15)

```ts
type AssuranceLevel = "none" | "confirm" | "verified";
```

| Level | Proof |
|---|---|
| `none` | None. |
| `confirm` | The owner sends the command after an explicit confirmation step in the client. |
| `verified` | Post-v1. A WebAuthn assertion with user verification over a host-issued challenge `H(hostId, decisionId or commandId, nonce, payloadHash)`. Single use; it expires after 5 min (11.2). |

**Built.**
- `confirm` is the client's confirmation (V0.68): before a script or `fyr` action, Delete project and Remove provider. A press carries the digest of what the owner saw (5.13); a different digest is rejected.
- An agent cannot supply a proof. What the agent asks Fyr to run becomes an effect Decision that only the owner answers (5.10).
- **Agent tool calls** go through the tool gate (12.3): a PreToolUse hook on `Bash` denies `gh pr merge` with "Use mcp__fyr__effect_request with action pr.merge" (V0.12). The gate is a convenience. The boundaries are the OS user and the credentials that the session receives (5.9).

**Post-v1.** `LevelPolicy` is a pure function `level(call, principal, entity, policy) → AssuranceLevel`, loaded from a static policy file as a `policy` Document; each gated call writes `level.evaluated`; a tool call that matches no rule gets `defaultsByMode[mode]` (`auto`: none; `ask` and `plan`: confirm). The `verified` Actions (`host.pair`, `host.revokeDevice`) are available only through the host CLI, which runs as `system(cli)`.

```ts
type ApprovalProof =
  | { level: "confirm"; payloadHash: string }          // the client showed the confirmation and echoes the hash it showed
  | { level: "verified"; assertion: WebAuthnAssertion }; // post-v1
```

### 5.5 Allowances (Post-v1)
When the user approves a blocking Decision, they can choose "allow this exact call in this thread for 10 minutes". The host then stores an **allowance** keyed by `(thread, payloadHash)`. An allowance is never widened to similar calls and never applies to another thread (I8). ("Grant" means only the coordinator's grant, 5.8.)

### 5.6 Decisions (D1)

**Built.**

| Kind | Opened by | Holds the run | After a restart |
|---|---|---|---|
| `ask` | the agent's AskUserQuestion (the `claude` adapter's permission callback) | yes | gone; the thread is `failed` with class `restart`, and Retry asks again |
| `approval` | any other permission request of the CLI | yes | gone, as `ask` |
| `effect` | the agent's `effect_request` or `change_request` (5.10) | no | stays open |

- **Answering.** Only the owner answers, with `thread.answer`. An `ask` answer and an `approval` yes or no resolve the waiting callback of the run. An `effect` yes runs the effect (5.10); a no is reported to the agent.
- **Withdrawn.** When the CLI cancels a request (after an interrupt), the Decision goes (`decision.withdrawn`).
- **Asking.** While a Decision is open the thread is `asking` (6.1) and `thread.send` is unavailable.

**Post-v1** (D1 as an entity): `question`, `plan` (ExitPlanMode) and `proposal` (coordinators) kinds; merging by `dedupKey` (approvals by `payloadHash`, I8); a default that never approves and a 24 h deadline that counts as a deny; parking of a blocking Blocker when its run ends, after 10 min when the governor needs the slot, or at a restart; answers to a parked Blocker as an allowance or a user message; "the first valid answer wins"; proposals that the user accepts in part; a coordinator may answer a `question` in its grant and nothing else.

### 5.7 Coordinator Decisions and attention (Post-v1)
A coordinator can block on its own Decision (a Blocker with the coordinator as entity). The coordinator then shows as `asking` (6.3).

### 5.8 Grants (Post-v1)
- A grant is a `grant` Document. Its `signature` is the user's WebAuthn assertion over `H(body)` at level `verified`.
- Grants are replicated eagerly (2). Every host verifies the signature before it accepts a `coordinator(id)` command, and checks that the grant is the head of its `supersedes` chain.
- A grant expires at `expiresAt`, which is at most 30 days after signing.
- A coordinator without a valid grant is stopped with status `failed` and the reason "grant expired".

### 5.9 Credentials
- **Built.** A session runs the plain `claude` CLI with the environment its provider's adapter sets: the config folder, the endpoint and the key from the secret store (3.16). The tool gate (5.4) stops the agent's own `gh pr merge`.
- **Post-v1.** The `Credentials` extension point (12.2) gives each session a GitHub token that can push branches and open, edit and comment on PRs. Branch rulesets on each repo's default branch allow merges only by Fyr's merge identity, a GitHub App; `pr.merge` is the only Action that uses it. `fyr doctor` checks the rulesets. Without them, "never merge without the user" cannot be enforced, and the check reports an error.

### 5.10 Effects run by Fyr (D5)

A reducer never does work. It returns effects, and Fyr's effect runner executes them after the transaction (decision 0020 (3)).

| Effect | Does | Durable |
|---|---|---|
| `send`, `answer`, `interrupt`, `stop`, `setModel`, `refresh`, `cancelMessage`, `handover`, `cancel` | the agent process: send a message, answer a callback, stop, restart with a new model, move to another provider | no: they die with the process |
| `effect` | a Fyr effect (below) | yes, when it carries a request |
| `script.run` | a script action's argv (5.13) | yes |
| `thread.create` | a new thread for an agent action with `in: "new"` | no |
| `agent.feed` | deliver the thread's notices (5.14) | no |
| `git.observe` | read git after a run (3.14) | no |
| `t3.adopt`, `provider` | adopt T3 threads; run a provider command (3.16). The engine runs them before it answers (Await, 5.1) | no |

**At most once.** A durable effect is stored with the reduce that returned it. The runner claims it (`pending → started`) before the work, so it runs at most once, also across a restart. The result is stored (`started → finished`) and lands as an Input `effect.result` with source `effect:<id>`, which the same reducer applies. An effect that was `started` when Fyr stopped is not run again: it lands as a failure with the text "Fyr restarted while this ran, so Fyr does not know whether it finished; it was not run again. Check its outcome before you retry." A deploy reports its own result later, from the new daemon (V0.13).

**Fyr effects.** One guard per effect, used by every way to start it: the owner's command, the press of a built-in action, and the owner's yes to an agent's request. The runner checks the guard again before the work.

| Effect | Requested by the agent | Guard |
|---|---|---|
| `pr.merge {pr, method}` | `effect_request`, `change_request {ask: merge}` | archived; no PR; the PR's change is not in flight ("The PR is not open"); a conflict; changes requested. A PR with no change in the thread is left to GitHub, which the runner reads again. |
| `pr.rerunFailed {pr}` | no | archived; no PR; the PR is not open |
| `fyr.deploy {dir}` | `effect_request`, `change_request {ask: deploy}` | none; `dir` is required |

**The agent's request (D5).**
1. **Request.** The agent calls `effect_request {action, args, reason}` or `change_request {change, ask, args?, reason?}`. The reducer checks the args and the effect's guard, then opens a Decision of kind `effect` with the exact call. The tool returns at once.
2. **The run continues or ends.** Nothing waits inside the run. No callback and no slot are held.
3. **The owner answers.** A yes to a Fyr effect returns the effect `effect`; the runner checks the guard again and runs it. An answer to an ask that Fyr does not run (`review`, `approve`, `apply`, `revert`) is reported at once; a yes to `review` or `approve` also sets the change's `approval` condition ok (source owner) and clears `needs`. A no is reported at once.
4. **Result.** The result lands as `effect.result` on the thread, and the agent feed tells the agent with urgency `wake` (5.14).

**The owner's own effects.** `pr.merge` and `pr.rerunFailed` from the PR tab, and every press of a `fyr` or `script` action, end in an `effect.result` or `action` line in the thread. The agent hears each one as a notice (V0.43 (2), V0.60 (4)).

**Post-v1.** Blocking approvals of tool calls whose effect Fyr cannot run itself, which park (5.6); `pr.close`, `issue.close`, `document.send` and force push as Fyr effects; a Rule with `decisionResolved(D)` that sends a follow-up.

### 5.11 Read queries
A read query returns data and changes no state, so it is no Input and writes no event.

| Query | Built | Notes |
|---|---|---|
| `fyr_state {about?}` | yes | The thread's records in one compact text: the thread and its settle state, the changes with PR, git and deploy conditions and actions, the live view, the Rules (5.14). |
| `change_list {scope?: thread \| project, id?}` | yes | Changes with their facts; host records the thread shows included. |
| `threads {query?, state?}`, `thread_read {id, last?}` | yes | The owner's other threads (V0.22). |
| `GET /api/threads/{id}/events`, `/changes`, `/pr` | yes | The client's reads (V0.2, V0.25, V0.43). |
| `document.get`, `attention.list`, `thread.summary`, `events.since` | post-v1 | Documents and coordinators. |

### 5.12 Agent tools

Each session gets Fyr's MCP server (V0.12). A tool call becomes an Input with source `agent` on the agent's own thread, or a read query (5.11). The reply is the tool's text; a rejection is the tool's error with the reason.

| Tool | Input kind | Notes |
|---|---|---|
| `change_open`, `change_revise`, `change_status`, `change_attach` | `change.open`, `change.revise`, `change.status`, `change.attach` | 3.13. Each records the upkeep (`changesCheckedAt`) and one `change` event. |
| `change_unchanged {reason}` | `change.unchanged` | Records that the changes are current (V0.62). |
| `change_request` | `change.request` | Opens an effect Decision (5.10). |
| `pr_link {url}` | `pr.link` | Links a PR: a code change with the PR, and the main change. |
| `effect_request` | `effect.request` | 5.10. |
| `settle_when {condition: done \| merged \| change, change?, phase?}` | `settle.when` | Arms a settle Rule (7.6). |
| `watch {when, then: notify \| settle, note?}` or `{remove}` | `rule.arm`, `rule.remove` | The agent's own Rules (7.1). |
| `offer_action` | `action.offer` | Declares or removes an action (5.13). |
| `view_publish`, `view_state`, `view_unchanged` | `view.publish`, `view.state`, `view.unchanged` | Only for a process started with the live view (V0.37). |
| `thread_rename {title, id?}` | `thread.retitle` | This thread or one it started (V0.69). |
| `thread_new`, `thread_send` | `thread.create` and `thread.send` with source `agent` and `from` the sending thread, on the other thread | The field rights (5.2) apply to the agent: it cannot file the thread in a project or turn on its live view, and its message does not mark the thread seen. The new thread's `parent` and the message's `from` are the sending thread (V0.22, V0.71, #48). At most 5 new threads and 30 messages an hour per thread. |
| `show` | | A preview event (V0.19, V0.47). |
| `fyr_state`, `change_list`, `threads`, `thread_read` | | Read queries (5.11). |

### 5.13 Actions and the press

An action is one thing the owner does with one press (V0.60, #31). A change, the thread and the thread's live view each hold actions in one component `{offers, runs}`.

```ts
type ActionSpec = { id: string; on?: "thread" | "view"; label: string;
  run: "fyr" | "script" | "agent";
  effect?: string;                 // fyr: a Fyr effect (5.10)
  argv?: string[]; cwd?: string; timeoutSec?: number;   // script: an exact command, no shell; default 600 s, at most 3600 s
  prompt?: string; in?: "this" | "new";                 // agent
  when?: Phase[]; revision?: string; onOk?: Phase;      // only on a change
  primary?: boolean; by: "agent" | "observed"; digest: string };

type ActionRun = { on?: string; action: string; label: string; by: string; at: Time;
  running?: boolean; ok: boolean; exit?: number; output?: string /* the last 4 KB */; endedAt?: Time };
```

- **Declared.** The agent declares with `offer_action {on, id, label, run, …}`: `on` is `thread`, `view` or a change's id or key. A script needs argv; an agent action a prompt; ids that start with `fyr.` are Fyr's. At most 12 actions per holder; the 10 newest runs are kept. Removing an action drops its `action:<id>` condition; its runs stay.
- **Built in.** Fyr adds actions to a code change with a PR in review from what it observed, through the builtins Rule (7.6); they go when the change ends (V0.72): Merge (`fyr`), Re-run failed checks (`fyr`) and Fix CI (`agent`) while checks fail, Address review (`agent`) while changes are requested, Rebase (`agent`) on a conflict, Investigate the failed deploy (`agent`) on a landed change whose `deploy` failed.
- **The digest** is a hash over every field the owner sees. An action that changed after the owner looked does not run on the old press.
- **Available.** On a change, an action shows in the phases of `when` (default: every phase that is not an end) and, when bound to a revision, only while that revision is the newest. No action is available while it runs ("It is running"). A `fyr` action also needs its effect's guard.

**The press** is the owner's command `action.run {on, action, digest}`:
1. **Confirmation (V0.68).** The client asks before a `script` or `fyr` action, in Fyr's own sheet or dialog; an `agent` action is sent at once. The live view's `fyr.run(id)` and `data-action` go through the same confirmation, so a page cannot run a script or a Fyr effect without the owner's press.
2. **Checks.** A different digest is "The action changed since you saw it; look at it again."; the action must be available; a press of the same action within 2 s of the last press is "It was just pressed", whatever sent it (V0.71 (3)).
3. **Running until the result (V0.71 (2)).** The press starts a run (`running: true`) and returns its effect: the Fyr effect, `script.run`, or for `agent` a message to this thread (`send`) or a new thread (`thread.create`, which follows this thread's provider, model and effort). The run ends when the effect's result lands: an agent action to this thread when the message is sent, one with `in: "new"` when the thread exists ("Started thread <id>."). A second press while it runs is "It is running".
4. **Result.** On a change, a script that exits 0 sets `action:<id>` ok and moves the change to `onOk`; one that fails sets it to fail ("<label> failed (exit N)"), which blocks the thread. A failed run moves `activityAt`. Every press and result is an `action` event, and the agent hears the result (5.14).

### 5.14 The agent feed

The agent feed is the only way Fyr speaks to the thread's agent (decision 0020 (5), #47). After every reduce, the notice rules read the Input, its events and the thread before and after, and queue Notices on the thread. The agent manager takes them at three delivery points.

```ts
type Notice = { about: "thread" | "view" | `change:${string}` | `rule:${string}`;
  what: string;                    // one sentence
  urgency: "wake" | "next" | "stop" | "silent";
  coalesceKey: string;
  subject?: string; states?: string[];   // a coalesced line: "CI on PR #12: failed → passed."
  self?: boolean;                  // the agent's own Input caused it: it never wakes the agent
  at: Time };

type Feed = { queue?: Notice[];    // at most 50
  told?: Record<string, Time> };   // the last delivery per key in this run: the reminders' clock
```

**The notice rules** (`NoticeRules`; each has a test):

| Rule | Urgency | Notices |
|---|---|---|
| `result` | wake | The result of an effect the agent asked for, or of an action anyone pressed on the thread's records: "Your change_request merge on PR #12 succeeded." |
| `watch` | wake | The agent's own watch fired: "Your watch w_… fired: <note>." |
| `fact.blocks` | wake | A fact that makes a change fail or block: CI failed, changes requested, a conflict, a deploy failed, a PR closed or does not exist (V0.72). |
| `fact.moves` | next | A fact that moves a change forward: checks passed, approved, merged, live. |
| `record` | next | A host record the thread shows merged or went (3.14). |
| `owner` | next | The owner renamed the thread, moved its project, turned the live view on or off, changed its settle or archive state, or ended a change (V0.72). A settle by the owner never wakes the agent (V0.89). |
| `upkeep.stop` | stop | At the run's end: the live view or the agent's open changes were not touched in this run (V0.37, V0.62). |
| `upkeep.reminder` | next | At a tool call: a run went 4 min (`ReminderAfter`) since the latest of its start, the last touch and the last reminder (V0.52, V0.62). |
| everything else | silent | Only in the log and in `fyr_state`. |

- **Coalescing.** A notice with the key of a queued one merges into it: the higher urgency wins, and the same subject appends its state ("PR #12: merged → live."). A notice without a subject replaces the older text.
- **Delivery points.**
  - **Idle**: when the agent has no run, every queued notice is sent as one message that starts a turn, if at least one may wake (not `self`). `next` wakes an idle agent too (owner, 2026-11-15).
  - **Tool call**: after a tool call during a run, every queued notice that is not `stop`, plus the reminders that are due, go into the agent's context (PostToolUse). Only the thread's own agent takes them: a tool call inside a subagent (its hook input carries `agent_id`) takes nothing and leaves the queue as it is, and a subagent's stop never takes `stop` notices (#49).
  - **Run's end**: the `stop` notices and the upkeep go as the Stop hook's one reason and block the Stop once. Not when the hook is already active, not twice in a run, and not while background tasks run.
- **Asleep.** A settled or archived thread, or one in a handover, is not woken. Its notices wait in the queue until it is revived.
- **Loop guard.** A notice that the agent's own Input caused is `self`: it never wakes the agent and goes out with the next delivery.
- **Text.** Each notice reaches the agent as one line `[Fyr] <what>`, the most urgent first. A notice never carries the full state: the agent reads it with `fyr_state` (5.11).

## 6. Attention

### 6.1 Definition

```ts
type AttentionState = "asking" | "failed" | "blocked" | "done" | "working" | "waiting" | "seen" | "snoozed" | "settled" | "archived";
type Section = "attention" | "working" | "seen" | "snoozed" | "settled" | "hidden";
type Attention = { state: AttentionState; reason: string; section: Section; sortKey: SortKey; decisions: EntityId[] };
type Signal = { entity: EntityRef; state: "asking" | "failed" | "blocked" | "waiting"; reason: string; since: number; atEseq: number; source: string };   // atEseq: the entity's eseq when the Signal was raised
declare function attention(e: ConversationEntityProjection, view: View, decisions: Decision[], signals: Signal[]): Attention;
```

`attention` is computed per `(user, entity)` (R19). Decisions count for row 2 only when the user is their assignee.

**The vocabulary** (one label and one icon each, everywhere; 13.3):

| State | Label | Meaning |
|---|---|---|
| asking | Asking | A question or approval waits for the user. |
| failed | Failed | The last run failed or stopped unexpectedly. |
| blocked | Blocked | Something outside the agent needs the user: CI failed, review comments, a merge conflict, a thread conflict, no progress for 20 min. |
| done | Done | The agent finished, and the user has not seen the result. |
| working | Working | A run is in progress. |
| waiting | Waiting | The thread waits for something that is not the user (a build slot, a background task, a time, another thread). |
| seen | Seen | It is the user's turn, and the user has seen the latest result. |
| snoozed | Snoozed | Hidden until its snooze Rule fires. |
| settled | Settled | The work is finished. |
| archived | Archived | Hidden. |

- **Scope.** `attention` applies to every entity kind that has a Conversation: Thread and Coordinator.
- **Purity and totality.** It is pure, takes no clock, and returns exactly one state for every input (I10). Every input it reads is in the projection: `run.admitted` (8.2) for the slot, `waits` for waiting, `failureAck` and `lastVisited` for acknowledgement.
- **Who computes it.** The residence host computes it and sends it in the projection. Clients never compute status.
- **Modules.** Modules add Signals only (M1).

### 6.2 Precedence (first match wins)

Coordinators have no lifecycle, so rows 1, 4 and 5 never match a coordinator.

| # | Condition | State | Section |
|---|---|---|---|
| 1 | lifecycle = archived | archived | hidden |
| 2 | a Blocker in state `open` or `parked` on the entity, or a Signal `asking` | asking | attention |
| 3 | the last terminal run maps to "failed" in the end-class table (3.3) and has an eseq after `view.failureAck`, and no run is current; or a Signal `failed` whose `atEseq` is after `view.failureAck`; or the coordinator status is `failed` and `acknowledged` is before it | failed | attention |
| 4 | `view.snooze` is set | snoozed | snoozed |
| 5 | lifecycle = settled | settled | settled |
| 5a | a Signal `blocked` from a run-scoped source (the `claude` module: no progress, budget) | blocked | attention |
| 6 | a run is current and `admitted`, and `waits` is empty | working | working |
| 7 | `waits` is not empty, or a run is current and not admitted | waiting | working |
| 8 | a Signal `blocked` from any other source (GitHub, a thread conflict, devShell, a failing Rule) | blocked | attention |
| 9 | the last terminal run maps to "done" (3.3), has an eseq after `view.lastVisited`, and its trigger is not `system` | done | attention |
| 10 | otherwise | seen | seen |

- **Snooze.** Rows 2 and 3 rank above row 4. A snooze hides only quiet states.
- **Coordinators.** For a coordinator, the View has no snooze, and row 3 uses `acknowledged`.
- **Failed is sticky.** A visit does not clear row 3. Only `thread.send`, `thread.retry`, `thread.settle` and `thread.archive` move the acting user's `failureAck` (they append `thread.failureAck`). Settle and archive move it for every View.

### 6.3 The inbox
- **Rows.**
  - A Decision that blocks exactly one entity is shown as that entity's row. The question text is the row's summary, and the asking subagent, if any, is named.
  - A Decision that blocks two or more entities (a merge, 5.6) is shown as one Decision row with "blocks N threads". Its entities are shown under it, not as rows of their own.
  - Threads in state `failed`, `blocked` or `done` are rows.
  - Coordinators in state `asking` or `failed` are rows. Coordinators never appear in other sections.
- **Row shape.** Rows in the section `attention` have two lines (title; reason and summary). Rows in other sections have one line of 32 px (13.1 U16).
- **Stability.** A row that leaves the section `attention` stays in place, dimmed, until the keyboard focus or pointer leaves the section (U4).
- **Notifications.** Sinks receive the derived notifications (4.1) and every device's presence (3.11). The `push` Sink:
  - sends one notification when an entity enters `asking` or `failed`, to each device that is not `active`;
  - sends nothing to any device while some device of the user is `active` on a client that shows the inbox;
  - for `done` and `blocked`, sends one batched notification ("3 done, 1 blocked") after the user has been away from every device for 10 min;
  - respects the `hours` module (12.8).
- **Tab title.** It counts the rows in the section `attention`.

### 6.4 Read watermark
- **Visiting.** The client sends `thread.visit(eseq)` only when one of these is true:
  - the thread view has been focused for 1.5 s and the user has scrolled to the end of the last turn;
  - the user took an Action on the thread.
- **Peek.** Space opens a peek of the selected row. A peek never sends a visit.
- **Mark unseen.** `thread.markUnseen` is unavailable without a completed run. After it, the client stops sending visits for that thread until the user leaves the view and opens it again.

### 6.5 Sort order

```ts
type SortKey = { pinned: 0 | 1; urgency: number; at: HLC };
```

- **Comparison.** Sort by `pinned` first, then by `urgency` ascending, then by `at`.
- **Urgency.** asking 0, failed 1, blocked 2, done 3. Other sections: 0.
- **`at`.**
  - In the section `attention`: the time the row entered its state, oldest first (a queue).
  - In the section `working`: stable slot order (U14). A thread takes the lowest free slot when it enters `working` and keeps it while it stays in `working`, across runs. A freed slot is reused lowest first. Slots never compact.
  - In the section `seen`: the time of the last result, newest first. The group "PR merged" is first (U11).
  - In the section `snoozed`: the earliest `at` Condition of the snooze Rule; rows without one come last, by `since`.
  - In the section `settled`: the time of settling, newest first; only the last 24 h are listed, older threads are found through search (U16).
- **Explanation.** The order is always explained by the reason and the visible key. There is no learned ranking.

## 7. Rules

Everything that watches something and then acts is a Rule (decision 0020 (4), #46). This section states the Rules as built; subsections and kinds marked **Post-v1** are designed and not built.

### 7.1 Definition

```ts
type Rule = {
  id: string;
  purpose: "settle" | "snooze" | "deployWatch" | "builtins" | "watch";
  about?: string;                  // the change it watches or acts on; "" for the thread
  when: Condition;                 // 7.2
  then: { kind: string; args?: object };   // the Input a firing applies
  after: "disarm" | "keep";
  createdBy: "owner" | "agent" | "system";
  status: "armed" | "suspended";
  suspendedBy?: ("settle" | "archive")[];
  armed?: Record<string, string>;  // the anchored Conditions' snapshot (7.2)
  armedAt: Time;
  lastValue?: boolean; firings?: number; rejections?: number; retryAt?: Time;
  observe?: { observer: string; repo?: string; sha?: string; since: Time; read?: boolean };   // what an observer reads while it is armed
};
```

- **Owner record.** A Rule is a part of its owner's record: the thread's `rules`, or the owner's View for the snooze (7.5). A Rule `about` a change that is gone is dropped.
- **A disarmed Rule** is removed from the record. "Armed" and "suspended" are its only stored states.
- **Purposes.**

| Purpose | `then` | Armed by |
|---|---|---|
| `settle` | `rule.settle` | the owner (`thread.settle` while a run is live, `thread.settleWhenMerged`), the agent (`settle_when`, `watch {then: settle}`) |
| `snooze` | `rule.wake` | the owner (`thread.snooze`), on the View |
| `deployWatch` | `rule.end` | Fyr, when a change's PR merges (7.6) |
| `builtins` | `change.builtins` | Fyr, for a code change with a PR (7.6) |
| `watch` | `rule.notify` | the agent (`watch {then: notify}`) |

- **The agent's watches.** The agent arms its own Rules with the tool `watch`: Condition kinds `runEnded`, `changeReached`, `condition`, `at` and `idleFor`, nested at most 3 deep, `after: disarm`, at most 20 per thread. It removes only its own. The owner sees them on the thread; `fyr_state {about: "rules"}` lists them.

**Post-v1.** Purposes `wait`, `deferred`, `schedule` and `custom`; Rules owned by module entities; a Rule that a move carries (10.4); a `viewer` field for snoozes of several users; the suspend reasons `decision` and `move`.

### 7.2 Conditions

A Condition is one kind with its params, or `any`, `all` or `not` of others. It reads the thread record only, never the network: observers bring facts in as Inputs (5.1). The logic is three-valued: a Condition about a change the thread does not have is `unknown`.

| Kind | Anchored | True when |
|---|---|---|
| `runEnded` | yes | a run ended after the Rule was armed (`runEnds` is above the snapshot) |
| `at {at}` | yes | `now ≥ at` |
| `changeMoved {change}` | yes | the change's title, phase, PR, conditions or evidence differ from the snapshot |
| `changeReached {change, phase}` | no | the change got at least that far (3.13 Phases) |
| `changeEnded {change}` | no | the change ended: abandoned or reverted |
| `condition {change, type, status}` | no | the change has that condition with that status |
| `idleFor {ms}` | no | `now − activityAt ≥ ms` |
| `idle` | no | no run is live and the last run did not fail |
| `noDecisions` | no | no question or approval waits |

- **Anchored kinds** are made true by an event after the Rule was armed, or by a time. Arming takes their snapshot; a firing of a kept Rule takes it again. A state kind reads the current state.
- **Next check.** `at` and `idleFor` give the earliest time the Condition may change with time alone. The engine's clock sends the Input `clock` then.

**Post-v1** kinds: `schedule {cron, tz}`, `documentCreated {about, schema}`, `decisionResolved {decision}`, `contextAbove {entity, ratio}`, `budgetAbove {entity, ratio}`, `done {entity, user}`; the governor's `capacity`; Conditions over entities on another host, which read the replica and are `unknown` when it is stale (K23).

### 7.3 Evaluation and firing

- **When.** The scheduler (`EvalRules`) runs inside `Reduce`, after every Input, on the same copies of the records (5.1). Every Input that can change what a Condition reads is therefore followed by an evaluation, including `host.restarted` at start and `clock` at a next check.
- **Firing.** A Rule fires when its value goes from false to true. `unknown` leaves `lastValue` unchanged; any other value is stored.
- **One transaction (I9).** A firing increases `firings`, then reduces `then` as an Input with source `rule:<id>` on the same records, in the same transaction, through the same Input kinds. The kind must accept the principal `rule`, and its guard must pass. Then the scheduler looks again, so one firing can make another Rule true. At most 8 firings per Input: a loop between Rules ends there, with the event `rule.loop`.
- **Outcome `ok`.** With `after: disarm`, the Rule is removed. `rejections` is set to 0, and the anchored snapshot is taken again.
- **Outcome `rejected`.** The `then` changes nothing but the Rule. `lastValue` becomes false, `rejections` increases by 1, and the next try is no sooner than `min(2^rejections, 60)` minutes. After 10 rejections the Rule is removed.
- **Event.** Every firing appends `rule.fired {rule, purpose, then, outcome, reason?}`, except the builtins sync, which is not news.

**Post-v1.** A `then` on another host, submitted by a Job; the outcome `approvalRequested`; the Signal `blocked` "a rule keeps failing" after 10 rejections; firing limits per time (5.2).

### 7.4 Suspend and resume

Each evaluation first sets a Rule's `suspendedBy` from the thread's state. A suspended Rule is not evaluated.

| Purpose | Suspended while |
|---|---|
| `watch` | the thread is settled or archived |
| `deployWatch`, `builtins` | the thread is archived |
| `settle`, `snooze` | never: settling clears every settle Rule, and settling or archiving clears the snooze |

When the last reason goes, the Rule resumes:
- an anchored Condition keeps its last value, so an event during the pause still fires once (K5);
- a state Condition first takes its current value, so a state that became true during the pause does not fire.

### 7.5 Snooze, wait, deferred, schedule

- **Snooze (built).** One Rule on the owner's View: id `snooze`, purpose `snooze`, `when: at(until)`, `then: rule.wake`, `after: disarm`. Its firing removes it, which wakes the thread. A new `thread.snooze` replaces it. A View stored before #46 with `snoozeUntil` becomes this Rule when it is read. While the snooze holds, a new question or a new failure still shows the thread (6.1).
- **Wait (post-v1).** Purpose `wait`, `after: disarm`. While armed, it adds a Wait with its reason (3.3). Examples: `runEnded(X)` and `at(resetAt)`.
- **Deferred (post-v1).** Purpose `deferred`, `after: disarm`. The undo window (U7) creates `at(now + 5 s)` with `then: <the Action>`. `⌘Z` or the toast's Undo disarms it. A rejection produces the derived notification `deferred.rejected`, shown as a toast with the reason.
- **Schedule (post-v1).** Purpose `schedule`, `after: keep`. It is owned by a module entity, for example `schedule` or `workflow`.
- **Snooze until an event (post-v1).** "Until the agent finishes" (`runEnded`) and "until CI finishes" (`prChecksDone`).

### 7.6 The Rules Fyr arms

| Rule | Purpose | When | Then | After |
|---|---|---|---|---|
| `settle:done` | settle | `all(runEnded, noDecisions, idle)` | `rule.settle` | disarm |
| `settle:<change>` | settle | `all(changeReached(change, phase), idle)`; "settle when merged" is the main change reaching landed | `rule.settle` | disarm |
| `deployWatch:<change>` | deployWatch | the change is live, its `deploy` condition failed, the change ended (`changeEnded`), or the time limit passed: 24 h after the merge without a deployment of the commit, 7 days with one | `rule.end` | disarm |
| `builtins:<change>` | builtins | `changeMoved(change)` | `change.builtins`: set the built-in actions from what Fyr observed (5.13) | keep |
| `snooze` | snooze | 7.5 | `rule.wake` | disarm |

- **Settle.** A settle Rule settles only when the thread is idle, so a working thread is never hidden and a failed run keeps the thread in the inbox. The first settle Rule that fires settles the thread; settling clears every settle Rule. "When done" is dropped when a new message starts a new run. `thread.unsettle` clears the settle and every settle Rule.
- **The deploy watch.** While it is armed, the deployments observer reads the merge commit's GitHub Deployments (`observe`), and each read lands as `deploy.observed` (V0.66). A repo without any deployment ends the watch at the first read.
- **Quiet hours** are computed from the project's windows and the clock when the state is read (3.15), not a Rule: they change no record and start no Input, so a Rule would only store what the clock already says (decision 0020). The engine's clock broadcasts a project and its threads when a window starts or ends.

**Post-v1** (the default Rules of a new thread, from the `DefaultRules` extension point, created with `createdBy: system(defaults)`):

| Provider | Purpose | When | Then | Initial | After |
|---|---|---|---|---|---|
| core | settle | `all(idleFor(self, 3 days), not(done(self, owner)))` | `thread.settle` | current | keep |
| github | watch | `prChecks(pr) = failed` for a `serves` PR | `thread.send(notice)` | current | keep |
| github | watch | `prNewReview(pr)` (an author who is not the user, on a commit that no thread pushed) | `thread.send(notice)` | current | keep |
| github | settle (off by default) | `all(any(prMerged(pr), prClosed(pr)), not(done(self, owner)))` | `thread.settle` | current | keep |

Built instead: CI failures, reviews and merges reach the agent as notices of the agent feed (5.14), with no Rule per thread.

- **Usage limits (K1).** At a run end with class `usage_limit`, the `claude` module arms a `wait` Rule `at(resetAt)` with `then: thread.send("continue")`.
- **Budget.** At 80 % of a budget, the `claude` module emits the Signal `blocked` "80 % of budget used". At 100 % it ends the run with class `budget`.
- **Reactor ownership.** Threads that a coordinator or a workflow starts default to no default Rules. Their starter reacts to PR events, so two reactors never steer one thread.

## 8. Jobs and resources (R13)

### 8.1 Job

```ts
type JobSpec = { id: JobId; kind: string; class: "session" | "heavy" | "browser" | "light"; entity: EntityRef | null; params: unknown; notBefore?: number };
```

- **Request.** A Job is requested in `apply`.
- **Start.** The runner starts the Job when admission passes for its class (8.2) and `notBefore` has passed.
- **Events.** The Job writes `job.started`, then `job.step` after each durable step, then `job.ended` (`ok`, `failed` or `uncertain`).
- **Step kinds (K17).** A JobKind declares each step as one of:
  - **Action:** the step submits an Action as `system(job:<id>)` with `CommandId = H(jobId, step, index)`. A repeated step finds the receipt.
  - **Keyed I/O:** the step's outside effect is idempotent under a key (for example a git push of a known commit, or a clone into a fresh temporary directory that is renamed at the end).
  - **At-most-once I/O:** the step appends `job.attempting` before the effect. On resume after a crash, a step with `attempting` and no `job.step` ends the Job `uncertain` with a reason ("the email may have been sent"). The user sees it (I10).
- **Restart.** On `host.started`, each unfinished Job resumes from its last step.
- **Failure is visible (I10).** A Job that ends `failed` or `uncertain` with an entity gives that entity the core Signal `failed` with the Job's reason, until the user acknowledges it (6.2 row 3). A Job without an entity shows as a host row in the section `attention` until `job.acknowledge`.
- **Kinds.** All Job kinds come from modules.

### 8.2 Admission
- **Admission is the AND of every `RunAdmission` provider** (12.2), for runs and for Jobs of class `session`, `heavy` and `browser`.
- **Result.** A provider returns `admit`, `{wait: reason, until?: time}` or `{deny: reason}`.
  - On `admit`, the host appends `run.admitted`.
  - A `wait` adds a Wait with source `admission` (3.3). The runner re-checks at `until`, on each `resource.level` change, and when a slot is released.
  - A `deny` ends the run with class `admission_denied` and the reason.
- **Providers.** The `governor` provides one. The `hours` module (12.8) and a budget module can provide others.

### 8.3 Governor
The `governor` module is the only `ResourcePolicy` (command classes, slots, limits).

The values below come from the resource spike on the owner's machine (32 GB, 8 cores, zram swap, `swappiness` 120).

- **Inputs.**
  - Linux: PSI (`/proc/pressure/{memory,cpu,io}`), `MemAvailable`, and each scope's cgroup files.
  - macOS: `memory_pressure`.
  - Elsewhere: free RAM.
- **How memory is measured.** Use the cgroup values `memory.current + memory.swap.current` (or PSS + swap). Never use RSS. On the spike machine, RSS was about 4× lower than the real working set.
- **Level.** `normal`, `elevated` or `critical`, with hysteresis. `resource.level` is appended only when the level changes. The default thresholds use PSI `avg10`:

| Level | Memory | CPU | IO |
|---|---|---|---|
| normal | `some` < 5 | `some` < 25 | `some` < 15 |
| elevated | `some` 5–20, or `full` > 1 | `some` 25–60 | `some` 15–40 |
| critical | `some` > 20, or `full` > 5, or `MemAvailable` < 3 GB | `some` > 60 | `some` > 40, or `full` > 10 |

  The host level is the highest of the three columns.

- **Slots per class.** These are the defaults for a host with 32 GB. Host config can change them.

| Class | normal | elevated | critical |
|---|---|---|---|
| session, interactive (runs with trigger `user`, and handover Brief runs of a user's thread) | 4 | 4 | the runs that are current, no new |
| session, background (trigger `agent`, `coordinator`, `rule` or `system`) | 3 | 1 | 0 |
| heavy (builds, tests, `nix build`, installs) | 1 | 1 | 0 |
| browser (the `preview` browser, long-lived; post-v1) | 2 | 1 | 0 |
| light | unlimited | unlimited | unlimited |

- **Order.** Runs with trigger `user` go first, then `agent` and `coordinator`, then `rule` and `system`. The order within each group is FIFO.
- **Fairness (R19).** Slots are counted per owner user within the host limits. A user with free capacity in their share goes before a user above it. With one user this is the plain host limit.
- **Isolation.** With systemd, each session runs in a transient user scope (`systemd-run --user --scope`).
  - The daemon must export `XDG_RUNTIME_DIR`. Without it, the scopes cannot be created.
  - The spike confirmed that cgroup delegation of cpu, io, memory and pids works.

| Scope | `MemoryHigh` | `MemoryMax` | `MemorySwapMax` | `CPUWeight` | `IOWeight` |
|---|---|---|---|---|---|
| interactive session | 2 GB | 3 GB | 1 GB | 100 | 100 |
| background session | 2 GB | 3 GB | 1 GB | 50 | 100 |
| heavy tool call | 4 GB | 6 GB | 1 GB | 25 | 50 |
| browser | 1.5 GB | 2 GB | 512 MB | 50 | 50 |

- **The governor stops a stalled scope.** `MemoryHigh` does not kill a process: it throttles the process until it nearly stops. With zram swap, `MemoryMax` may never be reached, because swap takes the overflow.
  - **When it acts:** the scope's `memory.events` `high` counter keeps rising for more than 30 s, or the scope's `memory.pressure` `some avg10` is above 20.
  - **What it does to a background run:** it stops the scope. The run ends with class `paused`. A wait Rule `capacity` resumes it (critical level, below).
  - **What it does to a user run:** it stops the scope. The run ends with class `stalled` (row 3, failed), with the reason "stopped: used too much memory (N times today)". It is never resumed by itself (K20).
- **Pending check.** It is not yet proven that the kernel kills the process at `MemoryMax` when `MemorySwapMax=0`. The spike sandbox did not allow the test.
- **Session cost** (measured with `claude` CLI 2.1.325):
  - One session without a current run uses 210–450 MB, and about 3–5 MB more per turn.
  - A cold start or a resume takes 1.2–4 s.
- **Process stop without a run.** A session without a current run stops its process after 10 min. This frees 0.2–0.45 GB per session. The resume costs about 4 s and no tokens, because the prompt cache has already expired after 5 min.
- **Main risk.** The risk is the tool subprocesses (builds, tests, browsers), not the agent processes. On 13 Nov 2026 all agent processes together used about 1.25 GB, which is 4 % of RAM.
- **Blocking Decisions.** When the session class is full, the oldest blocking Decision that has been open for more than 10 min is parked, and its process stops.
- **Critical level.**
  - Interrupt current runs with trigger `agent`, `rule`, `system` or `coordinator`, newest first, with class `paused`. Brief runs of a handover are exempt.
  - For each one, create (as `system(governor)`) a wait Rule `capacity(session, self host)` with `then: thread.send("continue")`, `after: disarm`.
  - Never interrupt a `user` run.
  - A run that is paused three times in one day ends `failed` with class `stalled` on its next pause.
- **Condition `capacity {class, host}`.** State kind. True when that host has a free slot in the class. For another host (post-v1), the governor reads the peer's `resource.level` from the replica.
- **Heavy tool calls in the gate (12.3).** A heavy call waits for a slot for at most 5 min, with the Wait "waiting for build slot". While it waits, its run's session slot is released and taken again before the call starts. After 5 min the gate denies the call with the reason "No build slot for 5 min. Try again later, or run a smaller test." (K20).

## 9. Cross-thread awareness (R8)

- **Group by goal.** Grouping by `serves` Links is a lens inside the section `seen` only (13.1 U19). It is off by default.
- **Conflicts.**
  - Each host checks its resident threads against all threads it can see.
  - Two threads that are not archived conflict when they own the same Ref, or touch the same `file` Ref. The file key includes the branch.
  - The `git` module emits the Signal `blocked` "conflicts with <thread>".
- **Waiting.** Waits are wait Rules (row 7), local or across hosts (post-v1).

## 10. Handover (R7)

### 10.1 Producing a Brief
- **The current session writes it.** It runs a run with trigger `system` and calls `submit_document(schema: "brief", key: <handover job id>)`, which calls `document.create`. The key makes a retry return the same Document (3.9).
- **Fallback.** If that run fails, the `claude` module writes the Brief with a separate model call (`createdBy: system(claude)`), under the same key.
- **Claims.** Every `done` claim needs evidence, or it says `unverified`.
- **Editing.** The user edits a Brief by superseding it.
- **Reading the source.** The reader reads source events with `thread.read`. It reads Documents with `document.get`.

### 10.2 Renderers
Renderers are pure functions of a Document. Modules add them.
- **Core renderers for `brief`:** `oneLine` (at most 80 characters; the inbox row summary itself comes from `thread.summarized`, 12.4), `prompt`, `markdown` and `json`.
- **Destinations** are Actions: `thread.handover`, `thread.start` (with `documents`), `thread.send` (with `documents`), `entity.move` and `document.send`.

### 10.3 Handover to a fresh session or a child thread
- **Availability.** A handover is unavailable while a blocking Decision is `open`. A `parked` Blocker carries over.
- **Job `handover`** (from the `claude` module):
  1. Wait until no run is running. User sends that arrive meanwhile are held in the Job's state (`input.held`), not queued as runs, so they do not count as a current run (K19).
  2. Produce the Brief. The Brief run uses the slot class of the thread's last user run and is exempt from the critical-level interrupt.
  3. Do one of these:
     - `freshSession`: end the session, then start a new one in the same entity with the rendered prompt, on the same slot.
     - `childThread`: call `thread.start` with the Brief and lineage `spawn`.
  4. Release the held inputs to the new session (`input.released`).
- **Timeout.** After 10 min the Job ends `failed` (a visible Signal, I10), and the held inputs go to the old session.
- **Optional automatic handover (post-v1).** A Rule `contextAbove(self, 0.7)` with `then: thread.handover(freshSession)`, initial `false`, `after: disarm`. Each new session arms it again.
- **Coordinators** use the same mechanism, with their plan Document as the Brief, at 50 % context.

### 10.4 Move to another host (D2, post-v1)

**Precondition.** This protocol is post-v1. Before implementation, it is written as a state machine and model-checked in TLA+ (or similar) against E1, I1, I2, I3 and I11.

**I11. No open Decision on a fenced entity.** While an entity is fenced, every Blocker on it is parked or resolved.

**Who may move.**
- The user (`confirm`).
- A Rule or `system`, with the level that the policy sets. Automatic placement uses this path later.

**Availability.** `entity.move` is unavailable while a run is running or a blocking Decision is open. A queued run that has not started does not block a move.

**Steps.** The Job `move` runs on the source host A, with a `moveId`. It talks to the target host B through peer commands as `system(move)`, carrying the Job id as proof (2).

1. **Fence on A.** One transaction on A:
   - append `entity.leaving {to: B, epoch: e, moveId}`;
   - add `move` to `suspendedBy` of every Rule the entity owns;
   - withdraw a queued run (`run.withdrawn`), so B can queue it again;
   - detach the entity from Decisions that block other entities too: its Blocker moves into a new Decision with the same subject and `dedupKey`, which travels with the entity (K9).
   From now on, I1 holds: A appends nothing with this subject except `entity.left` or `entity.stayed`. Observers and timers (link updates, process stops, Decision defaults and expiry) buffer their events in the Job state.
2. **Branch.**
   - If the thread works on the checkout's default branch, A creates the branch `move/<id>`.
   - A commits uncommitted changes as `wip: move` and pushes.
   - A produces a Brief.
3. **Bundle.** The bundle holds the entity's events, its final `eseq`, the buffered events, its Rules, its Decisions, the withdrawn run, and the bodies of the Documents about it or given to it (K25).
4. **Checkout on B.** B gets a checkout of the repo (`checkout.add` if needed) and checks out the branch.
5. **Import on B.** `entity.import {bundle, epoch: e+1, moveId}` in one transaction:
   - rejects the import if B holds an abort tombstone for `(entity, moveId, e+1)`;
   - copies the events into `imported_events` and checks that the final `eseq` matches;
   - appends the buffered events, in order, with the following eseqs;
   - re-homes the Documents and Decisions;
   - stores the Rules with `move` still in `suspendedBy`;
   - appends `entity.resided {host: B, epoch: e+1}`;
   - removes `move` from `suspendedBy` (anchored Conditions keep `lastValue`, 7.4);
   - queues the withdrawn run again and starts a session (`startReason: "move"`) with the Brief.
6. **Finish on A.** `entity.finishMove` appends `entity.left {to: B, epoch: e+1}`. A keeps a read-only copy.

**Recovery.**
- **A crash after step 5.** All hosts trust epoch `e+1`. On restart, A sees B's higher epoch in the replica and appends `entity.left`. Until then, A rejects commands, because the entity is fenced.
- **A failure before step 5 is confirmed.** A calls `entity.abortMove` on B. B writes the tombstone and acknowledges, or replies that it has already resided at `e+1` (then A finishes as above). Only after the acknowledgement does `entity.cancelMove` on A append `entity.stayed {epoch: e}`, which unfences the entity, removes `move` from `suspendedBy`, and replays the buffered events (I2).
- **B unreachable.** The move stays fenced. The entity shows on A as failed with the reason "move to <B> uncertain: B is unreachable", with the Actions retry and cancel. Cancel is unavailable until B acknowledges the abort.

### 10.5 Document to a person or a Sink
`document.send(to)` starts the Job `deliver`.
- **A `person`:** the Job renders `markdown`, attaches `json`, and sends it through the `email` Sink. The send is an at-most-once step (8.1). The level is `confirm`, and the confirmation shows the full text.
- **A `sink`:** the Job passes the rendered Document to that Sink. The level is `none`.

## 11. Protocol

### 11.1 Messages
**Client → host:**
- `command {id, call, approval?}`
- `challenge {decision | command, payloadHash}`
- `subscribe {stream, cursor?}`
- `unsubscribe {stream}`

**Host → client:**
- `result {id, ok, value | error}`
- `challenge {nonce, expiresAt}`
- `snapshot {stream, seq, items}`
- `upsert {stream, seq, item}`
- `remove {stream, seq, key}`
- `event {stream, event}`
- `notice {kind, data}` (derived notifications, 4.1)

| Stream | Content | Subscribers |
|---|---|---|
| `inbox` | keyed by the connected user (R19): the resident entities that user can see, with that user's `attention` and `rowSummary`; Decisions assigned to them; coordinators in `asking` or `failed`; host rows for failed Jobs | clients |
| `entity:<id>` | the entity's events by eseq | clients, coordinators |
| `summary` | everything listed in 2 (Replication), plus a heartbeat | peers (post-v1) |
| `host` | host info, resource level, devices, peers, passkeys | clients, peers |

**Resume.**
- **Event streams.** The subscriber sends its last seq or eseq, and the host sends the events after it.
- **Upsert streams.** The host sends upserts after the given seq. If the cursor is older than 7 days, it sends a snapshot instead.

### 11.2 Approval proofs
- **Challenge.** The client requests a `challenge`. The host returns a single-use nonce that expires after 5 min.
- **Signing.** The passkey signs `H(hostId, decisionId | commandId, nonce, payloadHash)`.
- **Host checks.** The host checks:
  - the signature;
  - the RP ID and the origin, which must equal `clientOrigin`;
  - that the nonce has not been used;
  - the user-verification flag.
- **Signed Documents** (grant, policy). The signature covers `H(body)`. Every host verifies it.

### 11.3 Pairing
- **First device.** Run `fyr pair --first` on the host. It shows a code of at least 64 bits, valid for 10 minutes, with at most 5 attempts. The device gets a device token. (Post-v1, the device then also registers a passkey.)
- **More devices.** `fyr pair` on the host CLI (v1), or `host.pair` (`verified`) from a paired device (post-v1).
- **Peers (post-v1).** `host.pairPeer` (`verified`). The two hosts exchange host tokens and passkey public keys. Hosts store tokens hashed.
- **New passkeys.** An existing passkey signs each new one. The signed key is replicated through the `host` stream.

### 11.4 Capture endpoint
- **Endpoint.** `POST /capture` on each host takes text, images and an optional checkout, with the device token. It calls `thread.start` as `user(userId, deviceId)` (A3).
- **iOS.** A Fyr iOS Shortcut posts to this endpoint from the share sheet, because iOS PWAs cannot be a share target.
- **Android.** The PWA registers as a Web Share Target and posts to the same endpoint.

## 12. Modules (R14)

### 12.1 Module contract

```ts
type Module = {
  id: string; version: string; config: Schema<unknown>;
  provides: string[];   // capabilities, e.g. "agent:claude", "job:clone", "condition:prChecks"
  requires: string[];   // capabilities this module needs
  contributes: Partial<{
    entityKinds: EntityKind[]; refKinds: RefKind[]; conditionKinds: ConditionKind[]; actions: Action<any>[];
    eventTypes: EventType[]; observers: Observer[]; signals: SignalSource[]; guards: Guard[];
    runAdmission: RunAdmission[]; defaultRules: DefaultRules[];
    jobKinds: JobKind[]; documentSchemas: DocumentSchema[]; renderers: Renderer[];
    levelPolicy: LevelPolicy; resourcePolicy: ResourcePolicy; environments: Environment[];
    agents: Agent[]; credentials: Credentials[]; toolHooks: ToolHook[]; sinks: Sink[];
    uiSlots: UiSlot[]; sinkFilters: SinkFilter[]; mcpTools: McpTool[];
  }>;
};
```

- **Trust.** Modules are trusted code in the daemon.
- **Configuration.** The host config lists the modules: `services.fyr.modules` on NixOS, `config.toml` elsewhere.
- **Start check.** At start the host checks that every `requires` entry and every core requirement (12.3) is provided. If one is missing, the host does not start, and it names the missing capability.

### 12.2 Extension points

| Point | Contract | Providers |
|---|---|---|
| `EntityKind` | name, projection reducer, `replicated`, `movable`, has Conversation, lifecycle states | any |
| `RefKind` | `parse`, `canonical`, `lineageScope` | any |
| `ConditionKind` | `evaluate`, `dependsOn`, `nextCheck`, `anchored` (7.2) | any |
| `Action` | 5.1 | any |
| `EventType` | schema and reducer | any |
| `Observer` | subscribes to events, Refs or outside sources. Writes observation events of its own types, and may submit Action calls as `system(module)`. Respects I1 (buffers events for fenced entities). | any |
| `SignalSource` | `signals(proj, entity) → Signal[]`, pure | any |
| `Guard` | `check(call, projection, by) → ok \| {reason}`, pure; not applied to `restoresVisibility` Actions | any |
| `RunAdmission` | `admit(run or job, proj) → admit \| {wait, until?} \| {deny}` | any |
| `DefaultRules` | `rules(thread projection) → RuleSpec[]`, each with a `RuleKind` name | any |
| `JobKind` | steps, each declared as Action, keyed I/O or at-most-once I/O (8.1) | any |
| `DocumentSchema` | name, body schema, signature requirement, writing instructions | any |
| `Renderer` | `(document) → text \| json` | any |
| `LevelPolicy` | pure `level(call, principal, entity, policy)` | exactly one |
| `ResourcePolicy` | `classify(toolCall)`, `slots(level)`, `limits(class)` | exactly one |
| `Environment` | `applies(checkout)`, `prepare(checkout)` (heavy Job) | any |
| `Agent` | keyed by `kind`. `startSession(profile, …)`, `resumeSession`, `send`, `interrupt`, `stopProcess`, `endSession`. It emits Conversation observation events and **must send every tool call through the core tool gate** (12.3). | at least one |
| `Credentials` | `forSession(entity, profile) → {env, files}` | any |
| `ToolHook` | `before → allow \| deny`, `after`; called by the tool gate | any |
| `Sink` | receives derived notifications, every device's presence and Documents | any |
| `SinkFilter` | `filter(notification, user) → pass \| hold`, applied before a named Sink; a held notification is released by the filter later | any |
| `UiSlot` | declarative UI in a fixed slot, core components only | any |
| `McpTool` | calls Actions or read queries (5.11) only | any |

**Module rules**
- **M1.** A module cannot add an Attention state or a precedence row.
- **M2.** A module cannot style UI. A `UiSlot` uses only the core components, in a fixed slot. The slots are `rowBadge`, `threadPanel`, `threadTab`, `threadHeader`, `settingsSection`, `inboxFilter`, `inboxGroup` and `coordinatorPanel`.
- **M3.** A module changes state only through Actions, or through observation events of its own declared types.
- **M4.** The core never names a module. Core behaviour that needs a module names a capability, and the start check enforces that capability (12.1).

### 12.3 Core tool gate and core requirements

**Tool gate.** Every Agent sends each tool call of a thread or coordinator to the core tool gate, in this order:

1. **Level.** Ask the `LevelPolicy` with the run's effective mode (I5), and write `level.evaluated`.
   - If the tool call is an MCP call to an `external` Action, it is handled by `effect.request` (5.10), never as a blocking approval.
   - Otherwise, if the level is above `none` and no allowance matches, open a blocking approval Decision and wait. The session slot stays held; the governor may park the Decision after 10 min (8.3).
2. **ToolHooks.** Run every `before`. The first `deny` wins. A deny reason is shown to the model, so it says what to do instead (16.2).
3. **Class.** Ask the `ResourcePolicy`. For `heavy`, wait for a slot, at most 5 min (8.3).
4. **Allow.** Return `allow`. When the call ends, release the slot, run every `after`, and append `run.progress`.

**Exception: AskUserQuestion and ExitPlanMode** (ExitPlanMode Decisions are post-v1, with mode `plan`). For these two tools the gate returns no decision, so the CLI calls `can_use_tool`. A hook `allow` would skip `can_use_tool` (16.2). The `claude` module answers `can_use_tool` through a Decision.

**Core requirements** (checked at start):
- at least one Agent;
- exactly one LevelPolicy and one ResourcePolicy;
- the Job kinds `clone`, `worktree.cleanup` and `handover`; `deliver` when a module provides `document.send`; `move` when the mesh is enabled.

### 12.4 v1 modules

| Module | Provides |
|---|---|
| `claude` | Agent kind `claude` (16.2) with **profiles**. Each profile has an agent kind, a config dir or API endpoint, a model allowlist, a default model and a colour. Job kind `handover`. MCP tools `submit_document`, `thread.read`, `document.get`, `decision.open`, `effect.request`, `thread.settleWhen`, `thread.snoozeWhenDone`. The row summary: after each `run.ended`, a small model writes `thread.summarized {text ≤ 80 characters}`; the inbox shows it as `rowSummary`. Image content blocks in `tool_result` messages become run attachments (the filmstrip, 13.5). Condition `runEndedWith`. Usage accounting (budgets are post-v1). Waits with source `background` for awaited background tasks. The Signal `blocked` "no progress for 20 min" (a current, admitted run with no `run.progress` for 20 min and no awaited background task). Failure classes (3.3). The usage-limit wait Rule (7.6). Routing of AskUserQuestion and ExitPlanMode to Decisions. Prepending of `effect.result` context (5.10). |
| `git` | Observers for checkouts, worktrees, `owns` and `touches`. Checkpoints (3.10). Job kinds `clone`, `worktree.create` (post-v1), `worktree.cleanup` (only with no uncommitted changes and no unpushed commits; a crash leaves a marker so the next attempt can tell its own deletions from user changes). The conflict Signal. The Changes tab (`UiSlot`, 13.5). |
| `github` | Ref kinds `repo`, `pr`, `issue` and `branch` (all `lineageScope: false`), and `file`. A poller through `gh`, every 2 min, with a cheap check first (`updatedAt` or ETag). Detection of `gh pr create`. Conditions `prChecks`, `prChecksDone`, `prNewReview`, `prMerged` and `prClosed`. The Signal `blocked` (12.5). External Actions (agent: `viaEffect`): `pr.merge` (confirm; merge identity), `pr.close` and `issue.close`, and at level `none` `pr.ready`, `pr.comment`, `pr.reply` and `pr.resolveThread` (comments are marked as agent-written). Credentials (5.9). DefaultRules (7.6). A `rowBadge` `UiSlot` with each `serves` PR's state, and the PR tab (`UiSlot`, 13.5). |
| `policy` | LevelPolicy. Post-v1: shadow evaluation; v1.1: Observers that write `decision.outcome`. |
| `governor` | ResourcePolicy, RunAdmission, the Condition `capacity`, `resource.level`, the Wait "waiting for build slot", the resource `UiSlot`. |
| `nix` | Environment (12.6). |
| `push` | Sink: Web Push (6.3). |
| `email` | Post-v1. Sink for `person` Refs. |
| `deliver` | Post-v1. Job kind `deliver`. |
| `preview` | Post-v1. A headless browser per thread for agent UI checks, exposed as MCP tools. The browser process runs as Job class `browser`. |
| `hours` | Work hours and silent hours (12.8). |
| `move` | Post-v1. Job kind `move`. |
| `coordinator` | Post-v1. The coordinator runtime (12.7) and its `UiSlot`. |

### 12.5 GitHub blocked Signal
The Signal holds when a `serves` PR exists, no run is current, and at least one of these is true for the head commit. Its reason names the cause:
- "CI failed";
- "N review comments";
- "merge conflict".

### 12.6 Nix (R11)
- **Packaging.** The flake exports `packages.fyr`, `nixosModules.fyr` and `homeManagerModules.fyr`. Together they set:
  - the unit and the tailnet identity;
  - `clientOrigin` (and the peers, post-v1);
  - the state directory, the modules, the policy file and the slots;
  - the pinned `claude` CLI (16.2).
- **Without Nix.** Use the release binary with a systemd or launchd unit. Everything works except the `nix` Environment. The `claude` CLI version is then checked at start against the supported range.
- **Environment `nix`.**
  - It applies when the checkout has `flake.nix` with `devShells.<system>.default`, or `shell.nix`.
  - A heavy Job runs `nix print-dev-env` once per checkout and `flake.lock` hash, and caches the result.
  - If that fails, the agent runs in the plain environment, and the Signal `blocked` "devShell failed" shows.

### 12.7 Coordinator runtime (R17, post-v1)
- **Agent and tools.** A coordinator runs on an Agent profile with its own system prompt. Its tools are:
  - MCP tools for the Actions its grant allows;
  - the read tools `attention.list`, `thread.summary`, `document.get`, `events.since(host, cursor, filter)` and `thread.read(range, maxTokens)`.
- **Digests.**
  - The runtime subscribes to the summary streams of the hosts in the grant. It batches changes of entities in scope into a digest: attention changes, Decisions, Document headers, finished runs, and `ref.state` changes of Refs in the grant's `scope.repos`.
  - It delivers the digest as one message, at most every 30 s, and only when something changed.
  - `coordinator.wake(reason)` delivers a digest at once. Rules can call it.
- **Context.**
  - The coordinator reads row summaries, Briefs and Decisions.
  - Transcript reads are capped by `maxTokens`, and each read is recorded.
  - Its plan is a `plan` Document.
- **Delegation.**
  - A thread that the coordinator starts receives a `delegation` Document.
  - It has lineage `spawn` from the coordinator, `defaults: none`, and a mode ceiling no broader than `modeCeiling` (I5).
- **Marking.** Every Action a coordinator takes on a thread sets the thread's `actedOnBy`, which the thread header shows ("Snoozed by PR babysitter").
- **Proposals.** For anything outside its grant, the coordinator opens a `proposal` Decision.
- **Limits and failure.**
  - The grant's limits apply, charged to the coordinator as lineage root (I6).
  - At 100 % of the budget, or when the grant expires, the status is `failed` with the reason. Agent failures also set `failed`. Both show in the inbox (6.3).
- **Kill switch.** `coordinator.stop`.

### 12.8 Hours module (R18)
The `hours` module uses only extension points: DocumentSchema, SinkFilter, RunAdmission and UiSlot. It does not change Attention.

- **Document schema `hours`:**

```ts
type HoursBody = {
  scope: { repo?: Ref; goal?: Ref; all?: true };
  tz: "Europe/Stockholm";
  windows: { days: ("mon" | "tue" | "wed" | "thu" | "fri" | "sat" | "sun")[]; from: "HH:MM"; to: "HH:MM" }[];
  silent: boolean;                                   // true: outside the windows, the scope is silent
  outsideHours: { push: "hold" | "allow"; ruleRuns: "hold" | "allow" };
};
```

- **Scope match.** A thread is in a scope when it `serves` the goal Ref, or when its checkout is of the repo. With several matching `hours` Documents, the most specific one applies: goal, then repo, then all.
- **Notifications.** When `silent` is true and `push` is `hold`, the module filters the notifications that `push` receives for threads and Decisions in the scope outside the windows. When a window opens, it sends one summary notification ("N items need you"), which opens the away view (13.1 U13). It is a `SinkFilter` for the `push` Sink, so other Sinks keep their own choice.
- **Background runs (post-v1).** When `ruleRuns` is `hold`, its `RunAdmission` provider returns `{wait: "outside work hours", until: <next window start>}` for runs with trigger `rule` or `coordinator` in the scope. The runner re-checks at `until` (8.2). Runs with trigger `user` are never held.
- **Truthful status.** Attention does not change. A held thread shows `waiting` with the reason "outside work hours" (6.2 row 7).
- **Lens.** An `inboxFilter` `UiSlot` lens "in hours now" hides rows of scopes that are outside their windows. It is off by default and is chosen from the palette or the filter menu.

## 13. UX (R12)

### 13.1 Principles (testable)
- **U1. Inbox.** The default view is the inbox. It is one list with the sections in fixed order: attention ("Needs you"), working, seen, snoozed, settled. Working is expanded and collapses by itself only above 8 rows.
- **U2. One vocabulary.** Each state has one label and one icon (6.1). `StatusBadge` has no label override. Decision kinds ("Question", "Approval") are plain text beside the badge. Coordinators use the same states. Plan steps use their own small step marks, never state icons.
- **U3. One Action, one name.** Each Action has one label, one icon and one shortcut, and the same position everywhere. An unavailable Action is shown disabled, with the reason.
- **U4. Nothing moves under the user.** Reorders wait until the pointer and the keyboard focus leave the section, then animate for 150 ms. A row that leaves `attention` stays in place, dimmed, until then. Streamed text never moves content above the reading position.
- **U5. Keyboard-complete.**

  | Key | Action |
  |---|---|
  | `j` / `k` | move |
  | `Enter` | open |
  | `Space` | peek (no visit, 6.4) |
  | `n` / `N` | next / previous row that needs you, with its Decision focused |
  | `x` | select a row |
  | `e` | settle; on a running thread: settle when done (this wins over an open PR); on a thread without a current run and with an open PR: settle when merged |
  | `Shift+E` | settle the selection or the focused group |
  | `s` | snooze (presets) |
  | `#` | archive |
  | `u` | mark unseen |
  | `r` | retry a failed thread |
  | `⌘Z` | undo the last deferred Action (settle, archive, snooze) within its 5 s window |
  | `⌘K` | palette |
  | `⌘1`–`⌘9` | working thread in slot 1–9 (stable start order) |
  | `⌘[` | previous thread |

- **U6. Calm by default.**
  - Working rows have one static icon. The only spinner is in the header of a running thread.
  - No badges for working.
  - Push only for `asking` and `failed`; `done` and `blocked` only as a batch after 10 min away (6.3).
  - Sound is off.
  - `beam` is never used on toasts or decoration.
- **U7. Undo instead of confirm.** Settle, archive and snooze go through a deferred Rule with a 5 s undo (7.5). A rejected deferred Action shows a toast with the reason. Dialogs appear only for the levels `confirm` and `verified`.
- **U8. Phone parity.** The phone has the same inbox, Actions and Decisions. At 760 px or less the layout has one column, touch targets are at least 44 px, and option tiles are full width and at least 56 px high. On a phone, settling or archiving the open thread (from its header, ⋯ menu or a key) returns to the inbox through Back history, and the Undo toast shows there; undo reopens the thread. The translation rules are in `design/mobile/README.md`.
- **U9. Visual system.**
  - Geist and Geist Mono (decision 0006), self-hosted in the PWA (no outside font requests).
  - Four type sizes: 12, 14, 16 and 20 px. 12 px is the floor.
  - A 4 px grid.
  - A neutral palette with one accent (`beam`) and four state colours.
  - Light and dark themes.
  - No module styles.
  - Text never uses `ink-faint`.
- **U10. Speed.** An inbox Action shows its result within 100 ms, as an optimistic update. A thread with 10,000 events opens within 300 ms.
- **U11. Clearing work.** The section `seen` starts with a group "PR merged". `Shift+E` settles a group or the selection with one toast and one undo. `e` arms "settle when done" or "settle when merged" (`thread.settleWhen`).
- **U12. Explainable order.** Every row's position can be explained from its reason.
- **U13. Away strip.** When the user returns after 15 min or more, a one-line strip shows what happened since: "Since 14:02: 3 done · 2 PRs merged · 1 failed · 4 settled". Clicking it filters the inbox to those rows. "Dismiss" clears the strip; it does not visit rows.
- **U14. Switching is instant.** `⌘1`–`⌘9` are stable slots; `⌘[` goes back; the palette finds any thread. The design budget is 1,900 visits a day (0.2).
- **U15. Composer.** Enter steers the current run (or starts one). `⌘Enter` queues. There is no third send mode. While a question is open, the composer becomes the answer field for the focused question's "Other" option, so there is one Enter target.
- **U16. Density.** Rows in the section `attention` have two lines. Other rows have one line of 32 px: icon, title, last activity (for working: "vitest · 38 s", "waiting on monitor: deploy · 12 min", "3 subagents"), PR badge, time. Settled lists the last 24 h.
- **U17. Words.** The UI uses the user's words, never internal names: no "run 7", "parks", "eseq", "digest", "thread.send". Token counts appear only on hover. A default appears only when there is one; a deadline appears only within 2 h of it.
- **U18. Icons.** Each state icon must pass a test at 14 px and 1× pixel density, in grayscale and with a deuteranopia simulation, against every other state icon. The shapes are:

  | State | Shape | Colour (light / dark) |
  |---|---|---|
  | asking | filled disc with a "?" cut out | `beam-ink` / `beam` (at least 3:1 on every surface) |
  | failed | filled diamond with a "!" cut out | `signal-ink` |
  | blocked | filled square with a horizontal bar cut out | `harbour` |
  | done | small filled dot | `ink` |
  | working | three-quarter ring | `ink-muted` |
  | waiting | hourglass | `ink-muted` |
  | seen | no icon | – |
  | snoozed | crescent | `ink-muted` |
  | settled | check | `calm` |

- **U19. Lenses.** Lenses (in hours now, by repo, by goal) are chosen in the palette or the filter menu, not in the inbox header. Grouping by goal or repo applies inside the section `seen` only.
- **U20. Capture.** The New screen shows a "Paste screenshot" chip and a photo picker at the top. On iOS the share sheet uses the Fyr Shortcut (11.4); on Android the PWA is a share target. Host, profile and checkout default to the last used. iOS PWAs cannot be share targets; this is a platform limit.
- **U21. Phone navigation.** The bottom bar has Inbox · New (centre) · Search. Hosts are in settings.
- **U22. Push to answer.** A push opens the Decision card with the first question focused and a sticky Answer bar. If the Decision was answered on another device, the card says "Answered on desktop 14:03".
- **U23. Outbox.** When the target host is unreachable, a capture or a send goes to the client outbox and is sent when the host returns, or to another online host if the user chooses one.
- **U24. No header chrome.** The inbox header has the name, the host chips (only with more than one host) and the search field.
- **U25. Asker.** A Decision card names who asked: the thread, or "subagent 'explore gestures' in <thread>".

### 13.2 Decision UX
- **Questions are the main kind (0.2).**
  - The card shows every question of one AskUserQuestion. One question is active at a time, marked with a focus bar ("Question 1 of 2").
  - `1`–`4` pick an option. In a single-select question, a pick moves to the next question. In a multi-select question, `1`–`4` toggle and Tab moves on.
  - Each question has an option `0` "Other…", which opens an inline field (the composer, U15).
  - Enter sends all answers when every question is answered. Then the next row that needs the user opens (`n`).
  - The same card works from a phone push (U22).
- **Docking.** The card docks above the composer, at the end of the conversation.
- **What every card shows:**
  - who asked (U25);
  - the subject, exactly as `payloadHash` covers it (for a Bash command: the command, the working directory and the host);
  - the default and its time, only if there is one;
  - the deadline, only within 2 h of it.
- **Approvals.** An async approval card says what Fyr will do after approval ("Merge #4170 with squash"), and that the agent does not wait. `confirm` is inline. `verified` uses the passkey prompt (post-v1).
- **Proposals** list their calls. Each call shows a sentence, its target thread, why, its level, whether a deferred undo window applies (7.5), and an expandable exact payload. The buttons are "Accept N" and "Decline all".

### 13.3 Core components
Row, CompactRow, DecisionRow, Section, AwayStrip, Group, Badge, Panel, Card, DecisionCard, Button, Menu, Palette, Toast, Field, Tabs, Diff, Filmstrip, Terminal, Markdown, DocumentView.

### 13.4 Coordinator pane (post-v1)
- **Content.** The pane shows the plan Document (with step marks, U2), the open proposals (13.2), a log of Actions in the user's words ("Sent a message to Listing cards v2"), each linked to its event, the usage against the budget, and the grant.
- **Header.** Stop, start and wake are in the header.
- **Acknowledgement.** Opening the pane acknowledges the coordinator's failure (`coordinator.acknowledge`).

### 13.5 Thread view
- **Header.** Title, state badge, PR badge, profile, host chip (only with more than one host), the coordinator that last acted on it, and the Actions Snooze and Settle.
- **Tabs.** Conversation · Changes · PR.
  - **Conversation:** messages, runs with tools and subagents collapsed, a filmstrip per run, `effect.result` lines, and the Decision card docked at the end. The filmstrip shows the image content blocks of the run's `tool_result` messages, from any tool (for example the agent's own Playwright MCP server); the `claude` module stores them as attachments of the run.
  - **Changes:** the branch diff against its base, from checkpoints and git (`git` module).
  - **PR:** checks, review threads and the external Action "Merge" (`github` module; v0: 0.5 V0.43).
- **Composer.** U15.

## 14. Workflow module (probe for R16, post-v1)

- **Kinds.** The module registers the entity kind `workflow` (replicated, movable) and the Document schema `workflow.definition`.
- **Definition.** A definition has:
  - stages, each with a prompt template, an input schema and an output schema;
  - transitions, which are Conditions on output Documents;
  - limits on loops, wall time, threads and tokens.
- **Instance.** An instance is a `workflow` entity. Its own events are `workflow.started`, `workflow.stageEntered` and `workflow.ended`, and it owns its Rules.
- **Stages.**
  - Each stage runs in a thread that `serves` the instance, with `defaults: none` and a `delegation` Document.
  - The `workflow` RefKind has `lineageScope: true`. The instance is the lineage root for quotas (I6).
- **Advancing.**
  - The instance owns the Rule `documentCreated(stage, output schema)` → `workflow.advance`.
  - The `workflow.advance` `apply` returns the `thread.start` calls for the next stage in `calls`. They are applied one after another in one transaction (A6).
  - Fan-out returns several calls. Fan-in is `all(...)`.
- **Other mechanics.**
  - **Gates.** A gate is a transition Action with level `confirm` or `verified`.
  - **Loops.** A loop is counted in the instance projection.
  - **Review stages.** A review stage always runs in a new thread.
- **Stages on other hosts.** A stage thread on another host is started by a Job (A6). The fan-in reads Document headers from replicas.
- **Cancel.** `workflow.cancel` interrupts the stage threads and ends the instance.
- **User injections.** `workflow.inject(text)` sends the text to the running stage threads with `thread.send(documents)`. With `restartStage`, it starts the current stage again.

## 15. Open questions
- Q1. How to detect that a Brief lost a constraint. Claims with evidence (3.9) cover part of it.
- Q2. Secret scanning for Documents before `document.send` to a `person`.
- Q3. Policy proposals from the answer log and the shadow data (D1). The data is in place; the UX is not specified.
- Q4. Automatic placement. The move protocol and `capacity{host}` are in place; the placement Rule is not specified.
- Q5. Subagent model routing (for example DeepSeek subagents inside a Claude session). It is out of scope for v1.
- Q6. Merge enforcement depends on GitHub rulesets (5.9). Repos without rulesets fall back to the policy only.
- Q7. The mesh state machine and its TLA+ model (0.4).
- Q8. OS-level isolation between users on one host (separate Unix users or containers per user) (R19, 3.12).

## 16. Technology

### 16.1 Stack

| Part | Choice | Reason |
|---|---|---|
| Daemon (core, Rules, Jobs, governor, mesh) | Go | One static binary, low RAM (about 30 MB), simple Nix packaging (`buildGoModule`). The pure core (`apply`, `attention`, Conditions) is tested with table and replay tests. |
| Contracts | Protobuf with ConnectRPC (`connect-go`, `connect-es`) | Types are generated for Go and TypeScript. Server streaming works in browsers for `inbox` and `entity:<id>`. |
| Storage | SQLite (`modernc.org/sqlite`, pure Go) | One file per host: the event table, projections, `imported_events` and replicas. |
| Modules | Go packages compiled in, selected at build time through the Nix module option | They are trusted and run in the same process. External modules later run as subprocesses or MCP servers. |
| Reachability | `tsnet` (embedded Tailscale) | Each daemon is a tailnet node with automatic HTTPS. This gives `clientOrigin` and certificates without configuration. |
| cgroups, pressure | `go-systemd` D-Bus `StartTransientUnit`; `/proc/pressure/*` | Native, with no shell-outs. |
| WebAuthn, Web Push | `go-webauthn/webauthn`, `webpush-go` | |
| Git, GitHub | The `git` and `gh` CLIs; `githubv4` for polling; a GitHub App for the merge identity | go-git does not support worktrees well enough. |
| Client | TypeScript, Vite, React or Solid, TanStack Virtual, Radix or Ark primitives with own components, xterm.js, Shiki; Geist self-hosted (decision 0006) | A PWA. v0 uses Preact and esbuild. The own components are needed for U9. |
| Model checking (before the mesh) | TLA+ / TLC | 0.4, 10.4 |

### 16.2 Agent `claude` (decided: Go-native)

**Decision.** Fyr uses a Go-native client for the `claude` CLI. It speaks stream-json plus the control protocol: `control_request`/`control_response`, `can_use_tool` and `hook_callback`. The protocol spike proved this with CLI 2.1.325. The spike's client had 469 lines of Go, of which about 200 were the control client.

**What the spike proved:**
- Several turns run in one process over stdin stream-json.
- `initialize` registers a PreToolUse hook, and `hook_callback` arrives before a Bash call.
  - The CLI respects both deny and allow from the hook.
  - A hook `allow` skips `can_use_tool`, so the core tool gate (12.3) is the only gate.
  - A deny reaches the model as "PreToolUse:Bash hook error: <reason>". Gate reasons are therefore written for the model to read.
- `can_use_tool` for AskUserQuestion is answered with `updatedInput.answers`, keyed by the question text.
- `interrupt` is acknowledged in 4 ms.
- `set_model` works on a live session.
- After SIGKILL, `--resume` keeps the context. The whole resume takes 1.18 s.

**Consequences for the implementation:**
- `set_model` does not survive a resume. The `claude` module stores the model on the Conversation and passes it at every start or resume.
- `get_usage` raises the process's memory to about 410 MB. Call it rarely.
- The CLI sends message types that are not in the SDK's union, for example `keep_alive`, `control_cancel_request`, `transcript_mirror`, `active_goal` and `autocompact_state`. The client ignores unknown types and logs them once per type.

**Protocol drift.** Across 7 SDK versions from May to Nov 2026, the needed subtypes were never removed or renamed. Each change only added optional fields.

**The subset the client implements:**

| Subtype | Status |
|---|---|
| `initialize` with hook registration, `hook_callback`, `can_use_tool`, `interrupt`, `set_model` | proven |
| `set_permission_mode` | in the SDK at every version checked |
| `stop_task`, `mcp_set_servers`, `rewind_files` | not yet tested live |
| `get_usage` | proven, but expensive |

**Keeping it working:**
- **Version pin.** The Nix module pins the CLI version. Without Nix, the host checks the version range at start.
- **Conformance.** For each supported CLI version, the traffic between the official Agent SDK and the CLI is recorded once, and CI runs the Go client against these recordings. CI also re-runs the drift check for each new SDK release.
- **Fallback.** A Node bridge that runs the official TypeScript Agent SDK behind the same `Agent` contract. It is only for the case where Fyr needs in-process SDK MCP servers. Fyr's own MCP server runs over HTTP and does not need it.

**MCP binding.** Each session gets a per-session bearer token for Fyr's MCP server through the `Credentials` point (12.2). The host maps the token to `agent(entityId)` (or `coordinator(entityId)`) and revokes it at `session.ended`.
