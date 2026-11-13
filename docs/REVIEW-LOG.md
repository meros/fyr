# Spec review log

## Round 1 (v0.1 → v0.2)

25 findings (3 blocker, 15 major, 7 minor). All accepted. Also added user requirements R11–R15 (Nix, UX, resources, modules, approvals).

| Finding | Resolution in v0.2 |
|---|---|
| F1 Actions vs observations | 3.6 intent vs observation events; A1 |
| F2 no side-effect layer | New concept Job (8.1), resumable steps |
| F3 lifecycle hides failures | 6.2 rows 2–3 above snoozed; send un-settles; settle disarms watch Rules |
| F4 edge-only rules | `trigger: edge \| level` (7.1, 7.2) |
| F5 rejected firing, rule principal | `rule(ruleId)` principal, outcome recorded, stays armed (5.2, 7.2) |
| F6 handover bypasses email confirm | Renderers vs destinations (10.2); `brief.send` level presence |
| F7 needsYou two meanings | States `asking/failed/review/unread`, section `attention`; notify on entering section (6.3) |
| F8 sort across hosts, clock | SortKey with `at`; clock assumption (2); `at` Rules fire on own clock |
| F9 downtime | Evaluate all Rules on `host.started`, compare stored value (7.2) |
| F10 restart recovery | Runs interrupted (class restart), Requests cancelled (5.5); first answer wins |
| F11 handover deadlock | Unavailable with pending Request; sends queued; trigger `system` (10.4) |
| F12 waitsOn undefined | Removed; wait = Rule purpose `wait` (7.3) |
| F13 review vs PR watch | Separate on purpose; review Signal tied to head commit (7.4, 12.4); "foreign" defined |
| F14 archive leaves work | Archive interrupts, cancels Requests and Rules (5.3) |
| F15 conflicts | null worktree owns checkout; touches per branch via file key; host-side Signal (9) |
| F16 checkout | `checkout` Ref kind (3.3) |
| F17 move | `thread.move` Job with moveId, WIP commit, push (10.5) |
| F18 principals, default rules | Principal type; default Rules table (7.4) |
| F19 activity | Activity events (4.2); `threads[]` on events |
| F20 lifecycle gaps | Complete lifecycle table (5.3); L1 invariant |
| F21 last run, usage reset | Definitions (3.2); default wait Rule (7.4) |
| F22 protocol | upsert/remove, 7-day receipts and cursor (11) |
| F23 duplicate watchers | Ref state documented as per-host cache (3.3) |
| F24 fork, brief data | `thread.fork`; Brief is data; `createdBy: model` |
| F25 host actions | `host.pair`, `host.revokeDevice`, `host.rename`; per-device host list (2) |

## Round 2 (v0.2 → v0.3/v0.4)

25 findings (4 blocker, 15 major, 6 minor). All accepted. v0.4 also applies owner decisions D1–D4 (Decision entity, host-independent ThreadId, mesh, Coordinator concept), the coordinator research, Fable 5.2 ideas (Decision pricing/defaults/answer log, Claims with evidence, checkpoints per run, user presence) and the owner's measured T3 usage (section 0.2).

