# 0031 The client names the host: one host-addressed API, the same for every host; the front's own host is one entry in the list

Date: 2026-11-17 · Status: **proposed** · Builds on 0016 (fronts and hosts), 0025 (the client keeps one uplink), 0026 (an install is its key), 0030 (every hop names who acts) and its Amendment 1 (two-way links) · Amends V0.51, V0.54, V0.121, V0.127, V0.132, V0.134, V0.139, V0.140, V0.169 where they say "the first host", "this install" or name a host by its name on the wire · Spec rows: one per slice, numbered when the slice's PR opens

**The owner's words, 17 Nov:** "Start a new review and refactor of the frontend. A frontend connects to a backend and gets a list of hosts. From that point on, the whole protocol is neutral to who it is connected to. All operations need to include what host to direct to. I.e. a frontend doesn't *really* know what host it's connected to (it does, but it doesn't affect the UX)."

**In one paragraph.** A browser connects to one front, signs in (0030) and reads the list of hosts. After that, every read, command, stream frame, upload and preview names its host by install id, in the path: `/api/h/<hostId>/…`. What follows the prefix is the host protocol, unchanged, so every host answers the same calls, and the host that runs in the front's own process is reached the same way as a host on another machine. No call has a default host. What belongs to no host (the host list, the stream, push, presence, devices, pairing, the owner's orders) lives under `/api/front/…`. The client keys everything it keeps by (host id, record id).

Part 1 is the review of the code at `a58bb97`, part 2 the decision, part 3 the tests, part 4 the migration, part 5 the slices.

---

## Part 1. Review: where the client or the front assumes a host

Terms: *first host* is `byName("")`, the first in the owner's order (V0.132); *local* is `Options.Local`, the host in the front's process; *self* is `state.host`, the front's own name, which the client keeps as `store.host`.

### 1.1 The front's routes (`internal/front`)

