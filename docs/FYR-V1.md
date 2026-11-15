# Fyr v1: a single-host Claude workbench that replaces T3 Code for daily use

Date: 2026-11-13 · Spec: `SPEC.md` (v0.6.1) · Screens: `../design/screens.html` · Design system: `../design/system/`

## In short

Fyr v1 is a small server on your desktop machine that runs Claude Code sessions, and a web app (desktop and phone) that shows one list: what needs you on top, running work below. It replaces T3 Code for the loops you use every day. It is single-machine, as you work today; several machines come in v1.2.

**Limits stated up front:**
- v1 runs on **one host**. The data model is ready for several hosts, but moving work between hosts is not built until its protocol is model-checked (round 4 found split-brain and deadlock paths).
- No coordinator agent, no software-factory workflows, no passkey approvals in v1. They are v1.3–v1.5 and the core already has the slots for them.
- Two assumptions are still unproven: the kernel OOM-kill at `MemoryMax` with swap off, and three control requests (`stop_task`, `mcp_set_servers`, `rewind_files`). v1 does not depend on them.

---

## 1. Why v1 looks like this

Measured from your T3 database, 3–13 Nov 2026:

| Fact | Number | What v1 does with it |
|---|---|---|
| Your own threads | ~18/day, 27 % started on the phone | Phone is a first-class client; capture with a screenshot in ≤ 4 taps |
| Running at once | median 1, p90 4, peak 13 | Working section expanded, one line per thread, live last activity |
| Thread visits | ~1,900/day | Switching speed and a trustworthy read model are the core UX budget |
| AskUserQuestion | 312 in 11 days, median answer 1.0 min, often 2–4 questions | Keyboard-first question card, push straight into it |
| Tool approvals | 0 (auto mode on 89 % of threads) | Sensitive effects run as async effects; blocking approvals only for tool calls Fyr cannot run itself (rare, minimal card) |
| PRs | 369 distinct, ~1 per thread | PR state on every row; watch CI and reviews; settle when merged |
| Manual settles | 180 (auto-settle on merge turned off by you) | Bulk settle, "settle when merged", no automatic merge-settle by default |
| Native subagents | ~90/day | Shown inside the parent's run, never as rows |
| Claude backends | 3 instances (two accounts, DeepSeek) | Agent profiles from day one |
| Pin, plan mode, worktree mode, scheduled tasks, peers | 0 or near 0 | Not in v1 |
| Browser preview tool calls | ~1,300 | Agents keep their own Playwright MCP in v1; Fyr shows the screenshots |

The agent protocol and the resource model were proven by spikes on this machine (section 9).

---

## 2. What "replaces T3" means: the acceptance checklist

v1 is done when all of these pass on your machine for one working week, with T3 closed.

| # | Loop | Target |
|---|---|---|
| A1 | Answer a 2-question ask from the inbox | `n`, `1`, `2`, Enter: 4 keys, next item opens |
| A2 | Answer from the phone push | Tap the push, 2 taps on options, Answer: ≤ 4 taps |
| A3 | Start a thread from the phone with a screenshot | ≤ 4 taps (iOS: paste chip or Shortcut; Android: share target) |
| A4 | Check a running thread | One key (`⌘1–9` stable slots) or one click; last activity visible without opening |
| A5 | Clear threads whose PR merged | Select with `x`, settle with `Shift+E`, one undo toast |
| A6 | Triage after a break | "Since 14:02" strip lists what changed; peek with Space does not mark seen |
| A7 | Restart the host during work | All sessions resume; no thread shows a wrong state; nothing silently disappears from Needs you |
| A8 | Machine under memory pressure (13 running threads) | No swap storm; background work queues; your own runs keep going |
| A9 | Merge a PR the agent asks to merge | Agent requests, you approve when you like, Fyr merges, the agent sees the result on its next turn |
| A10 | Silent hours for one repo | No pushes for that repo outside its hours; one summary push when the window opens |

---

## 3. Architecture v1

