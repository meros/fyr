// The app shell: state, commands and the keyboard.

import { render } from "preact";
import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Decision, ImageArg, ThreadDTO } from "./types.ts";
import { createApi } from "./api.ts";
import {
  SECTIONS,
  applySnapshot,
  flatOrder,
  initialStore,
  isAvailable,
  lastSeq,
  liveEvent,
  mergeEvents,
  recordUndo,
  sectionOf,
  sections,
  setConn,
  snoozePresets,
  undoCommands,
  undoKindOf,
  undoLive,
  upsertThread,
  UNDO_MS,
  type SectionKey,
  type SnoozePreset,
  type Store,
  type UndoEntry,
} from "./store.ts";
import { closeView, initialTracker, markSent, markedUnseen, openView, setAtEnd, setWindowFocus, shouldMarkSeen, type SeenTracker } from "./seen.ts";
import { answersOf, cardKey, goTo, initCard, setOther, type CardState } from "./question.ts";
import { keyHints } from "./keys.ts";
import { Inbox, Rail, SECTION_LIMIT, isOpen, type Theme } from "./inbox.tsx";
import { ThreadPane } from "./thread.tsx";
import { AdoptDialog, NewThreadDialog, SnoozeMenu, type NewThreadArgs } from "./dialogs.tsx";

/** Events per page of a thread timeline. */
const PAGE = 400;

const api = createApi();
const params = new URLSearchParams(location.search);

type Dialog = "new" | "adopt" | "snooze" | null;
type Zone = "list" | "thread";

interface Toast {
  text: string;
  undo: boolean;
  error?: boolean;
  at: number;
}

function readTheme(): Theme {
  try {
    const t = localStorage.getItem("fyr.theme");
    if (t === "light" || t === "dark") return t;
  } catch {
    /* storage blocked */
  }
  return "system";
}

function applyTheme(t: Theme) {
  if (t === "system") document.documentElement.removeAttribute("data-theme");
  else document.documentElement.setAttribute("data-theme", t);
  try {
    if (t === "system") localStorage.removeItem("fyr.theme");
    else localStorage.setItem("fyr.theme", t);
  } catch {
    /* storage blocked */
  }
}

function isTextTarget(t: EventTarget | null): boolean {
  const el = t as HTMLElement | null;
  if (!el || !el.tagName) return false;
  const tag = el.tagName;
  if (tag === "TEXTAREA" || tag === "SELECT") return true;
  if (tag === "INPUT") {
    const type = (el as HTMLInputElement).type;
    return type !== "checkbox" && type !== "radio" && type !== "button";
  }
  return el.isContentEditable;
}