| Finding | Resolution |
|---|---|
| G1 level Rules loop | Edge-only firing on stored `lastValue`; own-cause events excluded; `initial` false/current (7.3) |
| G2 snooze until agent finishes | `runEnded(self)` since armed; snooze available while running (7.5, 5.3) |
| G3 L1 broken | Every exit from snoozed disarms the Rule in the same apply (L1, lifecycle table) |
| G4 evaluation triggers | `dependsOn` + `nextCheck` drive evaluation (7.2, 7.3) |
| G5 requests outlive runs | L2: run end parks its Decisions |
| G6 restart drops to idle | Row 3 includes interrupted/restart and budget |
| G7 approvals hold slots | Level step before slot; parking after 10 min (12.4, 5.6, 8.2) |
| G8 presence not a proof | Levels are none/confirm/verified; push is delivery only (5.4) |
| G9 WebAuthn challenge | Host nonce + decision/command id; one clientOrigin (11.2, 2) |
| G10 pairing weak | 64-bit code, 5 attempts; new passkeys need verified; export without tokens (11.3, 2) |
| G11 writes outside Actions | `system` principal; Jobs act via Actions (5.2, 8.1) |
| G12 auto-settle swallows follow-up | Edge firing + `not(unread)`; merge-settle off by default (7.6) |
| G13 watch never re-armed | Suspend/resume with `pausedBy` (7.4, 5.3) |
| G14 move gaps | Host-independent ThreadId (D2); move keeps thread; imported events; branch rule (10.4) |
| G15 rule needing approval | Outcome `approvalRequested` suspends the Rule (7.3) |
| G16 signals without rows | Signal asking/failed/waiting have rows (6.2) |
| G17 modules vs extension points | Observer, ToolHook, DocumentSchema, Renderer points; single LevelPolicy/ResourcePolicy; Job kinds all in modules (12.2) |
| G18 agent scope | ownLineage, spawn and rule limits, mode defaults, Bash patterns not a boundary (5.2, 5.4) |
| G19 default rules inherit agent | `createdBy: system(defaults)` (7.6) |
| G20 missing Actions | `document.create`, `checkout.add`, `thread.import` (5.3) |
| G21 agent settle/snooze unusable | Agent helpers via `rule.create(runEnded(self))` (5.3) |
| G22 undo not inverse | Deferred Rule with 5 s window; cleanup delayed 10 min (7.5, 5.3) |
| G23 handover under pressure | Fallback model Brief; slot takeover; 10 min timeout (10.3) |
| G24 auto-handover skipped | Rejection resets `lastValue`, retries with backoff (7.3) |
| G25 small gaps | Timer service writes expiry; snooze sort; markUnread suppresses visits (5.6, 6.5, 6.4) |

## Round 3 (v0.4 → v0.5), focus: extendability and foundation

20 findings (3 blocker, 12 major, 5 minor/medium). All accepted. Main structural change: the Entity primitive replaces thread-centric residence, routing and Rule ownership.

| Finding | Resolution |
|---|---|
| H1 modules cannot define durable entities | Entity concept with registered kinds; Thread, Decision, Coordinator built in; subject, routing (A7), Rule owner, replication keyed on EntityRef (1, 3.2, 12.2) |
| H2 EventId order across hosts | Per-entity gapless `eseq` that travels with the entity; HLC `at` for display/sort; Rule `armed` is an eseq snapshot (2, 3.2, 7.1) |
| H3 dual residence during move | Epoch fencing: leaving → import (epoch+1) → resided → left; highest epoch wins; cancel → stayed (10.4) |
| H4 agent helpers always rejected | `thread.settleWhenDone` / `thread.snoozeWhenDone` Actions create Rules as `system(core)` (5.3) |
| H5 merge without user not enforceable | Credentials point; session token cannot merge; merge identity via rulesets; doctor check (5.9, Q6) |
| H6 Documents not in mesh | Header replication, `document.get`, eager grant/policy bodies (2, 3.9) |
| H7 core depends on modules | M4 + capabilities `provides/requires` + start check; DefaultRules point; tool gate in core; `capacity` moved to governor; generic `lineageScope` (12.1–12.3, 7.6) |
| H8 no guard/admission point | `Guard` AND-ed into `available`; `RunAdmission` AND of providers (5.2, 8.2) |
| H9 no reactive path, no composition | `apply → calls` in one transaction; Observers may submit calls; Job CommandId = H(jobId, step, index) (5.1, 8.1, 12.2) |
| H10 coordinator cannot be woken | `coordinator.wake` (rule: yes); digests include Ref state of `scope.repos` (5.3, 12.7) |
| H11 coordinator failures invisible | Coordinator is an entity with Conversation; status `failed`; inbox rows for asking/failed; Decisions with only a coordinator (3.6, 5.7, 6.2, 6.3) |
| H12 single Claude-shaped Agent | Agents keyed by kind; profiles name the kind; subagent routing out of scope (Q5) (12.2, 12.4) |
| H13 placement cannot automate | `entity.move` allowed for rule/system under policy; queued run withdrawn and re-queued; `capacity{class, host}` (5.3, 8.3, 10.4) |
| H14 cross-host conditions fixed to host | Residence resolved at each evaluation from replicas; `entity.resided` triggers re-evaluation; staleness = no heartbeat for 30 s (2, 7.2, 7.3) |
| H15 double reactors | `thread.start(defaults)`; coordinator/workflow threads default `none` (5.3, 7.6) |
| H16 threads cannot read later Documents | `document.get` for threads; `documents` on `thread.send` (5.3) |
| H17 no recurring schedule | `schedule{cron, tz}` Condition with single catch-up (7.2, 7.5) |
| H18 policy shadowing impossible | `level.evaluated` for every gated call, replicated; `shadow` policy Documents (4.1, 5.4) |
| H19 small Action gaps | `checkout` optional; `worktree.create`; `pr.ready`, `pr.reply`, `pr.resolveThread` (5.3, 12.4) |
| H20 narrow Sinks | `document.send(to: Ref)` with `sink` Ref kind; presence to Sinks; derived notifications declared (3.7, 4.1, 10.5) |

