# 0026 An install is its key: discovery from many sources, trust from pairing, TLS pinned to keys; the tailnet is one route

Date: 2026-11-16 · Status: accepted · Amends 0016 (front auth), 0021 (6) and 0025 (1), (2) · Supersedes the parts of 0025 that rest on the tailnet · Spec rows follow with each slice · Superseded in part by 0030 ((3)–(5) and the slices)

**Context.** Since V0.123 (0025) everything between Fyr installs rests on Tailscale:

| Job | Today | Where |
|---|---|---|
| Discovery | tailscaled's peers (online, untagged, the owner's user), then `GET https://<MagicDNS>:7443/.well-known/fyr` | `internal/tailnet.Candidates`, `internal/peers` |
| Front → host auth | tailscaled whois of the connection must be the owner's login | `server.PeerGuard` (V0.51 (5)) |
| Browser → front auth | loopback `Host`, or Serve's `Tailscale-User-Login` on a `*.ts.net` host | `server.guard` (V0.14) |
| Encryption | WireGuard. The host protocol itself is plain HTTP (`http://100.x:7778`) | `hostListen tailnet:PORT` (V0.54 (7)) |
| A trusted HTTPS origin for the PWA, service worker and push | Serve's `*.ts.net` certificate | NixOS `services.fyr.tailnet` |

On a plain LAN none of these exist. And because the tailnet's identity is a *user*, a tagged node such as host-b has no owner and cannot take part (0025 (1), put off by the owner).

The fix is to stop letting the network say who someone is. **An install is known by its key. A network only carries bytes and proposes addresses.**

## Decision

### 1. Each install has a key, and that key is its identity

- At first start fyrd makes an **Ed25519 key pair** in `<data>/identity.key` (mode 0600). Its **id** is the SHA-256 of the public key, shown as 4×4 base32 groups (`F7QK-2M9X-…`), the first group as the short form.
- A host is known by its id, never by its address or name. One id may have many addresses (loopback, LAN, tailnet), and a link is to the id: the front dials whichever address answers with the right key.
- The name stays a label and the routing key of V0.51 (3). The roster (point 3) binds each name to one id; a second id that claims a taken name is shown to the owner as a conflict and is not followed.
- `/.well-known/fyr` adds `id`. Unauthenticated, it answers only `{fyr: 1, id}`; the rest (`name, role, build, host`) only to a paired peer. It no longer carries the owner's login (it means nothing off the tailnet).

### 2. Discovery is a set of sources; a source proposes, it never trusts

Every source yields `(id?, address)` candidates into one list. A candidate becomes a followed host only if its id is in the roster and the TLS handshake proves the key.

