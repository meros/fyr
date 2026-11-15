# 0021 Dual installs by default, and how a front learns its hosts

Date: 2026-11-15 · Status: accepted (owner, 2026-11-15: "most installs should be dual... Simply because it's an easier model. Then we add a mechanism for discovery and routing to other hosts from all clients... Either static list or autodiscovery"; "Keep the assumption for now" on the tailnet) · Amends 0016 and SPEC V0.54 · The spec rows follow in a later change

**Context.** 0016 split the program into hosts (run threads) and fronts (serve the client) and left two questions open for a second machine: which front a device installs, and how a front knows which hosts exist. V0.54 answered the first by accident: its text reads as though a front belongs to a device set (the laptop's localhost front, the phone's server front), which means two origins, two installed apps and two push sets.

The owner's finding, 15 November: **dual should be the default**. Every install runs a front and a host (`-role both`, already the default), and a client talks to one front, which merges the threads of every host on its list. No device has to choose an origin to get one inbox, and a second machine adds a line to a list rather than a new topology.

The second finding: discovery is **static list plus a tailnet assist**. The list is what routes commands, so the owner can read where a command will go; the tailnet only suggests candidates.

**Options.**

| Question | Options | Chosen |
|---|---|---|
| Which front a device installs | one canonical front; a front per device set (V0.54 as written); dual by default | **dual by default** |
| How a front learns its hosts | static list only; static list plus a tailnet assist; full tailnet discovery | **static list plus a tailnet assist** |
| Identity between front and host | tailnet whois; TLS plus a shared secret per host; no check | **tailnet whois, unchanged** |

**Decision.**

1. **Dual by default.** The `both` role stays the default for every install: a laptop, a server, a spare machine. Each runs a host part that owns its threads and a front part that serves the client at one origin. A client talks to exactly one front; the front merges every host on its list and routes each command to the thread's owner. The client discovers nothing and holds no host list.
2. **The front is the routing table.** Which threads a device sees is the front's list, not the network's contents. Installing the client from the laptop's origin or the server's is the owner's choice per device, and both see the same threads, because both fronts list the same hosts.
3. **Discovery assists, never routes.** A front reads tailscaled's peer list and asks each peer for `GET /.well-known/fyr`, which answers `{name, role, build}`; a peer that answers becomes a candidate in Settings → Hosts, added with one press. A candidate that is not added changes nothing. A host can also be added by typing its URL. The front's stored list remains the only source of routing.
4. **Device data stays per front.** Push subscriptions, presence and the cached state of each host belong to the front that holds them, as 0016 and V0.54 (1)–(2) say. Two fronts are two device sets for push and for the "watched" rule; the threads and their inbox state are shared through the hosts.
5. **The tailnet assumption stays, and is named.** A host accepts a front only when tailscaled's whois says the peer belongs to the owner. Discovery is tailnet-only. The front's host entry carries a `transport` field whose only value today is `tailnet`, so a later direct link (TLS plus a secret per host) is a new value, not a rewrite. No such link is built now.
6. **A lost host ages out.** A front keeps a host it cannot reach, with that host's cached threads marked offline and read-only (`internal/front/cache.go`), and forgets it after **30 days** without a successful link. Forgotten means: its cached threads leave the merged state and the counts, its cache file is removed, and the host stays on the front's list so a host that comes back is picked up again with a fresh snapshot. The owner can also remove a host from the list at any time, which forgets it at once. The timeout is a device preference of the front, stored with its other device data, so a front that watches many machines can keep them longer.
7. **Nothing crosses host to host.** Every record has one owner (0016). A front merges and proxies across hosts; hosts never talk to each other. A thread's move between hosts, when it comes, is a front-mediated transfer: the source exports, the destination imports, the source marks the thread moved, so no record ever has two writers.

**Consequences.**

- V0.54's wording about a front per device set is replaced: a device installs one front, and dual is the ordinary case.
- Settings → Hosts gains the candidate list and the add-by-URL path, and each row shows its transport.
- Every install that serves a client also needs a reachable origin of its own (the tailnet name of the machine, or localhost): the front's origin is what the browser stores its preferences under.
- The front's cache is still per host and per front. Two fronts on the same machine may cache the same host twice; that is device data, not a second copy of the record.
- A decision to accept clients outside the tailnet becomes a separate record, because it changes point 5 alone.
