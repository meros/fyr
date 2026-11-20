# Plan

Status lives in GitHub: https://github.com/meros/fyr/milestones. This file holds the milestone definitions, their order and exit criteria only.

## Where we are (20 Nov 2026)

The original goal of v1 was "the owner works only in Fyr". It has been met for weeks, and the repo is public with a README, an INSTALL guide and an MIT licence. What v1.0 lacks is not features but **a stranger's path**: Fyr still assumes NixOS or a Mac with a dev toolchain, a tailnet for the owner's identity, defaults sized for one machine, and an API with routes due for removal. Releases are named `build-<sha>`, so nothing tells a user what is stable.

| Milestone | State | What happened |
|---|---|---|
| Solid core | Closed | Exit met. |
| Two machines | Done, ready to close | #21 and #22 shipped, along with per-host providers, models, update state and LAN pairing. #30 (a real login) moves to v1.0. |
| Projects and workspaces | Done except two | Worktrees for each change (decision 0019: #375, #376, #354), and threads are filed by folder. Archiving a project (#24) and the folder in the header (#26) move to v1.0. |
| Conversation and live view | Done except two | Missed read events fixed (#29), and the View-tab reply shipped (#20). #19 (background on a new message) moves to v1.0. The live-view cost (#17) becomes a one-off query after v1.0. |
| M4 Calm machine | Done past its scope | The guard far exceeds the plan: decision 0036 slices 0–6, adaptive admission, freeze and thaw, kill and rerun for opted-in classes, invariants with a Fix it card, a simulation over 17 scenarios. A forced memory hog is frozen and shown, not hidden. Not built: batching several pushes into one, the no-progress signal, one summary push when quiet hours end, and the policy file. The policy file is superseded by async effects, where the agent asks and the owner approves: Run cards, merge asks and the heavy-command gate. |
| M5 Cut-over | Met | Weeks of daily work only in Fyr. Removing the T3 code (#68) moves to v1.0. |
| Mesh | Later | After v1.0. |

## v1.0: a stranger can install it, and it stays put

**Exit criterion.** Someone follows only the README and `docs/INSTALL.md`:
- on a fresh Ubuntu machine without Nix or Tailscale;
- on a fresh macOS user account, from `Fyr.dmg` without a terminal.

On each, they install a signed, versioned `v1.0.0` release, log in to Claude through Fyr, pair a phone on the LAN, and take a thread to a merged PR. The install updates itself to `v1.0.1` without `gh auth`. The owner's own daily use runs a week on the release candidate with no regression.

### Track A: install and run anywhere (blocking)

| Issue | What |
|---|---|
| #199 | Linux without NixOS: install from the signed release as a systemd unit with cgroup delegation that updates itself, with the agent PATH from the login shell |
| #198 | Resource defaults are a share of the machine, not 13G/14G and 4 cores; fyrd logs the budget it chose |
| #197 | The owner comes from config or a first-run step, not from the Tailscale login |
| #200 | Public releases are read without `gh`; the release repo and key are config shown in Settings → Updates; the launchd label is neutral |
| #434 | The Mac app: Fyr.app in a DMG (self-signed for now), registered with SMAppService, in a native window with notifications, plus a setup wizard that reaches a first thread. Owner decision, 20 Nov: in v1.0 |
| new | Versioned releases: a `vX.Y.Z` tag and changelog beside `build-<sha>`, and a stable update channel that follows tags while main follows builds |
| #203 | README: the trust model (decision 0028) |
| #30 | A real Claude login through Fyr, desktop and phone. A new user's first step, so it must work |
| #259 | Settings → Hosts lists LAN candidates with Pair or Ignore, plus one real multicast run. Without a tailnet, this is how a second install appears |
| #236 | The guard on a Mac and on Linux without cgroups, with a real agent thread |

### Track B: settle the surface before 1.0 (blocking)

| Issue | What |
|---|---|
| #372 | Decision 0031 slice 5: the old routes and their fallbacks go, so `docs/API-v0.md` is the API 1.0 ships |
| #68 | The T3 import goes from code, data fields, docs and the Nix module comments |
| #156 | Audit what an update wipes; keep what must survive (the `ciRepos` cache and others), with a restart test each |

### Track C: daily-use gaps (should; cut if late)

| Issue | What |
|---|---|
| #19 | A new message backgrounds the running Bash command (the CLI's `background_tasks` request) |
| new | The no-progress signal: a run with no tool call for 20 min becomes blocked (the M4 leftover) |
| new | One summary push when quiet hours end, and several pushes batched after 10 min away (the M4 leftovers) |
| #26 | The folder chip in the header, and a tap that shows path, branch and Copy path |
| #24 | Archive a project and its threads |
| #61 | The desktop filmstrip folds like the phone's, with a hide for each thread |
| #55 | Jump to the previous or next owner message and to the cards that need the owner |

### Order

1. **A and B together, Linux first.** #199 → #198 → #197 → #200 → versioned releases → #434 (it builds on #200 and on versioned releases; the Swift shell can start earlier on the owner's Mac runner). In parallel: #372, #68, #156, which need no install work.
2. **The fresh-machine runs** (#30, #259, #236), on a VM and a new Mac account. A failure becomes an issue in this milestone.
3. **Track C** fills the gaps while 1 and 2 wait on review and CI.
4. **`v1.0.0-rc1`**: the owner runs it for a week. Then **`v1.0.0`**.

## After v1.0

| Milestone | Scope |
|---|---|
| v1.x Polish | Mobile pass with an adversarial review (#59); the bug button (#63); a mini conversation beside a running stack, as an experiment (#52); one query over stored turn usage to measure what a live view costs (#17) |
| Mesh | Hosts dial each other (decision 0021, point 8): facts and idempotent relayed commands, a host reachable through a third, and a thread move designed as a record protocol with one writer at each step. Exit: a host two hops away is reached through a third; no record has two writers; a thread moved between hosts leaves one record and arrives as one |

## Closed

- **v0 first viable** (SPEC 0.5): done 2026-11-14; the V0 rows after it shipped on `main` without a milestone.
- **M0–M3**: closed 2026-11-15. v0.x delivered most of their scope; fronts and hosts (decision 0016) replaced M0's ConnectRPC and tsnet pairing.
- **Solid core**: closed 2026-11-15. One action vocabulary, the PR on its change, one command model for desktop and phone.
- **Two machines, Projects and workspaces, Conversation and live view, M4, M5**: see "Where we are"; their open issues moved into v1.0 or later.
