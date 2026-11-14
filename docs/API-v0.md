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
  "host": "meros-laptop",
  "threads": [ThreadDTO],
  "actions": [{"name": "thread.settle", "label": "Settle", "key": "e", "bulk": true}],
  "profiles": [{"id": "claude", "name": "Claude", "color": "#d97757", "models": ["claude-opus-5-5"], "defaultModel": "claude-opus-5-5"}],
  "projects": [{"id": "p_…", "name": "app", "color": "teal", "icon": {"kind": "monogram|emoji|line", "value": "…"}, "sources": [{"kind": "folder", "host": "meros-laptop", "path": "/home/me/src/app"}, {"kind": "repo", "url": "github.com/me/app"}], "profile": "", "model": "", "threads": 4, "lastUsed": "…", "cwd": "/home/me/src/app"}]
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
  "tasks": [{"id": "a803…", "description": "Design mobile translation", "type": "local_agent", "status": "running|completed|failed|stopped", "activity": "Running build", "summary": "…", "toolUses": 14, "tokens": 143218, "startedAt": "…", "updatedAt": "…"}],
  "pr": {"url": "…", "repo": "acme/app", "number": 42, "title": "…", "state": "OPEN", "checks": "pass|fail|pending|none", "review": "APPROVED", "conflict": false},
  "settledAt": null, "archivedAt": null, "settleWhenDone": false, "settleWhenMerged": false,
  "source": "fyr|t3",
  "parent": "t_… (the thread whose agent started this one, V0.22)",
  "branch": "fix/map-lasso (empty when not a git checkout)",
  "rev": 1794643457123456789,

  "attention": {"state": "asking|failed|blocked|done|working|waiting|seen|snoozed|settled|archived", "section": "needs|working|seen|snoozed|settled|archived", "reason": "…"},
  "view": {"seenAt": "…", "snoozeUntil": "…"},
  "unavailable": {"thread.interrupt": "No run is going"}
}
```

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
| `user` | `{"text": "…", "images": 1, "from": "t_…"?}` what the user sent; with `from`, another thread's agent sent it (V0.22) |
| `msg` | a raw `claude` stream message: `{"type": "assistant" \| "user", "parent_tool_use_id": null \| "…", "message": {"content": [blocks]}}`. Blocks: `text`, `thinking`, `tool_use {id, name, input}`, `tool_result {tool_use_id, content, is_error}`, `image`. A non-null `parent_tool_use_id` is a subagent message: fold it under that tool use. |
| `run.started` | `{}` |
| `run.ended` | `{"failure": Failure \| null, "interrupted": false, "costUsd": 0.01, "durationMs": 1200}` |
| `decision.opened` | `{"decision": Decision}` |
| `decision.closed` | `{"id": "…", "answer": "text shown in the log"}` |
| `task` | `{"kind": "started" \| "ended", "id", "description", "status", "summary"}` a subagent or background task started or ended (V0.16) |
| `preview` | `{"kind": "image" \| "html", "attachmentId", "title", "caption", "width", "height", "url"?}` something the agent showed with the `show` tool (V0.19). Images load from `/api/attachments/{id}`; HTML loads in an iframe from `/api/previews/{id}` |
| `effect.result` | `{"action": "pr.merge", "ok": true, "text": "…"}` |

Other types may appear; the client ignores them.

## `POST /api/command`

```json
{"commandId": "client-made unique id", "action": "thread.send", "threadId": "t_…", "args": {"text": "…", "images": [{"mediaType": "image/png", "data": "<base64>"}]}}
```

Reply `200 {"ok": true, "threadId": "t_…"}`, or `409 {"ok": false, "error": "reason"}` when the action is unavailable, `400` for bad arguments. The same `commandId` twice returns the first reply and changes nothing (I7).

Actions and their args:

| action | args |
|---|---|
| `thread.create` (no `threadId`) | `cwd`, `text`, `images?`, `profile?`, `model?`, `title?` |
| `thread.send` | `text`, `images?` |
| `thread.answer` | `decisionId`, then `answers: {question: label}` for `ask` (multi-select: labels joined with ", "; Other: the free text), or `allow: bool` for `approval` and `effect` |
| `thread.markSeen` | `upTo`: the thread's `activityAt` the client showed (RFC 3339). Without it, now. |
| `thread.interrupt`, `thread.retry`, `thread.dismiss`, `thread.unsettle`, `thread.unsnooze`, `thread.archive`, `thread.unarchive`, `thread.markUnseen` | none |
| `thread.settle` | `now?: true` for bulk settle |
| `thread.snooze` | `until` (RFC 3339, future) |
| `thread.rename` | `title` |
| `thread.setModel` | `model` |
| `t3.adopt` (no `threadId`) | `t3Ids: [..]` |

## `GET /api/t3/candidates`

`{"threads": [{"t3Id": "…", "title": "…", "cwd": "…", "profile": "claudeAgent", "model": "…", "sessionId": "…", "prUrl": "…", "updatedAt": "…", "hasTranscript": true}]}`, open top-level T3 threads not yet adopted.

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

`GET /api/previews/{id}` serves an HTML preview with `Content-Security-Policy: sandbox allow-scripts; …; connect-src 'none'`. Embed it as `<iframe src="/api/previews/{id}" sandbox="allow-scripts">`: scripts run, but the page has no origin, so it cannot call the API.

## Projects (SPEC 0.5 V0.23)

Commands (through `POST /api/command`, no `threadId`):

| action | args |
|---|---|
| `project.update` | `id`, then any of `name`, `color` (a palette name: slate blue sky teal green olive violet indigo rose brown), `icon` `{kind: monogram\|emoji\|line, value}`, `profile`, `model` |
| `project.create` | `name?`, `sources: [Source]` |
| `project.addSource` | `id`, `source` |

The stream sends `event: project` with a project (same shape as in the state) when one is made or changed. Threads carry `projectId`; `project` stays the display name.
