// The desktop keyboard (SPEC 6.x keys), and opening the thread a notification tap named.
//
// Reads:   data.store; cmds; open (view, marked row, zone, picked, decision cards and their setters); ui (dialog, help,
//          settings page); device (phone, pendingOpen); inbox (search, order, walkRows); h.cards, h.actions, h.menu,
//          h.list, h.projects (the folder picker's Esc), h.history.navGo.
// Returns: nothing: it only listens (one keydown handler, always the latest through keyRef; the effect deps are unchanged).
// Desktop only: the phone has a touch form for every key (design/mobile, section 2), so the handler returns early there.
// The notification tap is shared: the phone navigates to the thread, the desktop opens it.

import { isTextTarget } from "./app-util.tsx";
import { sessionStore } from "./prefs.ts";
import { useEffect, useRef } from "preact/hooks";
import {
  SECTIONS,
  isAvailable,
  resolveRef,
} from "./store.ts";
import { stepRow } from "./selection.ts";
import { cardKey, goTo } from "./question.ts";
import { escapeTarget } from "./settings-nav.ts";
import { snoozeChoiceForKey } from "./snooze.ts";
import { takeOpen } from "./push.ts";

import type { AppState } from "./app-state.tsx";
import type { DecisionCards } from "./app-cards.tsx";
import type { ThreadActions } from "./app-actions.tsx";
import type { ContextMenuState } from "./app-menu.tsx";
import type { ThreadList } from "./app-list.tsx";
import type { ProjectActions } from "./app-projects.tsx";
import type { NavHistory } from "./app-history.tsx";

