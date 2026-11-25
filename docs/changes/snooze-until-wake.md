# V0.snooze-until-wake: a thread that waits for a time is snoozed until then with one press

→ V0.waits-strip (the strip and its cancel), V0.67 (the snooze). *Extends V0.waits-strip (1): a wait on a time carries the time, and its row has a second button.* (owner, of the strip "Waiting: wakes 26 Nov 09:00": a button to snooze until then.)

1. **The time.** A wait of `waits` on a time (a wake-up, a quota reset) carries `at`, the time it wakes. A wait on anything else (a change's phase, a check, the run's end, idleness) has none.
2. **The button.** In the strip above the composer, a wait with a future `at` has **Snooze until then** before **Cancel wait**, on the desktop and the phone (one component, `web/src/waits-ui.tsx`). A press is the owner's `thread.snooze {until: at}`: no dialog, and the thread leaves as after the dialog's snooze. The wait stays armed; the snooze ends at the moment the agent wakes. The button is absent when the thread is snoozed already or the host does not take a snooze of it (`unavailable`).
3. **Phone.** The phone's button reads "Snooze" (its accessible name is the full one): two 44 px buttons and the wait's words share a 360 px row. The words take at most three lines there, so a long wait keeps the composer on a 360x640 screen (layout test, "thread on an offline host").

Tests: `internal/core/wake_test.go` (TestWaitOnATimeCarriesIt); `web/test/waits.test.ts` (`snoozeUntilWait`).
