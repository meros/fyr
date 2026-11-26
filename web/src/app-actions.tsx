// What the owner does to threads: open, close, settle, archive, snooze, mark unseen, and where the desktop goes when
// threads leave the list (V0.app-shell-tidy: only once the host took the command). A hook of the App in app.tsx; it
// takes and returns plain objects.

import { useRef } from "preact/hooks";
import type { Thread } from "./types.ts";
import {
  bulkSettle,
  bulkSettleText,
  sectionOf,
} from "./store.ts";
import { afterSettle } from "./selection.ts";
import { leaveAfterHost } from "./leave.ts";
import { closeView, markedUnseen, openView } from "./seen.ts";
import { snoozeChoices, snoozeCommand, type SnoozeChoice } from "./snooze.ts";
import { settleRun } from "./actions.ts";
import { SETTLE_DONE_TOAST } from "./menu.ts";
import { isDeskEntry, leaveDeskThread, recordOpen, type OpenCause } from "./desk-history.ts";
import type { useAppState } from "./app-state.tsx";

export function useThreadActions(c: Pick<ReturnType<typeof useAppState>, "store" | "setLastOpen" | "view" | "setView" | "sel" | "zone" | "setZone" | "picked" | "setPicked" | "setDialog" | "snoozeTargets" | "setSnoozeTargets" | "setTracker" | "phoneRef" | "cmds" | "sec" | "order" | "walkRows">) {
  const { store, setLastOpen, view, setView, sel, zone, setZone, picked, setPicked, setDialog, snoozeTargets, setSnoozeTargets, setTracker, phoneRef, cmds, sec, order, walkRows } = c;
  // ---- actions (#41: run, undo, toasts and errors live in useThreadCommands) ----

  /** V0.145: on the desktop a thread the owner opens is one history entry; Fyr's own openings (back and forward, the
   *  first thread on load) add none. The phone keeps its own history (nav.ts). */
  const deskAtRef = useRef(0); // the place of the desktop entry on screen (a pop's direction)
  const openThread = (id: string, mode: "open" | "peek" = "open", cause: OpenCause = "owner") => {
    if (!phoneRef.current) {
      recordOpen(id, cause);
      if (isDeskEntry(history.state)) deskAtRef.current = history.state.desk;
    }
    setLastOpen(id);
    setView({ id, mode });
    setTracker((tr) => openView(tr, id, mode, Date.now(), document.hasFocus()));
    if (mode === "open") setZone("thread");
  };

  const closePeek = () => {
    setView(null);
    setTracker((tr) => closeView(tr));
  };

  /** V0.status-home: the owner closes the open thread (a second Esc, a click on the list's empty space): no row stays
   *  marked, the thread pane shows the status screen, and a reload lands there too. */
  const closeThread = () => {
    if (!view) return;
    closePeek();
    setLastOpen(null);
    setZone("list");
    if (!phoneRef.current) leaveDeskThread();
  };

  /** The thread an action applies to: the open thread when the thread pane has focus, else the selected row. */
  const target = (): string | null => (zone === "thread" && view ? view.id : sel);

  /** V0.settle-leaves, V0.app-shell-tidy: threads that left the list (settled, archived, snoozed) are let go, from the
   *  list and from the thread pane alike: the next row opens (a peek stays a peek, the focus stays where it was), else
   *  the status screen. It runs once the host took the command (`leaveAfterHost`) and reads the marked row then, so an
   *  owner who moved on meanwhile stays where they went. The order is the one the owner saw when acting. The phone
   *  leaves through Back (U8, leaveOpenThread), so this is the desktop's. */
  const selRef = useRef(sel);
  selRef.current = sel;
  const leaveGone = (ids: string[]) => {
    if (phoneRef.current) return;
    const to = afterSettle(order, selRef.current, ids, walkRows);
    if (to === "stay") return;
    if (to === "home") return view ? closeThread() : setLastOpen(null);
    const z = zone;
    if (view) openThread(to, view.mode);
    else setLastOpen(to);
    setZone(z);
  };

  /** V0.203: settles now; on a running thread the host stops the run, and the toast says so. */
  const settle = (id: string) => {
    void leaveAfterHost(cmds.took("thread.settle", [id], undefined, settleRun(store.threads[id])), leaveGone);
  };

  /** V0.203: the explicit wait, or its cancel when it is armed. The thread stays where it is. */
  const settleWhenDone = (id: string) => {
    if (store.threads[id]?.settleWhenDone) void cmds.run("thread.unsettle", [id]);
    else void cmds.run("thread.settleWhenDone", [id], undefined, { label: () => SETTLE_DONE_TOAST });
  };

  /** V0.162: every bulk settle, desktop and phone, goes through here: pinned threads are left out and counted. */
  const settleBulk = (ids: string[], o: { sweep?: boolean; unreadKept?: number } = {}): Promise<string[]> => {
    const r = bulkSettle(ids.map((id) => store.threads[id]).filter((t): t is Thread => !!t), o.sweep);
    const { ids: take, kept, running } = r;
    const unread = r.unread + (o.unreadKept ?? 0); // V0.189: what the phone's Select all left out
    if (!take.length) {
      if (kept || unread || running) cmds.notify(bulkSettleText(0, kept, unread, running));
      else cmds.showError("Nothing here can be settled");
      return Promise.resolve([]);
    }
    return cmds.took("thread.settle", take, { now: true }, { label: (n) => bulkSettleText(n, kept, unread, running) });
  };

  const settleGroup = () => {
    let ids = [...picked];
    const sweep = !ids.length; // V0.189: the marked row's whole section; picked rows are the owner's explicit choice
    if (sweep && sel) {
      const k = sectionOf(store.threads[sel]);
      if (k) ids = sec[k].map((t) => t.id);
    }
    setPicked(new Set());
    void leaveAfterHost(settleBulk(ids, { sweep }), leaveGone);
  };

  /** The Seen header's "Settle merged" (the phone's button too): only the threads whose PR merged. */
  const settleMerged = (ids: string[]) => void leaveAfterHost(settleBulk(ids), leaveGone);

  const archive = (ids: string[]) => {
    setPicked(new Set());
    void leaveAfterHost(cmds.took("thread.archive", ids), leaveGone);
  };

  const openSnooze = (ids: string[]) => {
    if (!ids.length) return;
    setSnoozeTargets(ids);
    setDialog("snooze");
  };

  const snoozeList = () => snoozeChoices(new Date(), snoozeTargets.flatMap((id) => store.threads[id] ?? []));

  const snooze = (p: Pick<SnoozeChoice, "until" | "done" | "ids">) => {
    const c = snoozeCommand(p, snoozeTargets);
    setDialog(null);
    setPicked(new Set());
    void leaveAfterHost(cmds.took(c.action, c.ids, c.args), leaveGone);
  };

  /** V0.snooze-until-wake, V0.snooze-every-wait: a snooze until one of the thread's waits ends, with no dialog. */
  const snoozeUntilWait = (ids: string[], rule: string) => {
    void leaveAfterHost(cmds.took("thread.snoozeUntilWait", ids, { rule }), leaveGone);
  };

  const markUnseen = (id: string) => {
    if (view?.id === id) setTracker((tr) => markedUnseen(tr));
    void cmds.run("thread.markUnseen", [id]);
  };


  return { deskAtRef, openThread, closePeek, closeThread, target, settle, settleWhenDone, settleBulk, settleGroup, settleMerged, archive, openSnooze, snoozeList, snooze, snoozeUntilWait, markUnseen };
}