```
 phone PWA ─┐                       ┌───────────── fyrd (Go, one binary) ─────────────┐
            ├── HTTPS + ConnectRPC ─┤ tsnet listener · pairing · device tokens        │
 desktop PWA┘   (tailnet only)      │ command path → Action registry → apply (pure)    │
                                    │ SQLite: events · projections · receipts · rules  │
                                    │ Rule engine · Job runner · Attention (pure)      │
                                    │ modules: claude · git · github · governor ·      │
                                    │          push · hours · policy                    │
                                    └──────┬─────────────────┬──────────────┬──────────┘
                                           │ stdin/stdout     │ git, gh       │ systemd D-Bus
                                     claude CLI × N        repos, GitHub    user scopes, PSI
                                   (pinned by Nix)
```

**Decisions that hold for v1 and later:**

| Area | v1 choice | Why it does not block later versions |
|---|---|---|
| Daemon | Go, one static binary `fyrd`, ~30 MB RAM | Same binary runs every host in the mesh |
| Agent | Go-native client for the `claude` CLI control protocol (spike: 469 LOC, all core assumptions proven) | Agent registry keyed by kind; a TS bridge or other agent kinds fit behind the same contract |
| Store | SQLite, append-only event table + projections, receipts per CommandId | Events already carry `subject`, per-entity `eseq`, HLC `at`, residence and epoch |
| Protocol | Protobuf + ConnectRPC: `command`, `subscribe(stream, cursor)` with snapshot + upserts + events | Peers use the same streams in the mesh |
| Reach | `tsnet`: the daemon is its own tailnet node with HTTPS | Each future host is another node; one `clientOrigin` |
| Client | TypeScript PWA (React or Solid, TanStack Virtual), own components from the Fyr design system, IBM Plex self-hosted | Same client lists several hosts later |
| Packaging | Flake: `packages.fyr`, `nixosModules.fyr`, `homeManagerModules.fyr`; pins the `claude` CLI | Non-Nix: release binary + systemd/launchd unit + CLI version check |

**The one rule that keeps the UI consistent:** every operation is an Action in one registry. The palette, row menu, keys, bulk selection, agent MCP tools and Rules all call it through one command path, and every disabled control shows the reason `available` returned. Status comes from one pure function, `attention`, computed on the host. The client never computes status.

**The one rule that keeps effects safe (owner decision D5, async effects):** an effect Fyr can do itself (merge, deploy, force push, close issue, send a document) is never a blocking tool call. The agent requests it with `effect.request` (an MCP tool); that opens a Decision with the exact call and returns at once. When you approve, a Job runs exactly that call after re-checking it, and the result goes back into the thread as context for the agent's next turn. The agent's own GitHub token cannot merge (credential broker), so this is enforced, not advisory.

---

## 4. The core in v1

Implement all ten concepts, but only the parts v1 uses. The data model is complete from day one so later versions add behaviour, not migrations.

| Concept | In v1 | Deferred |
|---|---|---|
| Host | one host; `HostId`, HLC | peers, mesh, replicas |
| Entity | kinds `thread`, `decision`; residence, epoch, `eseq` stored | `coordinator`, module kinds, move |
| Ref, Link | `checkout`, `worktree`, `repo`, `pr`, `branch`, `file`; links `serves`, `owns`, `touches` | cross-host conflicts |
| Document | schemas `brief` (with Claims and evidence), `hours`, and `policy` (loaded from the static policy file at start) | `delegation`, `plan`, `grant`, `policy` as signed docs, `workflow.definition` |
| Event | full envelope; intent vs observation | `imported_events` |
| Action | registry, receipts, Guards, `calls` applied in sequence with each nested call checked | proposals |
| Rule | edge firing on stored value, atomic firing (I9), suspend reasons as a set (I3), deferred undo that never retries | cross-host Conditions |
| Job | runner, idempotent steps, at-most-once markers for outside I/O | `move`, `deliver` |
| Attention | full precedence table with v0.6 states | Coordinator rows |

**Attention states in v1** (renamed after the UX review):

| State | Meaning | Section |
|---|---|---|
| asking | A question or approval waits for you | Needs you |
| failed | The run failed (API, auth, MCP start-up, restart, memory stop) | Needs you; stays until you retry, send, settle or archive |
| blocked | Something outside the agent needs you: CI failed, review comments, merge conflict, or no progress for 20 min | Needs you |
| done | The agent finished and you have not looked | Needs you |
| working | A run is going | Working (expanded, one line, live activity) |
| waiting | Waiting for a build slot, a background task or a time | Working |
| seen | Your turn, already looked at | Seen |
| snoozed | Hidden until a time or an event | Snoozed |
| settled | Done for good | Settled (last 24 h; older through search) |

