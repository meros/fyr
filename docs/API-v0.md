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
  "projects": [{"id": "p_…", "name": "app", "color": "teal", "icon": {"kind": "monogram|emoji|line", "value": "…"}, "sources": [{"kind": "folder", "host": "host-a", "path": "/home/me/src/app"}, {"kind": "repo", "url": "github.com/me/app"}], "profile": "", "model": "", "threads": 4, "lastUsed": "…", "cwd": "/home/me/src/app", "quiet": [{"from": "17:00", "to": "06:00", "days": ["mon", "tue", "wed", "thu", "fri", "sun"]}, {"from": "00:00", "to": "00:00", "days": ["sat", "sun"]}], "quietUntil": "2026-11-16T06:00:00+01:00"}]
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

`liveView` is the live view flag and `live` the view itself (V0.37); both are absent on a thread without them. `live.headline` is `state.headline` when it is a string; `checkedAt` is the last `view_*` call, `updatedAt` the last change of page or state.

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
| `thread.create` (no `threadId`) | `cwd?`, `text`, `projectId?` (file it there by hand; without `cwd` it starts in the project's main folder here, V0.42), `images?`, `profile?`, `model?`, `effort?` (V0.50: `low`…`max`; empty is the provider's default when the model takes effort), `title?`, `view?: true` (start with a live view, V0.37). Without `cwd` and without a project folder here the host makes a fresh `~/.local/share/fyr/scratch/<YYYYMMDD-HHMMSS>-<slug>` (V0.42) |
| `thread.send` | `text`, `images?`, `from?: "view"` (a press in the live view, V0.37) |
| `thread.setView` | `on: bool`: turn the live view on or off (V0.37). The agent's next process gets the view's prompt and tools |
| `thread.answer` | `decisionId`, then `answers: {question: label}` for `ask` (multi-select: labels joined with ", "; Other: the free text), or `allow: bool` for `approval` and `effect` |
| `thread.markSeen` | `upTo`: the thread's `activityAt` the client showed (RFC 3339). Without it, now. |
| `thread.interrupt`, `thread.retry`, `thread.dismiss`, `thread.dismissPR` (V0.36: merged or closed PR only), `thread.unsettle`, `thread.unsnooze`, `thread.archive`, `thread.unarchive`, `thread.markUnseen` | none |
| `thread.settle` | `now?: true` for bulk settle |
| `thread.snooze` | `until` (RFC 3339, future) |
| `thread.rename` | `title` |
| `thread.setProject` | `projectId`: file the thread into a project by hand; folder matching keeps it (V0.23). `""` unfiles it: no project, still filed by hand, so matching leaves it unfiled (V0.30) |
| `thread.setModel` | `model?`, `effort?` (at least one; V0.50: an effort alone keeps the model; `""` returns to the provider's default). The thread DTO carries `effort` when set |
| `thread.handover` | `profile`, `model?` (default: the profile's default model): hand the thread over to another profile (V0.45). `409` while a run is live, a question is open, a handover is going or the thread is archived; `400` for an unknown profile or the thread's own. While it runs the thread has `"handover": {"to": "…", "model": "…", "at": "…"}` and sends are unavailable |
| `thread.cancelMessage` | `messageId`: withdraw a message the agent has not read (V0.33). `409` when it was read already, is unknown, or the agent cannot withdraw it |
| `t3.adopt` (no `threadId`) | `t3Ids: [..]` |
| `provider.*` (no `threadId`) | see Providers (V0.50) |

## `GET /api/threads/{id}/handover?profile=P` (SPEC 0.5 V0.45)

`{"rung": "resume" | "adopt" | "brief", "note"?: "why rung 1 does not apply"}`: the rung a handover to `P` would use now. The handover itself still falls back when the rung fails.

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

## Projects (SPEC 0.5 V0.23)

Commands (through `POST /api/command`, no `threadId`):

| action | args |
|---|---|
| `project.update` | `id`, then any of `name`, `color` (a palette name: slate blue sky teal green olive violet indigo rose brown), `icon` `{kind: monogram\|emoji\|line, value}`, `profile`, `model`, `quiet` `[{from: "17:00", to: "06:00", days?: ["mon"…"sun"]}, …]` (at most 14 windows; a single object is read as a list of one) or `null` or `[]` to clear (V0.46, decision 0014 addendum) |
| `project.create` | `name`, `sources?: [Source]` (a project may have no source: a label), `threadId?`: file that thread into the new project by hand in the same write (V0.30). Reply `result: {id}` |
| `project.createFrom` | `threadId`: make the thread's `suggest` project and file every unfiled thread it matches (V0.28) |
| `project.addSource` | `id`, `source` |
| `project.removeSource` | `id`, `index` (0-based, into `sources`) |
| `project.setMain` | `id`, `index` (0-based, a folder in `sources`): new threads start in this folder; the mark moves from the project's other folders on that host (V0.42) |

A folder source needs a full path (`~/…` is expanded); on this host the folder must exist, else `400` with the reason. `host` may be left out: the host fills in its own name. After `project.create`, `project.addSource` or `project.removeSource` the host re-matches threads and sends the changed ones on the stream. An icon's `value` is left out when empty.

A folder source may carry `main: true` (V0.42); without one, the first folder on a host is main. A project's `cwd` in the state is its main folder on this host. Every project carries `repos: ["github.com/me/app"]`: the normalised `origin` remotes of its folders on this host and its repo sources, deduplicated, read-only; a derived repo matches clones elsewhere like a repo source.

The stream sends `event: project` with a project (same shape as in the state) when one is made or changed. Threads carry `projectId`; `project` stays the display name. A thread without a project and in a clone with a remote carries `suggest: {name, sources}` (V0.28). Every thread with a workspace may carry `newProject: {name, sources}`: what "New project from this thread" starts with (V0.30): the suggestion for a clone with a remote, else the folder's name and folder; absent for a scratch folder, the home folder and `/`, and without the sources an existing project already has.

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
  "fetchedAt": "…"
}
```

- `reviews` is the latest review of each reviewer. `threads` are the unresolved review threads only, at most 50, each with at most 20 comments.
- A check is a CheckRun (`status`, `conclusion`, `workflow`) or a commit status (`status` empty, `conclusion` is its state: `SUCCESS`, `FAILURE`, `ERROR`, `PENDING`, `EXPECTED`). `state` is the host's reading of both.
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

`GET /api/state` `profiles` are the providers as launch configs (same ids), each with `cli`, `models` (ids), `modelCaps: [{id, name?, efforts?}]`, `defaultModel`, `defaultEffort?`, `quota: {kind}`; never `env` or a key.

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

- `kind`: `claude-account` (auth `cli-login`) or `endpoint` (auth `api-key`). `status`: `ready`, `needs-login`, `needs-key`, with `need` saying what to do. `keySet` and `keyLast4` (empty for a key under 12 characters) are all a client learns of a key; `keyFrom` names where a key not yet stored by Fyr is read (an environment variable name or a file path). `efforts` are the adapter's levels; a model's `efforts` narrow them.

`GET /api/providers/found`: what detection found on this host, each `{id, name, kind, source, configDir?, baseURL?, modelEnv?, models, defaultModel?, quotaKind?, keyFrom?, loggedIn?, status, note?, imported}`. `imported` is true when a provider with that `id` exists.

Commands (`POST /api/command`, no `threadId`; `400` bad arguments, `404` unknown id, `409` conflict):

| action | args |
|---|---|
| `provider.import` | `foundId`: create the provider from that find; a key found in fyrd's environment or a file is copied into the secret store. `409` when imported already. Result `{"id"}` |
| `provider.create` | `name`, `kind: "claude-account" \| "endpoint"`, `id?` (default from the name), `configDir?` (account; empty is `~/.claude`), `baseURL` (endpoint), `key?`, `models?: [id]` (account default: the CLI's list), `defaultModel?`, `defaultEffort?`, `modelEnv?: {NAME: value}`, `quotaKind?`, `color?`. Result `{"id"}` |
| `provider.update` | `id`, then any of `name`, `configDir`, `baseURL`, `models`, `defaultModel`, `defaultEffort`, `modelEnv`, `quotaKind`, `color` |
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

## Fronts and hosts (SPEC 0.5 V0.51, decision 0016)

The browser talks to a **front**; the front talks to its **hosts** over the **host protocol**, which is this API without the web client. The browser API above does not change; the front adds:

- `GET /api/state`: every thread and project carries `host` (its host's name); a thread whose host is unreachable carries `hostOffline` (RFC 3339, since when). The state adds `hosts: [{"name": "host-a", "online": true, "since": "…", "error"?: "…"}]`.
- Commands and thread calls (`/api/threads/{id}/…`) go to the thread's host. A call without a thread goes to `args.host` (command) or `?host=` (other calls) when given, a `project.*` command to the project's host, else to the first host. A thread on an offline host is read-only: `503 {"ok": false, "error": "host-a is offline since 19:40; its threads are read-only"}`.
- Attachments and previews are looked up on each host in turn; an upload goes to `?host=` or the first host.
- `POST /api/presence` goes to every online host.

The host protocol (front → host):

| Call | Use |
|---|---|
| `GET /api/stream` | subscribe first (`hello`, then `thread`, `thread.patch`, `event`, `project`, `deploy`), sent without gzip |
| `GET /api/state` | the snapshot after `hello`; again after every reconnect |
| `GET /api/threads/{id}` | a whole thread when a patch's `base` does not match |
| `POST /api/command` | with `X-Fyr: 1`, the client's `commandId` unchanged (I7 holds across the hop) |
| everything else | passed through with the browser's `Host`, so previews keep their CSP base |

Guard on the host: the loopback guard as above; on a `hostListen` tailnet address only peers whose Tailscale identity (whois of the connection) is the owner's login, and `Tailscale-User-Login` headers are ignored there. A host role never serves the web client.