Also folded into v0.5 (not review findings): product name Fyr (`fyr`, `services.fyr`, `nixosModules.fyr`); R18 work/silent hours as the `hours` module (12.8); resource spike values in the governor (8.3: PSI thresholds, cgroup values not RSS, split interactive/background session slots, scope limits, stall detection, `XDG_RUNTIME_DIR`; OOM-kill at `MemoryMax` with `MemorySwapMax=0` still unproven); protocol spike decision in 16.2 (Go-native client proven on CLI 2.1.325; hook `allow` skips `can_use_tool`, so the gate defers AskUserQuestion/ExitPlanMode; `set_model` not kept across resume; ignore unknown message types; `stop_task`, `mcp_set_servers`, `rewind_files` untested).

## Round 4 (foundation, v0.5 → v0.6)

25 findings (6 blocker, 16 major, 3 medium) and 12 proposed invariants (I1–I12, all added). Owner decision D5 (async effects) applied. Decision: v1 is single-host; mesh and moves are post-v1 and need a TLA+-checked state machine first (0.4).

| Finding | Resolution in v0.6 |
|---|---|
| K1 wait Rule pins `waiting` | No standing usage-limit Rule; `claude` creates a disarm-after wait Rule at a `usage_limit` run end (7.6); `waits` list is the only source of waiting (3.3) |
| K2 move deadlock on open Decision | `entity.move` unavailable while a run is running or a blocking Decision is open; I11 (10.4) |
| K3 cancel split brain | Acknowledged abort tombstone on target; `uncertain` state; I2 (10.4) |
| K4 eseq fork after bundle | I1 fence quiescence; observers/timers buffer; bundle carries final eseq (3.2, 10.4) |
| K5 snooze lost on move | `anchored` Condition kinds keep `lastValue` on resume (7.2, 7.4) |
| K6 single pausedBy | `pausedBy` is a set; I3 (7.1) |
| K7 disarmed snooze stays snoozed | L1/I4 strengthened; Guards skip `restoresLifecycle` Actions (3.4, 5.2) |
| K8 deferred retries stale intent | Deferred Rules disarm on any outcome; `deferred.rejected` toast (7.3, 7.5) |
| K9 multi-thread Decisions | Per-Blocker state; I12; move detaches the moving thread's Blocker (3.5, 10.4) |
| K10 dedupKey approval leak | Approvals: `dedupKey := payloadHash`; grouping needs equal hash; coordinator answers only if all Blockers in grant; I8 (3.5, 5.6) |
| K11 mode widening | `modeCeiling` per thread; I5; effective mode = min(...) (3.4, 5.2) |
| K12 limits multiply | Lineage quotas on the root; I6 (5.2) |
| K13 nested calls undefined | Sequential, each fully checked, depth ≤ 4, whole tree becomes one approval (A6) |
| K14 agents open approvals/proposals | Approvals only from core paths; proposals only from coordinators; `requestedBy` chain on the card (5.6) |
| K15 receipts and Job ids | Terminal outcomes only; Job receipts until job end + 7 d; approved effect writes the original receipt; I7 (A4) |
| K16 non-atomic firing | One transaction, `CommandId = H(ruleId, firing)`; I9 (7.3) |
| K17 Job I/O exactly-once | Step kinds: Action, keyed I/O, at-most-once with `job.attempting` → `uncertain` (8.1); unarchive unavailable during cleanup |
| K18 invisible failures | Failed/uncertain Jobs give a `failed` Signal or a host row; coordinator stops not by the user are `failed`; I10 (8.1, 3.6) |
| K19 handover starves | Held inputs (`input.held`) are not runs; Brief run uses the user's class and is exempt at critical; keyed Brief Document (10.1, 10.3) |
| K20 governor deadlock/loop | `browser` class; heavy wait bounded at 5 min and releases the session slot; stalled user run ends `failed`; 3 pauses → failed (8.3) |
| K21 archive window | Agent Actions unavailable on archived threads; final `run.ended` cancels leftovers (L3) |
| K22 attention not total | Total end-class table (3.3); `run.admitted`; coordinator `acknowledged`; Blockers may be Jobs or module entities |
| K23 replica coalescing | Remote Conditions must be anchored; state kinds rejected on remote entities (7.2) |
| K24 grant revocation, peer trust | Peer threat model; stale replica or non-head grant rejects coordinator commands (2, 5.8) |
| K25 Document bodies on move | Bundle carries Document bodies (3.9, 10.4) |
| Concept economy | `Action.undo` removed (deferred Rules only); one `waits` source; `settleWhen`/`snoozeWhenDone` as user+agent Actions; hours via RunAdmission `until`; `Decision.frozen` replaced by the entity fence |