**Multi-user readiness (R19).** v1 has one user, but the data model carries what several users on one host need, so it never migrates:
- principal `user(userId, deviceId)`; devices, passkeys and push subscriptions belong to a user;
- every entity has `owner` and `visibility` (v1: you, private);
- work state (settled, archived) stays on the entity; the per-user View (snooze, pin, read watermark, failure acknowledgement) is per `(user, entity)`, and `attention` is computed per user; presence is per device and hours are per user;
- Decisions have an `assignee`; only the assignee can answer;
- agent profiles and credentials belong to a user; the host has an `admin` role; the governor counts slots per user.

**Invariants to test in v1** (from rounds 1–4): L1 snooze iff its Rule is live, L3 archived threads have no live work, I3 suspend reasons, I4 strengthened L1, I7 exactly once per CommandId, I8 answer binding to `payloadHash`, I9 atomic Rule firing, I10 every failure maps to exactly one attention row, I12 one blocker per run. Each invariant becomes a property test over random command sequences against the pure core.

---

## 5. Modules in v1

| Module | v1 scope | Not in v1 |
|---|---|---|
| `claude` | Sessions over the control protocol; resume after restart (model passed on every start, since `set_model` does not survive resume); AskUserQuestion → Decision; interrupt; steer vs queue; profiles (config dir or endpoint, model allowlist, colour); subagents and background tasks recorded in the run; failure classes; the no-progress Signal (`blocked`, "no progress for 20 min"); manual handover to a fresh session with a Brief; row summary with a small model | auto-handover, budgets, subagent model routing |
| `git` | observe checkout, branch, worktrees the agent creates; `touches` after each run; per-run checkpoint refs; changes view (branch diff vs base) | worktree creation by Fyr, cleanup Jobs beyond "safe remove" |
| `github` | detect `gh pr create`; poll PRs every 2 min with a cheap check first; Conditions `prChecks`, `prNewReview`, `prMerged`; `blocked` Signal; PR badge on rows; `pr.merge` as an async Action with the merge identity; watch Rules that notify the agent | stacks, review guides, issues as goals |
| `governor` | session slots, heavy-command slots through the PreToolUse hook, process stop after 10 min without a run (0.2–0.45 GB saved each, resume about 1.2–4 s), one systemd user scope per session (`MemoryHigh` 2G, `MemoryMax` 3G, `MemorySwapMax` 1G), memory stop when `memory.events high` keeps rising for 30 s, PSI levels with the measured thresholds | per-project budgets, placement |
| `policy` | a static policy file: which tool calls are denied outright (for example force push to main) and which Actions need `confirm` | passkey `verified`, shadow policy, proposals from the answer log |
| `push` | Web Push for asking and failed; batched "3 done, 1 blocked" after 10 min away; per-device presence | other sinks |
| `hours` | silent windows per repo for push, with one summary when the window opens | holding rule-started runs outside hours |

The agents keep using their own MCP servers (Playwright and others) through their Claude config. Image content blocks in their tool results become attachments in the run's filmstrip. Fyr's own `preview` module is post-v1.

---

## 6. Screens in v1

Built from the Fyr design system with the round-5 fixes. The mock (`../design/screens.html`) shows the first draft; these changes are binding for v1:

