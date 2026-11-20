# V0.stable-groups: a row keeps its place in its group; a new row goes last

→ V0.26, V0.207, V0.your-move. *Amends V0.26 (1) (Working newest started first), V0.207 (1) (Unread newest first) and the state order inside a Your move group.* 

(1) **Order.** Inside each group of Your move (Answer, Approve, Unblock, Questions in text), in Working and in Unread, rows are oldest first by when the thread came into that group. A thread that comes in later goes to the bottom. Nothing a thread does while it stays in its group moves it: not activity, not a new run, not a change of state inside the group (failed and blocked are both Unblock).

(2) **The time** is kept by the client (`trackArrivals`, `web/src/still-list.ts`). A thread that leaves its group and comes back gets a new time. On a first sight (a reload) the time is the thread's own: its run's start in Working, else its last activity, so a reload keeps the same order as near as the host can tell.

(3) **Unchanged.** The order of the groups of Your move; Seen (read last first, V0.207), Snoozed and Settled (newest activity first), which are history, not work in hand; the queue's order (V0.137); the open row's held place on the desktop (V0.open-row-only). One implementation for both fronts.
