// What a person does to a thread: run, settle, archive, snooze, answer. (split from the App of app.tsx; takes and returns plain objects).

// The app shell: state, commands and the keyboard.

import { useRef } from "preact/hooks";
import type { Thread } from "./types.ts";
import {
  bulkSettle,
  bulkSettleText,
  sectionOf,
} from "./store.ts";
import { afterSettle, rowAfter } from "./selection.ts";
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

  /** In the list, the marked row is about to leave: open the next row (V0.109: the mark is the open thread). In the
   * thread pane the thread stays open. */
  const stepPast = (ids: string[]) => {
    if (zone !== "list") return;
    const next = rowAfter(order, sel, ids, walkRows);
    if (!next) return;
    if (view) openThread(next, view.mode);
    else setLastOpen(next);
    setZone("list");
  };

  /** V0.settle-leaves: a settle lets go of the marked thread, from the list and from the thread pane: the next row
   *  opens (a peek stays a peek, the focus stays where it was), else the status screen. The phone leaves through Back
   *  (U8, leaveOpenThread), so this is the desktop's. */
  const leaveSettled = (ids: string[]) => {
    if (phoneRef.current) return;
    const to = afterSettle(order, sel, ids, walkRows);
    if (to === "stay") return;
    if (to === "home") return view ? closeThread() : setLastOpen(null);
    const z = zone;
    if (view) openThread(to, view.mode);
    else setLastOpen(to);
    setZone(z);
  };

  /** V0.203: settles now; on a running thread the host stops the run, and the toast says so. */
  const settle = (id: string) => {
    // The next thread opens once the host took the settle, as the phone leaves (U8): a refused settle leaves the thread where it is.
    void cmds.run("thread.settle", [id], undefined, settleRun(store.threads[id])).then((n) => n && leaveSettled([id]));
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
    return cmds.run("thread.settle", take, { now: true }, { label: (n) => bulkSettleText(n, kept, unread, running) }).then((n) => (n ? take : []));
  };

  const settleGroup = () => {
    let ids = [...picked];
    const sweep = !ids.length; // V0.189: the marked row's whole section; picked rows are the owner's explicit choice
    if (sweep && sel) {
      const k = sectionOf(store.threads[sel]);
      if (k) ids = sec[k].map((t) => t.id);
    }
    setPicked(new Set());
    void settleBulk(ids, { sweep }).then((done) => done.length && leaveSettled(done));
  };

  /** The Seen header's "Settle merged" (the phone's button too): only the threads whose PR merged. */
  const settleMerged = (ids: string[]) => void settleBulk(ids).then((done) => done.length && leaveSettled(done));

  const archive = (ids: string[]) => {
    stepPast(ids);
    setPicked(new Set());
    void cmds.run("thread.archive", ids);
  };

  const openSnooze = (ids: string[]) => {
    if (!ids.length) return;
    setSnoozeTargets(ids);
    setDialog("snooze");
  };

  const snoozeList = () => snoozeChoices(new Date(), snoozeTargets.flatMap((id) => store.threads[id] ?? []));

  const snooze = (p: SnoozeChoice) => {
    const c = snoozeCommand(p, snoozeTargets);
    setDialog(null);
    stepPast(c.ids);
    setPicked(new Set());
    void cmds.run(c.action, c.ids, c.args);
  };

  const markUnseen = (id: string) => {
    if (view?.id === id) setTracker((tr) => markedUnseen(tr));
    void cmds.run("thread.markUnseen", [id]);
  };


  return { deskAtRef, openThread, closePeek, closeThread, target, settle, settleWhenDone, settleBulk, settleGroup, settleMerged, archive, openSnooze, snoozeList, snooze, markUnseen };
}