1. **Read model:** a thread counts as seen after 1.5 s of dwell and a scroll to its last turn, or after an action. Space peeks without marking. Failed never clears on a visit.
2. **Question card:** one active question at a time, `1–4` pick and advance, `0` opens "Other", Tab moves on in multi-select. While a question is open the composer is its answer field, so Enter has one meaning. The card says which subagent asked.
3. **Inbox:** Needs you (two-line rows) · Working (expanded, one line, elapsed time, last tool, subagent count) · Seen (with a "PR merged" group) · Snoozed · Settled (24 h). A "Since 14:02" strip after an absence.
4. **Thread:** tabs Conversation · Changes · PR. Runs collapse tools and subagents; preview screenshots show as a filmstrip. Decisions dock above the composer. Enter steers, `⌘Enter` queues.
5. **Keys:** `j/k`, `n/N` next/previous needing you, Enter, Space peek, `e` settle (on a running thread: settle when done, also when a PR is open; on a stopped thread with an open PR: settle when merged), `s` snooze, `#` archive, `u` mark unseen, `x` select, `Shift+E` settle selection, `⌘Z` undo, `⌘K` palette, `⌘1–9` stable slots.
6. **Phone:** bottom bar Inbox · New · Search; New has "Paste screenshot" and the photo picker on top; push opens the question card with question 1 focused; a client outbox holds a capture when the host is offline.
7. **Visual:** type scale 12/14/16/20, `ink-faint` never for text, asking icon filled with `beam-ink` in light theme (≥ 3:1), redesigned icons that pass a 14 px grayscale and deuteranopia check, beam never on toasts.

---

## 7. Build plan

*Historical: the plan as of 13 Nov 2026. The current milestones, their order and exit criteria are in [`PLAN.md`](PLAN.md).*

Sizes are my estimates, not measurements. They exist to make the order clear, not to promise dates.

| Milestone | Content | Exit criterion | Est. size |
|---|---|---|---|
| **M0 Skeleton** | `fyrd` with tsnet, pairing, device tokens; SQLite event store with receipts; Action registry; ConnectRPC streams; PWA shell with the design system; Nix flake and NixOS module | A phone on the tailnet pairs and sees an empty inbox over HTTPS | Go 2.5k, TS 2k |
| **M1 Conversation** | Go control client from the spike; sessions, runs, timeline streaming; AskUserQuestion Decisions; interrupt, steer, queue; profiles; resume after restart; attachments | A7 passes; a question is answered from the thread view; a 3-hour session survives two host restarts | Go 3k, TS 4k |
| **M2 Inbox** | `attention`, sections, read model with dwell and peek, settle/snooze/archive with deferred undo, bulk select, palette, keys, away strip, phone layout and capture | A1, A3, A4, A6 pass; property tests for L1, I3, I4, I9, I10 green | Go 2k, TS 5k |
| **M3 GitHub** | PR detection, poller, badges, watch Rules, `blocked` Signal, settle when merged, credential broker, `pr.merge` async Action, Changes and PR tabs | A5 and A9 pass; the agent's token provably cannot merge | Go 2.5k, TS 3k |
| **M4 Calm machine** | governor (slots, process stop, scopes, memory stop, PSI), push with batching and presence, hours, policy file, no-progress Signal, Brief + manual handover | A2, A8 and A10 pass; a forced memory hog is stopped and shown as failed, not hidden | Go 2.5k, TS 1k |
| **M5 Cut-over** | `fyr import-t3`: read T3's database read-only, adopt open threads by their Claude session id (title, cwd, PR links); one week side by side, then T3 off | Full checklist A1–A10 for one week | Go 0.5k |

Total estimate: about 13k lines of Go and 15k of TypeScript. T3 Code is about 1.6 M lines; most of the difference is other providers, other forges, the mobile app, the hosted relay and the Effect plumbing.

**Testing:**
- The pure core (`apply`, `attention`, Conditions) gets table tests and property tests over random command sequences checking every invariant in section 4.
- Replay tests: recorded event logs from real days re-run through the projections must give the same attention.
- Agent conformance: traffic between the official Agent SDK and each pinned CLI version is recorded once; CI runs the Go client against it. CI also diffs the SDK's control types on each release (the drift check from the spike).
- Crash tests: `kill -9` the daemon at each Job step and each Rule firing; the result must be exactly once or a visible failed state.

---

## 8. After v1: getting what you want, and more

*Historical: the plan as of 13 Nov 2026. The current milestones, their order and exit criteria are in [`PLAN.md`](PLAN.md).*

Each step adds modules or turns on parts of the core that already exist. None needs a migration.

