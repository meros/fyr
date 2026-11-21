# V0.quota-wait-midturn: detect mid-turn agy quota halts and keep quota waits out of snooze and settle-when-done failures

→ V0.quota-wait. *Amends V0.quota-wait (1, 4, 5).*

Three gaps remained in V0.quota-wait's handling of provider quota stops:

1. **Mid-turn agy quota halts.** The `agy` ACP adapter concatenates all assistant text across a turn into the `result` message. When a turn emitted prose earlier, ran tools, and then stopped with `⚠ Individual quota reached…`, `core.QuotaNotice` only inspected the first non-empty line of `result`, missed the notice, and ended the turn cleanly as Finished instead of Waiting. The agent manager now tracks the main agent's last assistant text after the latest tool call in the turn and checks `core.QuotaNotice` on that fallback when `result` does not open with the notice (ignoring subagent messages and normal agent prose reports).
2. **Snooze and Settle when done.** `Attention` woke a snoozed thread on any `t.Failure` newer than the snooze start, including an active quota wait, and `thread.settleWhenDone` disabled itself with `"The last run failed"` during a quota wait. Both now check `QuotaWaitText(t, now) == ""`, so an active quota wait breaks no snooze and permits arming Settle when done, while reverting to a failure once its reset time passes.
3. **Multi-day relative resets and `fyr_state`.** `resetsIn` now reads day prefixes (such as `1d2h30m` or `2d`) in addition to hours, minutes and seconds, and `stateThread` reports an active quota wait with its waiting text rather than as a failed run.

Tests: `internal/agent/quota_test.go` (`TestQuotaFailureMidTurnAfterEarlierProseAndTool`, `TestSubagentOrProseAfterToolIsNotAQuotaStop`); `internal/core/quota_test.go` (`TestQuotaReset` day durations, `TestQuotaWaitSnoozeAndSettleWhenDone`).
