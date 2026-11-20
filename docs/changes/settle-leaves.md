# V0.settle-leaves: settling the open thread lets go of it: the desktop opens the next row, or the status screen when none is left; the phone goes back to the list

→ V0.109, V0.status-home, V0.203, U8. *Amends V0.109 (2) ("from the thread pane the thread stays open": it no longer does) and V0.109 (2) for the last row (no row left now shows the status screen).* 

(1) **Desktop.** Settle of the open thread (`e`, the thread's Settle button, the row menu, `⇧E` when the group holds it) opens the next drawn row, else the one before, from the list and from the thread pane alike. A peek stays a peek and the focus stays in the pane it was in. When no row is left the thread closes as by V0.status-home (1): no row is marked and the status screen shows. Settling another row leaves the open thread where it is. Archive and snooze are unchanged.

(2) **Phone.** Unchanged (U8): settling the open thread goes back to the inbox through Back.

(3) **Tests.** `web/test/selection.test.ts` (`afterSettle`: next, the one before, none left, a group, another row, nothing marked).