| Source | Gives | Notes |
|---|---|---|
| **Tailnet** (today's) | MagicDNS names of the owner's online peers; **tagged peers included** | still the best source where it exists |
| **mDNS / DNS-SD** `_fyr._tcp.local` | address, port, TXT `v=1 id=<id>` | one LAN segment only; no name, no owner in the TXT |
| **By hand** | a URL the owner types (Settings → Hosts → Add, or config `peers`) | for other subnets, VLANs, a VPS |
| **Rendezvous / relay** | named, not built | for a host behind NAT with no tailnet; a later record |

An unpaired candidate shows in Settings → Hosts as "Fyr install F7QK… on 192.168.1.20 · not paired" with **Pair…**. A paired id seen at a new address is followed there with no press (the address follows the key).

### 3. Trust: pairing makes a roster, and the roster spreads

- **The roster** is the set of install ids the owner trusts, each with its name and who added it. Every install keeps a copy. A change (add, remove, rotate) is a statement signed by the install that made it, with a counter; installs pass new statements to each other over their links and accept one when its signer is in the roster and not removed. So pairing a new machine with *any* trusted install is enough; the rest learn it within a minute.
- **Pairing two installs** (front–host or front–front): the owner presses **Pair…** on install A, which shows a **one-time code** (6 digits, or a QR of it) valid for 5 minutes. The owner enters it on B: in B's Settings, or with `fyrd pair <code>` for a headless host. The two run **CPace** (a PAKE) over a TLS channel, bound to both ids. A wrong code costs the attacker one guess per attempt; three failures cancel the code. On success each adds the other to the roster.
- **Tailnet shortcut** (owner: yes). When both sides' tailscaled whois says the other is an untagged node of the owner's user, they pair with no code. This is today's trust, recorded as keys, so the live setup migrates with no press.
- **Removing.** Settings → Hosts → **Remove** on any trusted install writes a removal. A removed id is refused by every install once the statement reaches it, and cannot be added back except by a new pairing. Removal of a stolen machine works from any other.
- **Rotating.** An install makes a new key and signs "old → new" with the old one; others accept it while the old one is trusted. `fyrd rotate-key`, and automatically never.
- One owner: the roster has no users, roles or ACLs. Every member is the owner's.

### 4. Transport: mutual TLS pinned to the keys, on every network

- Every fyrd-to-fyrd link (the host protocol, discovery's probe after pairing, roster gossip) is **TLS 1.3 with a self-signed Ed25519 certificate** made from the install key. Both sides check the other's key against the roster (`VerifyPeerCertificate`); names, IPs and public CAs play no part. A client without a roster certificate gets only `/.well-known/fyr` and the pairing endpoint.
- **On the tailnet too.** The tailnet becomes a route under the same TLS, so there is one code path and WireGuard is a second layer, not the only one. The plain-HTTP whois listener (V0.51 (5)) stays during migration and is then dropped.
- `hosts[].transport` (0021 (6)) is no longer needed per host: a link is always `tls`; its addresses say which network carried it.

### 5. The browser: the hard part

A browser does not trust a pinned self-signed certificate by itself, and a PWA needs a secure context for its service worker and push. HTTP on a LAN address is not one.

**The browser path is the installation's choice**. The owner's own phone reaches its front over the tailnet, so Fyr builds no certificate machinery of its own now. What Fyr builds:

- localhost and Tailscale Serve, as today;
- a config `tls: {cert, key}` (file paths, re-read when they change). With it, the front serves its browser port over HTTPS with any certificate the install brings: from its own CA, from an ACME client, or from a reverse proxy in front. That is the hook a LAN-only install uses;
- device pairing (below) for a browser that does not arrive through Serve.

The paths an install can take, with their costs:

| Path | Where it works | Cost |
|---|---|---|
| **localhost** | the machine running the front | none; the laptop's own front stays as today |
| **Tailscale Serve** `*.ts.net` | any device on the tailnet, at home and away | needs the tailnet on that device; today's path, kept |
| **Local CA** (the install's own; a built-in one is a later step) | the LAN, no internet name | a CA, ideally a "Fyr local CA", **name-constrained** to `.local`, `.lan`, `.home.arpa` and private IPs, issues the front's LAN certificate. The owner installs the CA on each device once. Android shows a "network may be monitored" note; iOS needs a trust toggle |
| **Public name + ACME DNS-01** (the install's own ACME client; built in later if wanted) | the LAN, and away if the name also routes there | a domain the owner owns and a DNS provider token; the name points at a private IP, which some routers' rebinding protection and Android's Private DNS block |

One origin is one installed app with its own push subscription and storage (0021 (4)). A phone that should reach the same front at home and away needs one origin that resolves in both places: today that is `*.ts.net`.

**Browser authentication off the tailnet** is device pairing, not a network identity:
- Loopback stays trusted (the OS user) and is the first trusted device.
- On a trusted device, Settings → **Devices → Add a device** shows a QR: the front's origin and a one-time token (5 minutes). The phone opens it; the front sets an **HttpOnly, Secure, SameSite=Strict session cookie** for a new device record `{name, created, lastSeen}`, and stores only its hash.
- Settings → Devices lists every device with its last use, and **Revoke** ends its session at once. A session unseen for 90 days ends by itself.
- The X-Fyr rule for POSTs and the Host check against DNS rebinding stay; the Host must be one of the front's own names.
- A browser on Serve keeps the `Tailscale-User-Login` path (V0.14), so the tailnet stays a pairing shortcut for browsers too.
- Passkeys (WebAuthn) are a later step for a front with a real domain name.

All three paths depend on the install. The owner's own setup keeps Serve for the phone and loopback for the laptop, so slice 5 is not on the owner's path. It is there for an install without a tailnet.

### 6. How the tailnet fits

- It is one **discovery source** (tagged peers now included), one **route** under the pinned TLS, the phone's **away-from-home origin** via Serve, and optionally a **pairing shortcut** (point 3).
- **Tagged nodes such as host-b** stop being a special case. Their key is their identity; they pair once with a code (the whois shortcut does not apply to them, since a tagged node has no user). For the browser over Serve on a tagged node, `ownerLogin` is set in its config, as V0.14 already allows.

## Threat model

| Threat | What stops it |
|---|---|
| **Someone else on the LAN** connects to the host port | mTLS: no roster certificate, no route. Only `/.well-known/fyr` (`{fyr, id}`) and the pairing endpoint answer, and pairing needs a live code |
| They try to guess a pairing code | CPace: one online guess per attempt, no offline attack on a captured transcript; three failures cancel the code; codes live 5 minutes and are single use |
| They open the front's page | no session cookie, no data: the page shows "This device is not paired" |
| They sniff the LAN | TLS everywhere; mDNS shows only the id and port |
| **A spoofed mDNS answer** (or a wrong typed URL, a hijacked DNS name) | a candidate is only an address. The handshake must prove a roster key, so a fake gets no link and sees nothing; a new id shows as "not paired" and needs a code |
| A man in the middle at first pairing | the PAKE binds the code to both ids; without the code the attacker cannot finish either side |
| **A stolen phone** | its session is revoked from any other device (Settings → Devices); the cookie is HttpOnly, so a page script cannot copy it; unused sessions end after 90 days. The phone holds no install key, so it cannot join the roster |
| **A stolen or dead machine** | Remove it on any other install; the removal spreads and every link to it is refused. Its own data is as safe as its disk |
| **A host that is gone** (reinstalled, replaced) | its old id goes offline and ages out of the merged state after 30 days (0021 (7)); trust stays until the owner removes it. A reinstall has a new key and must pair again; it cannot take the old one's place by name |
| **A replay** | TLS 1.3 sessions (no 0-RTT); pairing codes and device tokens are single use; roster statements carry a counter per signer; commands are idempotent by `commandId` (V0.2) |
| A rogue install that was once trusted | it can sign roster changes until it is removed: the same blast radius as a compromised tailnet node today. The owner removes it from any other install |

Out of scope: several owners, ACLs, an attacker with root on a trusted machine.

## Migration from V0.123, and the slices

The live setup keeps working at every step: the old whois listener and Serve stay until slice 6.

| Slice | What | Shippable because |
|---|---|---|
| **1 (small)** | Install key and id. `/.well-known/fyr` adds `id`; Settings → Hosts shows each host's short id on desktop and phone; the front remembers the id it first saw per host and warns when it changes | no trust changes; the owner can see the ids before anything depends on them |
| 2 | Roster and pinned mTLS. A second host listener with TLS pinned to the roster. Over the tailnet the whois shortcut pairs the owner's untagged installs with no press, and fronts move their links to TLS | today's machines migrate with no action; whois remains the fallback |
| 3 | Pairing by code: CPace, **Pair…** in Settings (desktop and phone), `fyrd pair` for headless hosts; add a host by URL. host-b (tagged) pairs here | a LAN or a tagged node now works between fyrds, with hand-typed addresses |
| 4 | mDNS source `_fyr._tcp`; unpaired candidates in Settings; a paired id followed at any address | LAN discovery with no typing |
| 5 | The browser off the tailnet: config `tls` for the install's own certificate, device pairing by QR, Settings → Devices with Revoke (desktop and phone) | a phone on the LAN with no tailnet runs the PWA with push, given a certificate it trusts |
| 6 | Removal and rotation spread through the roster; the plain whois listener is dropped; tailnet discovery takes tagged peers | one transport; the tailnet is only a route |
| later | Rendezvous or relay for NAT; a built-in local CA or ACME client; passkeys | named only |

## Consequences

- 0016's "Front auth: tailnet identity" becomes "roster keys between fyrds, device sessions or tailnet identity for browsers".
- 0021 (6) (the tailnet assumption and `transport`) is replaced by points 1 and 4.
- 0025 (1)'s proof (`owner` in the well-known answer, whois both ways) is replaced by the key handshake; 0025's "untagged only" filter is dropped in slice 6.
- New device data on the front: the device sessions. New install data: the key and the roster.
- The `hosts` config gains `id` for a host added by hand; an entry without one is pinned at its first pairing.
