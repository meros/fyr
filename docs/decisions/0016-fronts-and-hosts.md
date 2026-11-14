# 0016 Fronts and hosts: no hub, one owner per record

Date: 2026-11-14 · Status: accepted (owner, two-round walk-through) · Builds on 0002, 0015 · Amends FYR-V1 v1.2

**Context.** A second host (meros-server) is coming. The owner named the two problems: "info sharing and who serves the client". The first proposal was a hub: one `fyrd` serves the client and the other hosts dial in. The owner rejected it: "I think it's not correct to have the *host* also serve the *frontend*? Maybe the laptop should serve from localhost and phone from meros-server? Both configured with one thing only: what hosts to connect to."

**The model.**

| Role | Does | Keeps |
|---|---|---|
| Host | runs threads; owns its folders, workspaces, providers, keys and quota readings; serves no page | its records (the only copy) |
| Front | serves the client; connects to the hosts in its list; merges their state; sends each command to the host that owns the record; sends push or desktop notifications | device data only (push subscription, appearance, a cache of the last state per host) |

One program, `fyrd`, runs as a host, a front, or both. The laptop runs both (its front on localhost). meros-server runs both, and its front serves the phone.

**Decisions.**

| Question | Decision |
|---|---|
| Who serves the client | **A front, configured with a list of hosts only.** No hub. Each front sees all hosts it lists. |
| Info sharing | **One owner per record.** A thread, its events and its inbox state live on the host that runs it. A provider and its keys live on their host; keys never leave it. No record has two writers, so there are no merge rules. |
| Projects | **Single-host for now** (owner: "start out with projects being single host"). A project lives on one host with its folders. Fronts show projects from all hosts. A project across hosts is a later decision. |
| Inbox state | **On the thread's host**, so the phone and the desktop agree. |
| Front auth | **Tailnet identity.** Hosts listen on the tailnet only and accept peers that the owner owns (Tailscale whois). No tokens to copy. |
| A host is offline | The front shows that host's threads from its cache, marked "meros-laptop offline since 19:40", read-only. |
| No hub fallback | Not needed: there is no hub. Each front works with whatever hosts it can reach. |
| First slice | **Split front and host on one machine.** The front talks to the local host only through the host protocol. Nothing changes for the owner; the protocol is proven before a second machine depends on it. |

**Consequences.** The host protocol (state stream with snapshot + patches, commands, event fetch, previews and attachments) becomes the boundary that 0002 put in `web/src/api.ts`; it moves to the front-to-host link, and the browser-to-front API stays as it is. Every id the client sees carries its host (a thread id stays unique; the front keeps an id-to-host map). Settings → Hosts lists the front's hosts with their state. The deploy drain (V0.24) is per host. FYR-V1's v1.2 mesh with replicas and moves is replaced by this model; moving a thread between hosts stays out of scope (Hand over… stays on one host).