| Version | Adds | Prerequisite |
|---|---|---|
| **v1.1 Decisions that scale** | Group equal approvals across hosts into one row (same-host merging by `payloadHash` is already in v1), prices and defaults on Decisions, the answer log with outcomes (merged, reverted, re-asked) | Round 4 fixes K9, K10 in place |
| **v1.2 Several hosts** | Mesh of up to 6 hosts, replicas, cross-host Conditions and conflicts, host switch in the UI, start a thread on any host | Move protocol and residence written as a state machine and model-checked in TLA+ (I1, I2, I3, I11) |
| **v1.3 Coordinator** | A coordinator per project with a grant you sign; digests, plan Document, proposals with exact payloads and undo; the PR babysitter as the first coordinator | Async effects, lineage quotas (I6) and mode ceilings (I5) |
| **v1.4 Software factory** | The `workflow` module: issue → plan → implement → independent review → fix rounds → CI → PR ready; your batch-fix and the 5-round review loop as definitions | Entity kinds for modules, `calls` composition, lineage quotas |
| **v1.5 Strong approvals** | Passkeys (Face ID) for `verified` Actions, signed grants and policies, policy proposals mined from your answer log and shadow-run for a week | WebAuthn on one `clientOrigin` |
| **v2 The fleet** | Automatic placement of parked threads on the host with capacity and a warm Nix store; editions instead of a stream ("nothing needs you until 14:00"); speculative work on the predicted answer | Mesh, governor `capacity{host}`, checkpoints per run |

---

## 9. What ran and what did not

**Ran (on this machine, 2026-11-13):**
- Protocol spike, `claude` CLI 2.1.325, Go client 469 LOC: multi-turn over stdin, PreToolUse hook deny/allow, `can_use_tool` for AskUserQuestion, interrupt (4 ms ack), `set_model`, SIGKILL + resume in 1.18 s, idle RAM 210–250 MB per session in the protocol spike (the resource spike measured up to 450 MB with a long session; the spec uses 210–450 MB). Drift over 7 SDK versions (May–Nov 2026): the needed control types were never removed or renamed.
- Resource spike: PSI available; systemd user scopes work with `XDG_RUNTIME_DIR` set; `MemoryHigh` stalls a process instead of killing it, and with zram `MemoryMax` never fires, which is why the governor has its own memory stop.
- Usage analysis of your T3 database (read-only).
- Five adversarial review rounds on the spec: 120 findings. Rounds 1–3 are resolved in v0.5, rounds 4 (foundation) and 5 (UX) in v0.6, and a final consistency pass (40 edits between this report and the spec) in v0.6.1.

**Did not run:**
- OOM kill at `MemoryMax` with `MemorySwapMax=0` (the sandbox blocked it). Settle it with: `XDG_RUNTIME_DIR=/run/user/1000 timeout 60 systemd-run --user --scope -p MemoryMax=300M -p MemorySwapMax=0 -- python3 hog.py 25 16`; expect `oom_kill 1` in the scope's `memory.events`.
- `stop_task`, `mcp_set_servers`, `rewind_files` against the live CLI. v1 does not use them.
- iOS share-sheet capture. iOS PWAs do not support the Web Share Target; v1 uses a paste chip and an iOS Shortcut. Verify the Shortcut on your phone in M2.
- Any part of the UI beyond the static mocks.

---

## 10. Risks

| Risk | Effect | Mitigation |
|---|---|---|
| The read model feels wrong in daily use | You stop trusting Needs you and open every thread again | Dwell + peek + sticky failed in M2; log visits for a week and tune the thresholds |
| CLI protocol changes | Sessions break after a `claude` update | Nix pins the CLI; conformance recordings in CI; TS bridge as fallback |
| Building the UI takes longer than the core | v1 slips | The design system's components are already written; build screens only from them |
| Single host is a step back if a second machine becomes important early | Work cannot move | v1.2 is next after v1.1; meanwhile run a second `fyrd` and add both hosts in the client (no moves, but one list) |

---

## What follows

- **Unblocked now:** M0 can start. The stack, protocol, data model and design system are decided.
- **Open, owned by you:** whether v1.1 or v1.2 comes first.
- **Done:** spec v0.6.1 with the round 4 and round 5 fixes, the async-effects principle (D5), multi-user readiness (R19) and the consistency pass.
- **Separately needed before v1.2:** the TLA+ model of move and residence.
