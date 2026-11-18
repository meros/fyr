# Decision records

One file per decision: context, options, decision, consequences. A decision that changes the spec names the spec section; the spec links back. Discussion happens in a `type:decision` issue; the file is the result.

The owner decisions D1–D5 from the design phase stay in `../SPEC.md` 0.3. Records here start at 0001.

| # | Decision | Status |
|---|---|---|
| [0001](0001-spec-plan-status.md) | Spec in the repo, plan and status in GitHub issues, red/green per change | accepted |
| [0002](0002-v0-transport.md) | v0 uses HTTP + JSON + SSE on localhost; ConnectRPC and tsnet come in M0 | accepted |
| [0003](0003-fake-agent.md) | Tests drive a scripted fake `claude` over the real protocol | accepted |
| [0004](0004-t3-adoption.md) | Adopt open T3 threads by Claude session id, history from the session transcript | accepted |
| [0005](0005-android-pwa-first.md) | Phone client: PWA first, no APK; adb + Obtainium if an APK is needed | accepted |
| [0006](0006-rail-direction.md) | The v0 client uses the Rail direction | accepted |
| [0007](0007-nix-integration.md) | NixOS owns the unit, profiles, launcher and a seed build; dev builds live outside the store | accepted |
| [0008](0008-projects-are-topics.md) | A project is a topic and the folder is a hint; no auto projects (amended by 0012, then replaced in part by 0017) | accepted |
| [0009](0009-message-delivery.md) | Message delivery follows the CLI's own queue: `command_lifecycle` receipts, `cancel_async_message` | accepted |
| [0010](0010-live-views.md) | Live views: a page plus a merge-patched state, kept current by a once-per-run Stop hook | accepted |
| [0011](0011-visual-pacing.md) | Visual pacing: the outcome at full weight, the work muted and folded into one line per stretch | accepted |
| [0012](0012-projects-folders-picker.md) | Projects with folders, an automatic repo link and a host folder picker (amended by 0017) | accepted |
| [0013](0013-handover.md) | Handover: the owner moves a thread to another profile; resume, else the new agent reads the old conversation, else a handover the old agent writes | accepted |
| [0014](0014-quiet-hours.md) | Quiet hours per project: hidden from the inbox and no push; back silently; reachable through the project filter; addendum: several windows, presets, 24-hour times | accepted |
| [0015](0015-hosts-providers-models.md) | Hosts, providers, models, projects and workspaces: a provider is a CLI adapter, an auth, an endpoint and its models, per host | accepted |
| [0016](0016-fronts-and-hosts.md) | Fronts and hosts: no hub, one owner per record; each front connects to a list of hosts | accepted |
| [0017](0017-a-project-is-one-folder.md) | A project is one folder | accepted |
| [0018](0018-changes-are-records.md) | Changes are records: one model for git and non-git changes, six agent tools, facts beat claims, git observed first (identity, upkeep and attention replaced by 0024) | accepted |
| [0019](0019-worktree-per-change.md) | A worktree per change: parallel changes get their own worktrees, stacked ones branch from the change they build on, multi-repo changes are a group | accepted |
| [0020](0020-state-machine-and-agent-feed.md) | One state machine beside the agent: records, one reducer path, effects run by Fyr, Rules, and one feed of notices to the agent | accepted |
| [0021](0021-dual-installs-and-host-discovery.md) | Dual installs by default; a front's static host list routes, the tailnet only suggests candidates; tailnet identity stands | accepted |
| [0022](0022-keep-the-drain-with-a-forced-restart.md) | Keep the drain for server deploys; the owner can force a restart, and each cut turn gets "continue" (the drain stays only without a runner, 0023) | accepted |
| [0023](0023-a-runner-holds-the-agent-processes.md) | A runner in the launcher holds the agent processes; it only stores numbered lines, and all smarts stay in fyrd | accepted |
| [0024](0024-changes-v2-one-card-per-work.md) | Changes v2: one card per piece of work through one handle table; Fyr owns the facts, the agent only intent; code is done at merged; Needs you is computed; wake on failures and ask results only | accepted |
| [0025](0025-fronts-find-and-mesh.md) | Fronts find the owner's Fyr installs on the tailnet and follow each one automatically; the fronts mesh, hosts dial nothing, and the client keeps one uplink | accepted |
| [0026](0026-keys-not-networks.md) | An install is its key: discovery from many sources (tailnet, mDNS, by hand) only proposes; trust comes from PAKE pairing and a shared signed roster; fyrd links are mutual TLS pinned to the keys; the browser path is the install's choice; the tailnet is one route (amends 0016, 0021 (6), 0025 (1)–(2)) | accepted |
| [0027](0027-one-updater.md) | Auto-update installs only CI-built, signed releases (the tag `deployable`, batched, smoke-tested, health-checked, rolled back); a local build of a checkout is an explicit, pressed developer switch that pauses auto-update (amends V0.13 (2), V0.111) | accepted |
| [0028](0028-who-may-act.md) | Who may act: every browser holds a session minted locally (owner socket, `fyr open`), by the tailnet (Serve socket only) or by a code; loopback is no longer the owner; a host decides which fronts drive it; step-up for trust changes; agents with a shell are named as the owner (builds on 0026; owner answered Q1–Q4) | accepted |
| [0029](0029-adaptive-slots.md) | Adaptive slots: admit heavy jobs by a budget of learned costs (dominant share of CPU demand and memory peak), gated by CPU PSI and memory headroom; one job always runs; EASY backfill; the newest job frozen under memory pressure ; machine-independent: shares of the host's capacity, a slowdown probe on every host, PSI and cgroups where present (amends V0.86; owner answered Q1–Q4) | proposed |
| [0030](0030-every-hop-names-who-acts.md) | Every hop names who acts: a review of every way in; one authenticator per listener; a front forwards only with an explicit principal (`Fyr-Actor`) and hosts run no browser guard; installs link by TLS pinned to keys and join a signed roster only by the same tailnet user (through Serve), a member's voucher or the owner's invite; browsers hold a session got by tailnet identity, a link or QR, or an approval code; stored observe/enforce modes that no release changes; a test plan per way in and per threat, with a fake tailscaled (supersedes parts of 0025, 0026, 0028) | accepted |
| [0031](0031-the-client-names-the-host.md) | The client names the host: a review of every place the client or the front assumes a host; every read, command, stream frame, upload and preview is addressed by install id in the path (`/api/h/<hostId>/…`, the host protocol unchanged behind it); no default host; front-wide things under `/api/front/…`; one stream with the host on every frame; client state keyed by (host id, record id); the front's own host is one entry in the list (builds on 0016, 0025, 0026, 0030) | accepted |
| [0032](0032-bounded-agents.md) | Bounded agents: one thread's heavy work cannot starve the host; the guard freezes a long episode in time slices, releases a freeze that holds nothing, and logs each decision; later a small memory budget per thread that a reservation raises | proposed |