| # | Path or operation | Addressing today | What is wrong | Target |
|---|---|---|---|---|
| F1 | `GET /api/state` | none; one merged state | `actions` are the first host's; `profiles` are merged by id and the first host's wins; `deploy` is the local host's; `host` and `hostId` are the front's own; each thread is stamped `host` (a name) and `hostOffline`, and its `rev` is raised past the host's when only the stamp changed | `GET /api/front/state` (the list of hosts and the front's own things) and `GET /api/h/<id>/state` (one host's state as the host sent it, from the front's cache while the host is offline) |
| F2 | `GET /api/stream` | none; one hub | `thread` and `project` frames carry `host` as a name; `event`, `thread.patch` and a project's delete carry none (the client finds the thread by id alone); `deploy` is published only for the local host; `stall` carries a name | `GET /api/front/stream`; every frame that comes from a host carries `host: <id>`; front frames (`hello`, `hosts`, `projectOrder`) carry none |
| F3 | `GET /api/threads/{id}` | by thread id | scans every host's cache; with the same id on two hosts the **last** wins | `/api/h/<id>/threads/{id}` |
| F4 | `/api/threads/{id}/…` (events, changes, pr, handover, commands) | by thread id through `owner()` | scans the caches, the **first** match wins (the opposite of F3); an id the front does not know goes to the first host | the same |
| F5 | `POST /api/command` | `threadId` → `owner()`; else `args.host` (a name); else the project's host; else the first host | four rules and two fallbacks; a host-wide command without `args.host` (`host.deployNow`, every settings command on a one-host front) runs on the first host; the host is hidden in `args`, which belong to the command | `POST /api/h/<id>/command`, the body unchanged and without `args.host` |
| F6 | `front.projectOrder`, `front.hostOrder`, `front.hostId` | actions inside `/api/command` | front commands and host commands share a route and are told apart by name; `front.hostOrder` takes names, `front.hostId` takes a name | `POST /api/front/command`; host ids |
| F7 | `POST /api/attachments` | `?thread=` or `?host=` (a name), else the first host | the fallback; the front remembers id → host in memory (`objs`) | `POST /api/h/<id>/attachments` |
| F8 | `GET /api/attachments/{id}`, `GET /api/previews/{id}`, `GET /api/previews/{id}/{path…}` | `?thread=` or `?host=`, else `objs`, else the first host | a preview's own files (relative URLs in its page) carry no host and are routed by `objs`, which is lost when the front restarts and cleared at 10 000 ids: after a restart they go to the first host. This is the class of #225 and #226: where a request goes is recovered from context, not stated | `/api/h/<id>/attachments/{id}`, `/api/h/<id>/previews/{id}/…`; a page's relative URLs keep the prefix; `objs` is removed |
| F9 | `/api/projects/{id}/icon` | by project id through `projectOwner()` | scans the caches; no host in the URL | `/api/h/<id>/projects/{id}/icon` |
| F10 | `GET /api/quota`, `GET /api/load`, `GET /api/profiles` | none: gathered from every online host; `?host=` for one | merged, the first host's wins by key; stamped with names; one slow host delays all | `/api/h/<id>/quota`, `/api/h/<id>/load`; profiles come in each host's state |
| F11 | the six settings reads (V0.127) | `?host=` (a name), "" is the first host | the fallback; only these six paths are served from the cache while a host is offline | `/api/h/<id>/…`; the cache rule stays, stated once for every host |
| F12 | `/api/fs/*`, `/api/t3/candidates`, `/api/commands`, `/api/updates/{build}/{name}` and every other path (the `/api/` catch-all) | `?host=` (a name), "" is the first host | the fallback. V0.127 refuses only a name the front does not follow | `/api/h/<id>/…`; an unknown id is 404, no id is 404 |
| F13 | `POST /api/board/visit` | none (the catch-all) | the owner's `boardSeenAt` is kept on the first host only: reorder the hosts and the board's "new since" changes | `POST /api/h/<id>/board/visit`, once per host whose changes the board shows |
| F14 | `POST /api/presence` | front; `threadId` only | the front finds the thread's host by `owner()` to forward the focus (V0.85) | `POST /api/front/presence {host, threadId, device}` |
| F15 | `/api/push/*` | front | correct scope. The payload names the thread by id and the host by name (V0.140) | `/api/front/push/*`; the payload carries the host id |
| F16 | `/api/auth/*`, `/api/peer/*` | front | correct scope, not named as such | `/api/front/auth/*`, `/api/front/peer/*` |
| F17 | inside the front: host pins, the cache file per host, the host order, `byName()` | by name | 0030: the name is config and can change, the install id cannot. A renamed host loses its cache file and its place in the order | by install id; the name is only shown |
| F18 | `isLocal()` | `Options.Local`, else the first host | only this host's deploy reaches the stream; the client's banner means "the front I am connected to restarts" | the front names itself once, in `/api/front/state` (2.6); every host's deploy travels the same way |

### 1.2 The client (`web/src`)

