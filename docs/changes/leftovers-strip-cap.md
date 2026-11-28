# V0.leftovers-strip-cap: group and cap leftover processes in the thread strip, load card, and ended line

→ V0.176, V0.hover-cards.

(1) **The bug.** When a thread accumulated hundreds of leftover processes (for example, repeated `dbus-daemon` or `gnome-keyring-daemon` daemons), `stripText` printed every process name in a comma-separated list inside `.lftb` (`flex: none`) with no height bound. The strip expanded to fill the whole viewport on phone and desktop, pushing its own buttons, the conversation, and the composer off screen. The same unbounded list also overflowed the desktop rail's hover card, the phone's load sheet, and `LeftoverLine`'s conversation note.

(2) **The fix.**
- Group leftovers by process name (`name ×N` when `N > 1`, summing their RSS and sorting heaviest combined RSS first, then larger count, then pid/order).
- Cap the open thread's strip (`stripText`) and the ended note (`LeftoverLine`) at 3 groups, appending `+M more` for the remaining processes.
- Cap the rail hover card and phone load sheet (`hostLeftovers`) at 6 groups, appending a `+M more` row with the remaining memory.
- Clamp `.lftb-b` to 3 lines on both desktop and phone (with the full summary in `title`) so even long process names or errors cannot push the conversation off screen, and refresh load once after `Keep`/`End` finishes the loop instead of once per process.

Tests: `web/test/leftovers.test.ts` ("repeated names group with ×N and long lists cap with +M more on the strip and load card"), `internal/engine/leftovers_test.go` (`TestV176TheEndedLineIsOnTheThreadAndANoticeNotAWake`), and `web/test/layout.e2e.mjs` (`V0.176 desktop` and `V0.176 phone` with `?leftovers=many`, light and dark).
