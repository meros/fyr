# 0030 Every hop names who acts: one authenticator per listener, an explicit principal across every proxy, keys between installs, sessions for browsers, and tests for every way in

Date: 2026-11-17 · Status: **accepted** (owner, 17 Nov: "Bygg klart"); amended 17 Nov (Amendment 1: one two-way link per pair); the owner answered Q1–Q4 on 17 Nov (see "The owner's answers") · Supersedes the overlapping parts of 0025 ((1) proof, (2) following), 0026 ((3) pairing by PAKE and the roster's rules, (4), (5) browser authentication, the slices) and 0028 ((1), (3), (4), (7), the slices); what it keeps from them is listed under "What stands" · Builds on 0016, 0021, 0027 · Spec rows: one per slice, numbered when the slice's PR opens

**The owner's words, 17 Nov:** "Start a Fable review, a clean-room redesign and then a Sonnet implementation of the whole authentication chain. It should be easy to set up on a tailnet, but work on any LAN. Clients must be able to authenticate either automatically or via some form of pairing/token. Hosts must be able to find each other on a LAN, with automatic pairing under certain safe circumstances." And: "the whole thing must have tests too".

**How this record was made.** Part 1 is a review of the code at `3f83c69`. Part 2 compares two designs: one written by a subagent that was given only the requirements and the threat model (no code, no decisions), and one written after the review. Part 3 is the decision, part 4 the test plan, part 5 the migration, part 6 the slices.

---

## Part 1. Review: every way in today

Three decisions (0025, 0026, 0028) describe the chain. Of them, the code has: discovery over the tailnet (V0.123), an install key that authenticates nothing yet (V0.124), the MCP token in a file and the in-process link (V0.129), and sessions in **observe** mode: a cookie is minted and never required (V0.130). Everything that decides who may act is therefore still the V0.14 and V0.51 guards.

| # | Way in | Who uses it | What it trusts today | Weakness | Setup today |
|---|---|---|---|---|---|
| A | Loopback TCP `127.0.0.1:7777`, the browser API | the laptop's browser, `fyr-app` | **a loopback `Host` header**, plus `X-Fyr` on POSTs. The session cookie is looked up and ignored | every local process, every local user and every host-networked container is the owner (0028's Plex finding, still open) | none |
| B | The same port with `Tailscale-User-Login` and a `*.ts.net` Host | nobody legitimate since V0.130 | the header, when the Host ends in `.ts.net` | both are typed by any local process. Observe mode logs it and serves it | none |
| C | `serve.sock` | the phone through Tailscale Serve | the header on that socket. The Host is overwritten with the public name (#226) | the socket's peer is not checked (file mode only). The node is not looked up, so a device cannot be told from another of the same login, and cannot be blocked | NixOS: one option. Mac: by hand. A tagged node also needs `ownerLogin` |
| D | Front → its own host, in-process (`both`) | the front's proxy and its own stream | **the host runs the browser guard again** on what the front forwards: the browser's Host and the browser's `Tailscale-User-Login` | the host decides from headers a browser typed, relayed by a proxy. The mark "came through Serve" is a context value and does not cross the hop: **#225 and #226**. The fix made the front rewrite the Host so that a second Host check passes. The host never learns which device acts | none |
| E | Front → a remote host, plain HTTP on `100.x:7778` | other installs' fronts | tailscaled whois of the connection = the owner's login | (1) whois names a *node*, not a program: **any app on the owner's phone and any process on any of the owner's untagged nodes is the owner here**. (2) No Host or Origin check: a web page in any browser on such a node reaches it by DNS rebinding, and can set `X-Fyr`. (3) A tagged node has no login: **meros-server's front cannot follow the laptop**. (4) The front forwards the browser's `Cookie` (the session) to every host. (5) No encryption or key check of its own | per host: `hostPort`, a firewall rule, a static `hosts` entry when discovery misses it |
| F | `hostListen` on loopback (development) | a second install on one machine | the loopback guard, as A | as A | config |
| G | `/mcp` on the main port **and on the peer listener** | each agent's CLI | a per-session bearer, in a 0600 file since V0.129; `Origin` refused | the token is accepted from the tailnet listener too. 0028 (4) said to remove it; not done | none |
| H | `owner.sock` | `fyr open` | the peer's UID (SO_PEERCRED, LOCAL_PEERCRED) | sound. It serves one call only, so pairing, revocation and recovery have no local channel yet | none |
| I | `runner.sock` | the launcher's fyrd | file mode and the peer's pid (V0.131) | sound | none |
| J | Discovery | fronts | tailscaled's status (online, untagged, the same user id), then `GET https://<MagicDNS>:7443/.well-known/fyr` through the peer's Serve, answered behind the guard with the owner's login in it | tagged nodes are invisible in both directions. The answer's `id` is pinned but **a changed id is only shown; the front keeps following** (V0.124 (3)). Nothing off the tailnet | Serve on every install that should be found |
| K | Releases | the updater | an ed25519 signature by the release key (0027) | sound, and separate from identity. A release could still lock the owner out by changing a default (0028 planned enforce as "the next release's default") | none |

### What the incidents show

| Incident | Cause in the table | The class |
|---|---|---|
| **#225**: tailscaled passes no browser Host on `serve.sock`; the guard refused the owner | C: identity was derived from two headers that had to agree, and one of them is not what the code assumed | trust read from the Host header |
| **#226**: live view and uploads gave 403 after #225 | D: the host re-derives identity from forwarded browser headers, and the Serve mark is lost across the hop | trust kept in a request context, and a second guard behind a proxy |
| **The phone could not connect; the owner hand-built a fix** | C and J: the phone's only door is Serve on one install, with a guard that reads Host; there is no second way in (a code, a link) to fall back on | one way in, no pairing |
| **meros-server is tagged** | C, E, J: every tailnet check asks for a user login. A tagged node has none, so it needs `ownerLogin` by hand, cannot follow another host, and is not found | identity taken from the network |
| **The Mac must see the laptop** | J and E: needs Serve on both, a host port on both, and a whois that happens to work | discovery and trust are the same mechanism |

### The findings, in order of weight

1. **A host cannot tell who acts.** On D and E the host sees "a front" or "a loopback Host". No hop carries a device or an install as a value. Every later feature (revoke, audit, step-up, drive lists) needs it.
2. **Guards run behind proxies.** The host's guard checks a browser's headers after a proxy has rewritten them. Each fix so far taught the front to forge what the host wants to see.
3. **E is wider than it reads.** "The owner's tailnet login" means every app and every browser tab on every untagged device of the owner. It is plain HTTP with no Host check.
4. **Observe mode has no end.** A, B and F are open until a release flips a default, and that flip is itself a lockout risk.
5. **The install key is decoration.** A changed id does not stop a link.
6. **The session cookie travels to hosts.**
7. **There is one way in per client.** No pairing exists for a browser or a host, so any break in the single path is an outage.

---

## Part 2. Two designs compared

The clean-room design and the review-based design agree on the shape, which is the best evidence for it: network position grants nothing; the tailnet only enrols; installs talk over mutual TLS pinned to install keys on every network; the proxy takes a typed principal and sends it as one header inside the pinned channel; identity headers are read on `serve.sock` only; tailnet auto-pairing is anchored in each node's Serve endpoint; tagged nodes join by a code.

Where they differ, the stronger choice and why:

| Topic | Clean-room | Review-based | Kept | Why |
|---|---|---|---|---|
| Where device records live | in the roster: every host knows every device id and checks revocation itself | per front | **clean-room** | a lost phone is revoked from any install, and a host refuses a revoked device even when a stale front still forwards it |
| The actor header | plain header inside mTLS; no signed assertion | the same | **both** | the signer would be the channel's own key. A signature adds a replay cache and a clock and stops nothing a compromised front can do |
| Rebinding and CSRF | `Origin` and `Sec-Fetch-Site`, Host only as depth | exact Host allowlist and `Origin` | **clean-room, with the Host allowlist kept on TCP** | Serve's socket has no useful Host (#225); `Origin` is what a page cannot forge |
| Host pairing code | an invite `fyr1.<id>.<secret>` and a 6-digit commit-and-reveal code | one code that carries the install id and a secret; no short code | **the invite only (Q1)** | the invite needs no custom cryptography: the joiner pins the id, then sends the secret inside the pinned channel. 0026's CPace has no standard-library implementation |
| Agent tokens | a Unix socket (`agent.sock`), never TCP | the loopback bearer of V0.129, removed from the peer listener | **review-based now; the socket is named for later** | the CLI's MCP client speaks HTTP to a URL; a socket needs a stdio bridge in every session. The token is not a boundary against a same-UID process either way (0028 Q3) |
| Cookie name | `__Host-fyr` | keep `fyr_session` | **review-based** | renaming signs every device out; the migration must not |
| Enforce | per rule, after 7 clean days, the owner confirms | stored state, never a release default; ready after 3 clean days | **stored state, per way in; automatic after 3 clean days (Q2)** | a release must never change who may act (finding 4) |
| A front that follows hosts through a third | forbidden: a request with an actor header is never proxied on | not addressed | **clean-room** | no chains, so one hop is the whole proof |
| Offline revocation | sync the roster with other members before honouring a peer | tombstones win; sync first on every connection | **sync first on every connection**, the 10-minute rule dropped | one rule that is easy to test; the residual is stated in the threat table |
| Revoked signers | adds signed by a revoked key are void; the revoker re-signs survivors | undecided | **clean-room** | it is the only rule that gives every host the same roster regardless of the order statements arrive in |
| The owner's login on a tagged node | carried by the roster | config `ownerLogin`, or learned from the first owner-socket link redeemed through Serve | **clean-room, with config still winning** | meros-server needs no hand-set option after it pairs |
| A test harness as slice 0 | a decision log first | a fake tailscaled, a process harness and today's behaviour pinned first | **review-based** | the owner's second message; and the migration test needs today's state as a fixture before anything changes |

---

## Part 3. Decision

### 3.1 Principals and credentials

| Principal | Credential | Kept where | Ends |
|---|---|---|---|
| **Install** | the Ed25519 install key of V0.124; id = first 80 bits of SHA-256, `F7QK-2M9X-AB34-CD56` | `<data>/identity.key`, 0600 | rotated or removed |
| **Device** (a browser or PWA) | the `fyr_session` cookie of V0.130 (32 random bytes; HttpOnly, SameSite=Strict, Secure off loopback) | the hash on the front that issued it; the device *record* `{id, name, how, by, node?}` in the roster | 90 days unseen, or revoked |
| **Local owner** | none: a Unix socket and the peer's UID | `<data>/owner.sock`, 0600 | per connection |
| **Agent session** | the bearer of V0.12 / V0.129 | a 0600 file while the CLI starts; fyrd keeps the hash | the session ends |
| **Tailnet node** | not a principal. It is *evidence* that can enrol a device (3.4) or pair an install (3.6) | tailscaled | |

There is no owner root key and no CA for installs: one person loses an offline key, and a handful of pinned keys need no chain.

### 3.2 One authenticator per listener

A listener is made with exactly one way to name its caller. The result is a value, `authn.Principal{Kind, ID, How, Front}`, and a handler that needs the owner takes it as an argument. Nothing reads identity from `Host`, and no listener reads another listener's headers.

| Listener | Authenticator | Yields | Also serves without a principal |
|---|---|---|---|
| `owner.sock` | peer UID is fyrd's or 0 | `local` | nothing |
| `serve.sock` | peer UID is 0 (tailscaled); then the session cookie; else enrolment by `Tailscale-User-Login` (3.4). `Host` is ignored; the origin is the listener's configured public name | `device` | the client's files, `/auth/*` |
| Loopback TCP (`fyr.localhost:7777`) | the session cookie only. `Tailscale-*` and `Fyr-Actor` headers are refused (400) | `device` | the client's files, `/auth/*`, `/healthz`, `/.well-known/fyr` (`{fyr: 1, id}`), `/mcp` (bearer → `agent`) |
| Browser TLS (`tls: {cert, key}`, off by default) | the session cookie only | `device` | the client's files, `/auth/*` |
| **Peer** (TLS 1.3, ALPN `fyr-peer/1`, any interface) | the client certificate's key is a live roster member | `install` | `/.well-known/fyr` (`{fyr: 1, id}`) and `/pair` while an invite is live |
| In-process pipe (`both`) | the listener itself: only this process can dial it | `install` (self) | nothing |
| `runner.sock` | V0.131, unchanged | | |

Every browser-facing listener also applies, before the cookie is looked at: `Origin` must be one of this front's own origins on every non-GET and every stream; `Sec-Fetch-Site` must be `same-origin` or absent on credentialed GETs; `X-Fyr` stays; on TCP the `Host` must be one of the listener's own names (defence in depth, not identity).

### 3.3 The hop: a front cannot forward without a principal

- `front.forward(p authn.Principal, …)` has no form without `p`. A request with no session has nothing to put in the header, so it is refused at the front (401) and never reaches a host.
- The front builds the outgoing request. It **drops** `Cookie`, `Authorization`, every `Tailscale-*` and `X-Forwarded-*` header and the browser's `Host`, and **sets**:
  - `Fyr-Actor: v=1; kind=device|local|install; id=<device id | "owner" | install id>; how=local|tailnet|code|link`;
  - `Fyr-Origin: <the public origin the browser used>`, from the listener's configuration, for a preview's CSP (V0.47).
- The host reads `Fyr-Actor` **only** on the peer listener and the pipe, where the channel has already proved which install speaks. It checks, per request: the install is a live roster member; the install may drive this host (3.7) when the call is a command; the header is present and well-formed (else 401, never a default); a `device` id is in the host's own roster and not revoked. The host runs no browser guard on these listeners.
- A request that arrives with `Fyr-Actor` is never proxied on. A front talks to each host directly.
- Every owner `Input` records `actor` (device or install) and `front`, which is the audit trail of 0028 (7).
- It is not signed: the channel's key would be the signer (Part 2).

This removes the class behind #225 and #226: there is no second guess at the browser's identity behind the proxy, and nothing to lose in a context.

### 3.4 Browsers: a session, and three ways to get one

| Way | Check | Result |
|---|---|---|
| **Tailnet** (automatic) | on `serve.sock`: `Tailscale-User-Login` equals the roster's owner login (config `ownerLogin` wins; else this node's own user; a tagged node has none until it pairs or the option is set). The node comes from tailscaled whois of Serve's `X-Forwarded-For`: untagged, not shared in, not blocked | a node already approved gets a session silently. A new node gets a **pending** session (0028 Q1). The one exception (Q4): while the install has no device at all, the first such node is approved at once |
| **Link or QR** | `fyr open` (60 s) or `fyr device add` (10 min, prints a QR in the terminal) over `owner.sock`, or **Add a device** on a signed-in device: `<origin>/auth/once#<128-bit token>`, single use, posted from the fragment | a session |
| **Approval code** | any browser without a session sees "This device is not signed in" with an 8-character code (40 bits, 5 min). The owner types it on a signed-in device (Settings → Devices → Approve) or runs `fyr device approve <code>`. 5 wrong tries end the code; 10 tries a minute per front. This is also how a pending tailnet node is allowed, and how an installed iOS PWA signs in (it does not share Safari's cookies) | a session |

- Approving and adding are trust-widening: they need a session older than the one being approved, or `owner.sock` (0028's step-up, kept).
- **Revoke** (any signed-in device on any install, or `fyr device revoke`) writes a roster tombstone. The issuing front deletes the session and the push subscription and closes the device's streams; every host refuses the id in `Fyr-Actor`; a tailnet device's node is blocked so it cannot enrol again; the devices it added are revoked with it (0028's cascade). Revoking needs no step-up.
- Settings → **Devices** on desktop and phone (one shared component): every device of every install, how it got in, last used, Revoke, and the pending ones with Allow and Deny.

### 3.5 Browsers on a LAN, honestly

A browser cannot pin a key, and a service worker, an installed PWA and Web Push need a certificate the device already trusts. A warning that was clicked through gives a page with no service worker and a session an attacker on the LAN can take.

| Device | Path | What works |
|---|---|---|
| A desktop that runs fyrd | its own front at `http://fyr.localhost:7777`; that front reaches the other installs over the peer link | everything. **This is the LAN answer for computers** |
| A phone or tablet, with Tailscale | Serve's `*.ts.net` certificate | everything, at home and away. **Recommended** |
| A phone, no Tailscale, own certificate | config `tls: {cert, key}` (ACME DNS-01 on the owner's domain, or the owner's CA); sign-in by QR or approval code | everything; push also needs the internet |
| A phone, no Tailscale, no certificate | **nothing is served.** fyrd offers no plain HTTP and no self-signed browser page off loopback | the page says which of the rows above to use |

A built-in, name-constrained local CA is not built (Q3).

### 3.6 Installs: discovery proposes, a roster decides

**Discovery** yields `(id?, address)` candidates and never trust:
- the tailnet: every peer in tailscaled's status, **tagged ones included**, probed at its Serve name and at the peer port;
- mDNS `_fyr._tcp.local`: instance `fyr-<short id>`, TXT `v=1 id=<id>`, the peer port. No machine name, owner, build or tailnet name. `discovery.mdns: false` turns advertising off;
- by hand: Settings → Hosts → Add, or config `peers`;
- address hints in the roster (last LAN addresses, MagicDNS name), so a member is found at a new address with no press.

**The link** is TLS 1.3 with a self-signed certificate over the install key, both ways, no 0-RTT. The dialer requires the exact id it meant to reach; the listener requires a live roster member. Names, addresses, expiry and public CAs play no part. The tailnet is one route under it.

**The roster** is a grow-only set of statements signed by member installs: `install-add`, `install-remove`, `key-rotate`, `device-add`, `device-revoke`, `owner-login`. Merge is union; a removal wins for ever. Statements are exchanged first on every peer connection, on every change, and every 60 s. An `add` signed by a removed install is void unless a live member re-signed it; the remover re-signs the entries the owner keeps, in the same batch.

**An install joins the roster only one of these ways.** "Automatic" means no press; the owner still gets a notice and an audit entry.

| # | Condition | The exact check | Automatic |
|---|---|---|---|
| 1 | **The same tailnet user** | This node is untagged with user U. The peer is in tailscaled's status with the same user id, no tags, not shared in, key not expired. `GET https://<peer's MagicDNS name>:<Serve port>/.well-known/fyr` over ordinary WebPKI returns the peer's install id; the peer link is then dialed pinned to that id. The peer runs the same check on this node. Serve is set by root or the node's operator, so no other program on that node, and no phone app, can answer | yes |
| 2 | **A member vouches** | the `install-add` verifies against a live member of this install's roster | yes (it is roster sync) |
| 3 | **The owner's invite** | `fyr pair` (over `owner.sock`) or Settings → Hosts → Pair (step-up) shows `fyr1-<install id>-<10 characters>`, as text, a QR and a link; 10 min, single use, 5 wrong tries end it. The other side (`fyr pair <code>`, or its Settings) pins the id from the code, dials, and sends the secret inside the pinned channel; the inviter learns the joiner's key from that channel. Discovery supplies the address | no: one action |

**Never:** the same LAN or subnet, an mDNS answer, a name, first contact, the same tailnet with another user, or a tag. **A tagged node** (meros-server) never uses condition 1 in either direction, since a tag says who may reach a node and not whose it is. It pairs once by 3 (`ssh meros-server fyr pair`, paste the code on the laptop) and is then known to every other install by 2.

**Removal and rotation.** Settings → Hosts → Remove, or `fyr host remove`, on any member: a tombstone; live links to it close; it can sign nothing. `fyr rotate-key`: `key-rotate` signed by the old key, presented in the handshake so a peer that was offline follows it. A key that may be stolen is removed and paired again, not rotated.

### 3.7 What a paired install may do

Kept from 0028 (4), Q2: every member reads every host; a member **drives** a host unless the host's owner switched that member off there. The switch is local to the host and never part of the roster, so no member can grant itself. The blast radius, stated: an install that drives a host is the owner on it. A compromised install can assert any live device id, add installs and revoke others; removing it from any other member ends that.

### 3.8 Agents and local processes

- `/mcp` answers only on the loopback listener, with the bearer; never on the peer listener (finding G). An agent principal is never forwarded to another host. The token dies with the session.
- `owner.sock` becomes the whole local channel: `fyr open`, `fyr device add|approve|revoke|list`, `fyr pair [code]`, `fyr host remove`, `fyr rotate-key`, `fyr auth status|observe|enforce`. No mode ever gates it: it is the recovery path.
- 0028 Q3 stands and is repeated here: an agent with a shell runs as the owner's UID and can use `owner.sock`. The token limits what model output does through MCP, not what the process can do. A Unix `agent.sock` with a stdio bridge is named for the day agents are sandboxed.

### 3.9 Releases

The release key (0027) is in no roster and signs no identity; install keys sign no release. Two rules tie the updater to this record: **a release never changes who may act** (the mode of each way in is stored state, 5.1), and `fyrd selftest` loads the install key, the roster and the device table, so a build that cannot read them is not switched to.

### 3.10 Threats

| Threat | What stops it |
|---|---|
| A local process, another local user, a host-networked container | loopback TCP has no ambient trust; `owner.sock` and `serve.sock` check the peer's UID; no loopback host port |
| A forged `Tailscale-User-Login` | read on `serve.sock` only, whose peer must be root; 400 elsewhere |
| A web page: CSRF, cross-site streams, DNS rebinding | no listener grants anything without a cookie or a key; `Origin`, `Sec-Fetch-Site`, `X-Fyr`, SameSite=Strict; the peer port speaks only mutual TLS, which a page cannot |
| An app on the owner's phone, a process on another node of the owner | the peer port needs a roster key; condition 1 is answered only by the node's Serve |
| Another tailnet user, a shared-in node, a tagged node | the user id must match and the node be untagged for anything automatic; otherwise a code |
| A spoofed mDNS answer, ARP spoofing, a man in the middle on the LAN | a candidate is an address; the handshake must prove the id that was meant. An unknown id is "not paired" and needs the owner's invite, which carries the id |
| A guessed or replayed code | single use, short life, 5 tries; the invite's secret travels only inside a channel pinned to the id in the same code |
| A confused front | 3.3: no principal, no forward; the host refuses a missing or revoked actor |
| A stolen cookie, a lost phone | Revoke from any device of any install or from a shell; hosts refuse the id; the node is blocked; widening trust needs an older session |
| A lost or compromised install | Remove from any member. **Residual:** a host that only the removed install can reach learns of it when it next meets another member or the owner runs the removal there |
| A stolen agent token | one session, loopback only, MCP verbs only, dead with the session. **Residual:** 3.8 |
| Lockout | `owner.sock` is never gated; modes are stored and reverted with `fyr auth observe`; part 5 |

Out of scope: several owners, root on a trusted machine, the tailnet's control plane.

### What stands from the earlier records

0016 and 0021 (roles, dual by default, one owner per record, the client's one uplink). 0025 (3)–(5). 0026 (1) the key and id, (2) sources, (6) the tailnet as one route, and its browser-path table. 0028 (2) the owner socket, (5) step-up, (6) agents named for what they are, Q1–Q4. V0.129 and V0.130 as built.

### The owner's answers (17 Nov)

| Q | Asked | Recommended | Owner |
|---|---|---|---|
| Q1 | The host pairing code | the invite only (`fyr1-…`, text, QR, link); no custom cryptography | **the invite only**. No 6-digit code is built; 0026's CPace is dropped |
| Q2 | When a way in moves from observe to enforce | Fyr says "ready" after 3 clean days and the owner presses | **automatically after 3 clean days**, with a notice |
| Q3 | A built-in local CA for a phone on a LAN with no Tailscale | not now | **not now**: Tailscale or `tls: {cert, key}` |
| Q4 | The first tailnet device of a fresh install | let it in when it is the owner's login and the device list is empty | **let it in when the list is empty** |

---

## Part 4. Test plan

Tests are part of each slice: a slice's PR opens with its tests red in the first commit, and does not merge without them.

### 4.1 Harness (slice 0)

| Piece | What it is |
|---|---|
| `internal/authtest.Tailscaled` | a fake tailscaled: the LocalAPI on a Unix socket (`status`, `whois`, with users, tags, shared-in nodes, several tailnet users) and a **Serve emulator** that proxies to `serve.sock` exactly as tailscaled does (strips incoming `Tailscale-*`, sets them from the fake node, sends `Host: localhost`, sets `X-Forwarded-For`), plus an HTTPS front with a test CA for the WebPKI probe of condition 1. fyrd takes the socket path from config `tailscaleSocket` |
| `internal/authtest.Fyrd` | starts a real `fyrd` (built once per test binary) in a temp data folder with every `FYR_*` variable removed (#167, V0.131), on port 0, and waits for its ready line. Two and three of them make a front and hosts |
| `internal/authtest.LAN` | an in-memory mDNS bus behind the `discovery.Source` interface, so a test can add, spoof and withdraw answers; the installs themselves use real loopback TCP with the real pinned TLS. One test uses real multicast on loopback and is skipped where the sandbox has none |
| `internal/authtest.Live` | a fixture of today's state: an install key, `auth.devices` with a `local` and a `tailnet` session, observe mode, `serve.sock`, a static `hosts` entry and a whois link |
| `web/test/auth.e2e.mjs` | headless Chrome (as the layout harness) against real scratch fyrds and the fake tailscaled |
| CI | the Go job runs the integration tests with `go test ./...` (parallel, every wait a bounded poll on a condition, no fixed sleep); the web job runs `auth.e2e.mjs`. Budget: under 60 s for the Go integration tests |

### 4.2 Every way in

| Way in | Proof (positive) | Proof (negative) |
|---|---|---|
| `owner.sock` | this UID gets a one-time link, a device code, an invite | another UID is closed (unit, with a faked credential); works with every TCP listener down |
| `serve.sock` | the owner's login through the Serve emulator mints a session with `Host: localhost` (the #225 case) | a peer that is not root; another login; a tagged source; a shared-in node |
| Loopback TCP | a cookie gets the API | no cookie: 401 in enforce, served and counted in observe; `Tailscale-User-Login` or `Fyr-Actor`: 400 |
| Browser TLS | QR sign-in over a test certificate | plain HTTP off loopback serves nothing |
| Peer | two installs paired by each of conditions 1, 2, 3 follow each other | a key not in the roster fails the handshake; `/mcp` is 404; a removed member's open link closes |
| The hop | **every route of the front's mux** is requested through the Serve emulator and through loopback, against a recording host: each forwarded request carries `Fyr-Actor` for the session's device and `Fyr-Origin`, and none of `Cookie`, `Tailscale-*`, `X-Forwarded-*` or the browser's Host. Upload, read-back, a preview's CSP and a live view pass through Serve (the #226 case) | no session: 401 at the front and the host records no request; a host given no `Fyr-Actor`, a malformed one or a revoked device answers 401; a request with `Fyr-Actor` is not proxied on |
| `/mcp` | the session's token works on loopback | refused with `Origin`, on the peer listener, after the session ends |
| Releases | selftest passes on the live fixture | selftest fails on a broken key, roster or device table |

### 4.3 Every threat

| Threat | Test |
|---|---|
| DNS rebinding | a foreign `Host` and `Origin` on loopback and on browser TLS get nothing; the peer port answers no plain HTTP |
| CSRF and cross-site streams | a cross-origin POST, SSE and WebSocket are refused before the cookie is read |
| A spoofed `Tailscale-User-Login` | over loopback TCP: 400; on `serve.sock` from a non-root peer: closed |
| A spoofed mDNS candidate | the bus announces a paired id at an attacker's address: the handshake fails and the real address is kept; it announces an unknown id: shown "not paired", never followed |
| A replayed pairing code | an invite, a one-time link and an approval code each work once; a sixth wrong try ends them; an expired one is refused |
| A stolen cookie after revoke | the cookie gets 401 on its front; the same device id forwarded by another front gets 401 at the host; its open stream closes within 1 s; its tailnet node cannot enrol again |
| A tagged node | never paired by condition 1 in either direction; its Serve reads no login until the roster carries one; it pairs by the invite and is then vouched for |
| Another tailnet user | another user id, and a shared-in node of the same login, are neither enrolled nor paired |
| A phone app on an owner's node | a connection from an owner's node with an unknown key, and one that answers the peer port but not Serve, are not paired |
| A man in the middle at pairing | a relay with its own key between two installs cannot complete an invite |
| A compromised install | after Remove on a third member, its link and its statements are refused everywhere within one sync; a host it alone reaches refuses it once the tombstone is delivered there |
| Rotation | a rotated key is followed by a peer that was offline; the old key is refused |

### 4.4 Migration test

`TestMigrationKeepsTheOwnerSignedIn` starts from `authtest.Live` and applies every slice's state change in order, in one test that each slice extends. After each step it asserts: the `local` cookie and the `tailnet` cookie still get the API; the phone's path through the Serve emulator still works; the front still follows its host (over the old link, then the new); `fyr open` over `owner.sock` still mints a session; and after `enforce`, `fyr auth observe` restores the old ways in. The browser harness runs the same from a real cookie jar.

### 4.5 The browser harness

`auth.e2e.mjs`, desktop and phone sizes: sign-in by tailnet identity (approved node, and a pending one allowed from an older session); by QR link; by approval code; a revoked device lands on the signed-out page while its other tab's stream closes; Settings → Devices and Settings → Hosts (pair, a not-paired candidate, remove) on both fronts, as CLAUDE.md requires.

---

## Part 5. Migration: the owner is never locked out

### 5.1 Rules

1. **`owner.sock` is never gated.** `fyr open`, `fyr device add` and `fyr auth observe` work from any shell in every mode.
2. **Each way in has a stored mode**, `observe` or `enforce`, in the install's database: `browser`, `peer`. A release never changes one. In observe the new rule decides, counts what it would refuse (`fyr auth status`, Settings → Devices and Hosts) and serves it the old way.
3. **Ready** means 3 days with no would-refuse on that way in and at least one success of each credential in use. The install then moves that way in to enforce by itself (Q2), tells every device with a push and an in-app notice that names `fyr auth observe` as the way back, and writes an audit entry. A mode the owner set back by hand stays until the owner changes it.
4. **New beside old.** A new credential works for a full slice before the old one can be switched off, and the old listener is removed only in the slice after its mode reached enforce on every install.
5. **Nothing is re-issued.** Existing cookies, device records and the install key are used as they are.

### 5.2 From today's live state

| Today | Step | The owner does |
|---|---|---|
| The laptop's and the phone's V0.130 sessions | they become roster device records in place (slice 2); the phone's node is approved because it signed in during observe | nothing |
| `fyr-app` on `127.0.0.1:7777` without a cookie | counted as would-refuse until it runs `fyr open --app` | one line in `~/nixos-config` |
| laptop ↔ Mac (untagged, the same user) | condition 1 pairs them when both run slice 3; the front moves to the peer link and keeps the whois link as a fallback until `peer` is enforced | Serve on the Mac (the install script) |
| meros-server (tagged) | keeps today's one-way whois link and `ownerLogin` until it pairs: `ssh meros-server fyr pair`, paste on the laptop (slice 4). Then it follows the laptop too, and learns the owner's login from the roster | one code, once |
| the plain `100.x:7778` listener | from slice 1 it requires `Fyr-Actor` from fronts that send it and refuses `Origin` and foreign Hosts; removed in slice 5 | nothing |
| `browser` enforce | by itself when ready (rule 3), with a notice. Closes ways A, B and F | nothing |

---

## Part 6. Slices

One Sonnet thread builds them in order, one PR per slice, because every slice touches `internal/server`, `internal/front` and `cmd/fyrd`. Each PR: red tests first, `make test-go` and the web tests, the migration test extended, desktop and phone together.

| Slice | What | Acceptance tests (red first) | Lockout guard |
|---|---|---|---|
| **0. Harness, and today pinned** | 4.1 entire; config `tailscaleSocket`; no behaviour change | the fake tailscaled reproduces #225 (Host `localhost` on `serve.sock`) and #226 (upload and preview through Serve) against today's code and they pass; the live fixture loads; `TestMigrationKeepsTheOwnerSignedIn` step 0; `auth.e2e.mjs` signs in by tailnet identity; CI runs both | none needed |
| **1. The principal on every hop** | `internal/authn`; one authenticator per listener (3.2), still serving the old ways as `Principal{How: legacy}` in observe; `forward(p, …)`; `Fyr-Actor`, `Fyr-Origin`; the host stops guarding by Host on the pipe and the peer listener; headers stripped; `/mcp` off the peer listener; `Origin` and Host refusal on the whois listener; `Input.actor`; `serve.sock` checks its peer | the hop row of 4.2 over every route; `Fyr-Actor` on loopback is 400; a rebinding request to the whois listener gets nothing; `/mcp` on the peer listener is 404; the audit entry names the device | an old front (no `Fyr-Actor`) is still served by a new host and counted; the migration test passes with mixed versions |
| **2. Sessions decide; Devices** | stored modes and `fyr auth`; the `browser` way enforceable; whois of `X-Forwarded-For`, approved and blocked nodes, the pending session; links, QR and approval codes; `fyr device …`; device records in a local roster; Settings → Devices (desktop and phone); revoke with cascade; the `Origin` / `Sec-Fetch-Site` rules | the `serve.sock`, loopback and browser rows of 4.2; rebinding, CSRF, spoofed header, replayed code and stolen cookie rows of 4.3; the first node of an empty install is approved and the second waits (Q4); a way in enforces itself after 3 clean days on a fake clock and not after a would-refuse (Q2); e2e: tailnet, QR, code, revoked | enforce by rule 3 or by a signed-in device or `owner.sock`, never by a release; a mode set back by hand stays; `fyr auth observe` restores; the fixture's two cookies survive |
| **3. Roster and the pinned link; the tailnet pairs** | the peer listener; signed statements and sync; condition 1; fronts prefer the peer link and fall back; device statements spread, hosts check them; drive switch; discovery reads `{fyr, id}` | two scratch installs under one fake user pair with no press and follow each other over TLS; another user, a shared-in node, a tagged node and "peer port without Serve" do not; a device revoked on A is refused on C through B; a wrong key fails the handshake | the whois link stays and is counted; a host with no peer link yet is followed as before |
| **4. The invite; tagged nodes; Hosts** | `fyr pair`, the invite as text, QR and link; Settings → Hosts: Pair, Add, not-paired candidates (desktop and phone); tagged peers as candidates; the owner's login in the roster; vouching shown | three installs: A–B by invite, C learns B by vouching; the invite's replay, expiry, wrong tries and man-in-the-middle rows; a tagged install serves no tailnet browser until paired, then does | pairing only adds; nothing existing is refused |
| **5. Removal, rotation, and the old link ends** | `install-remove` with re-signing; `fyr rotate-key`; step-up on trust-widening changes everywhere; `peer` enforceable; the plain whois listener and loopback `hostListen` removed once every install enforces | the compromised-install and rotation rows; a removed member's adds are void unless re-signed; the whois listener is gone and the front still follows every host; selftest fails on a broken roster | `peer` enforces itself only when every followed host is on the pinned link (rule 3); `fyr auth observe` on each host |
| **6. The LAN** (spec row V0.175) | mDNS advertise and browse; address follows the key; the browser TLS listener with `tls: {cert, key}`; the "nothing is served" page text in the docs | the spoofed-mDNS rows; two installs with no tailscaled find each other on the bus and pair by invite; a paired id is followed at a new address; no plain HTTP off loopback; QR sign-in over the test certificate | off by default for browsers; nothing on the tailnet path changes |

## Amendment 1 (17 Nov): one two-way link per pair of installs

**The owner's words, 17 Nov:** "Can we not make the connections two-way? Any part can open? I want a robust mesh."

**Why.** Slices 3 to 5 made trust spread through the roster, but reachability did not: a front dials each host's peer port itself, so a link exists only in the direction the network allows. meros-server is `tag:server`; the owner's tailnet policy lets members reach the server and not the server reach members, so its front cannot see the laptop or the Mac even after pairing ("rejected due to acl"). A host behind a NAT or an ACL *can* dial out. The link must be usable the other way over a connection opened from the side that can open it.

**Review.** A Fable review (17 Nov) read this text against the code and found four blockers and six things to fix; they are folded in below and marked (B1…S6). One blocker was already in slice 5's code (a stranger's certificate could write to the roster) and is fixed there.

### A1.1 The link

1. **One link per pair of members.** A link is one TLS 1.3 connection with ALPN `fyr-peer/2`, certificates over the install keys both ways. Whichever install dialed it, **both** can send requests over it. The dialer is the HTTP/2 client of the connection and opens exactly one tunnel stream on it (`POST /link/tunnel`); inside that stream a second HTTP/2 session (unencrypted, "h2c") runs with the roles reversed: the acceptor is its client. The dialer's requests go on the outer session, the acceptor's on the inner one; both carry the host protocol, live streams and uploads as today. Both sessions are made with the standard library's `http.Transport.NewClientConn` (`Protocols` set to HTTP/2, the established connection handed over) and `http.Server.Serve` on a one-connection listener; there is **no pooled `http.Transport`** (it redials by itself and would make links the manager does not know about, B4) and no mux of our own. HTTP/2 streams are duplex; the stream adapter flushes after every write (SETTINGS and PING would stall otherwise). **A fallback if the tunnel proves fragile in the build: the dialer opens two connections, `fyr-peer/2` and a role-reversed `fyr-peer/2r`, closed together.**
2. **A `/2` connection exists only for a member (B2).** The listener closes a `fyr-peer/2` connection straight after the handshake unless its key is a live member or a *verified* rotation of one. A stranger still reaches HTTP/1.1 only: `/.well-known/fyr` and `/pair/tailnet`, `/pair/invite` stay on `fyr-peer/1`. One tunnel per connection; `/link/tunnel` is refused on `/1` and inside the inner session; the HTTP/2 settings are bounded (concurrent streams, header size) and every connection has an idle timeout and a count limit, the peer `http.Server`'s too.
3. **Identity of a request on the inner session (B3).** The inner server's connection carries the key the **outer handshake proved** (the real key on the certificate, not the id that was wanted: a rotated peer differs), fixed for the link's life; it serves only the host's `LinkHandler` and `/roster`. Requests on both sessions are authorised exactly as on `/1`: member, `Fyr-Actor`, revoked and (in `peer` enforce) unknown devices, the drive switch. The link makes two more rules of 3.3 explicit: `kind=local` is refused on a link, and `kind=install` must carry the install id the link proved (S2).

### A1.2 Either side opens it

4. **Both sides dial.** Every install keeps a connector for each *member* (membership decides who is linked, followed and synced; a member's address only decides who is dialled, S3, so a member with no peer port that dials in is shown and synced too). It dials with a jittered backoff capped at 5 minutes while no link to that member is up, and stops when this install is removed. **Pairing follows (V0.180, #261):** an invite's joiner with no peer port (port 0: no `peerListen`, or a host that can only dial out) is accepted and added with port 0, as a member that dials out only; its connector opens the link to the inviter. Before V0.180 the inviter refused it with a bare "bad request".
5. **One link, decided without negotiation (S1).** A link is *up* only after the tunnel is open and one request has gone each way. When two are up the one opened by the install with the **smaller id** wins; the loser gets no new requests and is closed when its requests have drained or after 5 s. A newer link from the same dialer replaces an older one at once (a half-open link after a sleep must not block the redial). A dialer that finds the acceptor does not yet know it as a member (the roster has not synced) is told so before the close, and shows "not a member there yet", not "rejected".
6. **Robust.** HTTP/2 PING on both sessions (send after 15 s idle, dead after 10 s): a dead link is noticed within 25 s; both sides then reconnect. A request waits up to 5 s for a link to come up *only when the host has no whois fallback*, so migration is not slowed; it then fails with the link's last reason. Roster sync runs on every link-up (either direction), on every change and every 60 s.
7. **ALPN.** The dialer offers `fyr-peer/2` and `fyr-peer/1` in one ClientHello and never retries with `/1` alone; the choice is inside the TLS 1.3 transcript, so a stranger cannot force a downgrade, and `/1` is one way and harmless to trust. A peer that speaks only `/1` works one way as today and is shown "older version, one way".

### A1.3 What stays, and the honest blast radius

8. **The trust model.** A link is accepted only from a key that is a live member of the roster, pinned both ways; who dialed changes nothing about who is trusted. **A front still talks to each host directly, over that host's own link. It never relays through a third install, and a request that carries `Fyr-Actor` is never proxied on (3.3).** The link manager is keyed by the proven install id; `UsePeerLink` no longer matches a link by name.
9. **The reach grows (S2).** In roster terms nothing is new (a member reads every host and drives it unless the owner switched it off, 3.7). In network terms it is: `tag:server` gains read, and by default drive, of the laptop and the Mac, which the owner's ACL kept it from. The control is the drive switch (`fyr host drive`), and it is the right one: it is per host and local. **Owner's question Q5 (recommended: keep 3.7).** Should an install that reaches a host *only over a link that host dialed* start as read-only there until the owner switches it on? Recommended: no; these are the owner's own machines and the point is a mesh; the switch is one command. 3.7 and 3.10 now say so.
10. **Pairing, the roster, the key, rotation, the principal, the modes and the migration rules** are unchanged.

### A1.4 Removal, rotation, revoke and the old link

11. **Closing is of the connection, not only of requests (S4).** The roster's change hook closes every link whose peer is no longer a member (a removal) or whose key was replaced (a rotation: the new key dials; its certificate carries the rotation chain, which is accepted only when it leads from a key the roster holds, B1), cancels the requests in flight in both directions, and closes both sessions with the connection. A link is registered first and membership re-checked after, so a link that comes up while the removal merges is closed too; the per-request member check stays as the backstop. A revoked device is refused per request as before.
12. **The enforce gate counts a link that is up (S5).** `peer` enforces by itself only when every followed host has a link up *now*, in either direction, and a request has been served over it since the last use of a whois link (in either direction); a front's own fallback to a whois link counts as a would-refuse on the peer way and restarts the three clean days, so a front that was away does not return to a listener that was removed behind it. By hand, as before, `fyr auth enforce peer` is the owner's call; `fyr auth observe peer` is the way back and the old listeners are restored.

### A1.5 What the owner sees (S6)

13. **Settings → Hosts and `fyr host list`** (desktop and phone, one component) show, per host, `link = {state: up | connecting | down, via: peer | whois | pipe, opened: here | them, since, error?: {kind, text}}`. The reasons are what fyrd can observe, not what the network did: `timeout` (a Tailscale ACL drops silently), `refused`, `key-mismatch`, `not-a-member` (the acceptor said so), `removed`, `revoked`, `older-version`, `no-address` (it has none yet and has not connected), and `connecting` before `down`. A host reachable one way only is shown as reachable with the direction it was opened from.

### A1.6 Test plan additions (4.1, 4.2, 4.4)

The harness gets a fake ACL on the peer port (a one-way block in the dial map). Acceptance: A→B blocked and B→A open: A's front still sees B and B's sees A, over the one link B opened; both blocked: both shown down with `timeout`; a flap from either end: one link, no duplicate, and a redial while the stale link is still registered works; simultaneous dials with a stream open: one link, the stream survives or reconnects; B not yet knowing A: "not a member there yet" and then a link once it syncs; a removal and a rotation each end an open stream within a second **in both directions**; a stranger (not a member) gets no `/2` and 0 statements written; an inner request is recorded with `Front` = the id the link proved and is refused after removal, and the drive switch gives 403 on it; `kind=local` and a wrong `kind=install` id are refused; an older peer works one way; the ACL quick fix (server→members allowed) leaves one link; `TestMigrationKeepsTheOwnerSignedIn` from today's state with the laptop, the Mac and a tagged box.

**Slice 5b. Two-way links** (between 5 and 6; spec row V0.174). `internal/peerlink` gains the link (ALPN, tunnel, inner session, keepalive, the smaller-id rule); a link manager with a connector per member; the front sends to a host over that host's link (`transport(installId)`, which 0031 builds on) and shows `hosts[].link`; sync over the link; the harness ACL. Lockout guard: pairing and the owner's modes are untouched; the per-request dial stays as the fallback for an older peer; `fyr auth observe peer` is unchanged. Slice 6 (the LAN) then adds mDNS as one more source of addresses for the same connectors.

## Consequences

- 0025 (1)'s proof and (2)'s "follow what answers" become "a candidate is followed when its key is in the roster".
- 0026's CPace is replaced by the invite (Q1), its slices by part 6; device records move into the roster.
- 0028's slices 1–5 are replaced by part 6; its Serve option (a) is what is built; "the next release turns enforce on" is replaced by 5.1.
- `docs/API-v0.md`'s Guard section is rewritten in slice 1.
- New data: `<data>/roster.json`, the stored modes, the node approvals and blocks; `Input.actor`.
- NixOS: `services.fyr.peerPort` with `openFirewall`; `hostPort` goes in slice 5; `ownerLogin` stays as an override.
