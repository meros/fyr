# Plan

Status lives in GitHub: https://github.com/meros/fyr/milestones. This file holds the milestone definitions, their order and exit criteria only.

## Where we are (21 Nov 2026)

The original goal of v1 was "the owner works only in Fyr". It has been met for weeks, and the repo is public with a README, an INSTALL guide and an MIT licence. On 20–21 Nov most of the blocking v1.0 groundwork shipped: Linux without Nix (#199), share-based resource defaults (#198), plain-HTTPS releases without `gh` (#200), versioned `vX.Y.Z` releases & channels (#447), `Fyr.app` / `Fyr.dmg` + Setup wizard (#434), `fyr-desktop` on Linux (#461), Decision 0031 final route cleanup (#372), all six blocking Solid core 2 items (#450–#455), plus the Antigravity (`agy`) CLI adapter (#492), trans-host handover (#503), multi-PR tracking (#504), per-project approval judge (#507), quota waiting & reset waking (#502, #518, #519), and 35 runtime thread invariants (#520, #526). Remaining v1.0 blockers are non-tailnet owner setup (#197), README trust model (#203), fresh-machine verification (#30, #259, #236), and restart-survival audit (#156).

| Milestone | State | What happened |
|---|---|---|
| Solid core | Closed | Exit met. |
| Two machines | Done, ready to close | #21 and #22 shipped, along with per-host providers, models, update state and LAN pairing. #30 (a real login) moves to v1.0. |
| Projects and workspaces | Done except two | Worktrees for each change (decision 0019: #375, #376, #354), and threads are filed by folder. Archiving a project (#24) and the folder in the header (#26) move to v1.0. |
| Conversation and live view | Done except two | Missed read events fixed (#29), and the View-tab reply shipped (#20). #19 (background on a new message) moves to v1.0. The live-view cost (#17) becomes a one-off query after v1.0. |
| M4 Calm machine | Done past its scope | The guard far exceeds the plan: decision 0036 slices 0–6, adaptive admission, freeze and thaw, kill and rerun for opted-in classes, invariants with a Fix it card, a simulation over 17 scenarios. A forced memory hog is frozen and shown, not hidden. Not built: batching several pushes into one, the no-progress signal, one summary push when quiet hours end, and the policy file. The policy file is superseded by async effects, where the agent asks and the owner approves: Run cards, merge asks and the heavy-command gate. |
| M5 Cut-over | Met. Of the original checklist, A6 is accepted as built ("Since you looked" on the status screen); ⌘1–9 stable slots (A4) and the Android share target (A3) are dropped; the paste chip and the photo picker stay (owner decisions 20 Nov) | Weeks of daily work only in Fyr. The T3 bridge is removed (#68). |
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
| #447 | Versioned releases: a `vX.Y.Z` tag and changelog beside `build-<sha>`, and a stable update channel that follows tags while main follows builds |
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
| #448 | The no-progress signal: a run with no tool call for 20 min becomes blocked (the M4 leftover) |
| #449 | One summary push when quiet hours end (A10; owner decision 20 Nov), and several pushes batched after 10 min away (the M4 leftovers) |
| #26 | The folder chip in the header, and a tap that shows path, branch and Copy path |
| #24 | Archive a project and its threads |
| #61 | The desktop filmstrip folds like the phone's, with a hide for each thread |
| #55 | Jump to the previous or next owner message and to the cards that need the owner |

### Track D: Solid core 2 (the model a stranger's setup meets)

An architecture review on 20 Nov (backend, web client, agent tools) found Solid core mostly holds. Every runtime change still goes through one reducer, nothing reads `Thread.PR`, and the layering is clean. Three things have drifted: the agents' tools, the client's action list, and the spec. The resource guard has no single owner. Owner decision, 20 Nov: the first six rows block v1.0 (fitted instructions, one tool vocabulary, one action list, slice 2b, host group codes, the spec restated). The last three are should-haves.

| Item | What | Size |
|---|---|---|
| #450, blocks | **Instructions that fit the setup.** Build the agent's instructions from facts Fyr already has (git or not, a GitHub remote, gh signed in, OS, pkexec) and drop the advice that does not apply. A merge ask without GitHub says so. Put the key rules where subagents see them (MCP `instructions`) | M |
| #451, blocks | **One tool vocabulary.** The `gh pr merge` gate names `change_request`, not `effect_request`. Fold `effect_request` into `change_request` and make deploy owner-only (`fyr.deploy` is requestable by every project's agent). Fold `settle_when` into `watch`. Remove `slot_acquire`/`slot_release` and the `pr_link` alias. Retries of `change_request` and `watch` are idempotent. A test checks that every `mcp__fyr__X` in prompts, gates and feed is a registered tool | M |
| #452, blocks | **One action list for both fronts.** One `threadActions(t)` behind the desktop context menu, the ⋯ menu and the phone sheet, which have drifted apart (the phone lacks Unsettle, Unsnooze, Unarchive, Stop run, Copy link). A test checks every action is reachable on both fronts | M |
| #453, blocks | **Decision 0031 slice 2b in the client.** State for each host, a required host id, and no `hostIdByName` or first-host fallbacks (about 22 `hostId ?? ""` sites). Makes #372 a deletion | L |
| #454, blocks | **Rows grouped by a code from the host**, not by parsing the English reason (`web/src/rows.ts:50-113`) | S |
| #455, blocks | **SPEC 3 and 5.12 restated from the code.** The tool table names tools that do not exist (`change_revise`, …) and leaves out ones that do; the front's own vocabulary and the guard's runtime state are named exceptions to "every change goes through 5.1". This was Solid core's exit criterion | S |
| #456 | **One versioned migration step at store open.** Drop migrations from before ~V0.100 and the `leakedFixture` hack, with a fixture test of an old database | M |
| #457 | **The mock checked against a real host.** Record each command's attention change on a scratch fyrd and assert the mock does the same; the layout browser test runs on the mock | M |
| #458 | **`internal/guard`.** One policy (`Tick → decisions`, admission included) over a `Mechanism` that `cgroup` implements and macOS implements with the process table. This moves the policy out of `engine/slots.go` and the closures out of `cmd/fyrd/main.go`. It follows the decision 0036 acceptance work and helps #236 and #198 | L |

After v1.0: the Claude seam (`cli.Session` emitting Fyr events instead of `claude.Msg`) unless events reach the wire; the guard's state as records; splitting `settings-ui.tsx` and sharing the question card and composer between the fronts.

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
