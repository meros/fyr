# 0025 Fronts find the hosts on the tailnet and mesh; the client keeps one uplink

Date: 2026-11-16 · Status: accepted (owner, 16 Nov, in the multi-host thread) · Amends 0016 and 0021 (points 3, 5 and 8) · SPEC V0.123 · Superseded in part by 0030 ((1) proof and (2)), once 0030 is accepted

**The owner's words, 16 Nov, in order:**
- "Start building multi host... The fyrd should detect other hosts on the tailnet and hand to client. Client maintains connection to all."
- "The hosts will likely need to mesh with each other and relay traffic to some degree..."
- "I think maybe it's the hosts that maintain connections, not the client. The client should only have one uplink."
- After seeing the diagram: "Yes this is good! It's the fronts that mesh of course!"
- On whether the host part dials its peers: "Only frontend fyrd."

The owner answered three questions while the client was still meant to connect to every host. Two of the answers still hold: discovery adds a host automatically, and push stays on the installed front. The third chose tailnet identity for the browser over tokens or pairing. That stays true without changing anything, because the browser still reaches only its own front through Tailscale Serve.

**Context.** 0016 split fyrd into a host, which runs threads, and a front, which serves the client and follows a list of hosts. 0021 kept the list static and made the tailnet only a source of suggestions, which one press adds. 0021 also planned host-to-host links, the "mesh", for facts and relayed commands. The owner now wants fyrd to find the other Fyr installs on the tailnet by itself. The connections stay between fyrds, never from the client. The meshing is done by the fronts.

**What changes against 0016 and 0021.**

| Topic | 0016 / 0021 | Now |
|---|---|---|
| How a front learns its hosts | a static list; tailnet candidates need a press (0021 (3)) | **the front's fyrd finds them and follows each one automatically**. The static `hosts` list stays, for hosts outside the tailnet and for development |
| Who dials whom | a front dials its hosts | unchanged, and now also every host it found. **The fronts mesh**: each front follows every other install's host |
| Host-to-host links (0021 (8)) | every host dials every other host for facts and relays | **dropped**. A host part dials nothing. "Only frontend fyrd." |
| The client | one front, one uplink (0021 (5)) | unchanged: **one uplink**, to the front the device opened |
| Identity | tailnet whois between front and host; Serve's login header for the browser | unchanged |

## Decision

**1. Discovery runs in the front.** Every fyrd that runs a front, in role `both` or `front`, looks for other installs at start and then every 60 s. An install in role `host` does not look.
- **Candidates.** tailscaled's status, filtered to peers that are online, untagged and owned by this node's own Tailscale user (`Peer[].UserID == Self.UserID`). The static origins in config `peers` are candidates too, and they need no tailnet.
- **Proof.** A candidate must answer `GET https://<its MagicDNS name>:<port>/.well-known/fyr` (port from `peerPorts`, default 7443, which is Tailscale Serve's port in the NixOS module) with `{fyr: 1, name, role, build, owner, host}`, where `owner` is this fyrd's owner login.
  - tailscaled vouches that the node is the owner's.
  - The peer's Serve and guard (V0.14) check the prober's login before the peer answers.
  - The answer names the same owner, so the check holds in both directions.
  - `host` is the URL where the peer serves the host protocol (its `hostListen`, V0.51). It is empty when the peer serves no host protocol.
- **How often.** At start and then every 60 s. Each candidate gets one probe of at most 3 s, and the probes run in parallel.
- **Hosts that come and go.** A found host that stops answering stays listed as offline. The front's link shows it offline and keeps its threads read-only from the cache, as V0.51 (4) and V0.54 (2) do. It is forgotten 30 days after its last answer, as 0021 (7) says.

**2. The front follows what it finds.** A found install with a `host` URL is added to the front's hosts while the front runs, as `Remote` (so the browser's identity headers are not passed on), and is followed exactly as a configured host is followed.
- An entry that matches a configured or already-followed host by name or by URL is not added twice.
- The host side is unchanged: its tailnet listener serves only the owner's login (whois, V0.51 (5)).
- Discovery never changes a route on its own. A thread's commands still go to the host that owns the thread (V0.51 (3)).

**3. The client keeps one uplink.** The browser talks only to the front it opened, through Serve, with tailnet identity and no tokens. That front hands over the hosts it follows in the state's `hosts[]`, and each row there carries `found` when discovery added it. Settings → Projects → Hosts shows each row's status, the same control on the desktop and the phone (V0.119). The client needs no CORS, no host list of its own and no extra connections, so a phone with several hosts uses the same battery and data as with one.

**4. What stays on the front.** The front keeps everything it kept before:
- push and presence (V0.54);
- the cache per host;
- the client build and its update banner;
- the project order (V0.121). It is stored in the front's own database under the kv key `front.projectOrder`, and the front applies it to the merged list.

**5. Failure.** An offline host is shown as V0.51 (4) shows it: "<host> offline since HH:MM · read-only" on its threads and on its row in Settings → Hosts. The front reconnects to it with its existing backoff, starting at 0.5 s, doubling, and stopping at 15 s.

**6. Migration and slices.**
- **Slice 1 (V0.123).** Covers:
  - discovery in the front;
  - `/.well-known/fyr` with `host`;
  - the front following the hosts it finds while it runs;
  - `found` in `hosts[]` and in Settings;
  - a `hostListen` on loopback, behind the loopback guard, so two installs on one machine can follow each other in development;
  - config `name`, so two such installs have different names.

  Existing configs keep working. A configured `hosts` entry and a found host with the same name are one link.
- **Later.**
  - Drop the static `hosts` entries from the NixOS configs once discovery has found the same hosts.
  - Keep found hosts across a front restart. The front's cache already does this for the threads.
  - A relay through a third front, for a host that one front cannot reach directly, if a network ever needs it.

**Consequences.**
- Each machine that should be found publishes its page with Serve (`services.fyr.tailnet.enable`) and serves the host protocol on the tailnet (`hostPort`). A host-only install is not found and is configured by hand.
- 0021 (3), "discovery never adds anything by itself", is replaced by point 2.
- 0021 (8), the host-to-host mesh, is replaced: the fronts mesh, and hosts dial nothing.
- 0021 (5) and (6) stand.
