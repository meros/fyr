# 0021 Dual installs by default, and how a front learns its hosts

Date: 2026-11-15 · Status: accepted (owner, 2026-11-15: "most installs should be dual... Simply because it's an easier model. Then we add a mechanism for discovery and routing to other hosts from all clients... Either static list or autodiscovery"; "dual by default but fully possible to run either or. Hosts have the other-hosts-config and autodetection"; "Keep the assumption for now" on the tailnet) · Amends 0016 and SPEC V0.54 · The spec rows follow in a later change

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

1. **Dual by default, single is a real mode.** The owner, 15 Nov: "dual by default but fully possible to run either or". The `both` role is the default for every install, so one process serves its own client and runs its own threads, and the ordinary case needs no second machine to make sense. A `host`-only install runs threads and serves no page; a `front`-only install serves a client and runs no thread, and is what a device with no work of its own uses. A front-only front must have at least one host; a host that runs alone with no front is reached by its own `both` sibling or shows nothing, and the startup log says so. Every mode goes through the same host protocol, so a `both` install is not a special case with a shortcut: it reaches its own host part over a loopback link, as V0.51 (1) says.

2. **The front is the routing table.** Which threads a device sees is the front's list, not the network's contents. Installing the client from the laptop's origin or the server's is the owner's choice per device, and both see the same threads, because both fronts list the same hosts.
3. **Each process has an other-hosts config, and autodetection fills the list.** The owner, 15 Nov: "Hosts have the other-hosts-config and autodetection". Both roles keep the same list, `hosts: [{name, url, transport}]`, in their own config:
   - **A front's list** is what it merges and routes: every host in it appears in the client's inbox and receives the commands for its own threads.
   - **A host's list** is its mesh (point 7): the peers it dials, whose facts it reads and to which it relays idempotent commands.
   Autodetection reads tailscaled's peer list, asks each peer for `GET /.well-known/fyr` (which answers `{name, role, build}`) and offers the peers that answer as candidates in Settings → Hosts. A candidate is added with one press and then behaves exactly like a typed entry. Discovery never adds anything by itself and never changes a route: the stored list decides where a command goes, so the owner can read it.

4. **Device data stays per front.** Push subscriptions, presence and the cached state of each host belong to the front that holds them, as 0016 and V0.54 (1)–(2) say. Two fronts are two device sets for push and for the "watched" rule; the threads and their inbox state are shared through the hosts.
5. **The tailnet assumption stays, and is named.** A host accepts a front only when tailscaled's whois says the peer belongs to the owner. Discovery is tailnet-only. The front's host entry carries a `transport` field whose only value today is `tailnet`, so a later direct link (TLS plus a secret per host) is a new value, not a rewrite. No such link is built now.
6. **A lost host ages out.** A front keeps a host it cannot reach, with that host's cached threads marked offline and read-only (`internal/front/cache.go`), and forgets it after **30 days** without a successful link. Forgotten means: its cached threads leave the merged state and the counts, its cache file is removed, and the host stays on the front's list so a host that comes back is picked up again with a fresh snapshot. The owner can also remove a host from the list at any time, which forgets it at once. The timeout is a device preference of the front, stored with its other device data, so a front that watches many machines can keep them longer.
7. **Hosts form a mesh, and nothing moves between their records yet.** The owner, 15 Nov: "It's good if hosts form a mesh, for other future reasons as well". Every host dials every other host on the front's list over the same tailnet link a front uses, and each pair can reach each other through a third: the mesh is how a front reaches a host that is not directly reachable, and how a host will later reach its peers. What crosses the mesh now is **read-only facts**, never a record: a host's status (online, build, a thread's state), and relayed traffic that is already idempotent by `commandId` (V0.2, I7), so a relay that runs twice is one command. Every record still has one owner (0016), and no host writes another's. A thread's move between hosts, when it comes, stays a front-mediated transfer (export, import, mark moved) until a record protocol with one writer at each step is designed in its own decision.

**Consequences.**

- V0.54's wording about a front per device set is replaced: a device installs one front, and dual is the ordinary case.
- Settings → Hosts gains the candidate list and the add-by-URL path, and each row shows its transport.
- Every install that serves a client also needs a reachable origin of its own (the tailnet name of the machine, or localhost): the front's origin is what the browser stores its preferences under.
- The front's cache is still per host and per front. Two fronts on the same machine may cache the same host twice; that is device data, not a second copy of the record.
- A decision to accept clients outside the tailnet becomes a separate record, because it changes point 5 alone.
- The other-hosts config appears twice with different jobs: a front's list is the client's world, a host's list is the mesh. In `both` one stored list may serve both, and the record says which entries are fronts and which are hosts through the `role` the well-known answer gives.
- Blast radius: a compromised or broken host can reach every other host on the mesh, so the whois check (point 5) and the rule "facts, not records" carry more weight than they did when only a front could reach a host.
