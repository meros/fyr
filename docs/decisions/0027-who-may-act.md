# 0027 Who may act: one session per browser, an owner socket instead of loopback trust, a host decides which fronts drive it, and agents are named for what they are

Date: 2026-11-16 · Status: **proposed** (draft for the owner; nothing here is built) · Builds on 0026 (keys, roster, mTLS, device pairing) · Amends 0026 (5) (loopback is no longer trusted; device sessions move from slice 5 to the first slice), V0.14, V0.51 (5), V0.12 (the token's transport) · Spec rows: the draft at the end, numbered when each slice is built

**Why now (16 Nov, adding meros-server as a second host).** On meros-server, `stacks-plex-1` runs with Docker host networking. It shares the host's network namespace ([Docker docs](https://docs.docker.com/engine/network/drivers/host/)), so it reaches `127.0.0.1:7777`, and `guard()` treats any loopback `Host` as the owner. A compromised Plex (LAN port 32400) can start agents as `meros` (wheel, docker, Nix trusted user), so it is effectively root. The same survey found four more ways in that trust only "you are on this machine" (below).

## What changes for the owner, day to day

- **Phone (PWA over the tailnet):** nothing visible. The first visit after the change signs the phone in by its tailnet identity, as today, and it stays signed in with a cookie.
- **Laptop:** `fyr-app` (Super+Ctrl+Return) becomes `fyr open --app`, a single line in `~/nixos`. Typing `http://127.0.0.1:7777` into another browser shows "Open Fyr from this machine: run `fyr open`". The address is `http://fyr.localhost:7777` from then on.
- **Settings → Devices** (desktop and phone) lists every browser that can open this Fyr, with how it got in and when it was last used. Revoke works on a tailnet phone too, without taking it off the tailnet.
- **A new device signs in:** a push "New sign-in: Pixel 9 through the tailnet" (Q1).
- **Pairing a host or a device** asks for a fresh confirmation on the device you are holding (see "Step-up").
- **meros-server** gets an `ownerLogin` option in the module (the `mkForce`'d ExecStart goes away), pairs with the laptop by code (0026 slice 3), and drives the laptop only if you tick that (Q2).
- **Agents** are unchanged. They still run with your full account (Q3). The record says so plainly instead of implying the MCP token contains them.

---

## Context: every way in today

From the code at `0ab7643`:

| # | Way in | Who uses it | What it trusts | Where |
|---|---|---|---|---|
| A | `127.0.0.1:7777`: the browser API, the front, `/.well-known/fyr` | laptop browser, `fyr-app` (Chrome `--app=http://127.0.0.1:7777`) | **a loopback `Host` header = the owner.** POSTs need `X-Fyr: 1` (blocks a cross-site form, not a local process) | `server.guard` |
| B | the same port with `Tailscale-User-Login` and a `*.ts.net` Host | phone through Tailscale Serve | **the header**, when the Host ends in `.ts.net`. It never checks that tailscaled sent it. Any local process can send both | `server.guard` |
| C | `/mcp` on `127.0.0.1:7777` and on `hostListen` | each agent's `claude` CLI | a per-thread bearer (24 random bytes), revoked when the session ends. No `X-Fyr`. **The token is passed on the command line** (`--mcp-config '<json>'`), so `/proc/<pid>/cmdline` shows it to every local user (`/proc` is mounted without `hidepid`). VS Code fixed the same leak with `--connection-token-file` ([GHSA-54p6-6j68-j5vr](https://github.com/microsoft/vscode/security/advisories/GHSA-54p6-6j68-j5vr)) | `agent.go:394`, `claudecli.go:111` |
| D | the `both` role's front → local host link on `127.0.0.1:<ephemeral>` | the front | **loopback guard.** The whole host protocol, unauthenticated, to any local process that scans the ports | `main.go:320` |
| E | `hostListen` on a tailnet IP (`:7778`, plain HTTP) | other fronts | whois of the connection = the owner's login. Tagged peers are refused | `server.PeerGuard`, `tailnet.Peers.Login` |
| F | `hostListen` on a loopback address (development, a second install) | a dev front | loopback guard | `main.go:342` |
| G | the launcher ↔ runner (`runner.sock`, 0600), the ready file | launcher, fyrd | file mode (same UID) | V0.90, `deploy/launcher.go` |
| H | systemd unit start and stop | swaypplet's bar mark (`bar/src/fyr.rs`) | polkit | not Fyr's |

What each local client needs: the laptop browser and `fyr-app` need a browser session (A). The `claude` CLI's MCP client needs its bearer only (C). The self-deploy health check uses the ready file and needs nothing over HTTP (G). The swaypplet mark uses systemd and needs nothing from Fyr (H). The `fyrd pair` and `fyrd device add` CLIs of 0026 need an owner channel that is not the network.

**Owner identity on a tagged node.** `tailnet.Self` names no user on `tag:server`. Before the `-owner-login` workaround, fyrd took the node's own name as the owner. Serve also sets no identity headers for traffic *from* tagged devices ([Tailscale KB 1312](https://tailscale.com/kb/1312/serve)).

### What comparable tools learned

| Tool | Mechanism | The failure that taught it |
|---|---|---|
| Ollama, Selenium Grid, Ray, MCP Inspector | trusted loopback, no token | DNS rebinding or CSRF to localhost turned into RCE: [CVE-2024-28224](https://www.nccgroup.com/research/technical-advisory-ollama-dns-rebinding-attack-cve-2024-28224/), [CVE-2022-28109](https://nvd.nist.gov/vuln/detail/CVE-2022-28109), [ShadowRay CVE-2023-48022](https://oligo.security/blog/shadowray-attack-ai-workloads-actively-exploited-in-the-wild), [CVE-2025-49596](https://www.oligo.security/blog/critical-rce-vulnerability-in-anthropic-mcp-inspector-cve-2025-49596) (fixed by adding a session token) |
| Jupyter Server | a token even on loopback, exchanged for a cookie; a Host allowlist (`local_hostnames`); XSRF | open instances were mined (Qubitstrike, 2023) ([docs](https://jupyter-server.readthedocs.io/en/latest/operators/security.html)) |
| code-server | a password even on loopback, a cookie, rate-limited logins | [CVE-2023-26114](https://nvd.nist.gov/vuln/detail/CVE-2023-26114): no Origin check on WebSockets. [CVE-2025-47269](https://nvd.nist.gov/vuln/detail/CVE-2025-47269): the port proxy sent the session cookie onward (confused deputy) |
| Syncthing | a device ID is the hash of its certificate, peers are pinned; the GUI on localhost with a Host check and an API key | the Host check came in 0.14.6 against rebinding ([FAQ](https://docs.syncthing.net/users/faq.html)) |
| Home Assistant | refresh tokens (90 days unused), access tokens (30 min), revoking the refresh token revokes its access tokens; `trusted_networks` logs in with no password | the docs warn that a trusted network must not be a trusted proxy ([providers](https://www.home-assistant.io/docs/authentication/providers/)); [CVE-2023-27482](https://nvd.nist.gov/vuln/detail/CVE-2023-27482): an auth bypass through the Supervisor proxy path |
| tailscaled LocalAPI | a Unix socket. Anyone may read; writing needs root or the operator UID by peer credentials; TCP gets nothing ([ipnauth](https://github.com/tailscale/tailscale/blob/main/ipn/ipnauth/ipnauth.go)) | the model Fyr's owner socket copies |
| Tailscale Serve | sets `Tailscale-User-Login`, strips incoming copies; "the service should listen only on localhost", else clients "could trivially provide their own values" ([KB 1312](https://tailscale.com/kb/1312/serve)) | Fyr's way in B is that warning: loopback is not "only Serve" |
| Tailscale app capabilities | grants in the tailnet policy, returned per request in whois `CapMap` ([KB 1537](https://tailscale.com/kb/1537/grants-app-capabilities)) | an option for scopes and tagged peers, weighed below |
| MCP spec 2025-06-18 | local HTTP servers SHOULD authenticate every connection, MUST validate Origin, should prefer a token or a Unix socket; no token passthrough ([transports](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports), [security](https://modelcontextprotocol.io/specification/2025-06-18/basic/security_best_practices)) | Fyr's `/mcp` already uses a bearer; Origin is not checked yet |

The lesson that repeats: **loopback is not a boundary, and an identity header is only as good as the socket it arrived on.** Chrome's Local Network Access prompt (since 142, [blog](https://developer.chrome.com/blog/local-network-access)) helps against web pages. It does nothing against a local process or a host-networked container.

---

## Decision

### 1. One browser mechanism: a session. Everything else only mints one

Every browser request to a front needs a **session cookie**: `fyr_session`, HttpOnly, SameSite=Strict, Secure on HTTPS, holding 32 random bytes. The front stores only the hash, in a **device record**: `{id, name, how, created, lastSeen, by, blockedNode?}`. Nothing else is standing trust. The **ways to mint** a session:

| Mint | How | Replaces |
|---|---|---|
| **local** | `fyr open` asks fyrd over the owner socket (point 2) for a one-time link `http://fyr.localhost:7777/auth/once#<token>` (60 s, single use) and opens it. The page posts the token and gets the cookie. A browser that already has a valid session keeps it, so no second device row | way A |
| **tailnet** | a request that arrived **through the Serve socket** (point 3) carries `Tailscale-User-Login` = `ownerLogin`, and the front looks up the node through whois. The device record is keyed by the node's **StableID**, so the same phone is one device across cookies | way B |
| **device code** | 0026's Add a device QR, from a session that passed step-up | 0026 slice 5, unchanged |
| **passkey** (later) | WebAuthn on a `*.ts.net` or own-domain origin | named only |

- **Revoke** deletes the session. For a tailnet device it also sets `blockedNode`, so that node cannot mint again until the owner allows it on the device's page. That makes revocation real for a phone that stays on the tailnet.
- **Cascade:** revoking a device also revokes the devices it added. A thief who added their own laptop from a stolen phone loses both with one press.
- **Expiry:** a session unseen for 90 days ends, as in 0026 and Home Assistant. A tailnet device re-mints silently while it is not blocked, so for the phone this only matters after a Revoke.
- **The origin `fyr.localhost`**, not `127.0.0.1`. Cookies are not isolated by port: any other local web app the laptop browser visits at `127.0.0.1:<port>` would receive a `127.0.0.1` cookie. Browsers resolve `*.localhost` to loopback and treat it as a secure context. The Host check accepts `fyr.localhost`, `localhost` and the loopback IPs (the last two only for `/auth/*` and a redirect).
- **CSRF and rebinding:** `X-Fyr` on POSTs and the Host allowlist stay. The cookie is SameSite=Strict. WebSocket and SSE upgrades check `Origin` against the front's own names (code-server's CVE-2023-26114).
- **The phone pays nothing:** its first request after the change mints through the tailnet. The laptop pays one changed line in `~/nixos`.

**Rejected:** a per-boot secret in a file read by the launcher (the Jupyter model). It works, but it is a second secret with the same reach as the socket, it lands in backups, and it does not stop a host-networked container any better than file permissions already do. A cookie bootstrapped from a "local-only page" is rejected because "local-only" is exactly what cannot be decided over TCP.

### 2. The owner socket: local authority by file and peer credentials, not by address

fyrd listens on **`<data>/owner.sock`** (mode 0600; the runner's short-path fallback when the path is too long, V0.90). It accepts a connection only when the peer's UID (SO_PEERCRED on Linux, `getpeereid` on macOS) is fyrd's own or root, as tailscaled's LocalAPI does. It serves:
- `POST /session/once`: the one-time link for `fyr open`;
- `POST /device/add`: a device code, printed as a terminal QR by `fyrd device add` (recovery and headless);
- `POST /pair/*`: 0026 slice 3's `fyrd pair`;
- the host protocol, for a development front (way F becomes `hostListen: unix:<path>`).

Why a socket: a host-networked container shares the network namespace, **not the filesystem** (unless the socket is mounted in). Other local users fail the mode and the UID check. It also works on macOS.

**What the socket does not stop: a process running as the owner's UID.** That includes every agent (point 6).

### 3. Tailscale Serve proxies to a socket only tailscaled uses; or fyrd terminates tailnet HTTPS itself

The header path B must stop being reachable from loopback. Two options:

| | (a) Serve → a Unix socket | (b) fyrd listens on the tailnet itself |
|---|---|---|
| How | `tailscale serve --https=7443 unix:<runtime>/fyr/serve.sock`. Only requests on that socket may carry `Tailscale-User-Login`; on TCP the header is refused | fyrd binds `100.x:7443` with the MagicDNS certificate from LocalAPI `cert/<name>` and whois-es each connection (as PeerGuard does), so no header is trusted at all |
| Pros | small; Serve stays the owner of the cert and its renewal | one less moving part; identity from the connection, not a header; works for tagged nodes' browsers with `ownerLogin` |
| Cons | needs Serve's Unix-socket backend: **to verify on 1.104 in the slice**. tailscaled runs as root, so it can open a 0660 socket | the cert call needs LocalAPI *write*: root or `tailscale set --operator=meros`. fyrd now owns renewal |

**Recommendation: (a)**, with (b) as the fallback if the Unix backend is missing or misbehaves. (a) is the smaller change and keeps today's NixOS shape (`fyr-tailnet.service`).

### 4. Fyrd ↔ fyrd: 0026's mTLS, plus a host decides which fronts may drive it

0026 slices 2–3 stand: pinned mTLS on every link, a roster, CPace pairing, `fyrd pair` for headless hosts. This record adds:

- **No `/mcp` on the peer port.** Agents are local, and the peer listener serves the host protocol only.
- **The local link (way D) is in-process.** The `both` role's front talks to its own host over an in-memory listener (`net.Pipe`-backed), not a TCP port. Nothing to scan, nothing to authenticate.
- **A host grants "may drive me" per front.** The roster says who is the owner's. It does not say that every one of them should run agents everywhere. A front on meros-server, which is exposed to the LAN through Plex and others, is a weaker machine than the laptop. If it is compromised, its install key drives every host in the mesh. So each host keeps a **drive list**: the fronts whose commands it accepts. Reads (state, stream) go to every roster front; commands go only to the listed ones. The list is signed by the host and **only the host can change it** (locally, or from a front already on it after step-up). It is not gossiped as a roster statement, so a rogue roster member cannot add itself. The default for a new pairing is "drives" (today's behaviour), with the switch on the pairing screen and the host's page. **Q2** is the owner's choice for meros-server.
- **Tagged nodes:** the key is the identity (0026 (6)). For the browser on a tagged node, the module gains **`services.fyr.ownerLogin`** (now: it removes the `mkForce` in `~/nixos`). fyrd stops taking the node's own name as the owner when `tailnet.Self` names no user: it logs "tagged node: set ownerLogin" and serves no tailnet browser until it is set.
- **Reaching the laptop from meros-server** is a tailnet policy grant (`tag:server` → the laptop's host port), not a Fyr change. After slice 2 that port is mTLS, so the grant opens a door that needs a roster key.
- **Rejected for host trust: tailnet app capabilities (`CapMap`).** They would let the tailnet policy say "tag:server is a Fyr peer" with no pairing, but they tie trust to the tailnet again, which 0026 moved away from. They could later be a pairing shortcut for tagged nodes, as whois is for untagged ones.

### 5. Authorization: one owner, two kinds of power, one step-up

A one-owner system does not need roles. It does need to tell "use Fyr" from "change who can use Fyr". Each scope considered, with the verdict:

| Scope | Verdict | Why |
|---|---|---|
| read-only device | **dropped unless the owner wants it (Q4)** | no device of the owner's needs it today. A wall display is the one use case, and it costs a second client mode in every view |
| approve effects / run agents / settings as separate grants | **dropped** | every device is the owner holding it. Splitting these adds prompts, not safety, because the attacker who holds a session holds all of them |
| per-host grants for a *device* | **dropped** | a device talks to one front (0025's one uplink). Per-host power sits on the front → host link (the drive list, point 4) |
| **step-up for trust changes** | **kept** | the one thing a stolen session must not do quietly is make itself permanent: add a device, pair or remove an install, change a drive list, allow a blocked node, rotate a key |

**Step-up** means a fresh confirmation within the last 5 minutes. It is a passkey with user verification where the device has one. Otherwise it is **a confirm on another device**: a push "Allow Pixel 9 to add a device?" answered on a device whose session is older than the asking one. A lone device with no other device and no passkey can step up through `fyr open` or `fyrd device add` on the machine, which proves shell access. Deploys and effect approvals do **not** need step-up. They are the daily work, and an approval is already a deliberate press on the Decision. Requiring more would train the owner to click through.

### 6. Agents → /mcp, and agents as untrusted input

**What a thread's token can do** (`internal/engine/threads_tools.go`): read and list other threads, `thread_send` to one with no open Decision (30 an hour), `thread_new` (5 an hour), rename its own and its children, open changes, request effects, offer actions. It **cannot** answer a Decision, approve an effect or press an offered action. The owner's source (`FromOwner`) is reached only through the browser API. As an MCP surface this is sound: a prompt-injected agent can at worst send loud messages to other threads (each marked with the sender), which their agents treat as input from a peer, not from the owner.

**But the MCP surface is not the boundary today.** An agent with `Bash` (permission mode `auto`) runs as `meros`. It can:
1. `curl -H 'X-Fyr: 1' 127.0.0.1:7777/api/command`: the owner, approving its own effects (closed by points 1–2);
2. read another thread's token from `/proc/*/cmdline` (closed by passing `--mcp-config` as a 0600 file, slice 1);
3. read `<data>/identity.key`, the session table and Chrome's cookie store; connect to `owner.sock`; and, beyond Fyr, `ssh meros-server` (Tailscale SSH), use `gh`, use the docker group.

Item 3 is not something Fyr can close from inside fyrd: the same UID is the same principal. So the record says it plainly. **An agent with an unrestricted shell is the owner on that machine, and Fyr's tokens scope only what it does through Fyr.** The options:

| Option | What | Cost |
|---|---|---|
| **(i) Name it (recommended now)** | the threat model and Settings say so; points 1–2 close every path that did *not* need the owner's UID | none |
| (ii) Confine agents | the runner starts agents in a mount namespace with `<data>/`, `owner.sock`, browser profiles and `~/.ssh` hidden (bubblewrap or `systemd-run --user -p InaccessiblePaths=`), or turns on Claude Code's own sandbox for `Bash` | real friction: agents deploy, ssh, use docker today. Worth it only as a product choice for all agents, not as a Fyr-auth detail |
| (iii) A separate UID for agents | `fyr-agent` with its own git and gh credentials | the strongest; a large change to providers, worktrees and logins |

**Q3** asks whether (ii) should become a slice. Either way, two cheap moves come now: the token goes in a file, and `/mcp` checks `Origin` (absent or own) per the MCP spec.

### 7. Audit, revocation, rotation, loss and recovery

- **Audit log:** an append-only table on each front and host: `{at, device | install, what}`. It records each session minted or revoked, each step-up, each pairing, removal or drive-list change, each effect approval, deploy and Decision answer. Every owner `Input` gains `device` (the session's device id, or `install:<id>` for a front's command), so a host's log says which front and which device. A device's page shows its last 50 entries. It costs one column on `Input` and one table, and it answers the question that matters after a loss: "what did that phone do?"
- **Lost phone:** Revoke it on any other device. That ends its session, blocks its tailnet node and cascades to the devices it added. Removing it from the tailnet is not needed.
- **Lost laptop:** Remove its install on any other (0026 slice 6, after step-up). Its sessions die with it.
- **Recovery with no trusted device left:** any shell on any install (console, SSH) runs `fyrd device add`, which prints a QR in the terminal. The roster and all the data survive, because recovery is a new session, not a new identity. With no shell on any install, there is nothing to recover *to*: that is the honest floor of a one-owner system.
- **Rotation:** install keys per 0026 (`fyrd rotate-key`). Session tokens are not rotated on a timer; Revoke is the tool. The owner socket has no secret to rotate.

### 8. Headless hosts (meros-server)

They have no browser and no screen. `ssh meros-server fyrd pair` shows the 6-digit code and a terminal QR, and `fyrd device add` mints a device code the same way. Both go through the owner socket, so they need no network trust at all.

---

## Threat model

| Threat | Today | After |
|---|---|---|
| **A host-networked container** (Plex on meros-server) connects to `127.0.0.1:7777` | owner | a page with no data. The API needs a session; `owner.sock` is in another mount namespace; the local link is in-process; the Serve socket is not in the container (slice 1) |
| it forges `Host: x.ts.net` + `Tailscale-User-Login` | owner | refused: the header counts only on the Serve socket, or with (b) no header counts at all |
| it reads an MCP token from `/proc` | it can, if the container shares the PID namespace (Plex does not) | the token is in a 0600 file |
| **Another local user** on the machine | owner via A, D, B; tokens via `/proc` | nothing: socket mode, peer credentials, file token |
| **A web page** (DNS rebinding, CSRF, 0.0.0.0) | stopped by the Host check and `X-Fyr` | the same, plus SameSite=Strict, Origin on upgrades, and no cookie for a foreign Host |
| **A prompt-injected agent** through MCP | messages to other threads; no Decisions | the same |
| a prompt-injected agent **through Bash** | owner via `curl` loopback | it needs the owner's files (the socket, the key, Chrome's cookies): still the owner's UID. **Not closed** without Q3 (ii)/(iii). Named in Settings |
| **A stolen phone**, unlocked | full Fyr until it leaves the tailnet; can't be revoked in Fyr | Revoke + node block + cascade; it cannot add a device or pair without step-up; the log shows what it did |
| a stolen phone, locked | as above | as above. Passkey step-up needs its biometrics |
| **A compromised meros-server** (root) | its whois login was refused anyway (tagged); after the `ownerLogin` workaround nothing to the laptop | its key reaches every host's reads. It **drives** only the hosts whose drive list names it (Q2); Remove from any other install |
| a compromised laptop | owner of everything | the same: a trusted install with the owner's shell. Remove it from another |
| **A tailnet device of the owner's login the owner did not expect** (a re-added old tablet) | full Fyr silently | a session plus a push "New sign-in"; Revoke blocks the node (Q1) |
| lockout during migration | n/a | each slice keeps the old way until the new one is proved (below); `fyrd device add` from any shell |

Out of scope: several owners, an attacker with root on a trusted machine, the tailnet control plane itself.

---

## Slices: each one ships alone, the loopback hole first

| Slice | What | Shippable because | Migration, no lockout |
|---|---|---|---|
| **0 (hours, independent)** | `--mcp-config` as a 0600 file path; `/mcp` refuses a foreign `Origin`; the `both` role's front → host link in-process; the module's `ownerLogin` option; a tagged node without `ownerLogin` serves no tailnet browser (instead of naming itself owner) | no change for a browser | `~/nixos` swaps the `mkForce` for `ownerLogin` in the same change |
| **1: loopback is not the owner** | `owner.sock`; sessions and device records; `/auth/once`; `fyr open [--app]`; `fyr.localhost`; the Serve socket (3a, or 3b); the browser API needs a session; loopback TCP serves only the shell page, `/auth/*`, `/.well-known/fyr` (`{fyr, id}`) and `/mcp` (bearer); `hostListen: unix:` for development | closes Plex, other users and the header forgery. **This is the point-1 fix** | The build first **mints silently** on the old paths (a loopback or Serve request without a cookie gets one) for one release, while it logs every request that would be refused. The owner switches `fyr-app` and the Serve unit. The next build turns refusal on. The launcher's rollback covers a bad build, and `fyrd device add` from a shell covers the rest |
| **2: Devices** | Settings → Devices (0026's "This device" renamed, desktop and phone): every device, how it got in, last used; Revoke with node block and cascade; the "New sign-in" push; the audit log and a device's last actions | the owner sees and controls what slice 1 made | nothing to migrate: slice 1's sessions appear as devices ("this laptop · local", "Pixel 9 · tailnet") |
| **3 = 0026 slice 2** | roster, pinned mTLS on the host port, no `/mcp` there | | as 0026 |
| **4 = 0026 slice 3, + drive lists + step-up** | pairing by code, `fyrd pair` and `fyrd device add` over `owner.sock`; each host's drive list; step-up by a confirm on another device | meros-server pairs, and the laptop decides whether it drives | today's links default to "drives", so nothing stops |
| 5 = 0026 slices 4–6 | mDNS, the device QR off the tailnet, removal and rotation spreading, the whois listener dropped | as 0026 | as 0026 |
| later | passkeys for step-up on `*.ts.net` (`ts.net` is on the Public Suffix List, so the RP ID is the machine's own name or the tailnet's `<tailnet>.ts.net`); agent confinement if Q3 says so | named only | |

**A stopgap for meros-server today, outside Fyr.** Until slice 1 ships, take Plex off host networking (bridge mode with its published ports), or stop `fyr.service` there. A UID firewall rule does not help: the container's UID is likely 1000 too. An nftables `socket cgroupv2` match on the Docker scope could work, but it is fiddly and unverified here.

## Questions for the owner

- **Q1. A new device of your tailnet login opens Fyr.**
  - (a) It is signed in at once, and you get a push "New sign-in: <device>" with Revoke. **Recommended:** today's trust, with an alarm and an undo.
  - (b) It waits until you allow it on a device you already use. This is safer, but every new phone or browser profile is a two-device dance.
- **Q2. meros-server's front.** It is more exposed than the laptop: Plex on host networking, a LAN service host.
  - Should it be able to *run agents on the laptop* (drive), or only *see* the laptop's threads?
  - **Recommended:** see only, until Plex is off host networking; then your choice.
  - Either way, the laptop's front drives meros-server.
- **Q3. Agents and your account.** Should a later slice confine agents (no Fyr data, no `~/.ssh`, no docker socket), knowing it breaks agents that ssh, deploy or use docker?
  - **Recommended:** not now. Name it, and revisit when an agent first reads untrusted web content at scale.
- **Q4. A read-only device** (a wall display, a shared screen): needed? **Recommended:** no.

## Consequences

- V0.14's guard becomes "a session, minted by local, tailnet or device code". The `Guard` section in `docs/API-v0.md` is rewritten in slice 1.
- 0026 (5): "Loopback stays trusted (the OS user)" is replaced by the owner socket and `fyr open`. Device sessions move from 0026 slice 5 to this record's slice 1.
- V0.51 (5): `/mcp` leaves the peer listener. The local link is in-process.
- V0.12: the token travels in a file, not in argv.
- New front data: device records, the audit log. New host data: the drive list, the audit log.
- `~/nixos`: `fyr-app` runs `fyr open --app`; `services.fyr.ownerLogin` on meros-server; the Serve unit's target changes (3a).

---

## Draft SPEC rows (numbered when built; the next free number is V0.126)

| V | Row |
|---|---|
| V0.126 | → V0.12, V0.51, V0.14, decision 0027 slice 0. **Small closings.** (1) A thread's MCP config goes to `claude` as a file path (`<data>/run/<proc>.mcp.json`, mode 0600, removed when the session ends), never in argv; an adopted process (V0.90) keeps its file. (2) `/mcp` refuses a request whose `Origin` is present and not one of this front's own names (403). (3) The `both` role's front reaches its own host over an in-process listener; no loopback port is opened for it. (4) A node whose tailscaled names no user (a tagged node) and no `ownerLogin` serves no browser over the tailnet and logs "tagged node: set ownerLogin"; it never takes its own name as the owner. (5) NixOS: `services.fyr.ownerLogin` (string, null by default) writes `ownerLogin` to the config. Tests: the token is absent from the spawned argv and present in a 0600 file; Origin refused/allowed; no listener on 127.0.0.1 besides `listen` in the `both` role; the tagged-node case with a fake LocalAPI. |
| V0.127 | → V0.14, decision 0027 slice 1. **Loopback is not the owner: one browser session, minted locally or by the tailnet.** (1) **Owner socket** `<data>/owner.sock` (0600; the runner's short path when too long), accepted only when the peer's UID (SO_PEERCRED / getpeereid) is fyrd's or 0: `POST /session/once` → `{url}`, a one-time link valid 60 s. `fyr open [--app]` calls it and opens the link (with `--app`: Chrome app window, focusing an open one). (2) **Sessions.** `GET /auth/once` serves a page that posts the fragment token to `POST /auth/once`; the front sets `fyr_session` (HttpOnly, SameSite=Strict, Secure on HTTPS, path /, 32 random bytes, only the SHA-256 kept) for a device record `{id, name, how: local\|tailnet\|code, created, lastSeen, node?}`; a browser that already holds a valid session keeps it. A session unseen for 90 days ends. (3) **Tailnet.** A request that arrives on the Serve socket (`<runtime>/fyr/serve.sock`, which `fyr-tailnet.service` targets) with `Tailscale-User-Login` = `ownerLogin` and no session mints one; the device record is keyed by the whois node StableID. On TCP the header gets 403. (4) **The guard.** Every browser API call and the stream need a session (401 `{error: "not signed in"}`); the client shows "Open Fyr from this machine: run `fyr open`" on a loopback Host, "Sign in through your tailnet" otherwise. Without a session only the shell page, `/auth/*`, `/.well-known/fyr` (`{fyr, id}` only) and `/mcp` (bearer) answer. Allowed Hosts: `fyr.localhost`, `localhost` and loopback IPs (the last two redirect to `fyr.localhost`), the front's MagicDNS name; `X-Fyr` stays; SSE and WebSocket check `Origin`. (5) **Development.** `hostListen: unix:<path>` serves the host protocol on a 0600 socket with the same peer check; loopback `hostListen` is refused. (6) **Migration build.** `authMode: "observe"` (the default for one release) mints silently on the old paths and logs each request that `enforce` would refuse; `enforce` is the next release's default. Tests: the peer check (another UID refused); a one-time link works once and not after 60 s; a loopback request with no cookie gets 401 in enforce and a cookie in observe; a forged ts.net header on TCP gets 403; the Serve socket mints and keys by StableID; a rebinding Host gets 403; layout e2e desktop and phone: the signed-out page. |
| V0.128 | → V0.127, V0.125, decision 0027 slice 2, `design/pairing/` slice 5. **Settings → Devices.** "This device" becomes Devices (desktop and phone, one shared row): each device with its name, how it got in ("on this machine", "through the tailnet", "by a code from <device>"), created and last used; this browser first. **Revoke…** (confirm; red) ends its session, sets `blocked` on a tailnet device's node (shown as "Blocked · Allow again…") and revokes the devices it added. A new device sends the push "New sign-in: <name> <how>" with Revoke. **Audit log:** every owner Input carries `device`; each front and host keeps `{at, device\|install, what}` for sign-ins, revocations, step-ups, pairing, removal, drive-list changes, effect approvals, Decision answers and deploys; a device's page shows its last 50. Tests: cascade, the node block, the push, the log entries; layout e2e desktop and phone at every size. |
| V0.129 | → 0026 slices 2–3, decision 0027 slice 4. **A host decides which fronts drive it; trust changes need a step-up.** (1) Each host keeps a drive list of roster ids, signed by itself and never gossiped; reads go to every roster front, commands only from a listed one (403 `{error: "<host> lets <front> read only"}`); a new pairing defaults to drives, with the switch on the pairing screen and the host's page. (2) Adding a device, pairing, removing an install, a drive-list change and allowing a blocked node need a step-up within 5 minutes: a confirm pushed to an older device, or `fyr open`/`fyrd device add` on the machine; a passkey later. (3) `fyrd device add` and `fyrd pair` use `owner.sock` and print a terminal QR. Tests: a read-only front's command refused, reads served; the step-up window; a front that is not on the drive list cannot add itself. |
