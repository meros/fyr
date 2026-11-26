# V0.snoozed-wake-order: Snoozed lists the thread that wakes first on top

→ V0.26, V0.67, V0.185.

Snoozed was ordered by newest activity, so the thread the section's "next: 09:00" hint named could sit anywhere in it. It is now ordered by when each thread wakes, earliest first. A snooze until work is done has no wake time of its own (its `snoozeUntil` is only the 24 h cap), so those go after the timed ones, as the hint leaves them out too. Ties keep newest activity first. The order is `sections()` in `web/src/store.ts`, which both fronts draw.

Test: `web/test/store.test.ts` ("snoozed sorts by wake time, earliest first; a snooze until work is done goes last").
