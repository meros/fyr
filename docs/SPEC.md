# Fyr spec

Version: 0.7 (adds 0.5 v0; earlier: review rounds 4 and 5, owner decision D5, R19, a consistency pass)
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

| ID | v0 requirement |
|---|---|
| V0.1 | One host, one desktop browser client on the same machine. The daemon listens on 127.0.0.1 only (decision 0002). |
| V0.2 | Transport: `POST /api/command` with a client-made `commandId` (exactly once per id, I7); `GET /api/state` snapshot; `GET /api/stream` Server-Sent Events with thread upserts and events; `GET /api/threads/{id}/events`. Requests with a non-loopback `Host` are refused; commands need the header `X-Fyr: 1`. Every thread DTO carries `rev`, which increases on every change the client may see, also across host restarts; the client keeps the copy with the higher `rev`. `thread.markSeen` takes `upTo`, the `activityAt` the client showed, so a result that arrives after it stays `done` (6.4). `GET /api/threads/{id}/events?before=SEQ` pages back. The state carries `host` (the host name), and a thread carries `branch` (the checkout branch, read with git after each run). |
| V0.3 | Threads run the `claude` CLI over the control protocol (16.2): start, resume by session id, send, steer while running (the CLI queues it), interrupt, change model, AskUserQuestion as an `ask` Decision, other permission requests as an `approval` Decision. |
| V0.4 | Attention as 6.1–6.2 with the states asking, failed, blocked, done, working, seen, snoozed, settled, archived. `waiting` is produced for background tasks only (V0.16); the governor's waits come later. `blocked` comes only from the PR (CI failed, conflict, changes requested). |
| V0.5 | The Actions of 5.3 that v0 needs, in one registry: create, send, answer, interrupt, retry, dismiss failure, settle (13.1 `e` semantics), unsettle, snooze, unsnooze, archive, unarchive, mark seen, mark unseen, rename, change model. Each has `available` with a reason. |
| V0.6 | A host restart never shows a wrong state: a run or a question that was live when the host stopped becomes `failed` with class `restart`; Retry resumes the session. |
| V0.7 | An agent process with no run and no open Decision for 10 minutes is stopped; the next message resumes it (8.3, process stop). |
| V0.8 | PR: a URL printed by `gh pr create` links the PR to the thread; open PRs are polled with `gh` every 2 minutes; badge on rows; settle when merged. |
| V0.9 | T3 adoption (decision 0004): adopt open top-level T3 threads, with history from the Claude transcript. |
| V0.10 | Desktop UI: inbox sections (6.3), thread view with the conversation, collapsed tool calls and subagent count, the docked question card (13.2), a composer that accepts pasted images, the keys of 13.1 that apply, and a new-thread dialog with folder, profile and model. |
| V0.11 | Agent profiles: a profile is a `claude`-compatible binary with a name, a colour and a model list. Profiles come from `~/.config/fyr/config.json`, or from T3's provider instances when that file is missing. |
| V0.12 | **Fyr tools for agents (minimal).** Each session gets Fyr's MCP server (16.2 MCP binding): HTTP at `/mcp` on the host, a per-session bearer token passed with `--mcp-config`, revoked when the session ends, and the tools allowed with `--allowedTools mcp__fyr__*`. Tools: `pr_link(url)` links a PR to the thread; `effect_request(action, args, reason)` with the one v0 action `pr.merge {pr, method}` opens an `effect` Decision and returns at once (5.10); `settle_when(condition: done \| merged)` arms the settle. After approval Fyr runs `gh pr merge` and sends the result to the thread as a message (`effect.result`), which starts a turn when the thread is idle. Steering has three layers: (1) tool descriptions and a short Fyr block in `--append-system-prompt`; (2) the tool gate (12.3): a PreToolUse hook on `Bash` denies `gh pr merge` with the reason "Use mcp__fyr__effect_request with action pr.merge"; (3) post-v0, the credential broker removes merge rights from the agent's token. |
| V0.13 | **Improve Fyr from Fyr (self-deploy).** A thread may work on Fyr's own repo. The agent requests `effect_request(action: "fyr.deploy", args: {dir})` (or the user runs the Action `host.deploy`); the user approves with one key, as any effect (5.10). A deploy Job then: (1) builds `fyrd` and the web client from `dir` into `~/.local/share/fyr/builds/<id>/`; (2) runs `make test` there and stops on red; (3) snapshots the database (`VACUUM INTO`); (4) drains: waits until no run is live and no background task runs (V0.16), the requesting thread's included; (5) points the `next` link at the build and exits with code 75. A small, rarely changed launcher (`fyr`) runs the daemon from `current`; on code 75 it promotes `next` to `current` (keeping `previous`) and starts it. If the new daemon does not report ready within 20 s or exits within 60 s, the launcher puts `previous` back and starts it, and the thread gets `failed` with class `deploy` and the log tail. The result of a successful deploy reaches the requesting thread as `effect.result` from the new daemon (pending effects are stored and delivered at start). Sessions resume with `--resume`. The switch leaves the host unreachable for a few seconds: clients retry a command that gets 502, 503, 504 or a network error with the same `commandId` (I7 makes this safe) for up to 20 s, showing "Fyr is restarting", before they report a failure. The client compares the `buildId` in the stream `hello` with its own and reloads itself when they differ, so the desktop UI updates without a manual refresh; the phone PWA does the same (decision 0005), so web-only changes need no APK. Schema changes are additive only, so `previous` can still open the database. |
| V0.14 | **Phone over the tailnet (first cut).** `fyrd` still listens on loopback. Tailscale Serve publishes it on the tailnet (`https://<host>.ts.net:7443`, tailnet only). A request that carries `Tailscale-User-Login` is served only when that login is the owner: `ownerLogin` from the config, else the Tailscale user of this machine (tailscaled LocalAPI). A request without the header must have a loopback `Host`. Commands still need `X-Fyr: 1`. At 760 px or less the client shows one column: the inbox, or the open thread with a back button. The PWA manifest, the bottom bar and Web Push follow in M0. |
| V0.15 | **Installable PWA with push.** The client is a PWA: `/manifest.webmanifest` (name Fyr, standalone, theme from the tokens, 192 and 512 px PNG icons plus a maskable one), a service worker at `/sw.js` that caches the app shell (never `/api`), and an install prompt where the browser offers one. Web Push with VAPID: the host makes a key pair once and keeps it in the database; `GET /api/push/key` returns the public key; `POST /api/push/subscribe {subscription, device}` and `POST /api/push/unsubscribe {endpoint}` (both need `X-Fyr`) store and remove subscriptions per user (R19); `POST /api/push/test` sends a test. `POST /api/push/status {endpoint}` returns `{known}`, so a client whose subscription the host deleted (404 or 410) shows the bell as off. The host pushes when a thread enters `asking` or `failed` (13.1 push rule; batching of `done` and presence come in M4), with payload `{title, body, threadId, state}`; the body starts with the inbox meta word ("2 questions · …", "Approval · …", "Fyr action · …", "Failed · …", design/mobile rule 12) and the thread id as the notification tag, so a newer push replaces the older one for the same thread. A subscription that the push service answers with 404 or 410 is deleted. Tapping a notification opens the thread (`/?open=<id>`), focusing an open Fyr window when there is one. A tap opens its thread even when the app reloads for a new build first (the thread is kept in session storage until it is on screen), and the service worker tells every open Fyr window, so the installed app gets it when a browser tab is open too. (Found live: taps opened the wrong thread.) |
| V0.16 | **Background work is visible.** The `claude` CLI reports native subagents and background tasks as `system` messages (`task_started`, `task_progress`, `task_updated`, `task_notification`, `background_tasks_changed`). Each thread keeps `tasks`: id, description, type, status (`running`, `completed`, `failed`, `stopped`), the last activity line, tool uses, tokens, start and update times. A thread with no live turn and at least one running task is `waiting` (6.2, section Working) with the reason "N agents"; it is never `done` while its agents work. When the process exits or the host restarts, running tasks become `stopped`. The timeline records `task` events (started, ended with the summary); progress updates change the thread only. The client shows the tasks on the row (count and activity) and in the thread (a strip with each task's state and activity). |
| V0.17 | **Phone layout invariants, tested.** On a phone every screen fits the visual viewport: the composer, the bottom bar, sheets and the commit button are fully visible and above the bottom safe-area inset; headers stay below the top inset; nothing is hidden under the keyboard. This holds after the app returns from the background, after rotation, and when the keyboard opens and closes. The layout height follows `visualViewport` (resize, scroll), `pageshow`, `visibilitychange` and `orientationchange`, never a height measured once. An automated layout test in headless Chrome checks the invariants for every phone screen at 360 × 640, 390 × 844 and 430 × 932, with emulated safe-area insets, a simulated keyboard (a smaller visual viewport) and a simulated resume (hidden, resized, visible); `make test` runs it. |
| V0.18 | **Attachments: images, files and photos.** The client uploads each file with `POST /api/attachments` (raw body, `X-Fyr`, the name in `X-Fyr-Filename`, the type in `Content-Type`); the host stores it under `<data>/attachments/<id>/<name>`, at most 25 MB a file, and returns `{id, name, size, mediaType}`. `thread.create` and `thread.send` take `attachments: [id…]`. The host sends each image under 5 MB to the agent as an image block, and lists every attachment in the message text with its absolute path ("Attached: <name> (<type>, <size>) at <path>"), so the agent reads PDFs, logs and other files with its own tools. `GET /api/attachments/<id>` serves the file to the owner. The `user` event records the attachments' ids, names, types and sizes, and the client shows them as chips with image thumbnails. On a phone the composer and the new-thread screen offer Camera (`capture="environment"`), Photos and Files; pasting and dropping still work on the desktop. |
| V0.19 | **In-thread previews (planned): flexible, quick and cheap.** One Fyr tool, `show`, takes exactly one of `path` (a local image), `html` (a page or a fragment) or `url` (rendered as a screenshot), plus `title`, `caption`, `size` (`phone` 390 × 844, `desktop` 1280 × 800, or `W×H`) and `full` (whole page). One tool keeps every session's tool list small. It returns a short text (attachment id, file path, pixel size), never the image itself, so a call costs a few tokens; the agent reads the file only when it wants to look. Screenshots use one long-lived headless Chrome on the host, a new tab per shot, so a shot takes well under a second; Chrome stops after 5 idle minutes. An HTML fragment is wrapped in a page with the Fyr theme variables and light and dark modes; identical HTML is stored once. Each call appends a `preview` event `{kind: image \| html, attachmentId, title, caption, width, height}`; the client renders it inline as a card (image: tap for full screen; HTML: an iframe with `sandbox="allow-scripts"`, no same-origin, served from `/api/previews/{id}` with a strict CSP, height clamped to 2,000 px), and keeps the thread's previews in a filmstrip. Markdown images that point at `/api/attachments/{id}` render inline. Limits: 5 MB an image, 512 kB an HTML page, 10 screenshots a minute a thread. A live `browser` the agent drives stays post-v1. |
| V0.20 | **Push for everything that needs the user.** The host pushes when a thread enters `asking`, `failed`, `blocked` or `done`. `asking` and `failed` push at once; `blocked` and `done` push after 20 s and only if the thread is still in that state, so a turn that ends and starts again does not notify. No push goes out for a thread the user is watching: a client that shows a thread while its page is visible sends `POST /api/presence {threadId}` every 20 s (and `{threadId: ""}` when it stops showing it); a thread counts as watched for 45 s after the last beat. The body starts with the meta word: "Done · <summary>", "Blocked · CI failed". |
| V0.21 | **Calm tool output, details on demand.** The conversation shows what the agent did, not how it called tools. (1) A tool whose result has its own card renders only as that card: `show` as the preview card, `effect_request` as the decision and its result, `pr_link` as the PR chip, `AskUserQuestion` as the question card; their calls never show as tool rows. (2) Plumbing is hidden: `ToolSearch` and other tool-loading calls, `TodoWrite`, `settask`-like shell bookkeeping; a hidden call that failed is shown. (3) Consecutive remaining calls fold into one summary line in plain words, counted by kind: "Read 3 files · ran 2 commands · searched the web · edited 1 file", with the total time and an error mark when one failed. (4) One tap or click opens the group into rows (friendly name, the key input in mono, duration, status); a second tap on a row shows the full input and output, with long output cut at 200 lines behind "Show all". (5) MCP names read as words (`mcp__fyr__show` → Show, `mcp__github__…` → GitHub …). (6) A preference "Show tool details" (off by default, remembered per device) opens every group. (7) The same holds for every other machine line: run ends (time, cost), agent start and finish lines, decision answers, effect results and Fyr's own messages to the agent ("[Fyr] Your effect_request …") show as one quiet line at most, or only as a mark on the turn (a check, a cross, a clock), with the exact data one tap away. The end of a run reads as words, never as a bare symbol: "Finished in 14 min" or "Stopped after 2 min" in small muted text (the cost one tap away); without a duration it says "Finished" or "Stopped"; a failure keeps its line with the reason. The default view reads as a conversation: the user's words, the agent's words, cards that need the user, previews. What is shown is human-readable: a row's label is the agent's own description of the call when there is one (the `description` input that Claude gives Bash and Agent calls, such as "Run the Go tests"), else a sentence built from the tool and its key input ("Read app.tsx", "Searched for \"presence\"", "Edited thread.tsx (+12 −3)"); paths are relative to the thread's folder and shortened in the middle; raw commands, ids and JSON appear only in the opened details. The same applies to the live activity line on inbox rows and in the Agents strip, in the present tense ("Running the Go tests", "Reading app.tsx", "Searching the web"); bookkeeping calls do not change it. Nothing is deleted: everything stays in the log and in "Show tool details". Subagent folding (V0.16) and the long-text rules stay. |
| V0.22 | **Threads for agents (cross-session).** Fyr tools let an agent work with the owner's other threads, as T3's `delegate_task` and `t3_thread_*` did (used 123 times in 11 days, 0.2): `threads {query?, state?}` lists threads (id, title, project, state, PR, age; at most 30, newest first); `thread_read {id, last?}` returns the last turns of a thread as plain text (the user's and the agent's words and one line per tool group, at most 8,000 characters); `thread_send {id, text}` sends a message to another thread as a `user` event marked `from: <thread id>`, which starts or steers its run; `thread_new {cwd, text, title?, model?}` starts a thread and records `parent: <thread id>`. A thread may start at most 5 threads an hour and send at most 30 messages an hour (runaway limits, research/coordinators.md). Messages and threads made by an agent show who made them. An agent can never answer another thread's questions or approvals: those stay with the user (12.3). |
| V0.23 | **Projects: an identity with sources.** *Matching, auto projects and the seeding at start are replaced by V0.28; the rest holds.* A project has its own stable id, a name, a colour, an icon and optional defaults (profile, model); it does not depend on a host. It points at its work through sources: `folder {host, path}` (a root folder on one host) and `repo {url}` (a normalised git remote such as `github.com/meros/fyr`); later kinds (an issue tracker, another host's folder) add sources, not new project types. A thread belongs to the project whose folder source on this host is the longest root containing the thread's folder; failing that, to the project with a repo source equal to the folder's `origin` remote. When none matches, Fyr creates a project with a folder source at the folder's git root (for a worktree, the main repository's root; outside git, the folder itself) and, when there is an `origin`, a repo source, named after the folder. Projects are seeded from T3's projects (root, title, icon) so workspace roots such as `~/locked/acme` keep all their repos together; a seed never overwrites a project the user edited. At every start the host seeds first and then re-matches every thread that was not filed by hand, so a thread moves to the best folder project; projects that matching made, that the user never edited, and that have no threads left are removed. A git root equal to the home folder or `/` is not a project root (a dotfiles repository in the home folder must not swallow everything): the thread's folder is used instead. The home folder and `/` themselves are never an auto project: a thread started there has no project, and the client shows "No project". A project that is neither auto, edited nor seeded (made by matching before the auto flag existed) counts as auto at the re-match. The colour comes from a palette of ten hues that work in light and dark and never use the beam amber; by default it is picked from the name. The icon is a monogram (default), an emoji, or one of a curated set of line icons (T3's lucide names map onto it). A project does not need git, and it does not need a source at all: a project with no sources is a label for threads about one subject (for example scratch threads about the household), and the user files threads into it with `thread.setProject {projectId}`; a project set by hand is kept, and folder matching never moves the thread again. `project.update {id, name?, color?, icon?, profile?, model?}`, `project.create {name, sources?}`, `project.addSource {id, source}` and `project.removeSource {id, index}` go through the command path with receipts. A folder source must be a full path (`~` is the home folder); on this host the folder must exist, else the command fails with 400 and names the path, so a typo never makes a project that matches nothing. The host fills in its own name when a folder source has none. After a change to a project's sources the host re-matches threads at once. The project screen adds a source from one field (a path starting with `/` or `~` is a folder, anything else a repo URL) and removes one with its × button. Icons may arrive without `value` (the host omits an empty one); the client then draws the monogram. Every thread DTO carries `projectId`; the state lists projects with their identity, sources and thread counts. The client shows the project's icon and colour on rows, in the thread header and in the new-thread picker, can filter the inbox by project, and has a project screen to edit the identity. |
| V0.24 | **Hardening: the client survives the host, deploys never go backwards.** (1) All host JSON passes one decoder (`web/src/decode.ts`) before the store: every field a screen reads gets its empty value, because the host omits empty fields. A Go test writes the host's real `/api/state` for edge cases (a thread with no project, an icon without `value`, a label project) to `web/test/fixtures/host-state.json`; a web test feeds it, and each variant with one optional field removed, through the store and the pure functions the screens use. (2) A render error stays in its part of the screen: the inbox, the open thread, each conversation message and the phone app each have an error boundary that shows "<part> could not be shown", the message, Try again and Reload. (3) A deploy refuses a checkout whose HEAD is an ancestor of the live build's commit ("older than the live build"), before it builds. Only one deploy runs: a newer one cancels an older one that has not linked `next`; past that point nothing cancels it. |
| V0.25 | **Changes: the branch diff, read-only.** The thread's Changes tab (13.5) shows what its branch changed against its base, so the owner can review an agent's work on desktop and phone without a terminal. v0 uses git directly, no checkpoints (3.10). The host runs `git` in the thread's folder, never through a shell, with a 10 s timeout per call. The base is `origin/HEAD`, else the first of `origin/main`, `origin/master`, `main`, `master` that exists; the diff starts at the merge base of the base and `HEAD` and ends at the working tree, so it holds the branch's commits, staged and unstaged edits, and untracked files that git does not ignore (shown as added). Renames are detected. Without a base the diff starts at `HEAD` (uncommitted work only); in a repository without commits, at the empty tree. `GET /api/threads/{id}/changes` returns `{git: true, root, branch, base, mergeBase, commits, files: [{path, oldPath?, status: added\|modified\|deleted\|renamed, additions, deletions, binary}], truncated}`, files sorted by path, at most 1,000 (then `truncated: true`); paths are relative to the repository root. A folder that is not a git checkout (or is missing) returns `{git: false}`; an unknown thread 404. `?path=<p>` returns `{path, patch, binary, truncated}`: the unified diff of that one file (a renamed file from its old path), cut at 400 KB at a line end with `truncated: true`; a path without changes returns an empty patch. The path is taken literally (no pathspec magic), and an untracked file is read only when git lists it as untracked, so the call never reads outside the repository. The client loads the list when the tab opens and on Refresh (no live updates). It shows one summary line ("4 files · +120 −18 · against origin/main · 3 commits"), then one row per file: a status mark (A, M, D, R), the path shortened in the middle so the file name stays visible, and `+N −M` (or "binary"). A click or tap on a row opens that file's diff below it and loads it once; another closes it. The diff shows hunk headers, old and new line numbers, added lines on a `calm` wash and removed lines on a `signal` wash, in light and dark; long lines scroll sideways inside the diff and never widen the page. A binary file says "Binary file, not shown"; a cut patch says so at its end. Empty states: "No changes against <base>." and "This thread's folder is not a git checkout." The phone shows the same tab at `/?open=<id>&tab=changes`, with 44 px rows. |
| V0.26 | **Calm, steady screens.** (1) The Working section keeps a stable order: rows sort by when the current run started (for a `waiting` thread, the earliest start of its running agents), newest first, then by id; activity and updates never move a row. (2) An opened tool group shows at most the last 3 calls and a "+N more tools" row above them; a click or Enter on that row shows the rest. A call that failed is never hidden behind it. The same holds on the phone and with "Show tool details" on. (3) The desktop thread header has a ⋯ menu with "Move to project…", which opens the project picker (search by name, keyboard: arrows, Enter, Esc) and files the thread with `thread.setProject {projectId}`, as the phone does. |
| V0.27 | **Plain decision cards.** A card for an effect says in words what happens and why, and keeps the technical form behind "Details". The header names the effect ("Update Fyr", "Merge #15"), the heading is one sentence ("Update Fyr to a new version", "Merge pull request #15: <title>"), the agent's reason follows as text, then one or two sentences on what Fyr does (for a deploy: it builds and tests, waits until no thread works, restarts with a few seconds offline, and goes back by itself on failure). The buttons are verbs ("Update Fyr", "Merge", "Not now"). "Details" (closed by default) holds who asked, that the agent does not wait, the action name and its arguments. An unknown effect gets its action name as words and Approve / Not now. The same wording is used on desktop, phone and in the inbox row. |
| V0.28 | **Workspaces and refs; no auto projects (decision 0008, slice 1).** A thread runs in a **workspace** `{id, host, path, provider, refs, branch}`, stored in its own table; the thread DTO carries `workspaceId` next to `cwd`. A **ref** is `{kind, key}`: `folder` with key `<host>:<path>`, or `repo` with a normalised remote (`github.com/meros/fyr`). A **provider** identifies a folder, in this order: `scratch` (the folder is inside a scratch root, by default `~/.t3/scratch` and `~/.local/share/fyr/scratch`, set by `scratchRoots` in the config): no refs; `clone` (the folder is in a git checkout): the folder ref of the main repository root, the folder's own folder ref when it differs (a subfolder or a worktree), and the `origin` remote as a repo ref; `folder` (anything else): the folder's own ref. Project sources are refs (the wire form stays `{kind, host, path}` / `{kind, url}`). A source covers a ref of its kind: a folder covers the same folder and every folder below it on the same host, a repo covers an equal key on any host. **Matching is one function:** the project with the most specific cover of any of the workspace's refs, a longer folder before a shorter one, any folder before a repo, ties by project id. The home folder and `/` have no rule of their own: they match only a project that has them as a source. **Fyr never makes a project by matching.** A thread with no project shows "No project". When its workspace has a repo ref, its DTO carries `suggest: {name, sources}` (the name is the repository's name, the sources are the main root folder and the repo), and the thread header (desktop and phone) offers "Make project “<name>”"; `project.createFrom {threadId}` makes that project (counted as edited) and files every thread without a project that it now matches; a thread with no suggestion gets 400. Matching runs when a thread gets its workspace (create, adoption, start for threads from before workspaces) and only if it has no project, and after a change to a project's sources for every thread not filed by hand; it moves a thread to the project it finds and never takes a project away (a thread that nothing matches keeps its project). A thread filed by hand never moves. At start the host gives every thread without a workspace one, clears the auto flag of auto projects that hold threads and deletes auto projects without threads that were never edited (migration; nothing makes new auto projects). The host no longer seeds projects from T3 at start; the projects seeded earlier stay. |
| V0.29 | **Drafts survive.** What the owner typed in a thread's composer is kept per thread outside the screen: a new message, a card that opens or closes, a screen change, a reload after a deploy and a restart of the app keep it. Sending clears it. It lives in the browser's storage on that device (memory only when storage is not allowed); it is never sent to the host before Send. (Found live: the input cleared when a new message arrived.) |
| V0.30 | **Projects: one picker, one create step, threads start from a project.** *The create step's source field, the folder text field of a new thread and the source lines of the project screen are replaced by V0.42 (folders, a main folder, the folder picker, a scratch folder by default); the rest holds.* One set of words: "Project…" files a thread, "New project" makes one, "No project" is a normal state. (1) **The picker.** "Project…" opens one picker from three places: the project chip in the thread header (desktop and phone, filed or not), the thread ⋯ menu (desktop key `p`; phone "Project…"), and the suggestion chip "Make project “<name>”" (V0.28), which opens the picker with its first row focused instead of making the project at once. The picker has a search field (desktop: arrows walk the rows, Enter picks, Esc closes) and these rows: first **New project from this thread**, then **No project**, then the projects by last use; the thread's current choice is marked. Typed text that matches no project turns the first row into New project “<text>”, and Enter in the search then opens it with that name. Picking a project files the thread by hand (`thread.setProject {projectId}`). "No project" unfiles it: `thread.setProject {projectId: ""}` clears the project and counts as filed by hand, so matching leaves the thread without a project. A filed thread's picker ends with "Edit <name>…", which opens the project screen. On the phone the picker is a sheet; the details sheet's Project row opens it too. (2) **Creating is one step.** Only a name is required. The step shows the name field and the sources the project starts with, each with one plain line of what it does and a × to leave it out, and one optional field that adds a source (a path or a repo URL, as on the project screen). The button says "Create and file this thread" from a thread and "Create project" elsewhere. From a thread the step is prefilled from the thread DTO's `newProject {name, sources}`, which the host computes for every thread with a workspace: for a clone with a remote, the suggestion's name and sources (main root folder and repo); otherwise the folder's name with its folder as the source. A scratch workspace, the home folder, `/` and a thread without a workspace give no `newProject`: the name field starts empty and there is no source. A source that an existing project already has is left out of the prefill; when none is left, there is no `newProject`. Creating from a thread sends `project.create {name, sources, threadId}`: the host makes the project and files that thread by hand in one write, then re-matches as after any source change (so other unfiled threads in the same place join it). The separate form with a folder field and a repo field is gone: every "New project" uses this step. (3) **The project screen** is where the rest is edited (name, icon, colour, defaults, sources). Each source shows one plain line: a folder on this host "Threads started in this folder land here", on another host "Threads started in this folder on <host> land here", a repo "Threads in any clone of this repo land here, on any computer"; no source: "No source: you file threads here by hand". (4) **A new thread starts from a project.** The new-thread dialog (desktop) and screen (phone) ask Project first, with the same picker. It starts on the inbox's project filter, else the project used for the last new thread on this device, else the most recently used project. The folder follows from the project's folder on this host and shows as one line with "Change"; a project without a folder on this host shows the folder field, and the thread cannot start until it has a folder. "No project" shows the folder field. "Pick a folder…" in the picker keeps the project empty and shows the folder field. "New project" in this picker makes the project (no thread to file) and picks it. |
| V0.31 | **Fyr's own right-click menu; no browser menu.** A right-click (or a long press on the desktop) never shows the browser's page menu. What it shows depends on where it lands: on a thread row, the thread's menu (Open; Settle or Unsettle; Snooze… or Unsnooze; Mark unseen or Mark seen; Archive or Unarchive; Project…; Stop run while it runs; Copy link), each with its key, unavailable ones disabled with the reason as a tooltip; on selected text, Copy; on a link, Open link and Copy link; on an image, Open image. In a text field the browser's own menu stays, because spelling suggestions and Paste need it. Anywhere else nothing opens. The menu works from the keyboard (arrows, Enter, Escape), stays inside the window and closes on a click outside, a scroll or Escape. On the phone, a long press on the page shows no browser callout or menu (rows and text fields keep their own behaviour). |
| V0.32 | **The installed app feels native on a phone.** The page never bounces and a pull at the top never reloads it; a double tap never zooms; a tap shows no grey flash; a long press on the app's own controls (rows, buttons, tabs, chips) shows no browser callout and selects no text, while text people read (messages, code, questions, details) stays selectable. A press reacts at once (the pressed colour, and a slight press on the main button). Android gives a short vibration when a swipe settles or snoozes, when a message is sent and when a card is approved. A new screen slides in from the right and Back slides it out (no motion with reduced motion); a sheet does not slide the screen. No scrollbars and no focus rings from touch. A screen change, transition included, is done within 800 ms on a phone (checked in the layout test; found live: open and Back took 4 s each). |
| V0.33 | **Message delivery is visible and cancellable (decision 0009).** Every message sent to an agent (by the owner, another thread's agent or Fyr) gets an id, which the host passes to the CLI as the user message's `uuid`. The CLI queues a message that arrives during a turn and takes it in at the next tool boundary, or starts a turn with it when idle, and reports each step as `command_lifecycle`. A message is `sent` (written to the agent, no report yet), `queued` (in the agent's queue, not read), `read` (the agent took it in), `cancelled` (withdrawn before it was read) or `lost` (the agent could not start, the process ended or Fyr restarted before it was read). The `user` event carries `id`; each change is a `message.state {id, state}` event; the thread DTO keeps the unread ones in `outbox: [{id, state}]`. `thread.cancelMessage {messageId}` withdraws a `sent` or `queued` message with the CLI's `cancel_async_message` and answers 200 only when the CLI confirmed; it answers 409 when the message is read, unknown or not in the outbox, or when the agent cannot withdraw it. After a host restart every message still in the outbox becomes `lost`; nothing is resent by itself. Without `command_lifecycle` (an older CLI) a message becomes `read` when its turn ends. **Client, desktop and phone:** a read message stands where the agent read it, so the log reads in the order the agent saw it. Unread messages stand at the end, after a quiet divider "Agent is here", dimmed, each with the word "Waiting" and a Cancel button (a `sent` one says "Sent", without Cancel). The last read message of the owner carries a small muted "Read"; older ones carry nothing. A cancelled message stays where it was cancelled, struck through, with "Cancelled" and "Send again"; a lost one says "Not delivered" with "Send again" (a message from another thread gets no Send again). No undo toast: a cancel is final at the CLI, and Send again is the honest way back. Stop (interrupt) keeps waiting messages: the agent stops its current step and then takes them in a new turn (the owner's choice, 2026-11-14). |
| V0.34 | **Every output copies in one tap; the rail is calm.** Each assistant message has a Copy button (the markdown source); each code block in a message has its own Copy button (the code); an opened tool call has Copy on its input and on its output; a quiet line's full text has Copy. On the desktop the message button shows on hover and on keyboard focus; on the phone it is always there, faint. A copy confirms with "Copied" on the button for about a second. In the rail, a section with no threads cannot be selected (dimmed, no jump, also not with g1–g5), and the host name stays on one line, cut with an ellipsis, the full name in the tooltip. |
| V0.35 | **Tables stay readable at any width.** A table in a message never breaks a word in the middle and never squeezes a column to a few letters. Where the message column is narrower than about 520 px (a phone), each row shows as a small card: every value with its column name above it, in the order of the columns. Where it is wider, the table keeps its columns; a table wider than the column scrolls sideways inside the message, and the page itself never scrolls sideways. (Found live: a commit column wrapped one character per line on the phone.) |
| V0.36 | **Dismiss a merged PR; the thread goes on.** `thread.dismissPR` removes a merged or closed PR from a thread (an open PR cannot be dismissed: "The PR is still open"). The thread keeps its conversation and its state; the PR chip and PR tab go away. Fyr remembers the dismissed URL, so polling or a later link never brings it back; a new PR the agent opens attaches as usual. The client offers "Dismiss PR" on the PR chip and PR tab of a merged or closed PR, and in the thread's right-click menu. A thread without a PR shows no PR chip and no PR tab (no "no PR" placeholder); a link or deep link to the PR tab of such a thread opens Conversation. |
| V0.37 | **Live views (experiment, decision 0010).** A thread may keep a **live view**: a page pinned above its conversation that shows the state of the agent's work, so the owner does not have to read the thread to know where it stands. (1) **Flag.** `liveView` is a thread field, set with `thread.create {view: true}` or `thread.setView {on}`. With it, the agent's process starts with a prompt block that explains the view and with three Fyr tools; without it the tools are neither listed nor callable. The prompt and the tools are fixed when the process starts, so `thread.setView` ends an idle process at once and a running one after its run (never while it holds unread messages). (2) **Tools.** `view_publish {html \| path, state}` stores a self-contained page (a fragment gets the Fyr theme; at most 512 kB) once by content hash, in the attachment store, with a bridge script first in `<head>`, and sets the full state. `view_state {patch}` applies a JSON Merge Patch (RFC 7396: keys merge, `null` deletes a key, arrays are replaced whole) and fails before any page exists. A state over 16 kB as JSON is refused with the size. `view_unchanged {reason}` records that the view is current. (3) **Events and DTO.** `view.published {pageId, state}`, `view.state {state}` (the full resulting state) and `view.unchanged {reason}`. The thread DTO carries `live: {pageId, state, updatedAt, checkedAt, headline}`; `headline` is `state.headline` when it is a string (first line, at most 160 characters); `checkedAt` moves on every `view_*` call. (4) **Stop hook.** Fyr registers a Stop hook (`callback view`) next to the PreToolUse gate on every `initialize`. At the end of a run it blocks once, with the reason "Update this thread's live view (view_state or view_publish), or call view_unchanged with a reason, then stop.", when all hold: the thread has the flag and its process has the view tools, no `view_*` call happened since the run started, Fyr has not blocked in this run, `stop_hook_active` is false, and no background task runs. Otherwise it allows. The CLI's synthetic user message with the reason is dropped, its `stop-hook-error` notification is ignored, and when the forced turn called only `view_*` tools the thread keeps the summary it had before the block. (5) **Bridge.** The page and the client talk with `postMessage` and MCP Apps names: the client sends `ui/notifications/state {state}` when the page loads, when it reports `ui/notifications/initialized` and on every change; the page reports `ui/notifications/size-changed {height}` and sends a press as `ui/message {role: "user", content: [{type: "text", text}]}`. The client accepts a message only when `event.source` is the view's own iframe, a JSON-RPC 2.0 message with a known method, and a press with 1 to 2,000 characters of text. In the page, `window.fyr.state` is the state, `fyr.onState(fn)` runs on every change, `fyr.send(text)` and an element with `data-send="text"` send a press, and `data-text="a.b"` shows a state field. (6) **A press is a message.** It becomes `thread.send {text, from: "view"}`; the agent reads `[From the live view] <text>`; the log shows a compact chip "View <text>" with the usual delivery line. A press never runs an Action. (7) **Client, desktop and phone.** The view shows above the conversation (Conversation tab), in the previews' sandbox, as tall as the page reports and at most 60 % of the pane, with "Live view", an "updated N min ago" stamp and a collapse control; collapsed it shows the headline in one line (remembered per thread on the device). While a run goes that has not touched the view, the view is dimmed and the stamp adds "agent working". The inbox row shows the headline in place of the last assistant text. "Turn on live view" / "Turn off live view" is in the desktop ⋯ menu and right-click menu, "Live view on/off" in the phone ⋯ sheet, and "Live view" is a checkbox in the new-thread dialog and screen. Not in this slice: structured blocks, live GitHub bindings, state schemas, multi-file pages, templates. Only an agent process started with the live view registers the Stop hook; other threads have no hook question at the end of a turn. Turning the view on or off restarts the process after its run, so the hook follows the flag. A thread with a live view has a View tab first (View · Conversation · Changes · PR) and opens on it; the view fills the tab. The Conversation tab is always there: above the conversation it shows the view as one headline bar, and a tap on it opens View again. An explicit tab choice wins and survives a reload. The text of the turn Fyr forced to update the view shows as one quiet line, and View buttons are refused like any message while a question is open (both the owner's choice). A question or approval card is never hidden behind the View tab: it shows under the view on the desktop and over it on the phone. |
| V0.38 | **A fast client: small transfers, deltas, a free main thread.** (1) Text responses (JSON, the event stream, HTML, JS, CSS) are gzipped when the client accepts it; the stream is flushed after each event. Images and attachments pass unchanged. (2) What the host sends is lean: a tool output over 16 KB (4 KB inside a subagent) is cut to its head with a line that says how much is left, and base64 image data in a message (screenshots in tool results, which the conversation does not draw) is left out with its type and size; the event carries `cut: true`; the store keeps everything. `GET /api/threads/{id}/events/{seq}` returns one event whole, and an opened tool call with a cut output offers "Load the full output". (Found live: opening a busy thread downloaded 2.1 MB uncompressed, 85 % of it base64 screenshots inside tool results; with V0.38 the same load is 73 KB.) (3) Thread updates on the stream are deltas: after the first whole thread, a connection gets `thread.patch {id, rev, base, patch}` (a JSON Merge Patch against the copy it was last sent) whenever that is smaller; a thread goes out at most every 100 ms per connection, the newest version winning. The client applies a patch only when its copy has `rev === base`; otherwise it fetches the thread whole (`GET /api/threads/{id}`) and changes nothing meanwhile. (Measured: a one-field change of a thread with 20 agents was an 8 KB update.) (4) The device keeps a copy (IndexedDB, written when the browser is idle): the last state and the newest 400 events of each opened thread. At start the app draws the stored state at once, offline too, and asks `GET /api/state?since=<rev>` for only the threads changed since (the reply has `partial: true`); opening a thread draws its stored events and fetches only newer ones. When more than one page is new, the stored events are dropped for the fresh page. Without IndexedDB everything comes from the network as before. (5) The main thread stays free: stream messages are applied together once per frame (a short timer while hidden), a message's markdown renders once, conversation items outside the screen skip layout and paint (`content-visibility`), and following new content at the bottom never reads the layout first. Budget, checked in the layout test at 4× CPU throttling while 300 thread updates and 300 messages stream in over 3 s: no main-thread task over 100 ms and under 250 ms of long tasks in all (before: 10 long tasks, 523 ms). |
| V0.39 | **An update you can follow.** The deployer reports each phase as it happens (`build`, `test`, `drain` with the ids of the threads it waits for, `switch`, then `done` or `failed` with the reason); the host sends it on the stream as `event: deploy` and puts the latest in the state (a finished one stays two minutes), so a page that reloads still sees how it ended. The client shows a banner with a spinner and a sentence: "Updating Fyr · building the new version · 1 min", "running the tests", "waiting for “<thread>” to finish its turn" or "waiting for 2 threads to finish: …", "Restarting Fyr · this page reloads by itself" (also while the host is away during an update), then "Fyr is updated · build <id>" or, in red, "The update failed: <first line>" with Details. The page reloads by itself onto the new build (V0.13). On a desktop the banner sits at the bottom left; on a phone at the top, below the safe area. (The owner found deploys opaque.) |
| V0.40 | **A hideable thread list.** On the desktop the thread list (search, project filter, sections) can be hidden with the List button in the rail or the `\\` key; the open thread then takes its room. The owner's choice is kept on the device. Without a choice, a window narrower than 1100 px starts with the list hidden. With no thread open the list always shows, and jumping to a section from the rail (or g1–g5) shows it again. (Found live: in a narrow window the list pushed the thread off the screen.) |
| V0.41 | **Visual pacing: the outcome stands out, the work is quiet.** Each conversation item is an outcome (the owner's messages, the agent's last message of a run or the newest one while it runs, previews, a failed run, effect or agent), a step (earlier agent messages, tool groups, successful effects, answers, notes) or a run's end. Outcomes read at full weight with room before them; steps are muted, smaller, borderless and close together; consecutive steps fold into one line ("Read 3 files · ran the tests and 1 more · 9 steps · 1m 15s") that opens to every step in order, with a failed call's mark kept on the line. Not folded: a single step, system lines only, the newest steps of a run still going, and everything with tool details on. The owner's messages are pale tinted bubbles, not dark ones. (The owner found every paragraph, tool card and system line equally loud; decision 0011.) |
| V0.42 | **Projects with folders, a main folder, an automatic repo line and a host folder picker (decision 0012).** *Amends V0.23, V0.28 and V0.30.* (1) **Folders and the main folder.** A project's folder sources keep their order; one folder per host is **main** (`main: true` on the source; without one, the first folder on that host). `project.setMain {id, index}` marks a folder source main and clears the mark on the project's other folders on that host; a repo source or a bad index is 400. The state's `cwd` of a project is its main folder on this host, and a new thread in the project starts there. (2) **The repo line is automatic.** The host reads the `origin` remote of each folder of each project on this host (normalised, cached, read again after a change to a project's folders and once at start) and the project in the state and on the stream carries `repos: ["github.com/meros/fyr", …]`: read-only, deduplicated, including repo sources made by hand earlier. A derived repo counts in matching like a repo source (V0.28): clones of the repository on any host land in the project; a folder still wins over a repo. The client has no repo field anymore; it shows "Also matches clones of <repo>". The word "source" is gone from the UI: the project screen lists **Folders** ("Threads started in these folders land here."), each with a star (filled: "Main folder: new threads start here."; empty: make it main) and ×, and "Add folder…" opens the folder picker. A project without folders says "No folder: you file threads here by hand." (3) **No project, no folder: a scratch folder.** `thread.create` takes `projectId?`; it files the thread there by hand in the same write. Without `cwd` the thread starts in the project's main folder on this host; with neither a `cwd` nor a project folder here the host makes `~/.local/share/fyr/scratch/<YYYYMMDD-HHMMSS>-<slug>` (slug: the first words of the text, lower case, letters and digits joined by `-`, at most 40 characters, else `thread`) and the thread has "No project" unless a project was given. Fyr's scratch folder is always a scratch root. The new-thread screen shows "A fresh scratch folder" when nothing is picked. (4) **The host folder picker.** One picker (a dialog on the desktop, a sheet on the phone) with one field: a word searches every section, a path (`~/…` or `/…`) browses there and completes. Sections: **This project** (the project's other folders, from "Change" in a new thread), **Recent** (distinct folders of threads, newest first), **Repos found** (git repositories found by a scan of the project folders on this host and the configured `scanRoots`, two levels deep: a folder with `.git` counts and is not descended into; hidden folders and `node_modules` are skipped; cached for five minutes and refreshed in the background), **Browse** (the subfolders of a folder starting at `~`, breadcrumbs back up, "Use this folder") and **New folder** (a name without `/`, not empty, not taken, made in the folder being browsed). Read-only host calls: `GET /api/fs/list?path=`, `GET /api/fs/recent`, `GET /api/fs/repos`, and `POST /api/fs/mkdir {parent, name}` (X-Fyr). Every path resolves through symlinks and must stay inside the owner's home folder, else 400; listing skips links that lead outside. The picker serves "Change" in the new thread (project folders first), "Add folder…" on the project screen and in the create step. (5) **The create step** lists the folders the project starts with, each "Threads started in <folder> land in this project.", "Add folder…", and the derived repo line; without folders: "No folder: you file threads here by hand." The config option `scanRoots` (`services.fyr.scanRoots`, a list of strings, default empty, `~` expanded) sets the extra scan roots. (The owner did not understand how folders take part in the picker; decision 0012.) |
| V0.43 | **The PR tab: read a PR, act on it, hand it to the agent.** (1) **Detail.** `GET /api/threads/{id}/pr` returns the thread's PR as the host sees it with `gh`: number, URL, repo, title, body (markdown), state, draft, author, head and base branch, additions, deletions and changed files, labels, `mergeable` and `mergeState` (GitHub's merge state status), the review decision, the latest review per reviewer, every check (name, workflow, `state` fail\|running\|pass\|skipped, start and end time, details URL; a CheckRun and a commit status map to the same shape), and the unresolved review threads (path, line, outdated, comments with author, text and time; the first 50 threads). The host caches it 30 s per PR; `?fresh=1` fetches again. A failure (gh not signed in, no network) is `200 {error}` so the tab can say it; a thread without a PR is `200 {error}` too, an unknown thread 404. The PR snapshot the poller keeps in the thread (V0.8) stays as it is. (2) **Merge and re-run** are owner commands on the command path, with receipts: `pr.merge {method: squash\|merge\|rebase}` (default squash) and `pr.rerunFailed`. Both are unavailable without an open PR; Merge also with a merge conflict or requested changes. The host merges with the same code as the `pr.merge` effect, after reading the PR again: it refuses a closed or draft PR and a merge state of conflicts, blocked or behind, with the reason. Re-run reruns, with `gh run rerun <id> --failed`, each failed, cancelled or timed-out workflow run of the PR's head commit, and nothing else. The result is an `effect.result` line in the thread (`by: owner`); the agent is not woken. The PreToolUse gate still stops the agent from merging (V0.12). (3) **Ask the agent.** A failed check offers "Ask the agent to fix this check": it sends the owner's message "The check <name> failed: <details URL>. Find the cause and fix it.". Unresolved review threads offer "Ask the agent to address N review comments": one message that lists each as `path:line` and its first comment, each comment cut at 300 characters. Both go through `thread.send` and are unavailable while `thread.send` is. (4) **The tab**, desktop and phone, important first: the header (title, `#number`, a state pill, Draft, head → base, author, `+N −M` and the file count, which opens the Changes tab); one merge status line in plain words ("Ready to merge", "Blocked: 1 failing check", "Conflicts with main", "Review required", "Changes requested", "Behind main", "Checks running", "Merged", "Closed"); the actions (Merge…, Re-run failed, Open on GitHub; Dismiss PR once merged or closed, V0.36); checks with the failed ones first, then the running ones, and the passed and skipped ones folded into one line ("12 passed ▸"); reviews; the unresolved threads folded per file, the comment text readable, each with a link to GitHub; labels; the description as markdown, folded after about 12 lines. Merge… opens a small dialog: method (Squash, the default; Merge commit; Rebase) and one confirm button that names the result ("Squash and merge #88"). Merge is disabled, with the reason as its label or title, while the status line is not Ready to merge, or the PR is not open. Loading and error states read in plain words; Refresh fetches fresh, and "updated N s ago" says how old the data is. The phone uses 44 px targets and keeps the actions and the dialog inside the safe areas. The PR chip on the inbox row and in the thread header uses the same words: its label is short ("#88 failing", "#88 conflicts", "#88 review", "#88 ready", "#88 running", "#88 merged"), its title the status line. |
| V0.44 | **Provider quota where a profile is chosen.** The host reads how much of each profile's provider allowance is left, with the logins stored on the host, and the client shows it calmly. (1) **Kinds.** Each profile has a quota kind: `anthropic-oauth` (a Claude subscription: the CLI's OAuth login in `<configDir>/.credentials.json`, field `claudeAiOauth.accessToken`; `configDir` defaults to the profile's `CLAUDE_CONFIG_DIR`, else `~/.claude`; `GET https://api.anthropic.com/api/oauth/usage` with `anthropic-beta: oauth-2025-04-20` gives the windows "5 h" and "7 d", each with used % and reset time), `deepseek` (`GET <baseURL>/user/balance`, default `https://api.deepseek.com`), `openrouter` (`GET <baseURL>/key`, default `https://openrouter.ai/api/v1`: the key's remaining limit, else what it has spent) or `none`. The two key kinds read an API key from `keyFile` (a path, `~` expanded) or `keyEnv` (an environment variable, looked up in the profile's `env` and then the daemon's; default `DEEPSEEK_API_KEY`, `OPENROUTER_API_KEY`). The kind and its fields come from the config's `quota` map by profile id, else the profile's own `quota`; without either, a profile whose binary is `claude` and whose env sets no `ANTHROPIC_BASE_URL` is `anthropic-oauth`, every other profile `none`. NixOS: `services.fyr.quota.<profileId> = {kind; configDir; keyEnv; keyFile; baseURL}`. (2) **Reading.** `GET /api/quota` reads providers whose data is older than 5 minutes and answers with every profile that is not `none`. A rate limit (HTTP 429 or a `rate_limit_error`) waits 5, 10, 20, then 30 minutes before the next try; the data from before stays, marked stale, with a note. An expired or rejected Claude login says "Login expired; run this profile once" (the host never refreshes a token). Between reads, the CLI's `rate_limit_event` on a running session of an `anthropic-oauth` profile updates its windows for free. A token or key never appears in a log line, an error or a reply. (3) **The client** shows one quiet line per profile, "Claude 5 h 34 % · 7 d 61 %" or "DeepSeek ¥12.40 left", under the profile picker of the new-thread dialog (desktop) and screen (phone), and the busiest window per profile in the rail's footer (desktop). A window at 80 % or more is coloured; nothing else is. Hover (desktop) or tap (phone) shows the reset times in local time, when the data was read, and the stale or error note. (The owner switched profiles without knowing which account had room left.) |
| V0.46 | **Quiet hours per project (decision 0014).** (1) **The setting.** A project may carry `quiet: {from: "17:00", to: "06:00", days?: ["mon"…"sun"]}`: times are `HH:MM` in the host's local time zone; a window whose `to` is not after `from` crosses midnight (`from` equal to `to` is a whole day); `days` are the days a window *starts* on, every day when absent or empty, so Fri 17:00–06:00 runs into Saturday morning. `project.update {quiet}` sets it and `quiet: null` clears it; a bad time or day is 400. (2) **Quiet now.** The host computes, per thread whose project is quiet at this moment, `quietUntil` (RFC 3339, the end of the current window) in the thread DTO, and the same `quietUntil` on the project; both are absent otherwise. The host sends the changed threads and the project on the stream when a window starts or ends (its tick), and when `quiet` changes. (3) **No notifications.** No Web Push for a thread while it carries `quietUntil`, including a done or blocked push whose 20 s wait ends inside the window. Nothing is queued: when the window ends no push is sent for what happened during it. (4) **Hidden from the inbox.** While quiet, the project's threads are left out of the inbox sections on desktop and phone, of the rail and section counts, of the "Needs you" count in the title and of the next-thread order. Picking the project in the project filter shows them, and the filter chip (desktop) or the filter line (phone) says "Quiet until 06:00". Search (desktop and phone) still finds them. The client re-renders at the nearest `quietUntil` with one timer, so the threads reappear when the window ends, without any notification ("back silently"). (5) **The project screen** has a row "Quiet hours": off, or from and to (time fields) with the seven day toggles (Mon–Sun), and one line "Threads hide and send no notifications from 17:00 to 06:00." (with the days when not every day). (6) An open thread of a quiet project works as always: the live view, decision cards and message delivery are unchanged. (The owner: "Add per project quiet hours! Hide threads by default and dont notify".) |

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

### 3.1 Identifiers
- `HostId`: random 128-bit id.
- `EntityId`, `DocumentId`, `RuleId`, `JobId`: ulid. They are globally unique and do not depend on the host.
- `EventId`: `(HostId, seq)`. `seq` strictly increases per host, with no gaps. An EventId identifies an event. It is never used to order events of different hosts.
- `CommandId`: UUID chosen by the caller. Jobs derive theirs as `H(jobId, step, index)` (8.1). Rule firings use `H(ruleId, firing)` (7.3).

### 3.2 Entity

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

### 3.3 Conversation (component of Thread and Coordinator)

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

type EffectResult = { decision: EntityId; call: ActionCall; outcome: "ok" | "failed" | "denied" | "expired"; text: string; at: HLC };

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

- **Native subagents and background tasks are part of a run.**
  - The Agent's own subagents (the Task tool) are not entities. The parent run's `subagents` field records them, and the view shows them collapsed inside the run.
  - A subagent's question becomes a Decision that blocks the parent run. The Decision card names the subagent that asked (13.2).
  - Background commands and monitors are recorded in `background`. While an `awaited` background task runs after the run ends, the projection holds a Wait with source `background`.
- **Waits.** `waits` is the only input for the state `waiting`. It is filled by:
  - admission that returned `wait` (8.2), source `admission`;
  - an armed Rule with purpose `wait` (7.5), source `rule`;
  - an awaited background task, source `background`;
  - a module Signal with state `waiting`, source `signal`.
- **Current run:** the last run that is `queued` or `running`.
- **Last terminal run:** the last run that has ended.

**Run end classes and attention** (I10). This table is total. 6.2 uses it.

| Status | Class | Attention after the run |
|---|---|---|
| completed | ok | done (row 9) |
| failed | auth, api, mcp_startup, workspace_missing, error, admission_denied | failed (row 3) |
| interrupted | user | seen (row 10); the user stopped it |
| interrupted | restart, budget, stalled | failed (row 3) |
| interrupted | usage_limit | waiting (row 7), through the wait Rule that the `claude` module creates at that run end (7.6) |
| interrupted | paused | waiting (row 7) while the governor's wait Rule is armed; failed (row 3) if no wait Rule is armed |
| interrupted | moved | the run is withdrawn and queued again on the target; no attention change |

### 3.4 Thread (built-in kind `thread`, replicated, movable, has a Conversation)

```ts
type Thread = EntityHeader & Conversation & {
  title: string;
  checkout: Ref | null;           // null = a scratch thread in its own folder
  worktree: Ref | null; branch: Ref | null;   // observed; also set when the agent creates a worktree itself
  mode: "plan" | "ask" | "auto";  // default "auto"
  modeCeiling: "plan" | "ask" | "auto";   // I5; inherited from the lineage parent
  lifecycle: Lifecycle;           // work state, shared by all users who see the thread
  lastActivityAt: number;         // HLC
  lineage: { kind: "fork" | "spawn"; from: EntityRef; atEseq: number } | null;
  actedOnBy: { coordinator: EntityId; action: string; at: number } | null;   // shown in the header (13.5)
};

type Lifecycle =
  | { state: "active" }
  | { state: "settled"; since: number; by: Principal }
  | { state: "archived"; since: number };

// Per (user, entity); R19. v1 has one View per thread.
type View = {
  user: UserId; entity: EntityRef;
  snooze: { ruleId: RuleId; since: number } | null;
  pinned: boolean;
  lastVisited: number;            // eseq watermark (6.4)
  failureAck: number;             // eseq of the last action by this user that acknowledged a failure (6.2 row 3)
};
```

"Snoozed" is a View state, not a lifecycle state: one user can snooze a thread that another user still watches. In the rest of this spec, "the thread is snoozed" means "the acting user's View of the thread has `snooze` set".

**Invariants**
- **L1 (I4).** A View's `snooze` is set if and only if its snooze Rule's status is `armed` or `suspended`. Every change of that Rule to `disarmed`, for any cause (a firing, `thread.wake`, `rule.disarm`, the rejection limit, the firing limit, a denied approval), is in the same transaction as clearing `snooze`.
- **L2.** A blocking Decision entry (5.6) for a run requires that run to be current. When the run ends, that entry becomes `parked` in the same transaction.
- **L3.** An `archived` thread has no current run, no armed Rules and no `open` Decisions. Agent-principal Actions on an archived thread are unavailable. Its final `run.ended` cancels any Decision or Rule that was created in the archive window.
- **Settle and archive clear snoozes.** `thread.settle` and `thread.archive` clear the `snooze` of every View of the thread (and disarm their Rules, L1).
- **I3. Suspend reasons.** A Rule is `suspended` iff `suspendedBy` is not empty. `settle ∈ suspendedBy` iff the Rule's purpose is `watch` and the owner's lifecycle is `settled`. `archive ∈ suspendedBy` iff the owner is `archived`. `move ∈ suspendedBy` iff the owner is fenced. `decision ∈ suspendedBy` iff the Rule's last firing had the outcome `approvalRequested` and that Decision is still open (7.3).
- **I5. Modes never widen down a lineage.** A thread's `modeCeiling` is at most its lineage parent's effective mode. The effective mode of a run is `min(thread.mode, thread.modeCeiling, ceiling of the triggering principal)`. Only a `user` with `confirm` raises a ceiling (`thread.setMode`). The order is `plan < ask < auto`.

### 3.5 Decision (built-in kind `decision`, replicated, moves with its threads) (D1)

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

### 3.6 Coordinator (built-in kind `coordinator`, replicated, movable, has a Conversation) (D4, post-v1)

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

### 3.7 Ref and Link

```ts
type Ref = { kind: string; key: string };
type Link = { thread: EntityId; role: "serves" | "owns" | "touches"; ref: Ref; source: Principal | "observed" };
```

**Core Ref kinds** (in addition to every entity kind):

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

What a thread waits for is a Wait (3.3), not a Link.

### 3.8 Event

```ts
type Event = { id: EventId; at: HLC; source: "intent" | "observation";
  subject: EntityRef | null; eseq: number | null;     // set together
  affects: EntityRef[];                               // entities whose projection or attention must be recomputed
  type: string; data: unknown;
  cause: { command: CommandId } | { job: JobId } | { rule: RuleId; firing: number } | null };
```

- **`source`.**
  - An event that `apply` writes is `intent`.
  - An event that the runtime or a module Observer writes is `observation`.
- **Observed values.** An observation event stores the value it observed. Projections, Rules and Attention are therefore deterministic over the log.
- **Subjects that change residence.** An entity's events are in the logs of its successive residences.
  - On a move, the new residence copies the entity's earlier events into a read-only `imported_events` table, keyed by EventId and ordered by `eseq`.
  - Reads by `eseq` therefore work on the new residence.
- **Retention.** Events are kept forever in v1.

### 3.9 Document

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

### 3.10 Checkpoint
Before and after each run, the `git` module writes the worktree's full tree to `refs/fyr/checkpoints/<entityId>/<runId>/{before,after}`. It uses a temporary index. These checkpoints make `thread.fork` at a run, rollback, the Changes tab (13.5) and replay possible.

### 3.11 User presence
- **Per device.** Each client reports `user.presence(state: "active" | "inactive" | "away")` for its own device to every connected host. A client reports at most once a minute, and on each change.
- **Projection.** Each host projects the last report of every device, grouped by user.
- **Use.** Sinks receive the presence of every device of the user concerned (12.2). The `push` Sink uses it (6.3). The away strip uses the user's own last `active` time (13.1 U13).

### 3.12 Users, ownership and views (R19)
v1 has one user. The fields below exist from day one, filled with that user's id, so a later multi-user host needs no migration.

- **User identity.**
  - `UserId`: ulid. A host stores its users; v1 creates one at first start.
  - Devices, passkeys, push subscriptions and device tokens belong to a user.
  - The principal is `user(userId, deviceId)`. Every event caused by a user records the `userId` in its `cause`.
- **Ownership and visibility.** Every entity has `owner` and `visibility` (3.2). v1: `owner` is the one user, `visibility` is private. A user sees an entity when they own it or it is shared with them.
- **Work state and view state.**
  - Work state is on the entity and shared: runs, Decisions, Links, lifecycle (`active`, `settled`, `archived`).
  - View state is per `(userId, entityId)` (the View, 3.4): snooze, pin, read watermark, failure acknowledgement. Section collapse, the away-strip cursor, lenses and `hours` Documents are per user too.
  - Snooze Rules are owned by the entity and carry `viewer: UserId` (7.1).
- **Attention per user.** `attention(entity, view, …)` takes the user's View (6.1). The `inbox` stream is keyed by user (11.1).
- **Decisions.** Every Decision has an `assignee` (3.5). Only the assignee may answer; delegation to another user is post-v1. A `verified` proof must come from a passkey of the answering user.
- **Profiles and credentials.** Agent profiles, the Claude account, the GitHub identity and the tokens that the `Credentials` point gives to sessions belong to a user. A session uses the profile and credentials of the thread's owner.
- **Host roles.**
  - `admin`: configures modules, the host policy and slots.
  - `user`: may add their own policy layer, which can only raise levels above the host policy, never lower them.
  - v1: the one user is `admin`.
- **Fairness.** The governor counts slots per user within the host limits (8.3). With one user this is the plain host limit.
- **Not covered.** Isolation between users at the OS level (separate Unix users, containers) is an open question (Q8). Until it is solved, a multi-user host trusts all its users at the OS level.

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

### 5.1 Definition

```ts
type Action<P> = {
  id: string;
  params: Schema<P>;
  target: (p: P) => EntityRef | HostId;
  callers: { agent: "never" | "viaEffect" | "ownThread" | "ownLineage" | "anyThread"; coordinator: boolean; rule: boolean };
  defaultLevel: AssuranceLevel;
  effect: "state" | "external";    // external: Fyr executes it as an async effect when an agent asks (5.10)
  restoresVisibility: boolean;     // Guards do not apply (5.2)
  available(s: Projection, p: P, by: Principal): true | { reason: string };
  apply(s: Projection, p: P, by: Principal): {
    events: EventDraft[]; jobs: JobSpec[]; rules: RuleSpec[];
    calls: ActionCall[];       // nested Actions, applied in the same transaction (A6)
  };
};
type Command = { id: CommandId; call: ActionCall; by: Principal; approval?: ApprovalProof };
```

Undo is not part of an Action. Undo is a deferred Rule (7.5).

**Rules**
- **A1.** Every intent event comes from `apply`.
- **A2.** `apply` is pure. All I/O runs in Jobs.
- **A3.** Every surface calls Actions through the same command path. The surfaces are:
  - the palette, menus, keys and bulk selection;
  - agent and coordinator MCP tools;
  - the CLI and the capture endpoint (11.4);
  - Rules, Jobs and Observers;
  - peer hosts.

  A surface may hide an Action. It may not change its behaviour or its availability.
- **A4 (I7). Receipts.** The host keeps one receipt per `CommandId`, holding the terminal outcome only (`ok` or a permanent rejection). A transient rejection (host offline, resident elsewhere, fenced, admission) is not stored, so a retry with the same id is tried again. Receipts are kept 7 days; receipts of Job-derived ids are kept until the Job's `job.ended` plus 7 days. For an async effect (5.10), the receipt of the `effect.request` CommandId holds `{decision}`; the result of the executed effect is stored in a second receipt keyed `H(commandId, "effect")`.
- **A5.** A rejection returns the reason from `available` (5.2 Guards included). Every surface shows that reason.
- **A6. Transactions and nested calls.** A host applies commands one at a time.
  - The calls in `calls` are applied one after another, each against the projection that the earlier calls left. Each call is checked like a command: `callers`, Guards, `available` and level.
  - The depth of nested calls is at most 4.
  - If any call is unavailable, the whole command is rejected with that call's reason.
  - If any call has a level above `none` and the principal is not a `user` with a valid proof, the whole command becomes one approval Decision whose `payloadHash` covers the whole call tree (K13).
  - Each call runs with the principal of the command.
  - A call to an entity on another host is not allowed in `calls`. It must go through a Job.
- **A7.** A command whose target is an entity goes to the entity's residence. A host that is not the residence rejects it. A fenced entity (10.4) rejects all commands except the move steps, with the reason "moving".

### 5.2 Principals, Guards and limits

**What each principal may call:**
- `user(userId, deviceId)`: all Actions on entities the user can see, subject to the level.
- `agent(entityId)`: as `callers.agent` allows.
  - `viaEffect`: only through `effect.request` on the agent's own thread (5.10); a direct call is rejected.
  - `ownThread`: only the agent's own thread.
  - `ownLineage`: threads with the same lineage root, or threads that `serve` the same Ref of a kind with `lineageScope: true`.
- `coordinator(id)`: an Action with `callers.coordinator`, only if its valid grant (5.8) allows the Action, every target entity and the host.
- `rule(id)`: an Action with `callers.rule`. The call must also pass the check of the Rule's `createdBy` principal.
- `system(component)`: all Actions. A level above `none` opens a Decision.

**Modes (I5).** A thread started by an agent, a coordinator, a Rule or a Job gets `modeCeiling = min(parent's effective mode, the requested mode)`. A coordinator's sends to a thread run with `min(thread mode, grant.modeCeiling)`; the coordinator cannot change the thread's own mode.

**Guards.** A module may contribute Guards (12.2). The `available` result of every Action is the Action's own check AND every Guard's `check(call, projection, by)`. The reason shown is the first that fails. This is how budgets block work. Guards do not apply to Actions with `restoresVisibility: true` (`thread.wake`, `thread.unsettle`, `thread.unarchive`), so a Guard can never keep a thread hidden (K7).

**Lineage quotas (I6).** Every thread start, every armed Rule and every token is charged to the lineage root of the principal that caused it, across every principal derived from that root (its agents, Rules, `system` on its behalf) and across hosts. The root holds the counters. A cross-host start reserves its share with an event on the root before the start. Defaults per root:
- 10 thread starts per hour;
- 5 armed Rules created by agents;
- the root's `budget`, or the coordinator's `grant.budget`.

**Firing limits.** A Rule fires at most once per 10 s and 100 times per day. Beyond that it is disarmed (7.3).

**Other limits.** A thread that a coordinator started cannot create a coordinator.

### 5.3 Core Actions

| Action | Target | Params | Agent | Coord. | Rule | Level | Notes |
|---|---|---|---|---|---|---|---|
| `thread.start` | host | checkout?, branch?, worktree?: new\|existing, prompt, attachments?, documents?, lineage?, serves?, mode?, profile?, budget?, defaults: all\|none\|RuleKind[] | ownLineage | yes | yes | none | `defaults` chooses the DefaultRules (7.6). Coordinator- and workflow-started threads default to `none`. The mode is capped by I5. |
| `thread.send` | thread | text, attachments, documents?, delivery: steer\|queue | ownThread | yes | yes | none | Clears the sender's snooze. From settled: → active. Unavailable when archived. Acknowledges a failure (6.2 row 3). |
| `thread.retry` | thread | – | never | yes | yes | none | Queues the last input again. Acknowledges a failure. |
| `thread.interrupt` | thread | – | never | yes | no | none | Ends the run with class `user`. |
| `thread.fork` | thread | atRun | ownLineage | yes | no | none | New thread from the run's `before` checkpoint. Mode capped by I5. |
| `thread.setMode` | thread | mode | never | no | no | confirm when the mode widens | Raises `modeCeiling` too. |
| `thread.snooze` | thread | until: Condition | ownThread | yes | yes | none | Unavailable when archived. Replaces an earlier snooze Rule. → snoozed |
| `thread.wake` | thread | – | never | yes | yes | none | Restores visibility. Clears the View's snooze. |
| `thread.settle` | thread | – | ownThread | yes | yes | none | Unavailable with a current run or an open blocking Decision. Adds `settle` to the `watch` Rules' `suspendedBy`. Acknowledges a failure. → settled |
| `thread.settleWhen` | thread | when: `done` \| `merged` | ownThread | yes | no | none | Creates a Rule (`runEnded(self)` or `prMerged(serves PR)`) → `thread.settle`, `after: disarm`, `createdBy` = the caller. Counts toward the lineage quota. |
| `thread.snoozeWhenDone` | thread | until | ownThread | yes | no | none | Creates a Rule `runEnded(self)` → `thread.snooze(until)`, `createdBy` = the caller. |
| `thread.unsettle` | thread | – | never | no | no | none | Restores visibility. Removes `settle` from `suspendedBy`. → active |
| `thread.archive` | thread | – | never | no | deferred only | none | Interrupts the run. Detaches the thread from its Decisions (cancels those it alone blocks). Adds `archive` to all its Rules' `suspendedBy`. Starts the Job `worktree.cleanup` after 10 min. Acknowledges a failure. → archived |
| `thread.unarchive` | thread | – | never | no | no | none | Restores visibility. Unavailable while the cleanup Job runs. Cancels a pending cleanup. Removes `archive` from `suspendedBy`. → active |
| `thread.pin`, `thread.unpin` | thread | – | never | no | no | none | |
| `thread.visit` | thread | eseq | never | no | no | none | The watermark only moves forward (6.4). Only `thread.markUnseen` moves it back. |
| `thread.markUnseen` | thread | – | never | no | no | none | Moves `lastVisited` back to just before the last terminal run. Unavailable without a completed run (6.4). |
| `entity.move` | entity | toHost | never | no | yes | confirm (user); policy (rule, system) | Post-v1. Unavailable while a run is running or a blocking Decision is open. Job `move` (10.4). |
| `entity.import` | host | bundle, epoch, moveId | never | no | no | none | Post-v1. Called by the `move` Job on the target. Rejected for an aborted `moveId`. |
| `entity.abortMove` | host | entity, epoch, moveId | never | no | no | none | Post-v1. Called on the target. Writes a tombstone; acknowledged. |
| `entity.finishMove`, `entity.cancelMove` | entity | epoch, moveId | never | no | no | none | Post-v1. Called by the `move` Job on the source. `cancelMove` needs the target's abort acknowledgement (I2). |
| `decision.open` | decision | kind ∈ {question, plan}, blocks, subject, default?, deadline?, dedupKey | ownThread | yes (kind `proposal` only) | no | none | 5.6. Approvals are opened only by core paths (5.10, 12.3). |
| `decision.answer` | decision | answer | never | 5.6 | no | the Decision's level | |
| `effect.request` | thread | call: ActionCall | ownThread | yes | no | none | 5.10. Opens an async approval Decision for an `external` Action. |
| `link.add`, `link.remove` | thread | role ∈ {serves, touches}, ref | ownThread | yes | yes | none | |
| `rule.create` | owner entity | RuleSpec | ownThread | yes | no | none | Counts toward the lineage quota. |
| `rule.disarm` | owner entity | rule | own Rules | own Rules | no | none | Disarming a snooze Rule clears the View's snooze (L1). |
| `document.create` | host | schema, about, body, key?, supersedes?, signature? | ownThread | yes | no | none (signed schemas need `verified`) | |
| `document.get` | host | id | ownThread (about it or given to it) | yes | no | none | Read query (5.11) |
| `document.send` | host | document, to: Ref (person or sink) | viaEffect | yes (sinks only) | yes (sinks only) | confirm for `person`; none for `sink` | `effect: external`. Job `deliver`. |
| `thread.handover` | thread | to: freshSession \| childThread | ownThread | yes | yes | none | Job `handover` (10.3) |
| `checkout.add` | host | repo, path? | never | no | no | none | Job `clone` |
| `coordinator.create` | host | name, grant | never | no | no | verified | Post-v1. 5.8 |
| `coordinator.setGrant` | coordinator | grant | never | no | no | verified | Post-v1. |
| `coordinator.wake` | coordinator | reason | never | no | yes | none | Post-v1. Starts a run with the reason as a digest. |
| `coordinator.stop` | coordinator | cascade | never | no | no | none | Post-v1. Kill switch. With `cascade`, interrupts every thread the coordinator started. |
| `coordinator.start` | coordinator | – | never | no | no | none | Post-v1. |
| `coordinator.acknowledge` | coordinator | eseq | never | no | no | none | Post-v1. Sent by the pane (6.2 row 3). |
| `policy.set` | host | policy Document | never | no | no | verified | Post-v1 (5.4). |
| `user.presence` | host | state | never | no | no | none | 3.11 |
| `host.pair`, `host.pairPeer` | host | – | never | no | no | verified | 11.3. v1: host CLI only (5.4). `pairPeer` post-v1. |
| `host.revokeDevice`, `host.revokePeer` | host | id | never | no | no | verified | |
| `host.rename` | host | name | never | no | no | none | |
| `job.acknowledge` | host | job | never | no | no | none | Removes a failed Job's host row (8.1). |

**View Actions (R19).** `thread.snooze`, `thread.wake`, `thread.snoozeWhenDone`, `thread.pin`, `thread.unpin`, `thread.visit` and `thread.markUnseen` change the acting user's View (3.4), not the thread. A Rule acts on the View of its `viewer`, or of the thread's owner when `viewer` is null. An agent's `thread.snooze` acts on the owner's View.

**Thread lifecycle transitions (complete; work state)**

| From | To | By |
|---|---|---|
| active | settled | `thread.settle` (clears every View's snooze, L1) |
| settled | active | `thread.unsettle`, `thread.send` (both remove `settle` from `suspendedBy`) |
| active, settled | archived | `thread.archive` (clears every View's snooze, L1) |
| archived | active | `thread.unarchive` |

**View snooze transitions (complete)**

| From | To | By |
|---|---|---|
| not snoozed | snoozed | `thread.snooze` (unavailable when archived) |
| snoozed | snoozed | `thread.snooze` (replaces the Rule) |
| snoozed | not snoozed | `thread.wake` (by the user, or by the snooze Rule's firing); any disarm of the snooze Rule (L1); `thread.send` by that user; `thread.settle`; `thread.archive` |

Every transition that leaves `settled` removes `settle` from the `watch` Rules' `suspendedBy`, by I3. A move does not change the lifecycle or the Views.

### 5.4 Assurance levels (R15)

```ts
type AssuranceLevel = "none" | "confirm" | "verified";
```

| Level | Proof |
|---|---|
| `none` | None. |
| `confirm` | A `user` principal sends the command after an explicit confirmation step. |
| `verified` | A WebAuthn assertion with user verification over a host-issued challenge `H(hostId, decisionId or commandId, nonce, payloadHash)`. Single use; it expires after 5 min (11.2). Post-v1. |

```ts
type ApprovalProof =
  | { level: "confirm"; payloadHash: string }          // the client showed the confirmation and echoes the hash it showed
  | { level: "verified"; assertion: WebAuthnAssertion }; // post-v1
```

**`verified` in v1.** The Actions at level `verified` (`host.pair`, `host.revokeDevice`) are available in v1 only through the host CLI (`fyr …`). The CLI runs as `system(cli)` on the host; local OS access is its proof. Pairing in v1 registers no passkey.

**Where the level comes from.**
- `LevelPolicy` is a pure function: `level(call, principal, entity, policy) → AssuranceLevel`.
- **v1:** the host loads the static policy file at start as a `policy` Document with `createdBy: system(policy)`. `policy.set` is post-v1.
- The active policy is the latest accepted `policy` Document.
- `Action.defaultLevel` applies when no rule matches.
- Each gated call writes `level.evaluated`. That event records the active level, and the level of every Document marked `shadow: true`.

**Who can supply a proof.**
- Principals other than `user` cannot supply a proof. Their call opens an approval Decision (5.10). When the user answers with a valid proof, the call runs once, as the original principal, after `available` is checked again.
- No coordinator and no agent can answer an approval Decision, at any level. No grant can widen this.

**Agent tool calls** go through the core tool gate (12.3).
- The policy's Bash patterns are a convenience, not a security boundary.
- The boundaries are the thread's effective mode (I5), the OS user and the cgroup, and the credentials that the session receives (5.9).
- A tool call that matches no rule gets `defaultsByMode[mode]`. The defaults are: `auto`: none; `ask` and `plan`: confirm.

**Delivery** (push, inbox) is separate from the level.

### 5.5 Allowances
When the user approves a blocking Decision, they can choose "allow this exact call in this thread for 10 minutes". The host then stores an **allowance** keyed by `(thread, payloadHash)`. An allowance is never widened to similar calls and never applies to another thread (I8). ("Grant" means only the coordinator's grant, 5.8.)

### 5.6 Decisions (D1)

**Who opens which kind**

| Kind | Opened by |
|---|---|
| `question` | the `claude` module for AskUserQuestion; agents with `decision.open` |
| `plan` | the `claude` module for ExitPlanMode |
| `approval` | only core paths: `effect.request` (async, 5.10), the tool gate (blocking, 12.3), and A6 (a nested call above `none`) |
| `proposal` | only coordinators |

**Merging**
- On one host, a new Decision whose `dedupKey` equals that of an open Decision merges into it (`decision.merged`). The new Blocker is added.
- For approvals the core sets `dedupKey := payloadHash`, so only identical calls merge (I8).
- Across hosts (post-v1), clients group Decisions into one row only when their `payloadHash` is equal. Answering the row sends one `decision.answer` per Decision, each with its own proof.

**Default and deadline**
- **Default.** `default.answer` is taken at `default.at` (`decision.defaulted`). A default never approves: for an `approval` above `none`, the default must be `deny`.
- **Deadline.** The default deadline is 24 h. When it passes, `decision.expired` is written, and it counts as a deny.

**Parking (blocking Decisions only)**
- A Blocker becomes `parked` when its run ends (L2), when it has been open for 10 min and the governor needs the session slot, or when the host restarts.
- A parked Blocker keeps the Decision's subject.

**Answering**
- **An open Blocker.** An answer resolves that run's waiting SDK callback.
- **A parked Blocker.**
  - An approval creates an allowance for that thread, then sends "The user approved: <subject>. Continue." to the thread.
  - A question answer is sent to the thread as a user message.
- **A fenced entity.** Answers to Decisions that block a fenced entity are rejected with "moving". The client retries.
- **Who wins.** The first valid answer wins. A later answer is rejected with "already answered".

**Proposals**
- A coordinator opens a Decision with `kind: "proposal"` and `subject: ActionCall[]`, each call with a one-line `why`.
- The user accepts all calls, a subset, or none ("Accept 2", "Decline all").
- Each accepted call runs as `coordinator(id)` and has its own level.

**Who may answer**

| Principal | May answer |
|---|---|
| `user` | any kind, if the user is the `assignee` (R19; v1: always; delegation is post-v1) |
| `coordinator` | a `question` whose every Blocker is in its grant, if the grant lists `decision.answer`. Never `approval`, `plan` or `proposal`. |
| others | nothing |

### 5.7 Coordinator Decisions and attention
A coordinator can block on its own Decision (a Blocker with the coordinator as entity). The coordinator then shows as `asking` (6.3).

### 5.8 Grants (post-v1)
- A grant is a `grant` Document. Its `signature` is the user's WebAuthn assertion over `H(body)` at level `verified`.
- Grants are replicated eagerly (2). Every host verifies the signature before it accepts a `coordinator(id)` command, and checks that the grant is the head of its `supersedes` chain.
- A grant expires at `expiresAt`, which is at most 30 days after signing.
- A coordinator without a valid grant is stopped with status `failed` and the reason "grant expired".

### 5.9 Credentials
- **Session credentials.** A session never receives the user's own full-power credentials. The `Credentials` extension point (12.2) gives each session its environment and files.
- **GitHub credentials.** The `github` module gives sessions a token that can push branches and open, edit and comment on PRs. Branch rulesets on each repo's default branch allow merges only by Fyr's merge identity, which is a GitHub App. `pr.merge` is the only Action that uses that identity.
- **Setup.** `fyr doctor` checks the rulesets.
- **Limit.** Without the rulesets, "never merge without the user" cannot be enforced. The setup check reports that as an error.

### 5.10 Async effects (D5)

An **external** Action (`effect: "external"`) changes something outside Fyr's state: `pr.merge`, `pr.close`, `issue.close`, `document.send`, a deploy Action, a force push Action.

**Flow**
1. **Request.** The agent calls the MCP tool `effect.request(call)`. The core computes the level for `call` with the agent as principal.
   - Level `none`: the call is submitted at once as `agent(id)` through `effect.request`, and the tool returns the result. For an `external` Action the `callers.agent` check is `viaEffect`.
   - Level above `none`: the core opens a Decision with `kind: approval`, `style: async`, the exact `call` as subject, `requestedBy` = the principal chain, and a Blocker for the thread with `runId: null`. The tool returns at once: "Requested. Waiting for the user's approval (Decision <id>)."
2. **The run continues or ends.** Nothing waits inside the run. No SDK callback and no slot are held.
3. **Approval.** The user answers the Decision with the level's proof.
4. **Execution.** The core starts a Job of the Action's kind. The Job re-checks `available` for the exact hashed call, as the original principal through `effect.request` (so `viaEffect` passes), then executes it. A call that is no longer available ends with the reason (for example "checks failed on the new head commit").
5. **Result.** The Job writes the result into the receipt `H(original CommandId, "effect")` (A4) and appends `effect.result` to the thread, for example "PR #4170 merged after your approval, 10:12, squash a1b2c3d".
6. **Context.** The `claude` module prepends every unseen `effect.result` to the next input of the thread's session. Then it appends `effect.delivered`, which removes them from `effects`.
7. **Wake (optional).** A Rule with `decisionResolved(D)` can send a follow-up to the thread ("PR merged: clean up the branch").

**Denial or expiry** appends `effect.result` with "denied" or "expired", which also reaches the agent as context.

**Blocking approvals** remain only for tool calls whose effect Fyr cannot execute itself, for example a `curl` to a production endpoint during debugging. They go through the tool gate (12.3), hold the run, and are the only Decisions that can park (5.6). They are expected to be rare (0 in 11 days, 0.2).

### 5.11 Read queries
A read query returns data and changes no state, so it is not an Action and writes no intent event. It applies the same visibility checks as Actions: a user sees what they own or what is shared with them; an agent sees its own lineage (5.2); a coordinator sees its grant scope.

| Query | Params | Notes |
|---|---|---|
| `thread.read` | entity, fromEseq, toEseq, maxTokens | Events and messages of a thread, capped by `maxTokens`, with a truncation flag. A coordinator's read is recorded (12.7). |
| `document.get` | id | A Document body. |
| `attention.list` | filter | Post-v1 (coordinators). |
| `thread.summary` | entity | Post-v1 (coordinators). |
| `events.since` | host, cursor, filter | Post-v1 (coordinators). |

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

### 7.1 Definition

```ts
type Condition = { kind: string; params: unknown } | { any: Condition[] } | { all: Condition[] } | { not: Condition };

type RuleSpec = { owner: EntityRef;
  viewer: UserId | null;          // set for snooze Rules: the View they belong to (R19)
  purpose: "snooze" | "wait" | "watch" | "settle" | "deferred" | "schedule" | "custom";
  when: Condition; then: ActionCall; initial: "false" | "current"; after: "disarm" | "keep" };

type Rule = RuleSpec & { id: RuleId; createdBy: Principal;
  armed: Record<string /*EntityRef key*/, number /*eseq*/>;   // snapshot of the dependencies' eseq when armed
  status: "armed" | "suspended" | "disarmed";
  suspendedBy: Set<"settle" | "archive" | "decision" | "move">;   // I3
  lastValue: boolean; rejections: number; firings: number };
```

- **Residence.** A Rule resides with its owner entity.
- **Ownership.** Any entity kind can own Rules, including module kinds (a workflow, a schedule).
- **Moves.** A move carries the owner's Rules (10.4).
- **Suspend and resume.** Each suspension adds one reason to `suspendedBy`; each resume removes only its own reason. The Rule is evaluated again only when `suspendedBy` is empty (I3).

### 7.2 Conditions
- **Contract.** Each Condition kind provides:
  - `evaluate(proj, params, rule.armed, now) → true | false | unknown`. It is pure. `proj` is the local projection plus the peer replicas (post-v1).
  - `dependsOn(params) → { eventTypes, refs, entities, resources }`. It names entities, not hosts.
  - `nextCheck(params, proj, now) → time | null`.
  - `anchored: boolean`. An anchored kind is true because of an event after `armed` (or a time). A state kind reads the current state.
- **Entities on another host (post-v1).** The evaluator looks up the entity's residence at each evaluation (the highest epoch in the replicas) and reads that replica. A stale replica gives `unknown`. Replicas merge transitions, so on remote entities only anchored kinds are allowed; `rule.create` rejects a state kind on a remote entity (K23).
- **Order.** "After armed" compares the entity's `eseq` with `armed[entity]`.
- **Logic.** `any`, `all` and `not` use three-valued logic.

**Core Condition kinds**

| Kind | Anchored | True when |
|---|---|---|
| `at {time}` | yes | `now ≥ time` |
| `schedule {cron, tz}` | yes | an occurrence of `cron` in `tz` has passed since the last firing; after downtime it catches up at most once |
| `runEnded {entity}` | yes | a `run.ended` of the entity has an eseq after `armed` |
| `documentCreated {about, schema}` | yes | a matching Document header has an `about` eseq after `armed` |
| `decisionResolved {decision}` | yes | the Decision is answered, defaulted, expired or cancelled |
| `noCurrentRun {entity}` | no | the entity has no current run |
| `idleFor {entity, ms}` | no | `now − lastActivityAt ≥ ms` |
| `contextAbove {entity, ratio}` | no | the current session's context ratio ≥ `ratio` |
| `budgetAbove {entity, ratio}` | no | usage / budget ≥ `ratio` |
| `done {entity, user}` | no | the user's attention state for the entity is `done` |

`capacity` belongs to the `governor` module (12.4).

### 7.3 Evaluation and firing
- **When an armed Rule is evaluated.**
  - After each local event that matches its `dependsOn`, except an event whose `cause` is a firing of this Rule.
  - After each replica update that concerns an entity in `dependsOn`, including an `entity.resided` of that entity (post-v1).
  - At each `nextCheck`.
  - On `host.started`.
- **Initial value.** `lastValue` starts as `false` if `initial` is `"false"`. Otherwise it starts as the current value, with `unknown` read as `false`.
- **Firing.**
  - A Rule fires when the result is `true` and `lastValue` is `false`.
  - A result of `unknown` leaves `lastValue` unchanged.
  - Any other result is stored as `lastValue`.
- **Atomic firing (I9).** A firing is one transaction that requires `status = armed`:
  1. Increase `firings`, with `CommandId = H(ruleId, firings)`.
  2. Apply `then` as `rule(id)`, if its target resides on this host. A `then` on another host is submitted by a Job with that CommandId (post-v1).
  3. Append `rule.fired` with the outcome: `ok`, `rejected` or `approvalRequested`.
  A crash before the commit leaves nothing; a retry after the commit finds the receipt.
- **Outcome `ok`.** With `after: disarm`, the Rule is disarmed. `rejections` is set to 0.
- **Outcome `rejected`.**
  - Deferred Rules (7.5) are disarmed and never retried.
  - Other Rules: `lastValue` becomes `false`, `rejections` increases by 1, and the next retry is no sooner than `min(2^rejections, 60)` minutes.
  - After 10 rejections the Rule is disarmed, and the owner gets the Signal `blocked` "a rule keeps failing: <purpose>". A disarmed snooze Rule clears the View's snooze (L1), so this Signal is visible.
- **Outcome `approvalRequested`.**
  - `decision` is added to `suspendedBy`.
  - If the user approves, the outcome counts as `ok`.
  - On deny, expiry or default, the Rule is disarmed.

### 7.4 Suspend and resume
- A Rule with a non-empty `suspendedBy` is not evaluated.
- When `suspendedBy` becomes empty:
  - an anchored Condition is evaluated against the stored `lastValue`, so an event that happened during the pause still fires once (K5);
  - a state Condition sets `lastValue` to its current value first, so a state that became true during the pause does not fire.

### 7.5 Snooze, wait, deferred, schedule
- **Snooze.** Purpose `snooze`, `then: thread.wake`, `after: disarm`.
  - `at(time)`: initial `false`.
  - "Until the agent finishes": `runEnded(self)`, initial `false`.
  - "Until CI finishes": `prChecksDone(pr)`, initial `current`.
- **Wait.** Purpose `wait`, `after: disarm`. While armed, it adds a Wait with its reason (3.3). Examples: `runEnded(X)` and `at(resetAt)`.
- **Deferred.** Purpose `deferred`, `after: disarm`. The undo window (U7) creates `at(now + 5 s)` with `then: <the Action>`. A deferred Rule's `then` is checked against its `createdBy` principal only, not against `callers.rule`. `⌘Z` or the toast's Undo disarms it. On any outcome it is disarmed; a rejection produces the derived notification `deferred.rejected`, shown as a toast with the reason.
- **Schedule.** Purpose `schedule`, `after: keep`. It is owned by a module entity, for example `schedule` or `workflow`.

### 7.6 Default Rules
- **Where defaults come from.** Default Rules come from the `DefaultRules` extension point (12.2).
  - `thread.start` asks every provider for its RuleSpecs, filtered by the `defaults` parameter.
  - The Rules are created with `createdBy: system(defaults)`.
- **v1 defaults:**

| Provider | Purpose | When | Then | Initial | After |
|---|---|---|---|---|---|
| core | settle | `all(idleFor(self, 3 days), not(done(self, owner)))` | `thread.settle` | current | keep |
| github | watch | `prChecks(pr) = failed` for a `serves` PR | `thread.send(notice)` | current | keep |
| github | watch | `prNewReview(pr)` (an author who is not the user, on a commit that no thread pushed) | `thread.send(notice)` | current | keep |
| github | settle (off by default) | `all(any(prMerged(pr), prClosed(pr)), not(done(self, owner)))` | `thread.settle` | current | keep |

- **Usage limits (K1).** The `claude` module does not create a standing Rule. At a run end with class `usage_limit`, its Observer calls `rule.create` as `system(claude)` with purpose `wait`, `when: at(resetAt)`, `then: thread.send("continue")`, `after: disarm`.
- **Budget (post-v1).** At 80 % of a budget, the `claude` module emits the Signal `blocked` "80 % of budget used". At 100 % it ends the run with class `budget`.
- **Reactor ownership.** Threads that a coordinator or a workflow starts default to `defaults: none`. Their starter reacts to PR events, so two reactors never steer one thread.

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