## Round 5 (UX, v0.5 + mocks → v0.6)

25 findings (3 blocker, 15 major, 7 minor). All accepted. Status vocabulary renamed: unread → done, idle → seen, review → blocked.

| Finding | Resolution in v0.6 |
|---|---|
| U1 looking clears attention | Dwell-based visits, Space peek, sticky failed via `failureAck`, rows stay until focus leaves (6.2, 6.3, 6.4) |
| U2 multi-question keys | One active question, `1`–`4`, `0` Other, auto-advance, composer as answer field (13.2, U15) |
| U3 iOS capture | Paste chip, iOS Shortcut to `/capture`, Android share target (11.4, U20) |
| U4 phone mocks | Merge is an async external Action (5.10); question card on phone (U22) |
| U5 Working collapsed, stalls | Working expanded with live one-line rows; stall Signal "no progress for 20 min" (U1, U16, 12.4) |
| U6 three labels for asking | No label override; kinds as plain text; coordinator and plan steps (U2) |
| U7 Review grab-bag | Renamed `blocked`; cause in the reason; budget/rule notices as reasons (6.1, 12.5) |
| U8 Unread/Idle names | `done` and `seen` (6.1) |
| U9 icons fail | Redesigned shapes, beam-ink fill, grayscale/deuteranopia test (U18) |
| U10 Decision vs thread rows | One-thread Decisions render as the thread row; grouped row only for ≥ 2 (6.3) |
| U11 clearing work | "PR merged" group, `x`, `Shift+E`, `thread.settleWhen` (U11, 5.3) |
| U12 noisy push | Push asking+failed only; batched done/blocked after 10 min away; per-device presence (6.3, 3.11) |
| U13 no away view | Away strip (U13) |
| U14 no Changes/PR | Thread tabs and filmstrip (13.5) |
| U15 untrustworthy proposals | Per-call sentence, why, level, undo, payload; user-word log; `actedOnBy` (13.2, 13.4, 12.7) |
| U16 scale | Two-line rows only in attention; 32 px elsewhere; Settled 24 h (U16, 6.5) |
| U17 shortcuts | `n`/`N`, `⌘Z`, stable `⌘1–9`, no "Later" (U5, U14) |
| U18 composer modes | Enter steers, ⌘Enter queues (U15) |
| U19 type | 12/14/16/20, self-hosted Plex, no `ink-faint` text (U9) |
| U20 beam leaks | Beam never on toasts (U6) |
| U21 jargon | U17 words rule; default/deadline shown only when relevant |
| U22 phone layout | Bottom bar Inbox · New · Search; sticky Answer; "Answered on desktop" (U21, U22) |
| U23 offline capture | Client outbox; one global banner (U23, 2) |
| U24 header chrome | Lenses in palette/filter; grouping only in `seen` (U19, U24, 9) |
| U25 asker | Card names the asking subagent (U25) |

