# V0.one-action-list: one list of a thread's actions behind the desktop's row menu, its ⋯ menu and the phone's sheets, run by one dispatch

→ V0.26 (the thread ⋯ menu), V0.31 (the right-click menu), V0.104 (pin), V0.189 (seenToggle), V0.203 (settle when done); CLAUDE.md (desktop and phone stay in sync).

(1) **The cause.** What a thread offers was written three times: the right-click menu (`threadMenu`, its ids mapped to commands by a second table in the app), the desktop header's ⋯ menu and the phone's action sheets. The lists had drifted. The phone had no Unsettle, Unsnooze, Unarchive, Stop run, Dismiss PR or Copy link. The desktop's row menu had no Retry. The toast "Settled; run stopped" was built in two places.

(2) **One list.** `threadActions(t, where)` (`web/src/actions.ts`) returns every action of the thread, in menu order: its id, its kind (a host command, a sheet, open the thread, copy its link, start a selection), its label, the desktop key, a hint, and the reason the host returned in `unavailable` when it cannot be used. `where` is a row (the right-click menu, the phone's peek sheet) or the open thread (the header, the phone's thread sheet). Only a row offers Open.

(3) **One dispatch.** `runAction` runs a pick on both fronts; a front gives only how a sheet opens and what its settle, archive and mark unseen do beyond the command (leave the thread, keep it unread). It replaces the app's id-to-command table. `settleToast` is the one place of the stopped-run toast.

(4) **Every surface renders from it.** The right-click menu draws the row's list with a line between groups. The ⋯ menu draws the open thread's list less the header's buttons (Retry, Pin or Unpin, Snooze, Settle: `DESK_HEADER_BUTTONS`), the thread's own entries first, so it still opens on Project… (V0.26). The phone's peek and thread sheets draw the same lists with their own icons and swipe hints and without the keys (the phone has no keyboard). A disabled entry shows the host's reason on both.

(5) **The differences, with their reason, written in the list.** Details is on the phone's thread sheet only: the desktop's header shows them at all times, the phone's header has no room. Select is on the phone's peek sheet only: the desktop picks rows with x or a ⌘/⇧-click, the phone has neither. The ⋯ menu shows Settle when done, Unsettle, Unsnooze and the rest it did not show before; the live view entry reads "Turn on live view" / "Turn off live view" on both.

Tests: `web/test/actions.test.ts` (every action is reachable on both fronts over a matrix of thread states unless its `only` says why; the drift above is gone; one dispatch; a disabled entry runs nothing).
