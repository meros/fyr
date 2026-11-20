# V0.open-row-only: only the open row keeps its place; every other row moves at once

→ V0.207, V0.your-move, V0.192. *Removes V0.207 (3) (the hold) and what depended on it: V0.207 (4) and V0.your-move (3) (the "2 updates · Show" line).* 

(1) **No hold.** The list does not wait for the owner's hand. The mouse pointer in the list, j/k, a finger on the list and a scroll hold nothing back, and no timer runs. A row changes section and place the moment its state does.

(2) **The open row** is the one thing that stays, as V0.207 (2) says: the current thread keeps the section and place it had when it was opened, until another thread is current. It still leaves at once for Snoozed, Settled or the queue, and when the owner marks it seen or unseen by hand.

(3) **What stands.** The order of V0.207 (1), so later activity does not move an unread row. The anchor of V0.207 (5), so a row that comes in above does not push the row under the pointer or the open row off its height; it corrects the scroll and delays nothing.

(4) **Both fronts.** One implementation (`web/src/still-list.ts`, `still-list-ui.ts`); the line and its styles are gone from the desktop and the phone alike.
