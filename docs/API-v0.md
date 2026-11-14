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
  "projects": [{"cwd": "/home/me/src/app", "name": "app", "lastUsed": "…"}]
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
  "pr": {"url": "…", "repo": "acme/app", "number": 42, "title": "…", "state": "OPEN", "checks": "pass|fail|pending|none", "review": "APPROVED", "conflict": false},
  "settledAt": null, "archivedAt": null, "settleWhenDone": false, "settleWhenMerged": false,
  "source": "fyr|t3",
  "branch": "fix/map-lasso (empty when not a git checkout)",
  "rev": 1794643457123456789,

  "attention": {"state": "asking|failed|blocked|done|working|seen|snoozed|settled|archived", "section": "needs|working|seen|snoozed|settled|archived", "reason": "…"},
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
| `user` | `{"text": "…", "images": 1}` what the user sent |
| `msg` | a raw `claude` stream message: `{"type": "assistant" \| "user", "parent_tool_use_id": null \| "…", "message": {"content": [blocks]}}`. Blocks: `text`, `thinking`, `tool_use {id, name, input}`, `tool_result {tool_use_id, content, is_error}`, `image`. A non-null `parent_tool_use_id` is a subagent message: fold it under that tool use. |
| `run.started` | `{}` |
| `run.ended` | `{"failure": Failure \| null, "interrupted": false, "costUsd": 0.01, "durationMs": 1200}` |
| `decision.opened` | `{"decision": Decision}` |
| `decision.closed` | `{"id": "…", "answer": "text shown in the log"}` |
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
