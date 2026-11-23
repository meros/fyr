// The desktop's thread list: shown or hidden (V0.40), its sections opening, closing by themselves and jumped to, the
// open row kept in view, and sending in the open thread. A hook of the App in app.tsx; it takes and returns plain
// objects.

import { useEffect, useState } from "preact/hooks";
import {
  sectionOf,
  isPinned,
  type SectionKey,
} from "./store.ts";
import { placeOf } from "./still-list.ts";
import { cutSection } from "./selection.ts";
import { isOpen } from "./inbox.tsx";
import { listPref, listShown, setListPref } from "./prefs.ts";
import { AUTO_HIDE_MS, AUTO_HIDE_SECTIONS, hideClocks, hideStep, type HideAt } from "./inbox-autohide.ts";
import type { useAppState } from "./app-state.tsx";
import type { useThreadActions } from "./app-actions.tsx";
import { type SendArgs } from "./composer.tsx";

export function useThreadList(c: Pick<ReturnType<typeof useAppState>, "store" | "setLastOpen" | "view" | "sel" | "setZone" | "sectionOpen" | "setSectionOpen" | "setExpanded" | "phone" | "cmds" | "limits" | "listed" | "rows" | "sec" | "guest" | "order"> & Pick<ReturnType<typeof useThreadActions>, "openThread">) {
  const { store, setLastOpen, view, sel, setZone, sectionOpen, setSectionOpen, setExpanded, phone, cmds, limits, listed, rows, sec, guest, order, openThread } = c;
  // ---- the thread list (V0.40): hideable, hidden by default in a narrow window ----
  const [listPrefState, setListPrefState] = useState<boolean | null>(() => listPref());
  const [winW, setWinW] = useState(() => (typeof window !== "undefined" ? window.innerWidth : 1440));
  useEffect(() => {
    const on = () => setWinW(window.innerWidth);
    window.addEventListener("resize", on);
    return () => window.removeEventListener("resize", on);
  }, []);
  const listOn = listShown(listPrefState, winW, !!view);
  const toggleList = () => {
    setListPrefState(!listOn);
    setListPref(!listOn);
  };

  // V0.48, V0.196: Snoozed and Settled opened from their header close again after 10 s, on both fronts, unless the
  // current thread is in them. V0.phone-no-guest: the phone has no current thread on the list (V0.109 (5)), so there a
  // section never waits for one.
  const [hideAt, setHideAt] = useState<HideAt>({});
  const [ahTick, setAhTick] = useState(0);
  const activeForHide: SectionKey | null = !phone && sel && store.threads[sel] ? sectionOf(store.threads[sel]) : null;
  const setHide = (k: SectionKey, at: number | null) =>
    setHideAt((h) => {
      const next = { ...h };
      if (at === null) delete next[k];
      else next[k] = at;
      return next;
    });
  useEffect(() => {
    const r = hideStep({ at: hideAt, open: (k) => isOpen(k, sectionOpen), active: activeForHide }, Date.now());
    if (r.close.length) {
      setSectionOpen((o) => ({ ...o, ...Object.fromEntries(r.close.map((k) => [k, false])) }));
      for (const k of r.close) setHide(k, null);
      return;
    }
    if (r.wait === null) return;
    const t = setTimeout(() => setAhTick((x) => x + 1), r.wait + 20);
    return () => clearTimeout(t);
  }, [hideAt, sectionOpen, activeForHide, ahTick, setSectionOpen]);
  /** A header's click or tap. */
  const toggleSection = (k: SectionKey) => {
    const opening = !isOpen(k, sectionOpen);
    if (AUTO_HIDE_SECTIONS.includes(k)) setHide(k, opening ? Date.now() + AUTO_HIDE_MS : null);
    setSectionOpen((o) => ({ ...o, [k]: !isOpen(k, o) }));
  };
  /** V0.186: the owner named this section in the rail or the strip: it opens and stays open (V0.48). */
  const nameSection = (k: SectionKey) => {
    setHide(k, null);
    setSectionOpen((o) => ({ ...o, [k]: true }));
  };
  /** Using a section restarts its time (at most a few times a second). */
  const sectionUsed = (k: SectionKey) => {
    const at = hideAt[k];
    if (at !== undefined && at - Date.now() < AUTO_HIDE_MS - 500) setHide(k, Date.now() + AUTO_HIDE_MS);
  };
  const hideShown = hideClocks(hideAt, activeForHide);

  const jump = (k: SectionKey) => {
    if (!sec[k].length) return; // an empty section is not a place to go
    if (!listOn) {
      setListPrefState(true); // jumping to a section shows the list (V0.40)
      setListPref(true);
    }
    nameSection(k);
    const first = sec[k][0];
    document.getElementById(`sec-${k}`)?.scrollIntoView({ block: "start" });
    if (first) {
      // V0.109: the row g1–g5 marks is the open thread.
      if (view) openThread(first.id, view.mode);
      else setLastOpen(first.id);
      setZone("list");
    }
  };

  // V0.109, V0.186: the open thread's row is drawn when its section is open: a cut section shows all, also when the row
  // moves there. A folded section stays folded, whatever opened the thread, and draws that row alone (V0.192). A row
  // that search hides stays hidden, and no row is marked; one the filters hide is added to `rows` (V0.192).
  const openSection = view && store.threads[view.id] ? sectionOf(store.threads[view.id]) : null;
  /* eslint-disable react-hooks/exhaustive-deps -- expands the open thread's section only when the open thread, the open section or whether it is listed changes; on every rows update it would re-expand a section the owner folded */
  useEffect(() => {
    const k = cutSection(rows, view?.id ?? null, limits);
    if (k) setExpanded((x) => ({ ...x, [k]: true }));
  }, [view?.id, openSection, !!(view && listed[view.id]), limits]);
  /* eslint-enable react-hooks/exhaustive-deps */

  // Keep the mark visible: when it changes, moves to another section or the pinned block, or is drawn again.
  const selPlace = sel && listed[sel] ? `${placeOf(rows, sel)?.section ?? sectionOf(listed[sel])}:${isPinned(listed[sel])}:${order.includes(sel)}:${guest?.why ?? ""}` : "";
  useEffect(() => {
    if (!sel) return;
    const id = CSS.escape(sel);
    document.querySelector(`.list [data-id="${id}"]`)?.scrollIntoView({ block: "nearest" });
  }, [sel, selPlace, listOn]);

  const send = async (args: SendArgs): Promise<boolean> => (view ? cmds.send(view.id, args) : false);


  return { winW, listOn, toggleList, hideAt, toggleSection, nameSection, sectionUsed, hideShown, jump, send };
}
