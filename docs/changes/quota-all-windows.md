# V0.quota-all-windows: a Claude account's quota shows every usage window the provider reports, not only 5 h and 7 d

→ V0.44 (1) and (3), V0.122, V0.quota-reset-wake. *Amends V0.44 (3): the usage reply is no longer read for `five_hour` and `seven_day` only.*

Why: a Claude subscription has more limits than the account-wide 5 h and 7 d windows. There is a weekly limit per model (`seven_day_opus`, `seven_day_sonnet`) and one for OAuth apps (`seven_day_oauth_apps`). Fyr read only the two account windows. An account could show "7 d 52 %" while its weekly Opus limit was nearly used up, and runs on it then stopped with no warning.

1. **Every window is read.** The host reads each key of the usage reply, and each key of a `rate_limit_event`'s `unifiedWindows` (or its `rateLimitType`), that has the shape `<span>_<unit>[_<scope>]` (`five_hour`, `seven_day`, `seven_day_opus`, …) and a numeric `utilization`. A `null` window is left out. Keys of other shapes are not windows: flags, `extra_usage` (a credit pool with no reset), `overage`, code names. So a window the provider adds later shows without a code change.
2. **Names.** The span is written as before ("5 h", "7 d"), and the scope follows it: "7 d Opus", "7 d Sonnet", "7 d apps"; an unknown scope is shown as its words. The client's card writes "7 days, Opus".
3. **Order.** Shorter spans come first. A whole-account window comes before its scoped ones ("5 h", "7 d", "7 d Opus", "7 d Sonnet").
4. **Everything else follows.** The chip, the card, the rail's busiest figure, "Running low" and "Used up" all go by the busiest window, so a scoped window at 80 % colours the account. A quota wake (V0.quota-reset-wake) takes the new names, either the provider's spelling (`seven_day_opus`) or Fyr's in any case (`7 d opus`).

Tests: `internal/quota/quota_test.go` (TestAnthropicUsageReadsEveryWindow, TestWindowName, TestStreamEventUpdatesWindows), `web/test/quota.test.ts`.
