# V0.watch-stale: a watch never stays armed on a condition its change can no longer report

→ Amends V0.46, decision 0018, V0.checks-follow-head. A watch read conditions of a change (`{kind: condition, change, type, status}`). Landing a change drops the conditions that describe the PR before the merge — checks, review, conflict and merge (`dropConditions`, decision 0018) — because they no longer need anyone once the PR is closed. A watch reading one of those was not affected: it stayed armed, its Condition evaluated to "not this value" forever, and nothing would ever make it fire. It looked armed in the UI and waited on a fact that no longer existed.

Found live: a thread marked a change landed, then armed a `checks: ok` watch on it a few seconds later. The watch showed as "armed, waiting for CI" for the rest of the session, while the condition it named had already been dropped. 419 thread events passed through it without a firing. Arming on an already-true condition that still exists was never the problem: that fires at once, and it still does.

(1) **Arming checks it can be met.** `checkAgentWhen` refuses a condition watch on `checks`, `review` or `conflict` when the change is no longer in flight (`Phase.InFlight()`), with "`<key>` no longer reports `checks`: the change is landed". The agent gets a reason it can act on instead of a watch that never fires.

(2) **Landing drops the watches it strands.** `EvalRules` drops any Rule whose Condition is one of those three on a change that has left the in-flight phases, next to the existing drop of a Rule whose change is gone. A dropped Rule does not act; it does not fire a notification the owner never asked for. Only those three kinds are dropped: a phase watch (`changeReached`) and `changeMoved` are still readable after landing and keep working.

(3) **What this is not.** A watch on a condition that is already true and still exists keeps firing at once, as before; no behaviour there changes. A watch whose `then` would have settled or notified is not converted into a firing by landing — the fact it waited for is gone, so there is nothing to report.

Tests: `TestWatchOnLandedChangeIsRefused`, `TestWatchIsDroppedWhenItsChangeLands`, `TestLandingKeepsOtherWatches` (a phase watch survives landing), `TestWatchOnANewChangeArms` (a new change is in flight, so its checks watch arms as usual). The first two fail without the fix.
