# V0.phone-no-guest: on the phone no row is current, so no row punches through a fold or a filter

→ V0.192, V0.109, V0.196. *Amends V0.192 (1) (the phone after Back had a current thread too, so a folded section drew its row) and V0.192 (2), (3) for the phone.*

(1) **Why the phone is different.** V0.109 (5) says the phone marks no row: it shows the inbox or one thread, never both, so there is no current thread for a mark to follow. V0.192 then carried the desktop's "last thread opened" over to the phone anyway, and the inbox drew that thread's row inside a folded section, dashed and tagged "In folded section". Nothing on the phone had asked for it: the row appeared under a section the owner had folded, in a list where the thread it names is not open.

(2) **What the phone does now.** A folded section draws its rows or nothing, as the fold says. A host or project filter and quiet hours hide their threads on the phone (V0.212 said this already). No row is tagged on the phone, and no row is drawn that the list would not draw anyway.

(3) **What stays.** The desktop is unchanged: the current thread's row is in its place, dashed and tagged, in a folded section alone (V0.192 (2), (3)). The rule is one function (`guestOf` in `web/src/selection.ts`), which the app calls with the front it is on.

(4) **The auto-close follows.** Snoozed and Settled opened from their header close after 10 s unless the current thread is in them (V0.196 (3)). The phone has no current thread, so there a section never waits for one, and its 10 s stand as written.

Tests: `web/test/selection.test.ts` ("V0.phone-no-guest"); `web/test/layout.e2e.mjs` section "V0.192 folded phone" (Back from a thread in folded Settled draws no row) and "V0.192 filter phone".
