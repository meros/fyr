# v0 HTTP API (SPEC 0.5 V0.2, decision 0002)

The contract between `fyrd` and the web client. All JSON. Times are RFC 3339 UTC; the client shows them in local time.

## Guard

- Every request must have a loopback `Host` (`127.0.0.1:*`, `localhost:*`, `[::1]:*`), else `403`.
- `POST` requests must have the header `X-Fyr: 1`, else `403`.

## `GET /api/state`

```json
{
  "now": "2026-11-14T09:00:00Z",
  "user": "owner",
  "buildId": "git sha or build time",
  "host": "host-a",
  "threads": [ThreadDTO],
  "actions": [{"name": "thread.settle", "label": "Settle", "key": "e", "bulk": true}],
  "profiles": [{"id": "claude", "name": "Claude", "color": "#d97757", "models": ["claude-opus-5-5"], "defaultModel": "claude-opus-5-5"}],
  "projects": [{"id": "p_…", "name": "app", "color": "teal", "icon": {"kind": "monogram|emoji|line", "value": "…"}, "folder": "/home/me/src/app", "profile": "", "model": "", "threads": 4, "lastUsed": "…", "quiet": [{"from": "17:00", "to": "06:00", "days": ["mon", "tue", "wed", "thu", "fri", "sun"]}, {"from": "00:00", "to": "00:00", "days": ["sat", "sun"]}], "quietUntil": "2026-11-16T06:00:00+01:00"}]
}
```

`ThreadDTO` is the `core.Thread` JSON plus three fields computed on the host:

```json
{
  "id": "t_…", "title": "…", "cwd": "…", "project": "app", "profile": "claude", "model": "claude-opus-5-5",
  "sessionId": "uuid", "createdAt": "…", "updatedAt": "…",
  "running": true, "runStartedAt": "…", "activity": "Bash yarn test", "subagents": 2,
  "activityAt": "…", "summary": "last assistant text, one line",
  "failure": {"class": "api", "message": "…", "at": "…"},
  "decisions": [Decision],
  "outbox": [{"id": "…", "state": "sent|queued"}],
  "tasks": [{"id": "a803…", "description": "Design mobile translation", "type": "local_agent", "status": "running|completed|failed|stopped", "activity": "Running build", "summary": "…", "toolUses": 14, "tokens": 143218, "startedAt": "…", "updatedAt": "…"}],
  "pr": {"url": "…", "repo": "acme/app", "number": 42, "title": "…", "state": "OPEN", "checks": "pass|fail|pending|none", "review": "APPROVED", "conflict": false},
  "settledAt": null, "archivedAt": null, "settleWhenDone": false, "settleWhenMerged": false,
  "source": "fyr|t3",
  "parent": "t_… (the thread whose agent started this one, V0.22)",
  "branch": "fix/map-lasso (empty when not a git checkout)",
  "rev": 1794643457123456789,
  "liveView": true,
  "live": {"pageId": "…", "state": {"headline": "Tests pass; PR open", "steps": ["…"]}, "updatedAt": "…", "checkedAt": "…", "headline": "Tests pass; PR open"},

  "attention": {"state": "asking|failed|blocked|done|working|waiting|seen|snoozed|settled|archived", "section": "needs|working|seen|snoozed|settled|archived", "reason": "…"},
  "view": {"seenAt": "…", "snoozeUntil": "…"},
  "unavailable": {"thread.interrupt": "No run is going"},
  "quietUntil": "2026-11-15T06:00:00+01:00 (absent unless the project is in its quiet hours, V0.46)"
}
```

`pr` is the PR of the thread's main change (`mainChange`, a change id) and `settleWhenMerged` says that `settleWhen` waits for that change to land; the host derives both (#32). The host stores the settle as one component, `{at, when: [{kind: done|change, change, phase}]}` (#33), and derives `settledAt`, `settleWhenDone` (a `done` condition), `settleWhenMerged` and `settleWhen` (a change condition, one that is not "when merged" first) from it; several conditions may be armed, any one that holds settles the thread, and never while a run is live. `project` is the name of the thread's project (`projectId`), derived and not stored. Every PR is stored on its code change (`changes[].pr`), so a thread with two PRs shows both on the Changes board.

