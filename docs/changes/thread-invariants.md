# V0.thread-invariants: Fyr checks 42 invariants on thread state, transitions, timing, and process consistency, and surfaces a broken one as a Needs-you card

→ SPEC sections 3, 5, 6 and 7; follows V0.load-invariants' reporting, persistence, card, Fix it, Dismiss, and Settings list. Extended by V0.thread-self-wake-churn and V0.thread-invariants-recent-bugs.

(1) **Pure state and persistence invariants** (`internal/core/thread_invariants.go`, `core.CheckThread`, `core.CheckSerialization`), checked after every `Engine.apply` commit and after `Engine.Load`:
- `thread-archived-live-work`: an archived thread still has live work (`Running`, `Starting`, `MachineWait`, `Handover`, `RunningTasks`), open decisions, held merge asks, a pin, a snooze, or an armed rule (SPEC L3).
- `thread-settled-contradiction`: a settled thread still has `Starting`, `MachineWait`, a pin, a settle offer (`SettleAsk`), a settle rule, a snooze, or an armed watch.
- `thread-run-state-contradiction`: a thread's live run fields disagree (`Running` without `RunStartedAt`, `!Running` with `RunStartedAt` or `Activity`, `Running` with `Starting` or `Failure`, `Starting` with `Failure`, or `Stopped` while running or tasks run).
- `thread-pin-contradiction`: a pinned thread is also archived, settled, actively snoozed, or has an unsuspended auto-settle rule (V0.104, V0.106).
- `thread-rule-invalid`: a thread or view rule has inconsistent suspend status vs `SuspendedBy` (SPEC I3), suspend reasons that disagree with thread state, duplicate or empty id, orphan `About` or `When.Change` reference, dead `WhenAt`/`WhenQuota`/`WhenIdleFor` timer, an unwatchable PR condition (including compound `All`/`Any`, V0.watch-stale), a stuck armed one-shot watch with `LastValue=true`, or more than one snooze rule on the view.
- `thread-decision-invalid`: duplicate decision ids across `Decisions` and `HeldAsks`, malformed effect decision, void ask left open or closed as gate instead of void (decision 0024 §5, V0.void-held-ask-and-prompts), open merge ask whose gate is not open, or held ask that is not a waiting merge ask (V0.136).
- `thread-fyr-tool-approval` (V0.thread-invariants-recent-bugs): a Fyr-owned MCP tool (`mcp__fyr__*` / `mcp_fyr_*`) or deterministic policy-gated tool (`core.Gate`) opened an owner tool-approval card instead of being handled by Fyr (V0.agy-reattach-allowlist).
- `thread-quota-misclassified` (V0.thread-invariants-recent-bugs): a provider quota notice was filed under a non-`quota` failure class, left without a matching quota wait and `NextCheck` schedule, or missed on a clean `run.ended` whose turn summary opened with a provider quota notice (V0.quota-wait-midturn).
- `thread-serialization-drift` (V0.thread-invariants-recent-bugs): a JSON save-and-load round trip changed computed attention, any change's `changeDigest`, overdue-rule status, or `NextCheck` (V0.rule-digest-stable).
- `thread-change-invalid`: duplicate change id or PR URL, invalid phase, observed PR state vs phase mismatch, `Landed`/`Live` missing `LandedAt`, done mark mismatch against `doneNow` (V0.98), stale park (V0.84), non-OPEN PR still carrying PR conditions, or `MainChange` pointing to a missing/dismissed/PR-less change.
- `thread-outbox-invalid`: empty or duplicate id, or a terminal/invalid state other than `sent` or `queued` (V0.33).
- `thread-feed-invalid`: `Queue` or `Said` over limit, invalid/silent urgency, or empty/duplicate coalesce key (5.14).
- `thread-attention-placement`: computed attention violates the State↔Section bijection, `NeedsYou` group rules (V0.row-group-code), required `Reason`, lifecycle/snooze precedence, or active placement guarantees (asks, quota wait, failures never hidden by visits, unresponsive, running, starting, machine wait, tasks, needs, Run cards, settle offers, CI wait, armed watches, done/seen).
- `thread-availability-mismatch`: key `Registry` command availabilities disagree with the thread's state (archived, settled, running, decisions, failure, quota wait, settle/snooze-when-done, wake cancel, unresponsive).

(2) **Pure transition invariants** (`internal/core/thread_invariants.go`, `core.CheckTransition`), checked on every `Engine.apply` commit:
- `thread-archived-mutated`: an archived thread started a run, set `Starting`, opened a decision, or added to `Outbox` without being unarchived.
- `thread-settled-run-started`: a run or `Starting` began on a settled thread without reviving it first.
- `thread-revive-incomplete`: `thread.send` or `thread.retry` left a failure, settle, settle offer, or snooze behind; an owner send did not advance `SeenAt`; or an agent send moved the owner's `SeenAt` (5.2).
- `thread-rights-violated`: a non-owner input overwrote an owner title, an owner-ended change, or an owner-marked-done change (5.2, V0.69, V0.72).
- `thread-counters-regressed`: `ID` or `CreatedAt` changed, `RunEnds` decreased, or `run.ended` on a running thread did not clear `Running` and increment `RunEnds` by 1.
- `thread-poll-stole-seen`: a PR poll of an open PR with no new blocker moved a `Seen` thread back to `Done`.
- `thread-rule-loop`: `EvalRules` hit the firing limit and emitted `rule.loop`.