## R19 (owner requirement, added in v0.6)

Multi-user readiness without migration: `UserId`, `owner`/`visibility` on every entity, per-(user, entity) View for snooze/pin/watermark/failure-ack, `attention(entity, view, …)`, inbox keyed by user, Decision `assignee`, per-user profiles and credentials, host roles, per-user governor fairness. v1 behaviour unchanged (one user). OS-level isolation is open (Q8).

## Consistency pass (v0.6 → v0.6.1)

An independent check compared SPEC.md v0.6 with FYR-V1.md (40 edits). The spec-side edits are applied; the report-side edits (FYR-V1.md) are separate.

| Item | Resolution in v0.6.1 |
|---|---|
| Run-scoped `blocked` Signals hidden by row 6 | New row 5a for `claude` Signals (no progress, budget); row 8 for the other sources |
| Deferred archive rejected for `rule(id)` | `thread.archive` Rule column "deferred only"; deferred `then` checked against `createdBy` (7.5) |
| Effects rejected by `callers` | `callers.agent` value `viaEffect`; `document.send` and GitHub external Actions use it (5.10) |
| `verified` needed in v1 | v1: `verified` Actions only through the host CLI; pairing registers no passkey (5.4, 11.3) |
| Confirm proof undefined | `ApprovalProof` type (5.4) |
| "View" with three meanings | Projection argument `proj`; UI point `UiSlot`; View = per-user state only |
| View Actions by agent or Rule | Rule acts on its `viewer`, else the owner (5.3) |
| Mark unseen vs forward-only watermark | `thread.markUnseen` moves it back; event `thread.markedUnseen` |
| Lifecycle words for snooze | send/wake clear the View's snooze; `restoresVisibility`; `⌘Z` text |
| Blocking wait and slots | The slot stays held; the governor may park after 10 min (12.3) |
| Critical interrupt omitted `agent` | Added (8.3) |
| Failed Signals and host rows not acknowledgeable | `Signal.atEseq` vs `failureAck`; `job.acknowledge` (5.3, 8.1) |
| `done` Condition not per user | `done {entity, user}`; defaults use `done(self, owner)` |
| Effect results untyped, receipt clash | `EffectResult` type, `effect.delivered`, second receipt `H(commandId, "effect")` |
| PR comment Actions marked state | External, level `none` |
| hours used a missing Sink point; stale "freezes" | `SinkFilter` point; Guard text fixed |
| 0.4 put post-v1 items into v1 | Explicit rows: preview, auto-handover, budgets, worktree.create, plan mode, pin, hours ruleRuns, email/deliver, policy.set, shadow; cut-over `fyr import-t3` |
| Core required `deliver` | Only when a module provides `document.send` |
| Stale refs U24, 13.4, missing E1 | Fixed |
| Pause vs suspend names | `suspendedBy`, "suspend reasons"; I3 gains the `decision` clause |
| Delegation undefined | Post-v1; v1: only the assignee answers |
| `thread.read` not defined | 5.11 Read queries; `McpTool` may call them |
| Row summary had two sources | `thread.summarized` from a small model; the Brief renderer is `oneLine` |
| MCP calls not bound to a principal | Per-session bearer token via `Credentials` (16.2) |
| Working slot order underspecified | Lowest free slot, kept while in `working`, no compaction |
| Leftover undo wording | Deferred undo window (7.5) |
| "Mark seen" on the away strip | "Dismiss" |
| "idle" with four meanings | `noCurrentRun`; coordinator `ready`; presence `inactive`; "process stop without a run" |
| Preview in v1 vs report | `preview` post-v1; filmstrip from `tool_result` images |
| `e` on running thread with open PR | Running wins: settle when done |
| Static policy in v1 | Loaded at start as a `policy` Document; `policy.set` post-v1; `decision.outcome` v1.1 |
| Poll interval | `github` polls every 2 min with a cheap check first |
