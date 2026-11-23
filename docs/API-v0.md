# v0 HTTP API (SPEC 0.5 V0.2, decision 0002)

The contract between `fyrd` and the web client. All JSON. Times are RFC 3339 UTC; the client shows them in local time.

## Guard (V0.156, decision 0030)

Each listener has **one way to name its caller**, and nothing reads identity from the `Host` header. The caller is a principal (`internal/authn`): a `device` (a browser, by its session), `local` (the owner socket), or an `install` (a front acting on its own). A host reads who acts from one header on the hop, `Fyr-Actor`, and only on a channel that already proved which install speaks.

| Listener | Authenticator | Notes |
|---|---|---|
| `<data>/owner.sock` | the peer's user is fyrd's own or root | `POST /session/once` gives a one-time link (`fyr open`) |
| `<data>/serve.sock` | the peer's user is root (tailscaled; config `serveSocketUsers` adds users for a test), then the `fyr_session` cookie, else `Tailscale-User-Login` matching the configured owner (`ownerLogin` or roster) mints one | optional Tailscale route; a request there takes the public origin (`X-Forwarded-Host` when it is a `ts.net` name, else the node's MagicDNS name and the first `peerPorts`, default 7443), over https |
| Loopback TCP (the front, and a host's main port) | a loopback `Host` (`127.0.0.1:*`, `localhost:*`, `[::1]:*`, any `*.localhost`), the `fyr_session` cookie; `POST` needs `X-Fyr: 1` | **a front's listener answers `400` to any request with `Fyr-Actor` or `Fyr-Origin`**. A request with no session is still served in this release as the `legacy` principal and logged |
| The in-process pipe (role both: front to its own host) | only this process can dial it: no browser guard | **`Fyr-Actor` is required**: missing or malformed is `401`, never a default. No `/mcp` |
| A host's tailnet listener (`hostListen`, V0.51) | tailscaled whois of the connection is the owner's login; a request with `Origin`, or a `Host` that is neither an address, a `ts.net` name nor one of the node's own names, is `403`; `POST` needs `X-Fyr: 1` | `Fyr-Actor` is read when present. A front of an older build sends none and is still served as `legacy`, and counted. **No `/mcp`** |
| **The peer port** (`peerListen`, V0.158; any interface) | TLS 1.3, ALPN `fyr-peer/2` (`fyr-peer/1` only as the one-way fallback to an older install), a self-signed certificate over the install's Ed25519 key **both ways**: the dialer requires the exact install id it meant to reach, the listener learns the dialer's key from the handshake and asks the roster. No name, address, expiry or CA plays a part | open to any key: `GET /.well-known/fyr` (`{fyr: 1, id}`) and `POST /pair/tailnet`. For a key in the roster: `GET`/`POST /roster` (statement sync) and the host protocol with `Fyr-Actor` required (the front is the key the link proved; a device the roster says is revoked is `401`; a call that changes things from an install switched off with `fyr host drive` is `403`). **No `/mcp`**, no plain HTTP, no TLS 1.2 |
| `/mcp` (the main port only) | the agent session's bearer; refuses any request that carries `Origin` with `403` (V0.129) | an agent principal never crosses a hop |

**The hop.** A front forwards a browser's call only as a principal. It drops `Cookie`, `Authorization`, every `Tailscale-*`, `X-Forwarded-*` and `Forwarded`, `Origin`, `Referer`, `Sec-Fetch-*` and the browser's `Host` (the host's own name is the `Host`), and sets:

- `Fyr-Actor: v=1; kind=device|local|install; id=<device id | owner | install id>; how=local|tailnet|code|link|legacy[; front=<install id>]`;
- `Fyr-Origin: <scheme>://<host[:port]>`, the public origin the browser used, from which a preview's CSP names its folder (V0.47).

The front's own calls (its stream, its state reads) are `kind=install` with its own id. A host puts the principal in each owner command's `command` event as `actor` (`device:<id>`, `local:owner`, `install:<id>`) and `front`: the audit trail. The header is not signed: the channel's key would be the signer.

**Sessions and modes (V0.130, V0.157).** `fyr_session` names the browser's device (HttpOnly, SameSite=Strict, Secure off loopback). Ways to get one: a one-time link (`fyr open`, 60 s; `fyr device add` or Add a device, 10 min, 128 bits, once), the owner's tailnet node through Serve's socket, an approval code. The browser way in has a **stored mode** in the install's database, `observe` or `enforce`; no release changes it. In `observe` a request with no session is served as the `legacy` principal and counted; in `enforce` it is `401` (`{"signedOut": true}`), a browser navigation gets the sign-in page with a code, and only these are served without a session: the client's own files, `/auth/*`, `/.well-known/fyr` and `GET /api/previews/…` (a sandboxed page has no cookie; the 80-bit id in its URL is its key). A mode moves by the install's own rule (three days in observe with nothing it would refuse and a sign-in of each way in use; the owner is told with a push and a notice, and the audit says so), by a signed-in device, or by `fyr auth enforce|observe`; a mode set back by hand stays until the owner enforces. `peer` is stored too and cannot enforce yet. In `enforce` a `Tailscale-*` header on any listener but Serve's socket is `400`, and a call that changes things must carry `Origin`.

**Before the cookie** (both modes): an `Origin` that is not this front's own origin is `403`; so is a credentialed `/api/` call whose `Sec-Fetch-Site` is not `same-origin` (or absent).

**Tailnet node (serve.sock).** whois of `X-Forwarded-For` names the node. A tagged node, a node shared in from another tailnet, or another user's is refused. A blocked node (its device was revoked) is `403`. An approved node gets a session in silence. A new node: in observe, a session and the node approved; in enforce, a session that waits (it can do nothing, and `/auth/status` shows its code) unless the install has no device at all, when the first node is let in at once.

**Approval code.** `POST /auth/code` (no session) makes a waiting session and an 8-character code (no 0, O, 1, I; 40 bits; 5 minutes). `GET /auth/status` tells it whether it was let in. The owner types the code on a signed-in device (`POST /api/front/auth/approve`) or runs `fyr device approve`. Used once; five wrong tries in five minutes end every open code; ten requests a minute per front. Approving needs a session older than the one approved, or the owner socket.

**Settings → Devices** (`/api/front/auth/*`, a signed-in device): `GET state`, `POST approve {code|id}`, `deny {id}`, `revoke {id}`, `add {name}` (a link and its QR as SVG), `mode {way, mode}`. **Revoke** ends the session and the devices it added, blocks the tailnet nodes, closes the device's open streams within a second and drops the push subscriptions its session made.

**The roster (V0.158).** `<data>/roster.json` is a grow-only set of statements signed with install keys: `install-add` (key, name, where to reach its peer port), `device-add`, `device-revoke`. An install is a member when it is this install or a member signed its add; a statement by a non-member is kept and counts for nothing, so every install holding the same statements computes the same members. A revoke wins for ever. Statements are exchanged with every member that has a peer port, at once, on every change and every `peerEverySeconds` (60). **Condition 1**: this node is untagged with user U; the other is in tailscaled's status with the same user id, no tags, not shared in, key not expired; `GET https://<its MagicDNS name>:<Serve port>/.well-known/fyr` over ordinary WebPKI names its install id and peer port (only its own Serve can answer); the link is dialed pinned to that id, `POST /pair/tailnet` asks the other side to run the same check on this node, and its Serve must name the key on the connection. Both sign the other's add. A tagged node, another user's, a shared-in node, an expired one, a node whose Serve does not answer, and the same LAN or a name or first contact never pair this way. A front follows a member over the peer link and keeps the whois link as a fallback, counted (`fyr host list`).

**The owner's invite (V0.159, condition 3).** `fyr pair` (or Settings → Hosts → Pair, a signed-in device) on a member opens an invite `fyr1-<install id>-<10 characters>` (the id in four groups, the secret 50 bits from an alphabet with no 0, O, 1, I): shown as text and a QR code (the Settings page also gives a link that opens Hosts with the code filled in). It lives 10 minutes, works once, and five wrong secrets end it; a new invite ends the old. The other install runs `fyr pair CODE [-addr host:port]` (or Hosts → Add): it pins the id from the code, dials the address (the given one, else every tailnet node on the default peer port 7779 and the roster's addresses, tagged nodes included), and sends the secret inside the pinned channel with `POST /pair/invite {secret, name, host, port}`; the inviter learns the joiner's key from that channel and signs its add, the joiner signs the inviter's. A relay with a key of its own fails the handshake and never sees the secret. No short code, no PAKE. A tagged install pairs only this way, and is then known to every other member by sync (vouched for).

**Removal, rotation and the old link ends (V0.160).** `fyr host remove ID` (or Settings → Hosts → Remove, a signed-in device) on any member writes an `install-remove` statement: the install is gone for ever, what it signed counts for nothing (the members only it vouched for are signed for again by the remover in the same batch), its open requests close within a second, and the next sync tells every member; a front stops following a host it reached only through it. A host that only the removed install could reach learns of it when it next meets another member, or when the owner runs the removal there. `fyr rotate-key` writes a `key-rotate` statement signed by the old key, a new key waits in `identity.key.next` and takes over at the restart (fyrd exits 75; the launcher restarts it); the certificate of the new key carries the rotation, so a peer that was offline follows it by the chain, and the old key is refused (`identity.key.old` is kept for the owner's records). A key that may be stolen is removed and paired again, not rotated. **Step-up** (config `stepUpMinutes`, default 5; the owner socket is never asked): approving a device or code, Add a device, Pair and Add need a session that is at least that old. **The peer way in** can enforce (by hand, by a signed-in device, or by itself after three clean days when every followed host is on a pinned link and a request has come over the link): the whois listener and the loopback host port are then not started, a device no member signed in and the legacy principal are `401` on the peer port; `fyr auth observe peer` brings the listeners back. `fyrd selftest` loads the install key, the roster and the device table, so a build that cannot read them is not switched to.

**The owner's login (V0.owner-without-tailnet).** Tailscale is one optional route; without it no owner login is needed. When using Tailscale Serve or condition 1 auto-pairing, the Tailscale owner login comes from config (`ownerLogin` / `-owner-login`), from the first-run CLI step (`fyr auth owner <login>`, stored in `<data>/roster.json`), or by roster sync from a paired install (`owner-login` statement); `fyrd` never falls back to `tailnet.Self`. Config `ownerLogin` wins over the roster. Settings → Hosts (`GET /api/front/peer/state`, `POST /api/front/peer/invite`, `POST /api/front/peer/join`) lists the installs and how each came in (paired, or vouched for by whom), unpaired candidates (LAN mDNS or tailnet), and the Pair and Add forms.

**Owner socket** (`<data>/owner.sock`, never gated by a mode): `POST /host/remove {id}`, `POST /host/rotate`, `POST /pair/invite`, `POST /pair/join {code, addr}`, `GET /host/list`, `POST /host/drive {id, on}`, `POST /session/once`, `POST /device/add`, `POST /device/approve {code}`, `POST /device/revoke {id}`, `GET /device/list`, `GET /auth/status`, `POST /auth/mode {way, mode}`, `GET /auth/owner`, `POST /auth/owner {login}`, `POST /push/native {id, name, remove?}` and `GET /push/native/stream?id=…` (native desktop push queue for `Fyr.app` and `fyr-desktop`, V0.mac-notify, V0.linux-desktop). `fyr open`, `fyr device add|approve|revoke|list`, `fyr auth status|observe|enforce|owner` and the native desktop apps use them.

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
  "failure": {"class": "api|quota|auth|mcp|process|restart|error", "message": "…", "at": "…"},
  "decisions": [Decision],
  "heldAsks": [Decision],
  "outbox": [{"id": "…", "state": "sent|queued"}],
  "tasks": [{"id": "a803…", "description": "Design mobile translation", "type": "local_agent", "status": "running|completed|failed|stopped", "activity": "Running build", "summary": "…", "toolUses": 14, "tokens": 143218, "startedAt": "…", "updatedAt": "…"}],
  "pr": {"url": "…", "repo": "acme/app", "number": 42, "title": "…", "state": "OPEN", "checks": "pass|fail|pending|none", "review": "APPROVED", "conflict": false, "checksDone": 3, "checksTotal": 5, "failedChecks": ["test"], "rules": ["pull_request"], "missingChecks": ["build"], "protected": false},
  "prs": ["all non-dismissed PR snapshots of the thread's code changes, primary first (V0.multi-pr)"],
  "settledAt": null, "archivedAt": null, "settleWhenDone": false, "settleWhenMerged": false,
  "parent": "t_… (the thread whose agent started this one, V0.22)",
  "branch": "fix/map-lasso (empty when not a git checkout)",
  "rev": 1794643457123456789,
  "liveView": true,
  "live": {"pageId": "…", "state": {"headline": "Tests pass; PR open", "steps": ["…"]}, "updatedAt": "…", "checkedAt": "…", "headline": "Tests pass; PR open"},

  "attention": {"state": "asking|failed|blocked|done|working|waiting|seen|snoozed|settled|archived", "section": "needs|working|seen|snoozed|settled|archived", "reason": "…", "group": "answer|approve|unblock (Needs you only, V0.row-group-code)", "word": "the short word a row shows: Run, Settle, Merge", "subject": "what the word is about: the why, or the change title (V0.row-group-code)"},
  "view": {"seenAt": "…", "snoozeUntil": "…", "snoozeWhenDone": true},
  "unavailable": {"thread.interrupt": "No run is going"},
  "quietUntil": "2026-11-15T06:00:00+01:00 (absent unless the project is in its quiet hours, V0.46)",
  "pushQuiet": "true while the attention state is only an auto-merge at work: no Web Push (absent otherwise, V0.200)"
}
```

`pr` is the PR of the thread's main change (`mainChange`, a change id) and `settleWhenMerged` says that `settleWhen` waits for that change to land; the host derives both (#32). The host stores the settle as one component, `{at, when: [{kind: done|change, change, phase}]}` (#33), and derives `settledAt`, `settleWhenDone` (a `done` condition), `settleWhenMerged` and `settleWhen` (a change condition, one that is not "when merged" first) from it; several conditions may be armed, any one that holds settles the thread, and never while a run is live. `settleAsk: {at, why}` (V0.settle-request) is the agent's word that its work is done, on a project whose `agentSettle` is not `auto`: the client shows a Settle chip under the newest message; a new run, a message and a settle clear it. `wakes: [{rule, at, note?}]` (V0.wakeups-via-watch) are the agent's armed watches on a time, soonest first, derived from `rules`: the client shows the soonest as a "wakes 06:00 · …" chip. `project` is the name of the thread's project (`projectId`), derived and not stored. `usage: {input, output, thinking, cacheRead, cacheWrite5m, cacheWrite1h, total}` (V0.usage-ledger) is the thread's tokens so far, from the host's usage ledger, absent before its first counted result; `thinking` is part of `output`, and `total` is a plain sum of the kinds. Every PR is stored on its code change (`changes[].pr`), so a thread with two PRs shows both on the Changes board.

`liveView` is the live view flag and `live` the view itself (V0.37); both are absent on a thread without them. The flag is stored as `live.enabled` (#33), which the DTO also carries. `live.headline` is `state.headline` when it is a string; `checkedAt` is the last `view_*` call, `updatedAt` the last change of page or state.

`heldAsks` (V0.136) are the agent's merge asks that wait for CI on their head: recorded, never in `decisions`, never counted as asking. A change with one carries `need {cause: "waiting", muted: true, text: "Waiting for CI: 3 of 5 checks done", head, ask}`. `checksDone`, `checksTotal` and `failedChecks` count and name the head's checks from the poller's read. `rules` (the base's active rule types), `missingChecks` (required checks that did not report on the head) and `protected` (classic branch protection) are read only when GitHub reads the PR as BLOCKED with green checks. A merge Fyr ran that failed leaves the change condition `mergeable` failed with "Merge refused: …" until a push or the merge.

`rev` increases on every change, also across host restarts: keep the copy with the higher `rev`. An action name missing from `unavailable` is available. The client never computes status; it renders `attention`.

`Decision`:

```json
{"id": "d_…", "kind": "ask|approval|effect", "createdAt": "…", "agent": "subagent name or empty",
 "questions": [{"question": "…", "header": "…", "multiSelect": false, "options": [{"label": "…", "description": "…"}]}],
 "tool": "Bash", "input": {"command": "…"}, "judgeNote": "optional approval judge escalation reason",
 "effect": {"action": "pr.merge", "args": {"pr": "…", "method": "squash"}, "reason": "…"}}
```

**Two-way links (V0.174).** `hosts[].link` is `{state: up|connecting|down, via: pipe|peer|whois, opened: here|them, since, error: {kind, text}}`. Two installs that are members of one roster keep one TLS 1.3 connection (ALPN `fyr-peer/2`) that either may open; requests go both ways over it (an outer h2c session whose one tunnel `POST /link/tunnel` carries an inner h2c session with the roles reversed). `kind` is one of timeout, refused, key-mismatch, not-a-member, removed, older-version, no-address, no-route, closed. An older peer (`fyr-peer/1` only) has no link and keeps the per-request route.

**The LAN (V0.175).** Config `tls: {cert, key, listen, name}` makes fyrd also serve the front over HTTPS (TLS 1.2+, the owner's certificate) on `listen`; the page, `/api/…` and `/auth/…` are the same as on loopback, but every request needs a session (the browser way's observe mode does not apply here), a `Tailscale-*` header is 400 and `fyr device add` returns a link on `https://<name>:<port>/auth/once#…`. `GET /host/list` on the owner socket gains `candidates[]` of kind `lan` (`id`, `addr`) for installs heard by mDNS that are not members. Config `discovery: {mdns: false}` turns mDNS off; `lanBusFile` reads candidates from a file (a LAN where multicast does not pass, and the tests).

**What a phone can do on a LAN, honestly.** A desktop that runs fyrd uses its own front at `http://fyr.localhost:7777` and reaches the other installs over the peer link. A phone with Tailscale uses Serve's `*.ts.net` certificate (recommended). A phone with no Tailscale and the owner's own certificate (an ACME DNS-01 certificate on the owner's domain, or the owner's CA) uses `tls` above and signs in by QR or code; Web Push needs the internet. A phone with neither gets nothing: fyrd serves no plain HTTP and no self-signed page off loopback, because a browser cannot pin a key and a warning that was clicked through gives a page with no service worker and a session an attacker on the LAN can take.

## The front's API: the host in the path (decision 0031, V0.172)

A front serves two prefixes and nothing else under `/api/`: there is no default host. Every other `/api/` path is `404 {"ok": false, "code": "no-route"}` and reaches no host (V0.old-routes-end; the old routes, `?host=`, `args.host` and the first host as a default are gone). Every other section of this document that names an `/api/…` path describes the host protocol, which does not change: a host never sees the prefix, and a browser reaches each of those paths as `/api/h/<hostId>/<path after /api/>`.

- `/api/h/<hostId>/<path after /api/>`: the host protocol on one host, named by its install id (`hosts[].id`). `GET /api/h/F7QK-2M9X-AB34-CD56/threads/t_12/events` is `GET /api/threads/t_12/events` on that host. An unknown id is `404 {"ok": false, "code": "no-host"}`; an unreachable host is `503 {"ok": false, "code": "host-offline", "link": {…}}`, except `state`, `threads/{id}` and the six settings reads, which the front answers from its cache with `X-Fyr-Read-At`. Uploads and previews use the same prefix; a preview's relative URLs resolve under it.
- `/api/front/…`: what belongs to no host. `GET state` (`{now, user, buildId, front: {id, name}, hosts, projectOrder}`), `GET stream` (each frame of a host carries `host: <id>`), `POST command` (`front.projectOrder`, `front.hostOrder {ids}`, `front.hostId {host}`, `front.hostMark`, `front.projectAdd`, `front.projectLink`, `front.projectUnlink`, `front.projectShared`, `front.threadHandover`), `POST presence {host, threadId, device}`, `push/*`, `auth/*`, `peer/*`.
- `hosts[].link` is `{state: "up"|"down", via: "peer"|"whois"|"pipe", opened, since, error?: {kind, text}}`.

## `GET /api/stream` (Server-Sent Events)

- `event: hello` `data: {"now": "…", "buildId": "…"}` once at connect. When a later `hello` has a different `buildId` than the first one the client saw, the client reloads itself once (V0.13); a missing `buildId` never reloads. The client then fetches the state (subscribe first, then snapshot, so nothing is lost): through a front, `/api/front/state` and then each host's `/api/h/<id>/state`.
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
| `run.ended` | `{"failure": Failure \| null, "interrupted": false, "durationMs": 1200}` |
| `quota.continued` | `{"attempt": 1, "resetAt": "…", "message": "…"}` Fyr automatically continued a quota-stopped thread after its reset time passed (V0.quota-auto-continue) |
| `decision.opened` | `{"decision": Decision}` |
| `decision.closed` | `{"id": "…", "answer": "text shown in the log"}` |
| `decision.autoApproved` | `{"tool": "Bash", "summary": "…", "reason": "…", "model"?: "…", "durationMs": 420, "cached"?: true}` quiet audit record when the per-project approval judge auto-allows a tool call (V0.tool-approval-judge) |
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
| `thread.answer` | `decisionId`, then `answers: {question: label}` for `ask` (multi-select: labels joined with ", "; Other: the free text) and `skipped: [question]` for the questions the owner left without an answer (V0.ask-cancel: at least one question must be answered), or `allow: bool` for `approval` and `effect` |
| `thread.cancelAsk` | V0.ask-cancel: `decisionId` of an open `ask`; the card closes without an answer, and the agent is told why it has none |
| `thread.markSeen` | `upTo`: the thread's `activityAt` the client showed (RFC 3339). Without it, now. |
| `thread.interrupt`, `thread.retry`, `thread.dismiss`, `thread.dismissPR` (V0.36, V0.multi-pr: optional `pr` or `change`; merged or closed PR only), `thread.unsettle`, `thread.unsnooze`, `thread.archive`, `thread.unarchive`, `thread.markUnseen`, `thread.pin` and `thread.unpin` (V0.104, owner only), `thread.restart` (V0.143: only while the agent does not respond) | none (or `pr?` / `change?` for `thread.dismissPR`) |
| `thread.settle` | settles now and stops a live run; `now?: true` for bulk settle, which refuses a pinned or running thread |
| `thread.settleWhenDone` | V0.203: settles once no run or task is live, nothing waits for the owner and no PR is open |
| `thread.wakeOnQuota` | V0.quota-reset-wake, V0.quota-auto-continue: `{provider?, window?, note?}` arms an owner-owned wake when that provider's quota window resets and clears a stopped thread's failure; falls back to the thread's own parsed future `failure` reset notice when the provider has no quota polling and no specific window was named |
| `thread.cancelWake` | V0.wakeups-via-watch: `{rule?}` cancels the agent's wake-up (its watch on a time or quota reset; no rule: the soonest); the agent is told for agent-armed wakes |
| `thread.cancelWait` | V0.waits-strip: `{rule?}` cancels one of the agent's armed watches of any kind (`thread.waits`; no rule: the first); the agent is told for agent-armed ones |
| `thread.snooze` | `until` (RFC 3339, future) |
| `thread.snoozeWhenDone` (V0.185): until the thread's work is done, at most 24 h; `view.snoozeUntil` is then that maximum and `view.snoozeWhenDone` is true. Unavailable, with the reason, when no work goes on or the thread needs the owner | none |
| `thread.rename` | `title` |
| `thread.regenerateTitle` (V0.108, owner only): Fyr writes the title from the conversation; the result lands later as `thread.renamed`, or a failed line | none |
| `thread.setProject` | `projectId`: file the thread into a project by hand; folder matching keeps it (V0.23). `""` unfiles it: no project, still filed by hand, so matching leaves it unfiled (V0.30) |
| `thread.setModel` | `model?`, `effort?` (at least one; V0.50: an effort alone keeps the model; `""` returns to the provider's default). The thread DTO carries `effort` when set |
| `thread.handover` | `profile`, `model?` (default: the profile's default model): hand the thread over to another profile (V0.45). `409` while a run is live, a question is open, a handover is going or the thread is archived; `400` for an unknown profile or the thread's own. While it runs the thread has `"handover": {"to": "…", "model": "…", "at": "…"}` and sends are unavailable |
| `thread.cancelMessage` | `messageId`: withdraw a message the agent has not read (V0.33). `409` when it was read already, is unknown, or the agent cannot withdraw it |
| `host.deployNow` (no `threadId`) | none: a deploy that waits for running threads switches now; their turns end and each gets "continue" after the restart (V0.83). `409` when no deploy drains |
| `host.setupFix` (no `threadId`) | `action`, `args?`: runs one fix step of the Setup wizard (`GET /api/setup`, V0.setup-wizard) |
| `provider.*` (no `threadId`) | see Providers (V0.50): `import`, `create`, `update`, `refreshModels`, `setKey`, `login`, `loginCode`, `loginCancel`, `delete` |

## `GET /api/threads/{id}/handover?profile=P` (SPEC 0.5 V0.45, V0.trans-host-handover)

`{"rung": "resume" | "adopt" | "brief", "note"?: "why rung 1 does not apply", "cli"?: "claude", "git"?: {"repo"?, "branch"?, "default"?, "head"?, "ahead"?, "unpushed"?, "dirty"?, "worktree"?}}`: the rung a handover to `P` would use now, plus the source CLI family and git checkout summary for cross-host handover (decision 0039). The handover itself still falls back when the rung fails.

## Trans-host handover (decision 0039, V0.trans-host-handover)

- `POST /api/front/command` with `action: "front.threadHandover"` and `args: {fromHost, threadId, toHost, profile, model?, effort?, cwd?, projectId?, useWorktree?}` orchestrates a cross-host handover between two online hosts. Reply `result: {host, threadId, rung, note?, cwd?, sourceWarning?}` where `threadId` is the target thread ref `<toHost>/<rid>`.
- `POST /api/threads/{id}/handover/export` (host protocol, `X-Fyr: 1`): validates handover availability on the source thread and exports `{thread, cli, git, events, conversation, sessionArchive?, sessionNote?, openChanges?, pr?}` (or with `{"wantBrief": true, "targetHost", "targetProfile"}`, runs rung 3 on the source and returns `{"brief": "…"}`).
- `POST /api/threads/handover/import` (host protocol, `X-Fyr: 1`): creates the linked target thread (`handedOverFrom`), copies the prior events and open PR, attempts the 3-rung ladder (extracting `sessionArchive` for `--resume` with a queued `[Fyr] Host handover:` notice, else Adopt with `conversation`, else returning `{"needBrief": true}` or running Brief), and returns `{ok: true, threadId, rung, note?, cwd, projectId?}`.
- `thread.handedOver` (via `POST /api/command` on the source host): records `handedOverTo`, appends a cross-host `handover` event, and settles the source thread.

## Slash commands (SPEC 0.5 V0.79)

`GET /api/threads/{id}/commands` returns `{"commands": [{"name": "review", "description": "…", "argumentHint": "<pr-number>"}], "from": "thread" | "folder" | ""}`: the commands and skills of the thread's session (its `initialize` answer, without the terminal-only ones), else the newest list the host saw for the thread's provider and folder, else `[]` with `from: ""`. `404` for an unknown thread.

`GET /api/commands?cwd=<folder>&profile=<id>` returns `{"commands": […]}`, the newest list for a provider and folder (New thread); without `profile`, the host's first profile.

The client sends a command as an ordinary message (`/review 12`); the CLI runs it.

## Web Push (SPEC 0.5 V0.15)

| Call | Body | Reply |
|---|---|---|
| `GET /api/front/push/key` | – | `{"publicKey": "<base64url VAPID public key>"}` |
| `POST /api/front/push/subscribe` | `{"subscription": PushSubscription.toJSON(), "device": "Pixel 9 · Chrome"}` | `{"ok": true}` |
| `POST /api/front/push/unsubscribe` | `{"endpoint": "…"}` | `{"ok": true}` |
| `POST /api/front/push/hidden` | `{"endpoint": "…", "hosts": ["lab"], "projects": [{"host": "desk", "id": "p_12"}]}` | `{"ok": true}`: false when the subscription is unknown. V0.207: this device gets no push about these hosts and projects |
| `POST /api/front/push/prefs` | `{"endpoint": "…", "finished": false}` | `{"ok": true}`: false when the subscription is unknown. V0.notification-polish: this device gets no push for a thread that finished (`state: "done"`); `finished` absent means true |
| `POST /api/front/push/test` | `{"endpoint": "…"}` or – | `{"delivered": 1}`. V0.notification-polish: to the device behind `endpoint` only; without one, to every device |
| `POST /api/front/push/status` | `{"endpoint": "…"}` | `{"known": true}`: false when the host deleted it after a 404 or 410 |

All POSTs need `X-Fyr: 1`. The front pushes `{"title", "body", "threadId", "state"}` when a thread enters `asking` or `failed` (at once) or `blocked` or `done` (after 20 s, if still true; V0.20); the service worker shows it with `tag = threadId` and `data.state`, opens `/?open=<threadId>` on a tap, and a client closes it when the thread leaves that state (V0.notification-polish).

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
| `project.update` | `id`, then any of `name`, `color` (a palette name: slate blue sky teal green olive violet indigo rose brown), `icon` `{kind: monogram\|emoji\|line, value}`, `folder` (a full path; `""` clears it, V0.55), `image` (`""` removes the image icon and its files; V0.116), `profile`, `model`, `effort` (V0.model-choice), `priority` (`normal\|high`, V0.project-priority), `agentSettle` (`chip\|auto`, V0.settle-request), `toolApproval` (`ask\|auto`, V0.tool-approval-judge), `quiet` `[{from: "17:00", to: "06:00", days?: ["mon"…"sun"]}, …]` (at most 14 windows; a single object is read as a list of one) or `null` or `[]` to clear (V0.46, decision 0014 addendum) |
| `project.create` | `name`, `folder?`, `threadId?`: file that thread into the new project by hand in the same write (V0.30). Reply `result: {id}` |
| `project.setupRun` | `id` (V0.116): starts the project setup run, a thread in the project's folder; the result is `{id, thread}`. No folder: `400`; a system agent that cannot run: `409` with the reason, also recorded as `setup.error` |
| `host.setupRun` | (V0.208): starts this host's mark setup run, a thread in its home folder; the result is `{thread}`. A system agent that cannot run: `409`, recorded in the state's `agentMark.setup`. |
| `project.link` | `id`, `link` (decision 0034, V0.211): owner only; `l_` and 16 to 32 hex digits, `""` clears it. A front sends it for `front.projectLink` and `front.projectUnlink` (refs of `<hostId>/<id>`, on `POST /api/front/command`) |
| `project.shared` | `id`, `link`, `at`, `name`, `color`, `icon`, `quiet` (decision 0034, V0.212): owner only; taken when `at` is newer than the record's `sharedAt`. A front sends it for `front.projectShared {link, name?, color?, icon?, quiet?}` and to catch a member up |
| `project.create` with `link` | (decision 0034, V0.213): the new project is a member of that link and gets no setup run. A front sends it for `front.projectAdd {ref, host, folder}`. A project's `repo` is its folder's normalised git origin |
| `project.delete` | `id` (V0.58). Unknown id: `404`. Every thread in it loses the project and the filed-by-hand mark, then the host matches again |
| `project.archive`, `project.unarchive` | `id` (V0.project-archive): owner only. Archive sets `archivedAt` and, in the same write, `shelvedAt` on each of the project's threads that is not archived; `409` with the reason (naming the thread) while one of them has a live run, a message on its way or an open question. Unarchive clears both. Sending it to a project already there answers `{id, kept: true}` |

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

`GET /api/threads/{id}/pr` returns a PR of the thread, read with `gh` on the host and cached 30 s per PR. `?pr=<url>` or `?change=<id>` selects a specific PR linked to the thread (V0.multi-pr; defaults to the main PR or newest non-dismissed PR). `?fresh=1` skips the cache.

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
| `pr.merge` | `pr?` or `change?` (V0.multi-pr: defaults to the main PR), `method?`: `squash` (default), `merge` or `rebase`, `head?`. Unavailable without an open PR, with a merge conflict or requested changes. The host reads the PR again and refuses a draft, a closed PR, and a merge state of `DIRTY`, `BLOCKED` or `BEHIND` |
| `pr.rerunFailed` | `pr?` or `change?` (V0.multi-pr: defaults to the main PR): reruns (`gh run rerun <id> --failed`) every failed, cancelled or timed-out workflow run of the head commit. Unavailable without an open PR |

Both reply at once; the result arrives as an `effect.result` event with `"by": "owner"` (the agent is not told). `ThreadDTO.unavailable` carries their reasons like any action.

## Speed (SPEC 0.5 V0.38)

- Responses with a text body are gzipped when the request has `Accept-Encoding: gzip` (`Vary: Accept-Encoding`); `/api/stream` too, flushed per event.
- `msg` events in `/api/threads/{id}/events` and on the stream are lean: a `tool_result` text over 16 KB (4 KB when `parent_tool_use_id` is set) is cut, base64 image data is left out (`source.omitted: true`, `source.bytes`), and the event data has `"cut": true`.
- `GET /api/threads/{id}/events/{seq}` returns that one event whole (`404` when the thread has no such event).
- Stream: after a connection has a thread whole, changes come as `event: thread.patch` `data: {"id", "rev", "base", "patch"}`; apply `patch` (RFC 7396) only when your copy has `rev === base`, else `GET /api/threads/{id}` (the ThreadDTO). A thread is sent at most every 100 ms per connection; the newest version wins.
- `GET /api/state?since=REV` lists only threads with `rev > REV` and adds `"partial": true`; the other fields are whole. Revs grow across host restarts.

**In the app (`internal/front/peerapi.go`).** `POST /api/front/peer/remove {id}` removes a member as `fyr host remove` does; it needs no step-up, so a lost phone goes at once. `POST /api/front/peer/ignore {id, on?}` hides a found install from Not paired (#259); `on: false` shows it again. Ignore is local to this install.

## Update progress (SPEC 0.5 V0.39)

- Stream `event: deploy` `data: {"phase": "build|test|drain|switch|done|failed", "detail"?, "blocking"?: [threadId], "build", "startedAt", "at"}`.
- `GET /api/state` has `deploy` with the latest progress while an update runs and for two minutes after it ended; otherwise `null`.

## A host's update state (V0.host-updates, #154)

- `GET /api/state` has `update` on a host with a release source: `{"running"?, "build"?, "wanted"?, "behind", "last"?, "bad"?, "paused"?}`. `running` is the commit it runs, `build` its build id, `wanted` the commit `deployable` pointed at when the host last read it, `behind` how many commits `wanted` is ahead of `running` (`-1`: not known; read once per move of either, with one GitHub compare), `last` the newest record of `GET /api/updates` without `phases` and `logs`, `bad` `{"commit", "why", "at"}` while the host holds that release back (it rolled back or failed its smoke test here, and `deployable` still points at it), `paused` while a local build runs. V0.versioned-releases adds `version` (the running build's `vX.Y.Z`, missing on a build of main) and `channel` (`main`, `stable` or a pinned `vX.Y.Z`); `wanted` is where the channel pointed. `GET /api/updates` carries the same two beside `live`.
- `GET /api/updates` lists the update records (`{"updates": [...]}`); `GET /api/updates/{build}/{name}` is one log kept with that build, as `text/plain` (V0.126).
- Stream `event: update` with the same object when any of it changes. A front keeps the newest per host and serves it as `hosts[].update`, and sends `hosts` again.
- `host.updateNow` also clears the bad mark (Update now · retry): the release is tried again, and marked again if it fails again. A release that goes live clears it too.

## Stalls (SPEC 0.5 V0.147)

- Stream `event: stall` `data: {"at", "what", "file", "exiting"}`: the host's watchdog saw a watched lock held, or a request running, past its limit (`what` names the longest holder; `file` is the goroutine dump on that host). A front adds `"host"`.
- `GET /api/state` has `stall` (a host's newest stall of the last 24 h, absent without one); a front sends each host's `stall` frame with `host: <id>`.
- A request other than the stream, `/mcp`, attachments, previews and update files answers `503` with `{"ok": false, "error": "Fyr did not answer in time: …", "code": "host-stalled"}` after 10 s (GET, HEAD) or 60 s (other methods).
- V0.181: a front that gets that 503 from a host, or a `stall` frame, asks the host for its state; while it gets no answer the host is offline (`hosts[].online: false`, `hosts[].error: "stalled: …"`, a link error of kind `stalled`) and is served from the front's cache, as a host that went away. It is online again when it answers.

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
- `kind`: `claude-account` (Claude CLI `cli-login`), `agy-account` (Antigravity CLI `cli-login`), or `endpoint` (auth `api-key`, default `cli: "claude"`, optional `cli: "agy"`). `status`: `ready`, `needs-login`, `needs-key`, with `need` saying what to do. `keySet` and `keyLast4` (empty for a key under 12 characters) are all a client learns of a key; `keyFrom` names where a key not yet stored by Fyr is read (an environment variable name or a file path). `efforts` are the adapter's levels; a model's `efforts` narrow them.

`GET /api/providers/found`: what detection found on this host, each `{id, name, kind, cli?, source, configDir?, baseURL?, modelEnv?, models, defaultModel?, quotaKind?, keyFrom?, loggedIn?, status, note?, imported}`. `imported` is true when a provider with that `id` exists.

Commands (`POST /api/command`, no `threadId`; `400` bad arguments, `404` unknown id, `409` conflict):

| action | args |
|---|---|
| `provider.import` | `foundId`: create the provider from that find; a key found in fyrd's environment or a file is copied into the secret store. `409` when imported already. Result `{"id"}` |
| `provider.create` | `name`, `kind: "claude-account" \| "agy-account" \| "endpoint"`, `cli?: "claude" \| "agy"`, `id?` (default from the name), `configDir?` (account; empty is `~/.claude` for Claude, `~/.gemini` for Antigravity), `baseURL` (endpoint; optional when `cli: "agy"`), `key?`, `models?: [id]` (account default: the CLI's list), `defaultModel?`, `defaultEffort?`, `modelEnv?: {NAME: value}`, `quotaKind?`, `color?`. Result `{"id"}` |
| `provider.update` | `id`, then any of `name`, `configDir`, `baseURL`, `models`, `defaultModel`, `defaultEffort`, `modelEnv`, `quotaKind`, `color`, `forget` (V0.120). `models` is the on-list in order; an id the catalog lacks joins it as `manual`. A `models` list without the current default is refused (`400 "Star another model first."`) unless the same update sets `defaultModel`. `forget: [id]` drops hand-typed (`manual`) models that are off |
| `provider.refreshModels` | `id` (V0.120): ask the provider for its models now: a Claude endpoint at `GET <baseURL>/v1/models` with its key (`x-api-key`, then `Authorization: Bearer` on 401/404; 15 s and 2 MB a page, up to 20 pages, no redirect off the host), a Claude login through its CLI's stream-json `initialize` answer plus Fyr's own list, or an Antigravity provider through `agy acp` `session/new`. `409` when the provider needs a login or a key. Waits for the check; a failed check is still `ok`, with `result.checkError` and the catalog unchanged. Result `{checkedAt, checkError, models}`. The host also checks each ready provider once a day, and a provider at once after `provider.create` |
| `provider.setKey` | `id`, `key` (an endpoint's API key, or an account's login token from `claude setup-token`); `""` removes it |
| `provider.login`, `provider.loginCode`, `provider.loginCancel` | `id` (and `code` for `loginCode`, `token?: true` for `login`): drives the CLI's interactive sign-in flow on the host (V0.53) |
| `provider.delete` | `id`; `409` while threads that are not archived use it. Its stored key is removed |

## Host helper & settings reads

Served per host at `/api/h/<hostId>/…` (the front caches the settings reads when a host is offline):

- `GET /api/setup`: live host readiness checks for the Setup wizard (machine tools, providers, GitHub/`gh`, projects, phone, peers; V0.setup-wizard).
- `GET /api/load`: live CPU, memory headroom, PSI stall, slowdown probe, cgroup layout, oomd limit, active/queued jobs and recent guard events (V0.load-card).
- `GET /api/autoDeploy`, `GET /api/autoMerge`, `GET /api/systemAgents`: per-host automation and system-agent settings (`Settings → Automation`).
- `POST /api/search {q}`: searches threads and events on that host (V0.ai-search).
- `POST /api/threads/{id}/focus` and `POST /api/board/visit`: tells the host which thread or board the owner is looking at so PR/git polling prioritises it.

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

The browser talks to a **front**; the front talks to its **hosts** over the **host protocol**, which is this API without the web client. The browser names the host of every call in the path (decision 0031, "The front's API" above); the front adds:

- `GET /api/front/state` lists the hosts in the owner's order: `hosts: [{"id": "F7QK-…", "name": "host-a", "online": true, "since": "…", "error"?: "…", "link": {…}, "profiles": […], "deploy"?: {…}}]` (V0.54: each host's profiles and update). Each row may add `found: true` (V0.123: discovery found it on the tailnet), `idWas` (V0.124: the pinned id when the host now reports another) and `url` (V0.125: the URL the front dials for it, as configured or as found, also while it is offline). A host's records come in its own state (`/api/h/<id>/state`) and frames (`host: <id>`), in the host's own shape: the front stamps nothing and merges nothing. After a front restart a host it cannot reach is served from the front's cache on disk, offline since the front last had it online.
- A call goes to the host its path names and to no other. A call for a host that is offline is `503 {"ok": false, "code": "host-offline", "error": "host-a is offline since 19:40; its threads are read-only", "link": {…}}`, except the reads the front keeps (above).
- Attachments and previews (V0.54): `/api/h/<id>/attachments`, `/api/h/<id>/attachments/{a}` and `/api/h/<id>/previews/{p}[/{path}]`; a preview's relative URLs resolve under the same prefix, so nothing is learned or remembered by the front.
- Quotas, loads and profiles are each host's (`/api/h/<id>/quota`, `…/load`, `…/profiles`); the client shows them per host.
- The owner's project order is the front's (V0.121): `/api/front/state` has `projectOrder: [ref]` (absent until set: clients sort by name), one list across all hosts. `POST /api/front/command {"action": "front.projectOrder", "args": {"ids": [ref]}}` sets it (blanks and repeats dropped; a repeated `commandId` answers its first reply); the front keeps it in its database and sends the stream event `projectOrder {"ids": […]}`. No host has this command, so no agent tool or MCP call sends it. A front that cannot store it answers `500` with the reason.
- Device data is the front's (V0.54): `POST /api/front/presence {host, threadId, device}` and `/api/front/push/*` (key, subscribe, unsubscribe, status, hidden, prefs, test) are served by the front, which sends the push for the threads of all its hosts. Hosts send none.

The host protocol (front → host):

| Call | Use |
|---|---|
| `GET /api/stream` | subscribe first (`hello`, then `thread`, `thread.patch`, `event`, `project`, `deploy`), sent without gzip |
| `GET /api/state` | the snapshot after `hello`; again after every reconnect |
| `GET /api/threads/{id}` | a whole thread when a patch's `base` does not match |
| `POST /api/command` | with `X-Fyr: 1`, the client's `commandId` unchanged (I7 holds across the hop) |
| everything else | passed through with the browser's `Host`, so previews keep their CSP base |

Guard on the host: the loopback guard as above; on a `hostListen` tailnet address only peers whose Tailscale identity (whois of the connection) is the owner's login, and `Tailscale-User-Login` headers are ignored there. A host role never serves the web client.

### Two machines on NixOS (V0.158, decision 0030)

`host-a` and `host-b` each run `fyrd` as host and front (`role` both, the default). Set `peerPort` (and `openFirewall = true` on a LAN) on both machines: they find each other over mDNS on a LAN and pair once with `fyr pair` / `fyr pair CODE`, over mutual TLS pinned to their install keys. Tailscale is an optional route: adding `tailnet.enable = true` and `ownerLogin` (or running `fyr auth owner <login>` once) also publishes the front on `*.ts.net` and lets untagged installs pair automatically.

host-b (LAN, with optional Tailscale lines commented):

```nix
services.fyr = {
  enable = true;
  user = "owner";
  # role = "both";                   # the default: a host and a front
  peerPort = 7779;                   # mutual-TLS peer link (pinned to install keys)
  openFirewall = true;               # allow LAN peers to reach peerPort
  # Optional tailnet route:
  # ownerLogin = "you@example.com";  # or set once with `fyr auth owner you@example.com`
  # tailnet.enable = true;           # Tailscale Serve: https://host-b.<tailnet>.ts.net:7443 → the front
  scanRoots = [ "~/git" ];
};
```

host-a:

```nix
services.fyr = {
  enable = true;
  user = "owner";
  peerPort = 7779;
  openFirewall = true;
  # Optional tailnet route:
  # ownerLogin = "you@example.com";
  # tailnet.enable = true;
};
```

Each front follows every member in its roster automatically; no static `hosts` list is needed. The phone subscribes to notifications on whichever front it opens (each front keeps its own device data).