(3) **Host, temporal, and process-consistency invariants** (`internal/engine/thread_invariants.go`), checked after `Engine.Load`, on `Engine.Tick`, and after `syncSlotQueue`:
- `thread-restart-left-live-work`: after `Engine.Load`, any thread still holds `Starting`, `MachineWait`, or `Handover`, or a non-kept thread still holds `Running`, running tasks, outbox entries, or process-bound decisions (V0.6, V0.33, V0.machine-wait-restart).
- `thread-starting-stuck`: `Starting` without `MachineWait` for more than 5 minutes.
- `thread-handover-stuck`: `Handover` in progress for more than 15 minutes.
- `thread-machine-wait-stuck`: `MachineWait` for more than 20 minutes (past `PressureMaxWait`).
- `thread-settled-still-running`: a settled thread stayed `Running` for more than 2 minutes after `Settle.At`.
- `thread-outbox-stuck`: unread messages stayed in `Outbox` on an idle thread for more than 2 minutes.
- `thread-waking-feed-stalled`: a wakeable idle thread held a waking notice within budget for more than 2 minutes without starting a turn.
- `thread-rule-overdue`: an armed rule without a future `RetryAt` still evaluated true with `LastValue=false` after `Tick`'s clock pass.
- `thread-action-run-stuck`: a pressed `ActionRun` stayed `Running=true` for more than 2 hours.
- `thread-attention-drift`: `e.last[id]` disagreed with `ComputeWith` after a commit or after `Tick`.
- `thread-slot-queue-drift`: `t.SlotQueued` disagreed with `Slots.Snapshot().Waiting` right after `syncSlotQueue`.
- `thread-phantom-running`, `thread-ghost-running`, `thread-phantom-tasks`, `thread-phantom-decision`, `thread-ghost-decision`, `thread-phantom-outbox`: mismatches between the engine's thread state and `agent.Manager.Inspect()` persisting past a 30-second grace window.
- `thread-self-wake-churn` (V0.thread-self-wake-churn): the CLI started 3 or more turns by itself within 10 minutes, each of which ended before the model answered (no measured duration, so the adapter's settle timer closed it) or was refused by the provider's quota.
- `thread-duplicate-send` (V0.thread-invariants-recent-bugs): identical non-empty owner `thread.send` text arrived twice on the same thread within 3 seconds with distinct command ids.
- `thread-failure-churn` (V0.thread-invariants-recent-bugs): a thread hit 3 or more consecutive failed runs within 10 minutes without an intervening non-duplicate owner send or retry.
- `thread-run-silent-stuck` (V0.thread-invariants-recent-bugs): a live turn with no open top-level tools, tasks, decisions, or approval judge emitted no stream output for more than 15 minutes.

(4) **Surfacing and Fixes.** Thread invariants share V0.load-invariants' log line (`invariant: <id> <evidence>`), kv persistence, 24 h per-invariant card rate-limit, Needs-you card (`Thread invariant broken: <id>`, showing the thread's last 30 transitions), `Fix it` (tailored prompt pointing to `docs/SPEC.md` and `docs/changes/thread-invariants.md`), `Dismiss`, and Settings → Automation (under a **Thread invariants** group next to **Load invariants**, shared by desktop and phone; exposed on `Load.Invariants` as a fallback when slots are off).

(5) **Transition bugs caught and fixed while building these invariants:**
- `rule.settle` now calls `Unsnooze(r.V)` (SPEC 7.4: settling clears the snooze).
- `send.failed` now removes messages marked `lost` in its log from `Thread.Outbox`.
- `thread.archive` now rejects archiving while background tasks or a handover run, and clears `Starting`, `SettleAsk`, and settle rules.
- `settleNow` now clears `Starting`.
- `WhenIdle` now requires `Starting == nil` and `thread.retry` sets `Starting`, so a pending settle rule cannot fire right as a send or retry starts a new turn.
- `rule.arm` now rejects settled and archived threads, and `settle.request` now rejects archived threads.
- `trimChanges` now updates `MainChange` if the trimmed change was the main one.
- `agent.Manager.read` now observes `process.lost` when a process exits with running background tasks (`s.bg > 0`) between turns, and `process.lost` only logs a `run.ended` failure when a run/decision was actually lost.

Tests: `internal/core/thread_invariants_test.go` (TestThreadInvariantsPureStateViolations, TestThreadInvariantsTransitionViolations, TestThreadInvariantUncoveredFixes, TestThreadInvariantsUnderRandomStateMachine); `internal/engine/thread_invariants_test.go`; `web/test/invariants.test.ts`.