function App() {
  const [store, setStore] = useState<Store>(initialStore);
  const [sel, setSel] = useState<string | null>(null);
  const [view, setView] = useState<{ id: string; mode: "open" | "peek" } | null>(null);
  const [zone, setZone] = useState<Zone>("list");
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [dialog, setDialog] = useState<Dialog>(null);
  const [snoozeTargets, setSnoozeTargets] = useState<string[]>([]);
  const [undo, setUndo] = useState<UndoEntry | null>(null);
  const [toast, setToast] = useState<Toast | null>(null);
  const [tracker, setTracker] = useState<SeenTracker>(initialTracker);
  const [cards, setCards] = useState<Record<string, CardState>>({});
  const [sectionOpen, setSectionOpen] = useState<Partial<Record<SectionKey, boolean>>>({});
  const [query, setQuery] = useState("");
  const [help, setHelp] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);
  const [expanded, setExpanded] = useState<Partial<Record<SectionKey, boolean>>>({});
  const [theme, setTheme] = useState<Theme>(readTheme);
  const [tick, setTick] = useState(Date.now());
  const [inText, setInText] = useState(false);

  const now = tick + store.clockOffset;
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return store.threads;
    const out: Record<string, ThreadDTO> = {};
    for (const t of Object.values(store.threads)) {
      if (`${t.title} ${t.project} ${t.pr ? "#" + t.pr.number : ""}`.toLowerCase().includes(q)) out[t.id] = t;
    }
    return out;
  }, [store.threads, query]);
  const sec = useMemo(() => sections(shown), [shown]);
  const limits = useMemo(() => {
    const l: Partial<Record<SectionKey, number>> = {};
    for (const s of SECTIONS) l[s.key] = !isOpen(s.key, sectionOpen) ? 0 : expanded[s.key] ? Infinity : SECTION_LIMIT[s.key];
    return l;
  }, [expanded, sectionOpen]);
  const order = useMemo(() => flatOrder(sec, limits), [sec, limits]);
  const viewThread = view ? store.threads[view.id] : undefined;

  // ---- connection ----
  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    api.start({
      state: (dto) => setStore((s) => applySnapshot(s, dto, Date.now())),
      thread: (t) => setStore((s) => upsertThread(s, t)),
      event: (e) => setStore((s) => liveEvent(s, e)),
      conn: (c) => setStore((s) => setConn(s, c)),
    });
    const iv = setInterval(() => setTick(Date.now()), 15_000);
    return () => clearInterval(iv);
  }, []);

  // Initial selection and ?open= for screenshots.
  const didInit = useRef(false);
  useEffect(() => {
    if (!store.loaded || didInit.current) return;
    didInit.current = true;
    const want = params.get("open");
    const first = want && store.threads[want] ? want : order[0];
    // On a phone the inbox comes first; open a thread only when asked (?open=).
    const phone = typeof window.matchMedia === "function" && window.matchMedia("(max-width: 760px)").matches;
    if (first && (!phone || want)) {
      setSel(first);
      setView({ id: first, mode: "open" });
      setZone("thread");
      setTracker((tr) => openView(tr, first, "open", Date.now(), document.hasFocus()));
    }
    const d = params.get("dialog");
    if (d === "new" || d === "adopt") setDialog(d);
    if (d === "snooze" && first) {
      setSnoozeTargets([first]);
      setDialog("snooze");
    }
  }, [store.loaded]);

  // Threads whose timeline has older events on the host (API v0 paging).
  const [earlier, setEarlier] = useState<Record<string, boolean>>({});
  const loadEarlier = (id: string) => {
    const first = store.events[id]?.[0]?.seq;
    if (!first) return;
    void api.eventsBefore(id, first, PAGE).then((evs) => {
      setEarlier((m) => ({ ...m, [id]: evs.length >= PAGE }));
      setStore((s) => mergeEvents(s, id, evs));
    });
  };

  // Load the open thread's events; after a reconnect fetch what was missed.
  const openId = view?.id ?? null;
  useEffect(() => {
    if (!openId || !store.loaded) return;
    let cancelled = false;
    const after = lastSeq(store, openId);
    const known = !!store.events[openId];
    api.events(openId, known ? after : 0, PAGE).then(
      (evs) => {
        if (cancelled) return;
        if (!known) setEarlier((m) => ({ ...m, [openId]: evs.length >= PAGE }));
        setStore((s) => mergeEvents(s, openId, evs, !known));
      },
      () => !cancelled && !known && setStore((s) => mergeEvents(s, openId, [], true)),
    );
    return () => {
      cancelled = true;
    };
  }, [openId, store.loaded, store.conn === "live"]);

  // Decision card state follows the open decision.
  const decision: Decision | null = viewThread?.decisions?.[0] ?? null;
  useEffect(() => {
    if (decision?.kind === "ask" && !cards[decision.id]) setCards((c) => ({ ...c, [decision.id]: initCard(decision) }));
  }, [decision?.id]);
  const card = decision?.kind === "ask" ? (cards[decision.id] ?? null) : null;

  // ---- mark seen (SPEC 6.4) ----
  const trackerRef = useRef(tracker);
  trackerRef.current = tracker;
  const threadsRef = useRef(store.threads);
  threadsRef.current = store.threads;
  useEffect(() => {
    const iv = setInterval(() => {
      const tr = trackerRef.current;
      const t = tr.threadId ? threadsRef.current[tr.threadId] : undefined;
      if (t && shouldMarkSeen(tr, t, Date.now())) {
        setTracker((x) => markSent(x, t));
        // upTo: only what this view showed counts as seen (API v0, SPEC 6.4).
        void api.command("thread.markSeen", t.id, { upTo: t.activityAt });
      }
    }, 250);
    const onFocus = () => setTracker((x) => setWindowFocus(x, true, Date.now()));
    const onBlur = () => setTracker((x) => setWindowFocus(x, false, Date.now()));
    window.addEventListener("focus", onFocus);
    window.addEventListener("blur", onBlur);
    return () => {
      clearInterval(iv);
      window.removeEventListener("focus", onFocus);
      window.removeEventListener("blur", onBlur);
    };
  }, []);

  // ---- title, toast timer ----
  useEffect(() => {
    const n = sec.needs.length;
    document.title = n ? `(${n}) Fyr` : "Fyr";
  }, [sec.needs.length]);

  useEffect(() => {
    if (!toast) return;
    const ms = toast.undo ? UNDO_MS : 4000;
    const id = setTimeout(() => setToast((t) => (t === toast ? null : t)), ms);
    return () => clearTimeout(id);
  }, [toast]);

  // Keep the selected row visible.
  useEffect(() => {
    if (!sel) return;
    document.querySelector(`[data-id="${CSS.escape(sel)}"]`)?.scrollIntoView({ block: "nearest" });
  }, [sel]);

  // Track whether focus is in a text field (for the key-hint bar).
  useEffect(() => {
    const f = () => setInText(isTextTarget(document.activeElement));
    document.addEventListener("focusin", f);
    document.addEventListener("focusout", () => setTimeout(f, 0));
    return () => document.removeEventListener("focusin", f);
  }, []);

  // ---- actions ----
  const showError = (text: string) => setToast({ text, undo: false, error: true, at: Date.now() });

  const runAction = useCallback(async (action: string, ids: string[], args?: Record<string, unknown>) => {
    if (!ids.length) return;
    const replies = await Promise.all(ids.map((id) => api.command(action, id, args).then((r) => ({ id, r }))));
    const ok = replies.filter((x) => x.r.ok).map((x) => x.id);
    const bad = replies.filter((x) => !x.r.ok);
    const kind = undoKindOf(action);
    if (kind && ok.length) {
      const u = recordUndo(kind, ok, Date.now());
      setUndo(u);
      setToast({ text: u.label, undo: true, at: Date.now() });
    }
    if (bad.length) showError(bad.length === 1 ? (bad[0].r.error ?? "Failed") : `${bad.length} failed: ${bad[0].r.error ?? ""}`);
  }, []);

  const doUndo = useCallback(async () => {
    const cmds = undoCommands(undo, Date.now());
    if (!cmds) return;
    setUndo(null);
    const res = await Promise.all(cmds.map((c) => api.command(c.action, c.threadId, c.args)));
    const bad = res.find((r) => !r.ok);
    if (bad) showError(`Undo failed: ${bad.error ?? ""}`);
    else setToast({ text: "Undone", undo: false, at: Date.now() });
  }, [undo]);

  const openThread = (id: string, mode: "open" | "peek" = "open") => {
    setSel(id);
    setView({ id, mode });
    setTracker((tr) => openView(tr, id, mode, Date.now(), document.hasFocus()));
    if (mode === "open") setZone("thread");
  };

  const closePeek = () => {
    setView(null);
    setTracker((tr) => closeView(tr));
  };

  /** The thread an action applies to: the open thread when the thread pane has focus, else the selected row. */
  const target = (): string | null => (zone === "thread" && view ? view.id : sel);

  /** Move the cursor off a row that is about to leave the list. */
  const stepPast = (ids: string[]) => {
    if (!sel || !ids.includes(sel)) return;
    const i = order.indexOf(sel);
    const next = order.slice(i + 1).find((x) => !ids.includes(x)) ?? order.slice(0, i).reverse().find((x) => !ids.includes(x)) ?? null;
    setSel(next);
  };

  const settle = (id: string) => {
    if (zone === "list") stepPast([id]);
    void runAction("thread.settle", [id]);
  };

  const settleGroup = () => {
    let ids = [...picked];
    if (!ids.length && sel) {
      const k = sectionOf(store.threads[sel]);
      if (k) ids = sec[k].map((t) => t.id);
    }
    ids = ids.filter((id) => isAvailable(store.threads[id], "thread.settle"));
    if (!ids.length) return showError("Nothing here can be settled");
    stepPast(ids);
    setPicked(new Set());
    void runAction("thread.settle", ids, { now: true });
  };

  const archive = (ids: string[]) => {
    stepPast(ids);
    setPicked(new Set());
    void runAction("thread.archive", ids);
  };

  const openSnooze = (ids: string[]) => {
    if (!ids.length) return;
    setSnoozeTargets(ids);
    setDialog("snooze");
  };

  const snooze = (p: SnoozePreset) => {
    const ids = snoozeTargets;
    setDialog(null);
    stepPast(ids);
    setPicked(new Set());
    void runAction("thread.snooze", ids, { until: p.until.toISOString() });
  };

  const markUnseen = (id: string) => {
    if (view?.id === id) setTracker((tr) => markedUnseen(tr));
    void runAction("thread.markUnseen", [id]);
  };

  const submitAsk = (d: Decision) => {
    const cs = cards[d.id];
    if (!cs || !view) return;
    const answers = answersOf(cs, d);
    if (!answers) return;
    void runAction("thread.answer", [view.id], { decisionId: d.id, answers }).then(() => nextNeeding(1, view.id));
  };

  /** Enter in the answer field: the text answers the active question, then submit or move on. */
  const answerEnter = () => {
    if (!decision || decision.kind !== "ask" || !card) return;
    const r = cardKey(card, decision, "Enter", { inOther: true });
    setCards((c) => ({ ...c, [decision.id]: r.state }));
    if (r.submit) submitAsk(decision);
  };
  const answerField =
    decision?.kind === "ask" && card && view?.mode === "open"
      ? {
          index: card.index,
          text: card.other[card.index] ?? "",
          setText: (text: string) => setCards((c) => (c[decision.id] ? { ...c, [decision.id]: setOther(c[decision.id], decision, text) } : c)),
          submit: answerEnter,
        }
      : null;

  const allow = (d: Decision, ok: boolean) => {
    if (!view) return;
    void runAction("thread.answer", [view.id], { decisionId: d.id, allow: ok });
  };

  const nextNeeding = (dir: 1 | -1, from?: string | null) => {
    const list = sec.needs.map((t) => t.id).filter((id) => id !== from);
    if (!list.length) return;
    const cur = from ?? sel;
    const i = cur ? list.indexOf(cur) : -1;
    const j = i < 0 ? (dir === 1 ? 0 : list.length - 1) : (i + dir + list.length) % list.length;
    openThread(list[j], "open");
  };

  const jump = (k: SectionKey) => {
    setSectionOpen((o) => ({ ...o, [k]: true }));
    const first = sec[k][0];
    document.getElementById(`sec-${k}`)?.scrollIntoView({ block: "start" });
    if (first) {
      setSel(first.id);
      setZone("list");
    }
  };

  const send = async (text: string, images: ImageArg[]): Promise<boolean> => {
    if (!view) return false;
    const r = await api.command("thread.send", view.id, images.length ? { text, images } : { text });
    if (!r.ok) showError(r.error ?? "Could not send");
    return r.ok;
  };

  const create = async (a: NewThreadArgs): Promise<string | null> => {
    const args: Record<string, unknown> = { cwd: a.cwd, text: a.text, profile: a.profile, model: a.model };
    if (a.images.length) args.images = a.images;
    if (a.title) args.title = a.title;
    const r = await api.command("thread.create", undefined, args);
    if (!r.ok) return r.error ?? "Could not start the thread";
    setDialog(null);
    if (r.threadId) {
      const id = r.threadId;
      // The upsert may arrive after the reply; open once it is there.
      const tryOpen = (n: number) => {
        if (threadsRef.current[id] || n > 20) openThread(id);
        else setTimeout(() => tryOpen(n + 1), 100);
      };
      tryOpen(0);
    }
    return null;
  };

  const adopt = async (ids: string[]): Promise<string | null> => {
    const r = await api.command("t3.adopt", undefined, { t3Ids: ids });
    if (!r.ok) return r.error ?? "Could not adopt";
    setDialog(null);
    setToast({ text: ids.length === 1 ? "Adopted 1 thread" : `Adopted ${ids.length} threads`, undo: false, at: Date.now() });
    return null;
  };

  const cycleTheme = () => setTheme((t) => (t === "system" ? "light" : t === "light" ? "dark" : "system"));

  // ---- keyboard ----
  const gPending = useRef(0);
  const lastEsc = useRef(0);
  const keyRef = useRef<(e: KeyboardEvent) => void>(() => {});
  keyRef.current = (e: KeyboardEvent) => {
    const k = e.key;
    const mod = e.metaKey || e.ctrlKey;

    if (dialog) {
      if (k === "Escape") {
        e.preventDefault();
        setDialog(null);
        return;
      }
      if (dialog === "snooze" && /^[1-3]$/.test(k)) {
        e.preventDefault();
        snooze(snoozePresets(new Date())[Number(k) - 1]);
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
      if (undoLive(undo, Date.now())) {
        e.preventDefault();
        void doUndo();
      }
      return;
    }
    if (mod || e.altKey) return;

    const el = e.target as HTMLElement | null;
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

    // g1–g5
    if (gPending.current && Date.now() - gPending.current < 1500 && /^[1-5]$/.test(k)) {
      gPending.current = 0;
      e.preventDefault();
      jump(SECTIONS[Number(k) - 1].key);
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
        const i = sel ? order.indexOf(sel) : -1;
        const j = Math.max(0, Math.min(order.length - 1, i + dir));
        if (order[j]) setSel(order[j]);
        setZone("list");
        if (view?.mode === "peek" && order[j]) openThread(order[j], "peek");
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
        if (tgt) void runAction("thread.retry", [tgt]);
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
      case "Escape": {
        e.preventDefault();
        if (view?.mode === "peek") return closePeek();
        if (picked.size) return setPicked(new Set());
        const t = viewThread;
        const fromComposer = Date.now() - lastEsc.current < 800;
        if (t?.running && (zone === "thread" || fromComposer) && isAvailable(t, "thread.interrupt")) {
          void runAction("thread.interrupt", [t.id]);
          return;
        }
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

  const activeSection: SectionKey | null = sel && store.threads[sel] ? sectionOf(store.threads[sel]) : null;
  const hints = keyHints({
    dialog,
    zone,
    hasSelection: !!sel,
    picked: picked.size,
    view: view?.mode ?? null,
    card: decision?.kind ?? null,
    cardMulti: !!(decision?.kind === "ask" && card && decision.questions?.[card.index]?.multiSelect),
    running: !!viewThread?.running,
    failed: (tgtThread(store, zone, view, sel)?.attention.state ?? "") === "failed",
    undo: undoLive(undo, tick) || (!!toast?.undo && undoLive(undo, Date.now())),
    inText,
  });

  return (
    <div class="shell">
      <div class={view ? "app open" : "app"}>
        <Rail sec={sec} active={activeSection} conn={store.conn} host={store.host || (api.mock ? "mock" : "local")} onJump={jump} />
        <Inbox
          sec={sec}
          now={now}
          selected={sel}
          picked={picked}
          open={sectionOpen}
          expanded={expanded}
          query={query}
          searchRef={searchRef}
          onQuery={setQuery}
          onToggle={(k) => setSectionOpen((o) => ({ ...o, [k]: !isOpen(k, o) }))}
          onClick={(id) => openThread(id, "open")}
          onExpand={(k) => setExpanded((x) => ({ ...x, [k]: true }))}
          onNew={() => setDialog("new")}
          onAdopt={() => setDialog("adopt")}
          onFocusZone={() => setZone("list")}
        />
        {viewThread && view ? (
          <ThreadPane
            thread={viewThread}
            mode={view.mode}
            events={store.events[view.id]}
            onEarlier={earlier[view.id] ? () => loadEarlier(view.id) : undefined}
            onBack={closePeek}
            profiles={store.profiles}
            card={card}
            answerField={answerField}
            onCard={(cs) => decision && setCards((c) => ({ ...c, [decision.id]: cs }))}
            onAllow={allow}
            onRun={(a, args) => {
              if (a === "thread.settle") return settle(view.id);
              if (a === "thread.archive") return archive([view.id]);
              void runAction(a, [view.id], args);
            }}
            onSnooze={() => openSnooze([view.id])}
            onSend={send}
            onAtEnd={(b) => setTracker((tr) => setAtEnd(tr, b))}
            onFocusZone={() => {
              setZone("thread");
              if (view.mode === "peek") openThread(view.id, "open");
            }}
          />
        ) : (
          <section class="th empty-th">
            <div class="sys">{store.loaded ? "Pick a thread with j / k and Enter, or start one with c." : store.conn === "offline" ? "Fyr is not reachable. Retrying…" : "Connecting…"}</div>
          </section>
        )}
      </div>
      {help && (
        <footer class="keys" aria-label="Keys">
          {hints.map(([key, label]) => (
            <span class="kh">
              <kbd>{key}</kbd>
              {label}
            </span>
          ))}
          <button class="kh theme" onClick={cycleTheme} title="Theme">
            theme: {theme === "system" ? "auto" : theme}
          </button>
          <span class="kh">
            <kbd>?</kbd>close
          </span>
        </footer>
      )}
      {toast && (
        <div class={`toast${toast.error ? " err" : ""}`} role="status">
          <span class="ell">{toast.text}</span>
          {toast.undo && undoLive(undo, Date.now()) && (
            <button class="btn sm" onClick={() => void doUndo()}>
              Undo <kbd>⌘Z</kbd>
            </button>
          )}
        </div>
      )}
      {dialog === "new" && <NewThreadDialog profiles={store.profiles} projects={store.projects} onClose={() => setDialog(null)} onCreate={create} />}
      {dialog === "adopt" && <AdoptDialog load={() => api.t3Candidates()} now={now} onClose={() => setDialog(null)} onAdopt={adopt} />}
      {dialog === "snooze" && <SnoozeMenu presets={snoozePresets(new Date())} count={snoozeTargets.length} onPick={snooze} onClose={() => setDialog(null)} />}
    </div>
  );
}

function tgtThread(s: Store, zone: Zone, view: { id: string } | null, sel: string | null): ThreadDTO | undefined {
  const id = zone === "thread" && view ? view.id : sel;
  return id ? s.threads[id] : undefined;
}

render(<App />, document.getElementById("app")!);
