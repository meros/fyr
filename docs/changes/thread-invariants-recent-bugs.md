# V0.thread-invariants-recent-bugs: seven new thread invariants and tighter checks covering recent adapter, quota, rule, serialization, send, and process-consistency failures

→ V0.thread-invariants, V0.thread-self-wake-churn, V0.agy-reattach-allowlist, V0.agy-reactive-wakeup, V0.agy-reattach-queue, V0.quota-wait-midturn, V0.rule-digest-stable, V0.merge-refused-after-merged, V0.void-held-ask-and-prompts, V0.watch-stale.

Recent bugs across the `agy` adapter, quota waits, rule persistence, client sends, and process state shared structural signatures that the original 35 thread invariants did not yet catch. This change adds 7 new invariants (42 total), tightens 6 existing checks, and fixes 3 latent bugs uncovered by the tighter checks.

(1) **New pure state and persistence invariants (`internal/core/thread_invariants.go`):**
- `thread-fyr-tool-approval`: flags any open `DecisionApproval` for Fyr's own MCP tools (`mcp__fyr__*` / `mcp_fyr_*`) or policy-gated tools (`core.Gate`), catching adapter launch/reattach allowlist drops (`V0.agy-reattach-allowlist`) and gate bypasses.
- `thread-quota-misclassified`: flags a provider out-of-quota notice filed under a non-`quota` failure class, a future quota reset not scheduled in `NextCheck` or missing its wait text, an expired quota reset still waiting, or (in `Engine.checkThreadApplied`) a clean `run.ended` whose turn summary opens with a provider quota notice (`V0.quota-wait-midturn`).
- `thread-serialization-drift` (`core.CheckSerialization`): verifies on `Engine.apply`, `Engine.Load`, and in the randomized state-machine test that a JSON save-and-load round trip preserves `ComputeWith`, every change's `changeDigest`, overdue-rule status, and `NextCheck` (catches `V0.rule-digest-stable` and the whole class of persistence asymmetry bugs).

(2) **New runtime, churn, and process-consistency invariants (`internal/engine/thread_invariants.go`):**
- `thread-duplicate-send`: flags identical non-empty owner `thread.send` text (without attachments/images) arriving on one thread from the same actor within 3 seconds (`InvDuplicateSendWindow`) with distinct `commandId`s, catching client double-submit and duplicate-listener bursts.
- `thread-failure-churn`: generalizes `thread-self-wake-churn` to any unattended failure loop (3+ consecutive failed runs within 10 minutes without an intervening non-duplicate owner send or retry), catching quota auto-continue loops, feed-wake failure loops, and watchdog crash loops.
- `thread-ghost-decision`: symmetric counterpart to `thread-phantom-decision`, flagging an agent process blocked waiting on a permission/question id (`AgentState.Asks`) that is not open in `Thread.Decisions` for more than 30 seconds.
- `thread-run-silent-stuck`: flags a live turn (`Running && st.Live && st.TurnLive`) with no open top-level tools (`OpenTools == 0`), no approval judge in flight, no open decisions, no running tasks, no machine wait, and not unresponsive, that has emitted no stream output for more than 15 minutes (`InvRunSilentStuck`, measured from `max(st.LastOut, *t.RunStartedAt)`). Exposes `LastOut`, `OpenTools`, and `Judging` on `agent.SessionState` / `engine.AgentState`.

(3) **Tightened existing invariants:**
- `thread-rule-invalid`: checks dead/malformed `When` leaves on armed rules (`WhenAt`/`WhenQuota` missing `At`, `WhenQuota` missing `Provider`, `WhenIdleFor` with `Ms <= 0`, change refs missing from `t.Changes`) and recursive `All`/`Any` unwatchable PR conditions.
- `thread-change-invalid`: checks non-OPEN PRs carrying PR conditions regardless of `PhaseBy`.
- `thread-decision-invalid`: catches void merge asks closed with `gate: true` and `"the PR is not open"` (`V0.void-held-ask-and-prompts`).
- `thread-availability-mismatch`: checks `thread.settleWhenDone`, `thread.snoozeWhenDone` (including during quota waits, `V0.quota-wait-midturn`), and `thread.cancelWake`.
- `thread-archived-live-work` and `thread-settled-contradiction`: flag stale `MachineWait`.
- `thread-rule-overdue`: also checked immediately in `Engine.checkThreadLoad`.
- `thread-phantom-outbox`: also flags unread outbox ids missing from a live agent's `st.Outbox` past 30 seconds (`V0.agy-reattach-queue`).
- `thread-phantom-tasks`: also flags an idle live agent still holding `st.BG > 0` when the engine has 0 running tasks past 30 seconds.

(4) **Latent bugs caught and fixed:**
- `unwatchable` (`internal/core/rules.go`) now recurses through `All`/`Any`, so a `then: "settle"` watch on a PR condition (which `reduceRuleArm` wraps in `All(WhenCondition, WhenIdle)`) is dropped when its change leaves in-flight phases instead of staying armed forever.
- `settleNow` (`internal/core/settle.go`) and `thread.archive` (`internal/core/actions.go`) now clear `MachineWait`.
- `KindMachineWait` (`internal/core/kinds.go`) no longer sets `MachineWait` on an already settled or archived thread.

Tests: `internal/core/thread_invariants_test.go` (`TestThreadInvariantsPureStateViolations`, `TestThreadInvariantsTransitionViolations`, `TestThreadInvariantUncoveredFixes`, `TestThreadInvariantsUnderRandomStateMachine` with `CheckSerialization` on every step); `internal/engine/thread_invariants_test.go` (`TestThreadInvariantDuplicateSend`, `TestThreadInvariantFailureChurn`, `TestThreadInvariantCleanEndQuotaNotice`, `TestThreadInvariantGhostDecisionLiveOutboxGhostBGAndSilentRun`).
