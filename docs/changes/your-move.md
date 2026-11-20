# V0.your-move: the inbox says whose move it is

→ V0.189, V0.199, V0.205, V0.207, V0.210, decision 0033. *Amends V0.207 (4) (a new row in the first section comes in at the top of its group, not above every held row) and the section names of V0.199; the order of V0.199 stands.* 

(1) **The names.** Needs you is **Your move**, Unread is **Finished, unread**, Seen is **Finished, seen**, Working is **Moving without you**. The rail and the phone's strip say Yours, Unread, Seen, Moving. The keys, the g keys, what lands in each section and the host's states do not change. "Needs you" stays on the Changes board and in Settings, which are about changes and hosts.

(2) **The groups.** Your move is grouped under small labels, in this order. **Answer**: an open question. **Approve**: an approval, a merge or other effect, a ready PR, a command to run. **Unblock**: a failure, an unresponsive agent, any other blocked cause. **Questions in text**: what Fyr only read in the agent's message; last, as decision 0033 asks. A thread with a question and a merge ask is in Answer. Inside a group the order is the state, then the newest activity. A group with no rows has no label.

(3) **What the hold keeps back.** While the list is held (V0.207) and rows wait to move, a floating line says "2 updates · Show". It takes no room, so it moves no row. A row counts once however many rows it shifts; the open row does not count. Show brings the moves now, and the hold goes on from there.

(4) **Both fronts.** The label, the line and the grouping are one implementation (`rows.ts`, `still-list.ts`, `row-ui.tsx`). The phone's line is 44 px high and its type a step larger, for the finger.

(5) **Not in this change.** A Stuck section: a 15-minute rule would have fired 25 times in 4.1 days, about half of them false (decision 0033). An "Optional, no rush" group: Questions in text answers no for an optional offer, so Fyr has no such fact to group by.
