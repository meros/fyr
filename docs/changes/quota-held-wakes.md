# V0.quota-held-wakes: a thread waiting for its provider's quota is not woken by notices or by other agents' messages

→ V0.quota-auto-continue (the wait ends by itself at the reset), V0.thread-invariants (`thread-failure-churn`).

`thread-failure-churn` fired on a thread stopped by the provider's quota: a queued notice (a closed question) woke it, the provider refused the turn, and then another thread's `thread_send` woke it again, so three failed runs came within two minutes with nobody at the keyboard.

1. **Notices wait.** A thread whose `QuotaWaitText` is not empty is not wakeable for the feed (`Thread.wakeableAt`): `project`, `feed.take` at idle, the snooze-when-done check and `OldestWakingNotice` all use it. The notices stay queued and are delivered by the auto-continue at the reset, which clears the failure first. Once loop protection hands the failure to the owner (`QuotaWaitText` is empty), notices wake it as any failed thread.
2. **An agent's message is refused.** `thread_send` to a thread in a quota wait returns an error naming the wait; the owner's own send is untouched.

Tests: `internal/core/quota_test.go` (`TestQuotaWaitHoldsFeedWakes`); `internal/engine/thread_invariants_test.go` (`TestAgentSendToQuotaWaitRefused`).