| # | Where | Today | What is wrong | Target |
|---|---|---|---|---|
| C1 | `api.ts`: `events`, `thread`, `fullEvent`, `changes`, `filePatch`, `prDetail`, `commands({threadId})`, `uploadProjectIcon`, `boardVisit`, `command` | a thread or project id, no host | F3–F5, F9, F13 | every `Api` method takes the host id first; `api.ts` builds every URL through one function |
| C2 | `api.ts`: `fsList`, `fsRecent`, `fsRepos`, `fsMkdir`, `t3Candidates`, `providers`, `providersFound`, `autoMerge`, `autoDeploy`, `updates`, `systemAgents`, `commands({cwd})`, `uploadAttachment`, `attachmentUrl`, `previewUrl` | `host?: string`, a name; missing or "" is the first host | optional; by name; three helpers build the query (`hostQuery`, `fsQuery`, `settingsPath`) | the same |
| C3 | `handover-ui.tsx`, `projects.ts` (the icon URL), `settings-ui.tsx` (the update log link), `signout.ts` | paths written in the component | they bypass `api.ts`; two have no host | through `api.ts` |
| C4 | `markdown.ts`, `previews.ts` `inlineAttachmentId` | an agent's markdown image `/api/attachments/<id>` on the page's origin renders inline, from the thread's host by a module variable (`curHost`) | the URL the agent writes has no host; the host comes from render state | the agent's path stays (it is the host's own path); the client rewrites it to the thread's host: `/api/h/<thread's host>/attachments/<id>` |
| C5 | `host-settings.ts` `withHost`, `settingsHosts` | a one-host front names its host `""`; commands carry `args.host` | a second code path for one host; the host inside `args` | the host id always, in the path |
| C6 | `store.ts` | one `threads` map by thread id; `projects`, `profiles`, `actions`, `deploy` merged; `host` and `hostId` are the front's own; `events` and `resumeFrom` by thread id | two hosts with one id overwrite each other; `profiles` and `actions` are the first host's | per-host state keyed by host id; lists are selectors over the hosts (2.5) |
| C7 | `cache.ts` (IndexedDB) | one state, one `since` rev | revs are each host's own clock; one `since` across hosts skips a slower clock's threads unless the front raises revs (F1) | one stored state and one `since` per host |
| C8 | `filters.ts` `threadHost(t, self)`, `hosts.ts` `hostOf`, `hostForProject`, `hostChip` → `hosts[0]`; `profilesOn` → the merged list | a record without a host is self's, or the first host's | fallbacks; never needed once every record has a host | removed; a record's host is the host whose state it came in |
| C9 | `hosts.ts` `hostChoices` "(this install)", `host-settings.ts` `groupLine` "This install", `hostLine`/`routeOf(…, self)` "this machine", `hostRows` (a row made from `self` when `hosts[]` is empty), `hostLabel` (the rail and the phone header show the front's name) | labels and rows that depend on which install the browser is connected to | the same phone shows other words through another front | 2.6: one place says where this browser is connected; nothing else |
| C10 | `app.tsx` `DeployBanner` ← `store.deploy`; `host.deployNow` with no host | the banner is the local host's update | F18; the command has no host | the banner is for the host that `front.id` names, read from that host's state like any other; `deployNow` names its host |
| C11 | `localStorage`: `fyr.draft.<threadId>`, `fyr.hostsOff` (names), `fyr.projectFilter`, `fyr.lastProject`, `fyr.newDefaults`, `fyr.newThread`, `fyr.view.collapsed`, `fyr.deploy.dismissed`, `fyr.stall.dismissed`, `fyr.pendingOpen` | by thread or project id alone, or by host name | two hosts with one id share a draft; a rename forgets the filter | keyed by host id and record id; migrated once (4.2) |
| C12 | deep links: `/?open=<threadId>`, Copy link, the service worker's push tap (`{type: "open", threadId}`), `desk-history.ts` | a thread id alone | the link does not say which host | `/?open=<hostId>/<threadId>`; a link without a host opens the one thread that has the id, and is refused when two have it |
| C13 | `quota.ts` `quotaFor(list, profile, host?)`, `load.ts` `HostLoad.host`, `stall.ts` | lists stamped with names; an item without a host matches any host | F10 | per-host state |

### 1.3 The passes so far

V0.127 (settings per host), V0.132 (folders and New thread on a chosen host), V0.139 and V0.140 (every screen says its host), V0.169 and V0.170 (the filter row, the marks) each added a host to one group of calls: an optional name beside the old path, with the first host as the default. The result is three query helpers, four routing rules in `command`, and a default host on nearly every route. Each pass was right; none could remove the default, because the routes without a host stayed.

### 1.4 Findings, in order of weight

1. **A default host exists.** Nearly every route goes to the first host when none is named (only the project routes answer 404), and the owner can reorder the hosts. V0.127's refusal covers only a wrong name.
2. **Records are found by scanning.** Thread and project routes have no host; the front looks the id up in its caches, with two rules that disagree (F3, F4). Ids are 64 random bits per host: a collision is unlikely by chance and certain when an install's data folder is copied to a second machine.
3. **Routing from memory.** A preview's files and an uploaded attachment depend on `objs`; a front restart sends them to the first host.
4. **Names are the wire id.** `?host=`, `args.host`, the thread's `host`, the host order, the pins and the cache files use the name. 0030 made the install id the identity; the name is config.
5. **The merge hides hosts.** Actions, profiles, quota, load and deploy are merged with "the first wins" or "the local one".
6. **The client knows `self`** and uses it for labels and fallbacks (C8, C9).
7. **One owner setting lives on the first host** (F13).

---

## Part 2. Decision

### 2.1 The host id

A host is named by its **install id** (0026, V0.124: `F7QK-2M9X-AB34-CD56`, the first 80 bits of the SHA-256 of the install's public key), in every path, frame, key and link. It is `hosts[].id` today and the roster's id. The name is shown and never sent back.

- The id is stable across restarts and renames. It changes only by `fyr rotate-key` (0030 slice 5): the front then lists the host under the new id with `idWas` for that rotation, serves `/api/h/<idWas>/…` as the new id while it does, and the client moves what it keeps from `idWas` to `id` (2.5). A removed install's id never returns.
- A host that reports no id (a fyrd older than V0.124) is listed with `id: ""` and the reason "needs an update"; nothing can be addressed to it. Every install updates itself (0027), so this is a state of minutes.

### 2.2 The API shape: the host in the path

```
/api/h/<hostId>/<the host protocol's path after /api/>
```

`GET /api/h/F7QK-2M9X-AB34-CD56/threads/t_12ab/events?after=40` is `GET /api/threads/t_12ab/events?after=40` on that host. The front has **one handler** for the prefix: it authenticates the browser (0030 3.2), finds the host by id, strips the prefix and forwards the request with `Fyr-Actor` (0030 3.3) over that host's link. It does not read the body and knows no host route by name, so a route added to the host protocol needs no change in the front.

Why the path, and not a field or a header on every request:

1. **Not every request is a fetch.** An `<img src>`, an `<iframe src>`, a link opened in a new tab and an `EventSource` cannot carry a header or a body. A query parameter can, and that is today's `?host=`, which is lost where it matters most: a preview page's own relative URLs (F8). In the path, `/api/h/<id>/previews/<p>/app.js` resolves from `/api/h/<id>/previews/<p>/` by the browser's own rules. This removes `objs`.
2. **The front routes without parsing.** An upload streams through; a command's body is the host's business. The host leaves `args`.
3. **It cannot be left out.** A path without the prefix is not a host route; there is nothing to default. With a field, a call that forgets it still has a valid shape.
4. **One shape.** Reads, commands, uploads, previews and files are addressed alike, so the client has one URL function and the tests one rule: no request outside `/api/h/` and `/api/front/`.
5. **It stays under `/api/`,** so the service worker's rule (never cache `/api/`), the browser guard and `X-Fyr` hold as they are.

Answers:

| Case | Answer |
|---|---|
| the id is not one of the front's hosts | 404 `{ok: false, error: "this front follows no host <id>", code: "no-host"}`; never another host |
| the host is known and cannot be reached | 503 `{ok: false, code: "host-offline", link: <hosts[].link>}`, with the reason from the link (2.7) |
| a read the front keeps (`state`, `threads/{id}`, the six settings reads) while the host is offline | 200 from the cache with `X-Fyr-Read-At` (V0.127's rule, now for `state` too) |
| the host answers | its answer, unchanged |

**What the front may still do for a host.** It keeps the last state and the settings reads of each host (so an offline host's threads show), it applies the push plan to each host's frames (V0.54), and it forwards the focus (V0.85). It does these the same way for every host; `isLocal`, `byName("")`, `owner()`'s fallback, `target()`, `objs` and `gather()` are removed.

**The host protocol does not change.** A host still serves `/api/…` to a front and never sees the prefix. So this decision changes the front and the client only, and a new front works with a host of the last release.

### 2.3 What belongs to no host: `/api/front/…`

| Route | What |
|---|---|
| `GET /api/front/state` | `{now, user, buildId, front: {id, name}, hosts: [...], projectOrder}`. `hosts[]` is in the owner's order (V0.132) and lists every host the front follows, the one in its own process included: `{id, idWas?, name, machine, online, since, link, found, url, deploy}` |
| `GET /api/front/stream` | the one stream (2.4) |
| `POST /api/front/command` | the front's commands: `front.projectOrder {refs}`, `front.hostOrder {ids}`, `front.hostId {host}` |
| `/api/front/push/key`, `…/subscribe`, `…/unsubscribe`, `…/status`, `…/test` | this front's devices (V0.54) |
| `POST /api/front/presence` | `{host, threadId, device}` |
| `/api/front/auth/*` | Settings → Devices (0030 3.4) |
| `/api/front/peer/*` | Settings → Hosts: the roster, the invite (0030 3.6) |

The rule for what goes here: it is about the device, the session, or the set of hosts. Everything about a thread, a project, a change, a folder, a provider or an update is a host's. The owner's host order and project order are the owner's, kept as V0.132 and V0.121 keep them; they are set through the front because they span hosts. `boardSeenAt` is per host (F13): the board marks a card new against its own host's time.

### 2.4 The stream: one, with the host on every frame

One `EventSource` on `/api/front/stream`. The event names stay (`thread`, `thread.patch`, `event`, `project`, `deploy`, `stall`, `hostOrder`); each frame's data gains `host: <id>`. The front's own frames are `hello` (with `buildId`), `hosts` and `projectOrder`.

Why one stream and not one per host:

1. **Connections.** On loopback the client speaks HTTP/1.1, where a browser allows six connections per origin. Three hosts and two tabs would use all six for streams and leave none for requests.
2. **The front follows every host already** (for the cache and for push), so a stream per host would be the same frames through the same hub, split again.
3. **One connection state.** "Live", "connecting" and "offline" describe the browser's link to its front. Whether a host is reachable is a fact in `hosts[]`, which arrives on this stream.

Start: the client opens the stream, and on `hello` reads `/api/front/state`, then `/api/h/<id>/state?since=<that host's rev>` for each host, in parallel (subscribe first, then the snapshots, as today). A host's state is the host's own shape. The front no longer stamps threads with `host` and `hostOffline` and no longer raises revs: a thread's host is the host whose state or frame it came in, and "offline, read-only" is read from `hosts[]`. A host that joins later arrives as a `hosts` frame, and the client reads its state then.

Order holds per host (one host's frames keep their order); nothing orders frames across hosts, and nothing needs it.

### 2.5 Ids, and what the client keeps

A record is named by a **ref**: the pair (host id, record id), written `<hostId>/<recordId>` where one string is needed (`F7QK-2M9X-AB34-CD56/t_12ab34cd56ef7890`). Hosts keep minting ids as they do (`t_`, `p_`, `c_`, `d_` and 64 random bits); nothing has to be unique across hosts, because nothing is looked up across hosts.

- **Inside a host's records,** an id that points at another record (a thread's project, a change's threads, "from <thread>") stays a bare id and means "on the same host".
- **What spans hosts carries refs:** the project order (`front.projectOrder {refs}`), a deep link (`/?open=<hostId>/<threadId>`), the push payload and its tap (`{host, threadId}`), presence.
- **The store** is `hosts: Record<hostId, HostState>` with `HostState = {info, loaded, threads, projects, profiles, actions, deploy, stall, load, quota, boardSeenAt}`, and `events` and `resumeFrom` keyed by ref. The inbox, the board, the project list and the filters are selectors over all hosts, ordered as today. Actions and profiles are read from the record's host; there is no merged list.
- **The device's cache** (IndexedDB) holds one state and one `since` per host id.
- **`localStorage`** keys that name a record use the ref (`fyr.draft.<hostId>/<threadId>`); `fyr.hostsOff` is a list of ids; `fyr.newDefaults` and `fyr.lastProject` are per host.
- **Rotation:** on `idWas`, the client moves the host's state, cache and keys from the old id to the new one.

These live per origin, as browser storage does: a draft typed through the laptop's front is not on the same phone through the server's front. That is true today, and this decision does not change it (see the open questions).

### 2.6 The one place that knows the front

`/api/front/state` names the front: `front: {id, name}`. The client may use it for exactly three things:

1. **Settings → This device** says where this browser is connected ("Connected through desk"), beside how it gets in (V0.125).
2. **The connection banner.** While the host with `front.id` runs an update, the banner says the connection will drop and come back (V0.39). The update itself is that host's `deploy`, read like any host's.
3. **The build check** (`buildId`, V0.13) reloads a client older than the front that serves it.

Nothing else reads it. "(this install)" in the host pickers, "This install" as a group line in Settings, and the front's name in the rail and the phone header go; a host is shown by its name and mark (V0.170) in the owner's order, the same through every front. Settings → Hosts shows each host's link from `hosts[].link`, so the route "this machine" becomes the link's `via: "pipe"`, a fact about the link and not a special case in the client. With one host the host is still not shown on rows and screens (V0.54); that rule counts hosts and does not ask which one is the front's.

A test holds this: no module outside `settings-ui.tsx` (This device), `deploy-ui.tsx` and `buildid.ts` imports the front's identity.

### 2.7 Auth: who acts, and where

This composes with 0030 and changes nothing in it.

- **Browser → front.** Every `/api/h/…` and `/api/front/…` request is authenticated by the listener's authenticator and the session (0030 3.2, 3.4), with `X-Fyr` on writes. The host id in the path is an address, not a credential: it gives no right.
- **Front → host.** The front forwards with `Fyr-Actor` (who: the device and the owner, `front:` this install's id) and `Fyr-Origin`; it strips whatever the browser sent (0030 3.3). The browser cannot set `Fyr-Actor`. `Fyr-Actor` says who acts; the path says where.
- **The transport** is the link manager's `transport(hostId)` of Amendment 1: one function per member install id, the same call for the front's own install (the in-process pipe) and for a link either side opened. The front never relays through a third install: `/api/h/<id>/…` goes over the link to `<id>` or fails.
- **The host decides.** It checks the pinned link and the actor per request as today (0030 3.7). A front that names a host it is not paired with has no link to it, so the answer is 404.
- **Reachability** is `hosts[].link` as slice 5b defines it: `{state: "up"|"down", via: "peer"|"whois"|"pipe", opened: "here"|"them", since, error?: {kind: "acl"|"no-route"|"key-mismatch"|"refused"|"timeout"|"removed"|"revoked", text}}`. The client shows a host's words from it and puts the same object in a 503.
- **Previews.** A preview is a sandboxed frame from `/api/h/<id>/previews/<p>/`, the same origin and the same sandbox as today. The prefix gives the page nothing: it can reach its own files by relative URLs and nothing else it could not reach before.
- **A revoked session** closes `/api/front/stream` (0030 3.4), which is the only stream.

### 2.8 Old clients and old routes

The client is served by the front it talks to, so the two are always of one build, except for a tab that was open across an update or a shell the service worker kept: those reload on the first `hello` whose `buildId` differs (V0.13). To carry such a tab until it reloads, and anything that scripts the old paths:

- The old routes stay for **one release** after the client stops using them, with their behaviour of today, and each answer carries `Deprecation: true`. The front counts their use (`oldRouteUses`, as it counts `whoisLinkUses`).
- The next release removes them, together with the fallbacks (`byName("")`, `owner()`, `target()`, `objs`, `gather()`, `args.host`). From then an old path is 404.
- An old deep link and an old push tap (`?open=<threadId>`) keep working by C12's rule.

### 2.9 What does not change

The host protocol; MCP (agents talk to their host); the roster, pairing and sessions; the owner's host order and project order and where they are kept; the push plan; the front's cache of an offline host; one uplink per client (0025).

### Open questions for the owner

| # | Question | Recommended |
|---|---|---|
| Q1 | The host in the path (`/api/h/<id>/…`), or a host field or header on every request? | the path (2.2) |
| Q2 | One stream with the host on every frame, or one stream per host? | one stream (2.4) |
| Q3 | "(this install)" and "This install" go from the pickers and Settings groups, and the rail and phone header stop naming the front; only Settings → This device says where the browser is connected. Or keep a small mark on the connected host everywhere? | they go (2.6) |
| Q4 | What a device remembers (drafts, the filter rows, New thread's defaults) lives per origin, so it differs between fronts on one phone. Leave that, or move the owner's view choices (filters, defaults) to the owner's settings on the hosts, as the host order is? | leave it in this work; a follow-up decision if it is wanted |

---

## Part 3. Tests

Red first, in the slice that makes them green.

1. **The neutrality test** (the acceptance test of the whole): two scratch fyrds A and B in temp dirs, role both, no `FYR_*` in the environment, paired, each following the other. One script of UI flows runs through A's front, then through B's front, with fresh data each time: list the hosts; make a project on A and one on B; start a thread on each; send, with an attachment; open a preview whose page loads its own file; read events, the Changes tab and a settings page of each host; reorder the hosts; restart the front in the middle and load the preview's file again. The results (what the hosts hold, and what the client shows, with `front` and the links left out) are equal for both runs.
2. **No default host** (Go, a table over every route of the front's mux): a request without `/api/h/<id>/` or `/api/front/` is 404 once the old routes are gone, and before that is counted in `oldRouteUses`; an unknown id is 404 `no-host` and reaches no host (each fake host counts its requests).
3. **The same id on two hosts** (Go and web): threads and projects with equal ids on A and B stay two records in the store, the cache, the drafts and the stream; a command reaches only the named host.
4. **The hop:** every forwarded request carries `Fyr-Actor` with the session's principal and no header of the browser's (0030's hop test, run over the prefix).
5. **Offline:** `state`, a thread and a settings read of an offline host come from the cache with `X-Fyr-Read-At`; a command is 503 `host-offline` with the link; nothing goes to another host.
6. **Rotation:** a host that comes back with a new id and `idWas`: the client moves state, cache and keys; `/api/h/<idWas>/…` still answers for that rotation.
7. **Web unit tests:** the URL function (every `Api` method, the host required by the type); the store's selectors over hosts; the storage migration (4.2); a guard that fails on `"/api/` in any module but `api.ts` and on the front's identity outside the three modules of 2.6.
8. **Layout e2e,** desktop and phone: the mock serves two hosts with one thread id in common; with one host no host is shown; the pickers and Settings groups have no "this install".

## Part 4. Migration

### 4.1 Order

Each slice leaves a working install, and installs update themselves one by one. A front and its client change together; hosts need no change (2.2), so a front of the new release follows hosts of the old one.

### 4.2 The device's storage, once, on the first load of the new client

After the first `/api/front/state`: `fyr.hostsOff` names → ids by `hosts[].name` (a name that matches none is dropped); `fyr.draft.<threadId>` → `fyr.draft.<hostId>/<threadId>` when exactly one host has the thread (kept under the old key until then, read as a fallback for one release); `fyr.projectFilter`, `fyr.lastProject`, `fyr.newDefaults`, `fyr.view.collapsed` the same way; the IndexedDB state is dropped and read again (it is a cache). A version key (`fyr.storage = 2`) marks it done.

### 4.3 The front's own data

Host pins, the cache files and the stored host order move from names to ids when the front first sees each host's id (the pin gives it). A cache file without a known id is kept under its name until its host is seen.

## Part 5. Slices

One Sonnet thread builds them in order, one PR per slice, desktop and phone together in each.

| Slice | What | Acceptance tests (red first) |
|---|---|---|
| **1. The front speaks both** | `/api/h/<id>/…` (one handler, the answers of 2.2, the cache rule for `state`, a thread and the settings reads) and `/api/front/…` (state, stream with `host` on frames, command, push, presence, auth, peer); hosts by id inside the front (4.3); the old routes unchanged, counted. The harness of test 1 with its script driving the API | tests 2 (the first half), 3 (Go), 4, 5; the script of test 1 at the API level through A and through B |
| **2. The client names the host** | `api.ts`: every method takes the host id, one URL function, no path outside it (C1–C5); the stream and the per-host snapshots; the store, the cache and the selectors per host (C6–C8, C13); the mock with two hosts | tests 3 (web), 7; the unit and layout suites |
| **3. Refs outside the store** | `localStorage` keys and their migration (C11, 4.2); deep links, Copy link, the push payload and tap, presence (C12, F14, F15); `front.projectOrder {refs}`, `front.hostOrder {ids}`; rotation (`idWas`) | tests 6, 7 (migration); a push tap opens the thread on the right host with one id on two hosts |
| **4. The client forgets the front** | 2.6: `front` used in three modules; the labels and fallbacks of C8–C10 go; Settings → This device; the banner from the front's host's `deploy` | test 1 whole, through the browser on A and on B; test 8 |
| **5. The old routes end** (one release after slice 4 is live) | the old routes, `args.host`, `byName("")`, `owner()`, `target()`, `objs`, `gather()`, `isLocal` and the storage fallbacks are removed; `docs/API-v0.md` describes the two prefixes | test 2 whole; `oldRouteUses` is gone |

## Consequences

- `docs/API-v0.md` gains "The front's API" (the two prefixes); the host protocol's section stays.
- V0.127's "a call meant for one host never goes to another" becomes true of every call, because no call can leave the host out.
- V0.132's "its first host is the default for a call that names none" ends: the first host is only the default *choice* in a picker, which the client then names.
- The front loses the merged state, the rev raising, `objs` and the gathered reads; it gains one handler.
- A third kind of client (a CLI, a native app) gets the same API: sign in, read the hosts, name the host.