`liveView` is the live view flag and `live` the view itself (V0.37); both are absent on a thread without them. The flag is stored as `live.enabled` (#33), which the DTO also carries. `live.headline` is `state.headline` when it is a string; `checkedAt` is the last `view_*` call, `updatedAt` the last change of page or state.

`rev` increases on every change, also across host restarts: keep the copy with the higher `rev`. An action name missing from `unavailable` is available. The client never computes status; it renders `attention`.

`Decision`:

```json
{"id": "d_…", "kind": "ask|approval|effect", "createdAt": "…", "agent": "subagent name or empty",
 "questions": [{"question": "…", "header": "…", "multiSelect": false, "options": [{"label": "…", "description": "…"}]}],
 "tool": "Bash", "input": {"command": "…"},
 "effect": {"action": "pr.merge", "args": {"pr": "…", "method": "squash"}, "reason": "…"}}
```

## `GET /api/stream` (Server-Sent Events)

- `event: hello` `data: {"now": "…", "buildId": "…"}` once at connect. When a later `hello` has a different `buildId` than the first one the client saw, the client reloads itself once (V0.13); a missing `buildId` never reloads. The client then fetches `/api/state` (subscribe first, then snapshot, so nothing is lost).
- `event: thread` `data: ThreadDTO` on every change, and when attention changes with time (snooze ends).
- `event: event` `data: Event` for each new timeline event.

## `GET /api/threads/{id}/events?after=SEQ&limit=N`

`{"events": [Event]}`, oldest first. With `after=0` it returns the newest `N` (default 400). `?before=SEQ&limit=N` returns the newest `N` events older than `SEQ`, to page back.

`Event`: `{"seq": 12, "at": "…", "subject": "t_…", "type": "…", "data": {…}}`. Types the client renders:

| type | data |
|---|---|
| `user` | `{"id": "…", "text": "…", "images": 1, "from": "t_…" \| "view"?}` what the user sent; with a thread id in `from`, another thread's agent sent it (V0.22); `from: "view"` is a press in the live view (V0.37). `id` is the message id (V0.33); older events have none |
| `message.state` | `{"id": "…", "state": "queued" \| "read" \| "cancelled" \| "lost"}` the delivery of a message changed (V0.33). A `user` event with an `id` starts as `sent` |
| `msg` | a raw `claude` stream message: `{"type": "assistant" \| "user", "parent_tool_use_id": null \| "…", "message": {"content": [blocks]}}`. Blocks: `text`, `thinking`, `tool_use {id, name, input}`, `tool_result {tool_use_id, content, is_error}`, `image`. A non-null `parent_tool_use_id` is a subagent message: fold it under that tool use. |
| `run.started` | `{}` |
| `run.ended` | `{"failure": Failure \| null, "interrupted": false, "costUsd": 0.01, "durationMs": 1200}` |
| `decision.opened` | `{"decision": Decision}` |
| `decision.closed` | `{"id": "…", "answer": "text shown in the log"}` |
| `task` | `{"kind": "started" \| "ended", "id", "description", "status", "summary"}` a subagent or background task started or ended (V0.16) |
| `preview` | `{"kind": "image" \| "html", "attachmentId", "title", "caption", "width", "height", "url"?}` something the agent showed with the `show` tool (V0.19). Images load from `/api/attachments/{id}`; HTML loads in an iframe from `/api/previews/{id}`; an HTML preview without `size` has no `width` and `height` (V0.47: the card is 16:10) |
| `effect.result` | `{"action": "pr.merge", "ok": true, "text": "…", "by"?: "owner"}`; `by: owner` marks a merge or re-run the owner started in the PR tab (V0.43) |
| `view.published` | `{"pageId": "…", "state": {…}}` the agent published its live view's page (V0.37); the page loads from `/api/previews/{pageId}` |
| `view.state` | `{"state": {…}}` the live view's full state after a patch (V0.37) |
| `view.unchanged` | `{"reason": "…"}` the agent said the view is current (V0.37) |
| `handover` | `{"from": "claudeAlt", "to": "claudeAcme", "model": "claude-opus-5-5", "rung": "resume" \| "adopt" \| "brief", "path"?: "…", "note"?: "why an earlier rung was skipped"}` the thread moved to another profile (V0.45); `path` is the file the new agent started from (`adopt`, `brief`) |
| `effect.error` | `{"kind": "handover", "error": "…"}` an effect failed; for a handover the thread stays on its profile |

Other types may appear; the client ignores them.

## `POST /api/command`

```json
{"commandId": "client-made unique id", "action": "thread.send", "threadId": "t_…", "args": {"text": "…", "images": [{"mediaType": "image/png", "data": "<base64>"}]}}
```

Reply `200 {"ok": true, "threadId": "t_…"}` (`thread.send` and `thread.create` add `"result": {"messageId": "…"}`, the id of the message's `user` event, V0.33), or `409 {"ok": false, "error": "reason"}` when the action is unavailable, `400` for bad arguments. The same `commandId` twice returns the first reply and changes nothing (I7).

Actions and their args:

| action | args |
|---|---|
| `thread.create` (no `threadId`) | `cwd?`, `text`, `projectId?` (file it there by hand; without `cwd` it starts in the project's folder, V0.42, V0.55), `images?`, `profile?`, `model?`, `effort?` (V0.50: `low`…`max`; empty is the provider's default when the model takes effort), `title?`, `view?: true` (start with a live view, V0.37). Without `cwd` and without a project folder the host makes a fresh `~/.local/share/fyr/scratch/<YYYYMMDD-HHMMSS>-<slug>` (V0.42) |
| `thread.send` | `text`, `images?`, `from?: "view"` (a press in the live view, V0.37) |
| `thread.setView` | `on: bool`: turn the live view on or off (V0.37). The running agent can fill the view at once (V0.107); its next process gets the view's prompt |
| `thread.answer` | `decisionId`, then `answers: {question: label}` for `ask` (multi-select: labels joined with ", "; Other: the free text), or `allow: bool` for `approval` and `effect` |
| `thread.markSeen` | `upTo`: the thread's `activityAt` the client showed (RFC 3339). Without it, now. |
| `thread.interrupt`, `thread.retry`, `thread.dismiss`, `thread.dismissPR` (V0.36: merged or closed PR only), `thread.unsettle`, `thread.unsnooze`, `thread.archive`, `thread.unarchive`, `thread.markUnseen`, `thread.pin` and `thread.unpin` (V0.104, owner only) | none |
| `thread.settle` | `now?: true` for bulk settle |
| `thread.snooze` | `until` (RFC 3339, future) |
| `thread.rename` | `title` |
| `thread.regenerateTitle` (V0.108, owner only): Fyr writes the title from the conversation; the result lands later as `thread.renamed`, or a failed line | none |
| `thread.setProject` | `projectId`: file the thread into a project by hand; folder matching keeps it (V0.23). `""` unfiles it: no project, still filed by hand, so matching leaves it unfiled (V0.30) |
| `thread.setModel` | `model?`, `effort?` (at least one; V0.50: an effort alone keeps the model; `""` returns to the provider's default). The thread DTO carries `effort` when set |
| `thread.handover` | `profile`, `model?` (default: the profile's default model): hand the thread over to another profile (V0.45). `409` while a run is live, a question is open, a handover is going or the thread is archived; `400` for an unknown profile or the thread's own. While it runs the thread has `"handover": {"to": "…", "model": "…", "at": "…"}` and sends are unavailable |
| `thread.cancelMessage` | `messageId`: withdraw a message the agent has not read (V0.33). `409` when it was read already, is unknown, or the agent cannot withdraw it |
| `t3.adopt` (no `threadId`) | `t3Ids: [..]` |
| `host.deployNow` (no `threadId`) | none: a deploy that waits for running threads switches now; their turns end and each gets "continue" after the restart (V0.83). `409` when no deploy drains |
| `provider.*` (no `threadId`) | see Providers (V0.50) |

## `GET /api/threads/{id}/handover?profile=P` (SPEC 0.5 V0.45)

`{"rung": "resume" | "adopt" | "brief", "note"?: "why rung 1 does not apply"}`: the rung a handover to `P` would use now. The handover itself still falls back when the rung fails.

## Slash commands (SPEC 0.5 V0.79)

`GET /api/threads/{id}/commands` returns `{"commands": [{"name": "review", "description": "…", "argumentHint": "<pr-number>"}], "from": "thread" | "folder" | ""}`: the commands and skills of the thread's session (its `initialize` answer, without the terminal-only ones), else the newest list the host saw for the thread's provider and folder, else `[]` with `from: ""`. `404` for an unknown thread.

`GET /api/commands?cwd=<folder>&profile=<id>` returns `{"commands": […]}`, the newest list for a provider and folder (New thread); without `profile`, the host's first profile. A front forwards it to `?host=`.

The client sends a command as an ordinary message (`/review 12`); the CLI runs it.

## `GET /api/t3/candidates`

`{"threads": [{"t3Id": "…", "title": "…", "cwd": "…", "profile": "claudeAlt", "model": "…", "sessionId": "…", "prUrl": "…", "updatedAt": "…", "hasTranscript": true}]}`, open top-level T3 threads not yet adopted.

## Web Push (SPEC 0.5 V0.15)

| Call | Body | Reply |
|---|---|---|
| `GET /api/push/key` | – | `{"publicKey": "<base64url VAPID public key>"}` |
| `POST /api/push/subscribe` | `{"subscription": PushSubscription.toJSON(), "device": "Pixel 9 · Chrome"}` | `{"ok": true}` |
| `POST /api/push/unsubscribe` | `{"endpoint": "…"}` | `{"ok": true}` |
| `POST /api/push/test` | – | `{"delivered": 1}` |
| `POST /api/push/status` | `{"endpoint": "…"}` | `{"known": true}`: false when the host deleted it after a 404 or 410 |

All POSTs need `X-Fyr: 1`. The host pushes `{"title", "body", "threadId", "state"}` when a thread enters `asking` or `failed`; the service worker shows it with `tag = threadId` and opens `/?open=<threadId>` on a tap.

The PWA files: `/manifest.webmanifest` (`application/manifest+json`) and `/sw.js` are served with `Cache-Control: no-cache`.

## Attachments (SPEC 0.5 V0.18)

| Call | Body | Reply |
|---|---|---|
| `POST /api/attachments` | the raw file; headers `X-Fyr: 1`, `X-Fyr-Filename: <percent-encoded name>`, `Content-Type` | `{"id", "name", "size", "mediaType", "path"}`; `413` over 25 MB |
| `GET /api/attachments/{id}` | – | the file, with its media type |

`thread.create` and `thread.send` take `attachments: [id…]`; a message may be attachments only. The `user` event then has `attachments: [{id, name, mediaType, size}]`.

## Previews (SPEC 0.5 V0.19)

`GET /api/previews/{id}` serves an HTML preview. Embed it as `<iframe src="/api/previews/{id}" sandbox="allow-scripts" allow="fullscreen; xr-spatial-tracking; accelerometer; gyroscope">`: scripts run, but the page has no origin, so it cannot call the API.

A folder preview (V0.47) redirects `GET /api/previews/{id}` to `/api/previews/{id}/{entry}`; `GET /api/previews/{id}/{path}` serves its files with their media types (`.glb` `model/gltf-binary`, `.wasm` `application/wasm`, …), `404` for `..`, a missing file or a single-page preview. Every preview response carries `Access-Control-Allow-Origin: *`, `X-Content-Type-Options: nosniff` and this CSP, where `{base}` is `http(s)://<Host>/api/previews/{id}/` (the page has no origin, so `'self'` would match nothing):

```
sandbox allow-scripts; default-src 'none'; script-src {base} {cdn} 'unsafe-inline' 'unsafe-eval' 'wasm-unsafe-eval' blob:; style-src {base} {cdn} 'unsafe-inline'; img-src {base} {cdn} data: blob:; font-src {base} {cdn} data:; media-src {base} {cdn} data: blob:; connect-src {base} {cdn} data: blob:; worker-src {base} blob:; frame-ancestors 'self'
```

`{cdn}` is `https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com https://esm.sh https://ajax.googleapis.com https://www.gstatic.com`. A page may report its height with `{"jsonrpc": "2.0", "method": "ui/notifications/size-changed", "params": {"height": 480}}` to its parent, as a live view does.

## Live views (SPEC 0.5 V0.37)

A view page is an HTML preview: `<iframe src="/api/previews/{pageId}" sandbox="allow-scripts">`. The client and the page exchange JSON-RPC 2.0 messages with `postMessage` (target `"*"`, because the page has no origin); the client accepts only messages whose `event.source` is the view's iframe.

| Direction | Message |
|---|---|
| client → page | `{"jsonrpc": "2.0", "method": "ui/notifications/state", "params": {"state": {…}}}` on load, on `initialized` and on every change |
| page → client | `{"jsonrpc": "2.0", "method": "ui/notifications/initialized", "params": {}}` once the bridge runs |
| page → client | `{"jsonrpc": "2.0", "method": "ui/notifications/size-changed", "params": {"height": 312}}` |
| page → client | `{"jsonrpc": "2.0", "id": "m1", "method": "ui/message", "params": {"role": "user", "content": [{"type": "text", "text": "Open the PR"}]}}`; the client sends `thread.send {text, from: "view"}` and answers `{"jsonrpc": "2.0", "id": "m1", "result": {}}` (or an `error` when the send failed) |

## Projects (SPEC 0.5 V0.23, V0.55)

A project is a name, a colour, an icon and one folder on its host (V0.55, decision 0017), with optional defaults and quiet hours. `folder` is a full path on the project's host, or absent: then threads are only filed in it by hand.

Commands (through `POST /api/command`, no `threadId`):

| action | args |
|---|---|
| `project.update` | `id`, then any of `name`, `color` (a palette name: slate blue sky teal green olive violet indigo rose brown), `icon` `{kind: monogram\|emoji\|line, value}`, `folder` (a full path; `""` clears it, V0.55), `image` (`""` removes the image icon and its files; V0.116), `profile`, `model`, `quiet` `[{from: "17:00", to: "06:00", days?: ["mon"…"sun"]}, …]` (at most 14 windows; a single object is read as a list of one) or `null` or `[]` to clear (V0.46, decision 0014 addendum) |
| `project.create` | `name`, `folder?`, `threadId?`: file that thread into the new project by hand in the same write (V0.30). Reply `result: {id}` |
| `project.setupRun` | `id` (V0.116): starts the project setup run, a thread in the project's folder; the result is `{id, thread}`. No folder: `400`; a system agent that cannot run: `409` with the reason, also recorded as `setup.error` |
| `project.delete` | `id` (V0.58). Unknown id: `404`. Every thread in it loses the project and the filed-by-hand mark, then the host matches again |

A folder needs a full path (`~/…` is expanded) and must exist on the host, else `400` with the reason. After `project.create` or a change of `folder` the host matches again every thread not filed by hand and sends the changed ones on the stream: a thread lands in the project whose folder is its folder or contains it, the longest folder first; matching never takes a project away. An icon's `value` is left out when empty. A thread with no `cwd` and a `projectId` starts in the project's folder.

A project may carry `image` (its image icon's version), `by: {name, color, image}` (`owner`, `agent` or `""`) and `setup: {thread?, at, error?}` (V0.116). `GET /api/projects/<id>/icon?size=64|128&v=<image>` serves the icon as PNG (`404` without one); `POST /api/projects/<id>/icon` (X-Fyr; the body is a PNG, JPEG or GIF of at most 2 MB) sets the owner's image and answers like a command (`400` not an image, `413` too large, `404` no project). A front sends both to the project's host.

The stream sends `event: project` with a project (same shape as in the state) when one is made or changed. After `project.delete` it sends `event: project` with `{id, deleted: true}` (V0.58). A project carries `rev`, when the host built that copy (it grows across restarts); a client keeps the copy with the higher `rev`, so a frame that arrives while `/api/state` is in flight is not overwritten by the older snapshot. Threads carry `projectId`; `project` stays the display name. A thread may carry `newProject: {name, folder}`: what "New project from this thread" starts with (V0.30, V0.55): its folder and the folder's name; absent for a scratch folder, the home folder, `/`, and a folder a project already has.

Removed in V0.55: `sources`, `main`, `repos`, `cwd` on a project; `suggest` and `workspaceId` on a thread; `project.createFrom`, `project.addSource`, `project.removeSource`, `project.setMain` (`400 unknown action`).

Quiet hours (V0.46, decision 0014 addendum): a project's `quiet` is a list of windows; each holds `from` and `to` as `HH:MM` in the host's local time zone, and optional `days` (`mon tue wed thu fri sat sun`, the days a window starts on; absent or empty is every day). A window whose `to` is not after `from` crosses midnight; `from` equal to `to` is 24 hours (all day). The project is quiet while any window holds. A project stored before the addendum has one object; the host reads it as a list of one and always sends a list. While a project is quiet, the project and each of its threads carry `quietUntil` (RFC 3339, the end of the current quiet stretch, where windows that overlap or follow on without a gap join, at most a week ahead); otherwise the field is absent. The host sends the project and its threads on the stream when a window starts or ends. A thread with `quietUntil` gets no Web Push, and none is sent when the window ends. Clients leave such threads out of the inbox sections and counts unless the project filter picks that project or a search is typed.

## Folders (SPEC 0.5 V0.42)

The host folder picker. Every path is resolved through symlinks and must be an existing folder inside the owner's home folder; `~` and `~/…` are expanded; anything else is `400 {error}`.

| call | reply |
|---|---|
| `GET /api/fs/list?path=` (empty: `~`) | `{path, parent, dirs: [{name, path, repo?}]}`: the subfolders, hidden ones included, sorted by name; `parent` is `""` at the home folder; `repo` when the folder has `.git`; a symlink that leads outside is left out |
| `GET /api/fs/recent` | `{dirs: [...]}`: the distinct folders of threads, newest first, at most 30 |
| `GET /api/fs/repos` | `{dirs: [...]}`: git repositories under the project folders on this host and the config's `scanRoots`, two levels deep (not inside a repository; no hidden folders or `node_modules`); cached for five minutes, refreshed in the background |
| `POST /api/fs/mkdir {parent, name}` (X-Fyr) | `{ok: true, path}`; `400` when the name is empty, has `/`, is `.`/`..`, or is taken |

## Changes (SPEC 0.5 V0.25)

`GET /api/threads/{id}/changes` returns the branch diff of the thread's folder against its base, computed with git (read-only, no cache):

```json
{
  "git": true,
  "root": "/home/me/src/app",
  "branch": "fix/map-lasso",
  "base": "origin/main",
  "mergeBase": "3f2a…",
  "commits": 3,
  "files": [
    {"path": "web/src/map.ts", "status": "modified", "additions": 40, "deletions": 12, "binary": false},
    {"path": "web/src/lasso.ts", "oldPath": "web/src/select.ts", "status": "renamed", "additions": 2, "deletions": 1, "binary": false},
    {"path": "docs/notes.md", "status": "added", "additions": 18, "deletions": 0, "binary": false}
  ],
  "truncated": false
}
```

- `base` is `origin/HEAD`, else `origin/main`, `origin/master`, `main` or `master`; empty when none exists (then the diff is the uncommitted work against `HEAD`). `branch` is empty on a detached `HEAD`. `commits` counts the commits from the merge base to `HEAD`.
- The diff runs from the merge base to the working tree: commits, staged and unstaged edits, and untracked files that are not ignored (`status: added`). `status` is one of `added`, `modified`, `deleted`, `renamed`; `oldPath` is set for a rename. Paths are relative to `root`. At most 1,000 files, sorted by path; more sets `truncated: true`.
- Not a git checkout, or the folder is gone: `{"git": false}`. Unknown thread: `404`.

`GET /api/threads/{id}/changes?path=<path>` returns one file's unified diff:

```json
{"path": "web/src/map.ts", "patch": "diff --git a/… b/…\n@@ -1,4 +1,5 @@\n…", "binary": false, "truncated": false}
```

The patch is cut at 400 KB at a line end (`truncated: true`). A path without changes returns `"patch": ""`. The path is matched literally; an untracked file is read only when git lists it as untracked.

**Since you looked (V0.65).** The reply also has `head` (the commit, `""` without commits) and per file `blob` (its content's blob id now, absent when deleted) and `viewed` (the owner marked this content Viewed). After a `thread.changesSeen` it adds `since: {head, at, mode: "ancestor"|"rebased"|"missing", commits, files: [File], rangeDiff?, rangeDiffTruncated?}`: the files whose content differs from the look, with status and +/− from the seen head to the working tree. `?path=<p>&since=<commit>` returns that file's diff from the commit to the working tree; a `since` that is not a hex id is `400`. Commands: `thread.changesSeen {head, files: {path: blob}}` records the look; `thread.fileViewed {path, blob, viewed: bool}` sets or clears a mark. Both change the owner's View, which the thread DTO sends without them.

## PR detail (SPEC 0.5 V0.43)

`GET /api/threads/{id}/pr` returns the thread's PR, read with `gh` on the host and cached 30 s per PR. `?fresh=1` skips the cache.

```json
{
  "url": "https://github.com/acme/app/pull/88", "repo": "acme/app", "number": 88,
  "title": "…", "body": "markdown", "state": "OPEN|MERGED|CLOSED", "draft": false,
  "author": "octocat", "head": "fix/cards", "base": "main", "headSha": "3f2a…",
  "additions": 120, "deletions": 18, "changedFiles": 4,
  "labels": [{"name": "bug", "color": "d73a4a"}],
  "mergeable": "MERGEABLE|CONFLICTING|UNKNOWN",
  "mergeState": "CLEAN|BLOCKED|BEHIND|DIRTY|DRAFT|UNSTABLE|HAS_HOOKS|UNKNOWN",
  "reviewDecision": "APPROVED|CHANGES_REQUESTED|REVIEW_REQUIRED|",
  "reviews": [{"author": "anna", "state": "APPROVED|CHANGES_REQUESTED|COMMENTED|DISMISSED", "at": "…"}],
  "checks": [{"name": "lint", "workflow": "CI", "state": "fail|running|pass|skipped", "status": "COMPLETED", "conclusion": "FAILURE", "startedAt": "…", "completedAt": "…", "url": "https://github.com/…/job/1"}],
  "threads": [{"path": "web/src/Card.tsx", "line": 42, "outdated": false, "url": "…", "comments": [{"author": "anna", "body": "markdown", "at": "…", "url": "…"}]}],
  "threadsError": "set when the review threads could not be read; the rest is valid",
  "fetchedAt": "…",
  "checked": "pass|fail|pending|none"
}
```

- `reviews` is the latest review of each reviewer. `threads` are the unresolved review threads only, at most 50, each with at most 20 comments.
- A check is a CheckRun (`status`, `conclusion`, `workflow`) or a commit status (`status` empty, `conclusion` is its state: `SUCCESS`, `FAILURE`, `ERROR`, `PENDING`, `EXPECTED`). `state` is the host's reading of both.
- `checked` (V0.112) is the checks summed up: `fail` when a check failed, `pending` while one runs, `pass` when at least one reported and none failed or runs. An empty list is `pending` on a repo with CI and `none` only on a repo with no CI. A change's `need` of an open ask carries `wait` while its yes cannot run (a merge on pending checks).
- Errors return `200 {"error": "…"}`: gh is missing or not signed in, the network failed, or the thread has no PR. An unknown thread is `404`.

Owner commands (through `POST /api/command`, with `threadId`):

| action | args |
|---|---|
| `pr.merge` | `method?`: `squash` (default), `merge` or `rebase`. Unavailable without an open PR, with a merge conflict or requested changes. The host reads the PR again and refuses a draft, a closed PR, and a merge state of `DIRTY`, `BLOCKED` or `BEHIND` |
| `pr.rerunFailed` | none: reruns (`gh run rerun <id> --failed`) every failed, cancelled or timed-out workflow run of the head commit. Unavailable without an open PR |

Both reply at once; the result arrives as an `effect.result` event with `"by": "owner"` (the agent is not told). `ThreadDTO.unavailable` carries their reasons like any action.

## Speed (SPEC 0.5 V0.38)

- Responses with a text body are gzipped when the request has `Accept-Encoding: gzip` (`Vary: Accept-Encoding`); `/api/stream` too, flushed per event.
- `msg` events in `/api/threads/{id}/events` and on the stream are lean: a `tool_result` text over 16 KB (4 KB when `parent_tool_use_id` is set) is cut, base64 image data is left out (`source.omitted: true`, `source.bytes`), and the event data has `"cut": true`.
- `GET /api/threads/{id}/events/{seq}` returns that one event whole (`404` when the thread has no such event).
- Stream: after a connection has a thread whole, changes come as `event: thread.patch` `data: {"id", "rev", "base", "patch"}`; apply `patch` (RFC 7396) only when your copy has `rev === base`, else `GET /api/threads/{id}` (the ThreadDTO). A thread is sent at most every 100 ms per connection; the newest version wins.
- `GET /api/state?since=REV` lists only threads with `rev > REV` and adds `"partial": true`; the other fields are whole. Revs grow across host restarts.

## Update progress (SPEC 0.5 V0.39)

- Stream `event: deploy` `data: {"phase": "build|test|drain|switch|done|failed", "detail"?, "blocking"?: [threadId], "build", "startedAt", "at"}`.
- `GET /api/state` has `deploy` with the latest progress while an update runs and for two minutes after it ended; otherwise `null`.

## Providers (SPEC 0.5 V0.50)

`GET /api/state` `profiles` are the providers as launch configs (same ids), each with `cli`, `models` (ids), `modelCaps: [{id, name?, efforts?}]`, `defaultModel`, `defaultEffort?`, `quota: {kind}`; never `env` or a key. `GET /api/profiles` returns the same list alone, for a client after a provider changed.

`GET /api/providers`:

```json
[{"id": "claudeDeepseek", "name": "DeepSeek", "kind": "endpoint", "cli": "claude", "auth": "api-key",
  "baseURL": "https://api.deepseek.com/anthropic", "modelEnv": {"ANTHROPIC_DEFAULT_HAIKU_MODEL": "deepseek-chat"},
  "models": [{"id": "deepseek-chat"}], "defaultModel": "deepseek-chat", "efforts": ["low", "medium", "high", "xhigh", "max"],
  "quota": "deepseek", "status": "ready", "keySet": true, "keyLast4": "4f2a", "keyFrom": "/run/secrets/deepseek",
  "imported": "profile claudeDeepseek (claude-deepseek)", "threads": 3},
 {"id": "claudeAcme", "name": "Claude (acme)", "kind": "claude-account", "auth": "cli-login", "configDir": "~/.claude-acme",
  "models": [{"id": "claude-opus-5-5", "name": "Opus 5.5", "efforts": ["low", "medium", "high", "xhigh", "max"]}],
  "quota": "anthropic-oauth", "status": "needs-login", "need": "Log in once in a terminal on this host: …", "keySet": false, "threads": 0}]
```

- V0.120: each provider also carries `catalog: [{id, name?, efforts?, firstSeen, new?, gone?, manual?}]` (what the provider reports; `new` already has its 14 days applied), `checkedAt?` (the last successful check, RFC 3339; absent: never checked), `checkError?` (why the last check failed; the catalog is then the last good one), `checkNote?` (the last check had only Fyr's known list: the CLI named its aliases only) and `checking?` (a check runs now). `models` keeps its meaning: the models that are on, in the owner's order; every picker lists only these.
- `kind`: `claude-account` (auth `cli-login`) or `endpoint` (auth `api-key`). `status`: `ready`, `needs-login`, `needs-key`, with `need` saying what to do. `keySet` and `keyLast4` (empty for a key under 12 characters) are all a client learns of a key; `keyFrom` names where a key not yet stored by Fyr is read (an environment variable name or a file path). `efforts` are the adapter's levels; a model's `efforts` narrow them.

`GET /api/providers/found`: what detection found on this host, each `{id, name, kind, source, configDir?, baseURL?, modelEnv?, models, defaultModel?, quotaKind?, keyFrom?, loggedIn?, status, note?, imported}`. `imported` is true when a provider with that `id` exists.

Commands (`POST /api/command`, no `threadId`; `400` bad arguments, `404` unknown id, `409` conflict):

| action | args |
|---|---|
| `provider.import` | `foundId`: create the provider from that find; a key found in fyrd's environment or a file is copied into the secret store. `409` when imported already. Result `{"id"}` |
| `provider.create` | `name`, `kind: "claude-account" \| "endpoint"`, `id?` (default from the name), `configDir?` (account; empty is `~/.claude`), `baseURL` (endpoint), `key?`, `models?: [id]` (account default: the CLI's list), `defaultModel?`, `defaultEffort?`, `modelEnv?: {NAME: value}`, `quotaKind?`, `color?`. Result `{"id"}` |
| `provider.update` | `id`, then any of `name`, `configDir`, `baseURL`, `models`, `defaultModel`, `defaultEffort`, `modelEnv`, `quotaKind`, `color`, `forget` (V0.120). `models` is the on-list in order; an id the catalog lacks joins it as `manual`. A `models` list without the current default is refused (`400 "Star another model first."`) unless the same update sets `defaultModel`. `forget: [id]` drops hand-typed (`manual`) models that are off |
| `provider.refreshModels` | `id` (V0.120): ask the provider for its models now: an endpoint at `GET <baseURL>/v1/models` with its key (`x-api-key`, then `Authorization: Bearer` on 401/404; 15 s and 2 MB a page, up to 20 pages, no redirect off the host), a Claude login through its CLI's stream-json `initialize` answer plus Fyr's own list. `409` when the provider needs a login or a key. Waits for the check; a failed check is still `ok`, with `result.checkError` and the catalog unchanged. Result `{checkedAt, checkError, models}`. The host also checks each ready provider once a day, and a provider at once after `provider.create` |
| `provider.setKey` | `id`, `key` (an endpoint's API key, or an account's login token from `claude setup-token`); `""` removes it |
| `provider.delete` | `id`; `409` while threads that are not archived use it. Its stored key is removed |

## Quota (SPEC 0.5 V0.44)

`GET /api/quota` returns one entry per profile whose quota kind is not `none`, in profile order. Providers on one auth identity (V0.50) share the entry's data. The host reads a provider when its data is older than 5 minutes; a rate-limited provider is tried again after 5, 10, 20, then 30 minutes.

```json
[
  {"profile": "claudeAlt", "kind": "anthropic-oauth",
   "windows": [{"name": "5 h", "usedPct": 34, "resetsAt": "…"}, {"name": "7 d", "usedPct": 61, "resetsAt": "…"}],
   "updatedAt": "…"},
  {"profile": "claudeDeepseek", "kind": "deepseek", "balance": {"amount": 12.4, "currency": "CNY"}, "updatedAt": "…"},
  {"profile": "router", "kind": "openrouter", "balance": {"spent": 3.2, "currency": "USD"}, "updatedAt": "…",
   "stale": true, "error": "Rate limited by the provider; trying again later", "retryAt": "…"}
]
```

- `windows` (`anthropic-oauth`): `usedPct` is 0–100; `resetsAt` may be missing. `balance` (`deepseek`, `openrouter`): `amount` is what is left, `spent` what a key without a limit has used.
- `updatedAt` is when the data was read (from the provider, or from a running session's `rate_limit_event`); missing when nothing was ever read.
- `stale: true`: the last read failed and the data is older. `error` says why in plain words ("Login expired; run this profile once", "No API key: set keyFile or keyEnv", "Rate limited by the provider; trying again later"). `retryAt` is the next try during a rate-limit wait.
- Never contains a token or key. The profiles in `GET /api/state` carry only `quota.kind`.

## Fronts and hosts (SPEC 0.5 V0.51, V0.54, decision 0016)

The browser talks to a **front**; the front talks to its **hosts** over the **host protocol**, which is this API without the web client. The browser API above does not change; the front adds:

- `GET /api/state`: every thread and project carries `host` (its host's name); a thread whose host is unreachable carries `hostOffline` (RFC 3339, since when). The state adds `hosts: [{"name": "host-a", "online": true, "since": "…", "error"?: "…", "profiles": […], "deploy"?: {…}}]` (V0.54: each host's profiles and update). Each row may add `found: true` (V0.123: discovery found it on the tailnet), `id` and `idWas` (V0.124: its install id, and the pinned one when the host now reports another) and `url` (V0.125: the URL the front dials for it, as configured or as found, also while it is offline). The state's `hostId` is the front's own install id (V0.124). The top-level `profiles` is every host's, merged by id (the first host's wins); `deploy` is the front's own machine's; `actions` the first host's. After a front restart a host it cannot reach is served from the front's cache on disk, `hostOffline` since the front last had it online.
- Commands and thread calls (`/api/threads/{id}/…`) go to the thread's host. A call without a thread goes to `args.host` (command) or `?host=` (other calls) when given, a `project.*` command to the project's host, else to the first host. A thread on an offline host is read-only: `503 {"ok": false, "error": "host-a is offline since 19:40; its threads are read-only"}`.
- Attachments and previews (V0.54): `POST /api/attachments`, `GET /api/attachments/{id}` and `GET /api/previews/{id}[/{path}]` take `?host=` or `?thread=` (the thread's host); without either, a read goes to the host the front learned for that id (an upload through it, or the preview page these files belong to), else to the first host. No host is tried in turn.
- `GET /api/quota` merges every online host's list (`?host=` for one host's).
- `GET /api/profiles` merges every online host's profiles by id, the first host's winning.
- The owner's project order is the front's (V0.121): the state adds `projectOrder: [projectId]` (absent until set: clients sort by name), one list across all hosts. `POST /api/command {"action": "front.projectOrder", "args": {"ids": [projectId]}}` sets it (blanks and repeats dropped; a repeated `commandId` answers its first reply); the front keeps it in its database and sends the stream event `projectOrder {"ids": […]}`. No host has this command, so no agent tool or MCP call sends it. A front that cannot store it answers `500` with the reason.
- Device data is the front's (V0.54): `POST /api/presence` and `/api/push/*` (key, subscribe, unsubscribe, status, test) are served by the front, which sends the push for the threads of all its hosts. Hosts send none.

The host protocol (front → host):

| Call | Use |
|---|---|
| `GET /api/stream` | subscribe first (`hello`, then `thread`, `thread.patch`, `event`, `project`, `deploy`), sent without gzip |
| `GET /api/state` | the snapshot after `hello`; again after every reconnect |
| `GET /api/threads/{id}` | a whole thread when a patch's `base` does not match |
| `POST /api/command` | with `X-Fyr: 1`, the client's `commandId` unchanged (I7 holds across the hop) |
| everything else | passed through with the browser's `Host`, so previews keep their CSP base |

Guard on the host: the loopback guard as above; on a `hostListen` tailnet address only peers whose Tailscale identity (whois of the connection) is the owner's login, and `Tailscale-User-Login` headers are ignored there. A host role never serves the web client.

### Two machines on NixOS (V0.54)

The laptop and host-b each run `fyrd` as host and front (`role` both, the default). Each serves the host protocol on its tailnet address and lists the other as a host; the laptop's front serves its browser on localhost, host-b's front serves the phone through Tailscale Serve. The owner applies these blocks in the NixOS configuration; tailscaled must be logged in as the owner on both machines.

host-b:

```nix
services.tailscale.enable = true;
services.fyr = {
  enable = true;
  user = "owner";
  # role = "both";                   # the default: a host and the phone's front
  hostPort = 7778;                   # serve fronts on this machine's tailnet address (tailnet:7778), port open on tailscale0 only
  hosts = [ { name = "host-a"; url = "http://host-a:7778"; } ];
  tailnet.enable = true;             # Tailscale Serve: https://host-b.<tailnet>.ts.net:7443 → the front
  scanRoots = [ "~/git" ];
};
```

host-a (its existing block, plus):

```nix
services.fyr = {
  hostPort = 7778;
  hosts = [ { name = "host-b"; url = "http://host-b:7778"; } ];
};
```

`host-a` and `host-b` are MagicDNS names; a full name (`host-b.<tailnet>.ts.net`) or a `100.x` address works too. Each front's local host comes first and needs no entry. The phone subscribes to notifications again on host-b's front (its own device data); a subscription made on the laptop's front stays with that front.