export function useKeyboard(s: Pick<AppState, "data" | "cmds" | "open" | "ui" | "device" | "inbox">, h: { cards: DecisionCards; actions: ThreadActions; menu: ContextMenuState; list: ThreadList; projects: ProjectActions; history: NavHistory }) {
  const { store } = s.data;
  const { cmds } = s;
  const { setLastOpen, view, sel, zone, setZone, picked, setPicked, setCards, viewThread } = s.open;
  const { dialog, setDialog, setHelp, settingsAt, setSettingsAt, openPicker } = s.ui;
  const { phone, phoneRef, pendingOpen, setPendingOpen } = s.device;
  const { query, setQuery, searchRef, order, walkRows } = s.inbox;
  const { decision, card } = h.cards;
  const { openThread, closePeek, closeThread, target, settle, settleGroup, archive, openSnooze, snoozeList, snooze, markUnseen } = h.actions;
  const { submitAsk, cancelAsk, allow, nextNeeding } = h.menu;
  const { toggleList, jump } = h.list;
  const { folderPickRef, closeFolderPick } = h.projects;
  const { navGo } = h.history;
  // ---- keyboard ----
  const gPending = useRef(0);
  const lastEsc = useRef(0);
  const keyRef = useRef<(e: KeyboardEvent) => void>(() => {});
  keyRef.current = (e: KeyboardEvent) => {
    if (phoneRef.current) return; // the phone has touch forms for every key (design/mobile, section 2)
    const k = e.key;
    const mod = e.metaKey || e.ctrlKey;

    if (folderPickRef.current) {
      if (k === "Escape") {
        e.preventDefault();
        closeFolderPick(null);
      }
      return;
    }
    if (dialog) {
      if (k === "Escape") {
        e.preventDefault();
        // V0.119: on a Settings sub-page Esc goes to its list (a changed text field took its Esc first).
        const up = dialog === "settings" ? escapeTarget(settingsAt) : null;
        if (up) setSettingsAt(up);
        else setDialog(null);
        return;
      }
      if (dialog === "snooze") {
        // SPEC 0.5 V0.67: the keys in the order shown; V0.185: a choice that is off has no key.
        const p = snoozeChoiceForKey(snoozeList(), k);
        if (p) {
          e.preventDefault();
          snooze(p);
        }
      }
      return;
    }

    if (mod && (k === "k" || k === "K")) {
      e.preventDefault();
      searchRef.current?.focus();
      searchRef.current?.select();
      return;
    }
    if (mod && (k === "z" || k === "Z") && !e.shiftKey && !isTextTarget(e.target)) {
      if (cmds.canUndo()) {
        e.preventDefault();
        void cmds.doUndo();
      }
      return;
    }
    if (mod || e.altKey) return;

    const el = e.target as HTMLElement | null;
    // A focused button (the project chip) takes Enter and Space itself.
    if ((k === "Enter" || k === " ") && el?.tagName === "BUTTON") return;
    if (isTextTarget(e.target)) {
      if (el?.dataset?.other && decision?.kind === "ask" && card && k === "Tab") {
        e.preventDefault();
        setCards((c) => ({ ...c, [decision.id]: goTo(card, decision, card.index + (e.shiftKey ? -1 : 1)) }));
        return;
      }
      if (k === "Escape") {
        if (el === searchRef.current && query) setQuery("");
        e.preventDefault();
        el?.blur();
        lastEsc.current = Date.now();
      }
      return;
    }

    // g1–g6
    if (gPending.current && Date.now() - gPending.current < 1500 && /^[1-6]$/.test(k)) {
      gPending.current = 0;
      e.preventDefault();
      const g = SECTIONS.find((s) => s.g === Number(k)); // V0.137: Waiting for a slot has no g key
      if (g) jump(g.key);
      return;
    }
    gPending.current = 0;

    // Decision card keys when the open thread has focus.
    if (zone === "thread" && view?.mode === "open" && decision) {
      if (decision.kind === "ask" && card) {
        const r = cardKey(card, decision, k, { shift: e.shiftKey });
        if (r.handled) {
          e.preventDefault();
          setCards((c) => ({ ...c, [decision.id]: r.state }));
          if (r.focusOther) document.getElementById("composer")?.focus();
          if (r.submit) submitAsk(decision);
          if (r.cancel) cancelAsk(decision);
          return;
        }
      } else if ((decision.kind === "approval" || decision.kind === "effect") && (k === "1" || k === "2")) {
        e.preventDefault();
        allow(decision, k === "1");
        return;
      }
    }

    const tgt = target();
    switch (k) {
      case "j":
      case "ArrowDown":
      case "k":
      case "ArrowUp": {
        e.preventDefault();
        const dir = k === "j" || k === "ArrowDown" ? 1 : -1;
        // V0.186: over the drawn rows only; a folded section is passed and stays folded.
        const to = stepRow(walkRows, order, sel, dir);
        // V0.109: j/k opens the row it marks when a thread is open; with nothing open it only marks it.
        if (to) {
          if (view) openThread(to, view.mode);
          else setLastOpen(to);
        }
        setZone("list");
        return;
      }
      case "n":
      case "N":
        e.preventDefault();
        nextNeeding(k === "n" ? 1 : -1);
        return;
      case "Enter":
        e.preventDefault();
        if (zone === "thread" && view?.mode === "open") document.getElementById("composer")?.focus();
        else if (sel) openThread(sel, "open");
        return;
      case " ":
        e.preventDefault();
        if (!sel) return;
        if (view?.mode === "peek" && view.id === sel) closePeek();
        else openThread(sel, "peek");
        setZone("list");
        return;
      case "e":
        e.preventDefault();
        if (tgt) settle(tgt);
        return;
      case "E":
        e.preventDefault();
        settleGroup();
        return;
      case "s":
        e.preventDefault();
        openSnooze(picked.size ? [...picked] : tgt ? [tgt] : []);
        return;
      case "#":
        e.preventDefault();
        archive(picked.size ? [...picked] : tgt ? [tgt] : []);
        return;
      case "u":
        e.preventDefault();
        if (tgt) markUnseen(tgt);
        return;
      case "r":
        e.preventDefault();
        if (tgt) void cmds.run("thread.retry", [tgt]);
        return;
      case "x": {
        e.preventDefault();
        if (!sel) return;
        const n = new Set(picked);
        if (n.has(sel)) n.delete(sel);
        else n.add(sel);
        setPicked(n);
        return;
      }
      case "p":
        e.preventDefault();
        if (tgt) openPicker(picked.size ? [...picked] : [tgt]);
        return;
      case "c":
        e.preventDefault();
        setDialog("new");
        return;
      case "g":
        gPending.current = Date.now();
        return;
      case "/":
        e.preventDefault();
        searchRef.current?.focus();
        return;
      case "?":
        e.preventDefault();
        setHelp((h) => !h);
        return;
      case "\\":
        e.preventDefault();
        toggleList();
        return;
      case "Escape": {
        e.preventDefault();
        if (view?.mode === "peek") return closePeek();
        if (picked.size) return setPicked(new Set());
        const t = viewThread;
        const fromComposer = Date.now() - lastEsc.current < 800;
        if (t?.running && (zone === "thread" || fromComposer) && isAvailable(t, "thread.interrupt")) {
          void cmds.run("thread.interrupt", [t.id]);
          return;
        }
        // V0.status-home: Esc in the list closes the thread; the first Esc brought the focus here.
        if (zone === "list" && view) return closeThread();
        setZone("list");
        return;
      }
    }
  };

  useEffect(() => {
    const h = (e: KeyboardEvent) => keyRef.current(e);
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, []);

  useEffect(() => {
    // V0.client-refs: a tap names the ref; one from an older build names the id alone (decision 0031 C12).
    const want = pendingOpen && resolveRef(Object.values(store.threads), pendingOpen);
    if (!want || !store.threads[want]) return;
    if (phone) navGo({ screen: { name: "thread", id: want }, sheet: null });
    else openThread(want, "open");
    takeOpen(sessionStore()); // on screen now: done
    setPendingOpen(null);
  }, [pendingOpen, store.threads]);

  // V0.50: Settings reloads the profiles after a provider changed.
}
