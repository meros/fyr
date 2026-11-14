import { deviceId, presenceTarget, shouldBeat, type BeatState } from "./presence.ts";
/** Run when the browser is idle (V0.38: storing never competes with input). */
const idle = (fn: () => void) => ((globalThis as { requestIdleCallback?: (f: () => void, o?: object) => void }).requestIdleCallback ?? ((f: () => void) => setTimeout(f, 0)))(fn, { timeout: 3000 });
import { listPref, listShown, setListPref } from "./prefs.ts";
import { DeployBanner } from "./deploy-ui.tsx";
import { cache, mergeEventLists } from "./cache.ts";
import { setFullEventSource } from "./thread.tsx";
import { rememberOpen, takeOpen } from "./push.ts";
const sessionStore = () => {
  try {
    return typeof sessionStorage !== "undefined" ? sessionStorage : null;
  } catch {
    return null;
  }
};
import { navTransition } from "./native.ts";
import { menuFor, targetAt, type MenuEntry, type MenuItem, type MenuTarget } from "./menu.ts";
import { ContextMenu } from "./menu-ui.tsx";
import { Boundary } from "./boundary.tsx";
import { shouldClearNotification } from "./push.ts";
import { clearThreadNotification } from "./pwa.ts";
// The app shell: state, commands and the keyboard.

import { render } from "preact";
import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Decision, ProjectSource, StateDTO, ThreadDTO } from "./types.ts";
import { createApi } from "./api.ts";
import { setChangesSource } from "./changes-ui.tsx";
import { setPrSource } from "./pr-ui.tsx";
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
  upsertProject,
  setDeploy,
  UNDO_MS,
  type SectionKey,
  type SnoozePreset,
  type Store,
  type UndoEntry,
  applyThreadPatch,
} from "./store.ts";
import { closeView, initialTracker, markSent, markedUnseen, openView, setAtEnd, setWindowFocus, shouldMarkSeen, type SeenTracker } from "./seen.ts";
import { answersOf, cardKey, goTo, initCard, setOther, type CardState } from "./question.ts";
import { keyHints } from "./keys.ts";
import { Inbox, Rail, SECTION_LIMIT, isOpen, type Theme } from "./inbox.tsx";
import { ThreadPane, useToolDetails, type SendArgs } from "./thread.tsx";
import { setToolDetails } from "./prefs.ts";
import { setAttachmentUrl } from "./attach.ts";
import { setPreviewUrl } from "./previews.ts";
import { PreviewViewer } from "./preview-ui.tsx";
import { setUploader } from "./attach-ui.tsx";
import { AdoptDialog, Modal, NewThreadDialog, SnoozeMenu, type NewThreadArgs } from "./dialogs.tsx";
import { disablePush, syncPushStatus, enablePush, isStandalone, promptInstall, pushEnv, registerServiceWorker, watchInstall } from "./pwa.ts";
import { unsupportedReason, type PushStatus } from "./push.ts";
import { PushDialog, PwaControls } from "./pwa-ui.tsx";
import { PhoneApp, type PhoneActions } from "./phone.tsx";
import { DirectoryContext, ProjectEditor, ProjectPicker, type Directory, type ProjectPatch } from "./project-ui.tsx";
import { FILTER_KEY, LAST_PROJECT_KEY, filterByProject } from "./projects.ts";
import { hideQuiet, nextQuietEnd } from "./quiet.ts";
import { FolderPickContext, FolderPicker, type FolderPick, type PickOptions } from "./folder-picker.tsx";
import { backTarget, initialEntries, leavesThread, navFromHistory, urlFor, type Nav } from "./nav.ts";
import { PHONE_UNDO_MS } from "./mobile.ts";
import { VIEWPORT_VV_EVENTS, VIEWPORT_WINDOW_EVENTS, rereadLater, viewportVars } from "./viewport.ts";

const PHONE_QUERY = "(max-width: 760px)";

/** True at phone width (design/mobile, V0.14 breakpoint). */
function usePhone(): boolean {
  const mq = typeof window.matchMedia === "function" ? window.matchMedia(PHONE_QUERY) : null;
  const [phone, setPhone] = useState(!!mq?.matches);
  useEffect(() => {
    if (!mq) return;
    const f = () => setPhone(mq.matches);
    mq.addEventListener("change", f);
    return () => mq.removeEventListener("change", f);
  }, []);
  return phone;
}

/**
 * The phone layout height follows the visual viewport (SPEC V0.17): --app-h, --app-top and kb-open on
 * <html>, re-read on every event that can change them, and again on the next frames after a resume.
 */
function useViewportVars(on: boolean) {
  useEffect(() => {
    if (!on) return;
    const root = document.documentElement;
    const vv = window.visualViewport;
    const read = () => {
      const v = viewportVars({ innerHeight: window.innerHeight, vvHeight: vv?.height, vvOffsetTop: vv?.offsetTop });
      root.style.setProperty("--app-h", v.height);
      root.style.setProperty("--app-top", v.top);
      root.classList.toggle("kb-open", v.keyboard);
      // With the keyboard up, keep the field being typed in on screen.
      const a = document.activeElement as HTMLElement | null;
      if (v.keyboard && a && (a.tagName === "INPUT" || a.tagName === "TEXTAREA")) requestAnimationFrame(() => a.scrollIntoView({ block: "nearest" }));
    };
    const timers: ReturnType<typeof setTimeout>[] = [];
    const onEvent = (e: Event) => {
      read();
      if (rereadLater(e.type, document.visibilityState)) {
        requestAnimationFrame(() => requestAnimationFrame(read));
        timers.push(setTimeout(read, 350));
      }
    };
    read();
    for (const t of VIEWPORT_WINDOW_EVENTS) window.addEventListener(t, onEvent);
    for (const t of VIEWPORT_VV_EVENTS) vv?.addEventListener(t, onEvent);
    document.addEventListener("visibilitychange", onEvent);
    return () => {
      for (const t of VIEWPORT_WINDOW_EVENTS) window.removeEventListener(t, onEvent);
      for (const t of VIEWPORT_VV_EVENTS) vv?.removeEventListener(t, onEvent);
      document.removeEventListener("visibilitychange", onEvent);
      timers.forEach(clearTimeout);
      root.style.removeProperty("--app-h");
      root.style.removeProperty("--app-top");
      root.classList.remove("kb-open");
    };
  }, [on]);
}

/** Events per page of a thread timeline. */
const PAGE = 400;

const api = createApi();
setUploader(api.uploadAttachment);
setAttachmentUrl(api.attachmentUrl);
setPreviewUrl(api.previewUrl);
setChangesSource(api);
setPrSource(api);
setFullEventSource(api);
const params = new URLSearchParams(location.search);

type Dialog = "new" | "adopt" | "snooze" | "push" | "project" | "picker" | null;
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
  const toolDetailsOn = useToolDetails();
  // Phone (design/mobile): screens and sheets are history entries.
  const phone = usePhone();
  const phoneRef = useRef(phone);
  phoneRef.current = phone;
  useViewportVars(phone);
  const [nav, setNav] = useState<Nav>(() => initialEntries(location.search).at(-1)!);
  const [selecting, setSelecting] = useState(false);
  // PWA (SPEC V0.15): push bell, install prompt, and a thread to open from a notification tap.
  const [pushSt, setPushSt] = useState<PushStatus>("unsupported");
  const [canInstall, setCanInstall] = useState(false);
  const [pendingOpen, setPendingOpen] = useState<string | null>(null);
  const env = useMemo(() => pushEnv(), []);
  const pushReason = unsupportedReason(env);

  const now = tick + store.clockOffset;
  // The project filter (V0.23), remembered per device.
  const [projectFilter, setProjectFilterState] = useState<string | null>(() => {
    try {
      return localStorage.getItem(FILTER_KEY);
    } catch {
      return null;
    }
  });
  const setProjectFilter = (id: string | null) => {
    setProjectFilterState(id);
    try {
      if (id) localStorage.setItem(FILTER_KEY, id);
      else localStorage.removeItem(FILTER_KEY);
    } catch {
      /* storage blocked */
    }
  };
  const [projectId, setProjectId] = useState<string | null>(null);
  // The project picker (V0.30): the threads it files, and whether it opens on New project.
  const [pickIds, setPickIds] = useState<string[]>([]);
  const [pickNew, setPickNew] = useState(false);
  const openPicker = (ids: string[], startNew = false) => {
    setPickIds(ids);
    setPickNew(startNew);
    setDialog("picker");
  };
  // V0.46: quiet hours. One timer to the nearest quietUntil re-renders when a window ends (the host also sends the
  // threads then); no polling.
  const [quietClock, setQuietClock] = useState(0);
  useEffect(() => {
    const next = nextQuietEnd(store.threads, store.projects, Date.now() + store.clockOffset);
    if (next === null) return;
    const t = setTimeout(() => setQuietClock((n) => n + 1), Math.min(next - (Date.now() + store.clockOffset) + 250, 2 ** 31 - 1));
    return () => clearTimeout(t);
  }, [store.threads, store.projects, store.clockOffset, quietClock]);
  const filterOn = !!projectFilter && store.projects.some((p) => p.id === projectFilter);
  // What search may find: the project filter only, quiet threads included (V0.46).
  const searchable = useMemo(() => filterByProject(store.threads, filterOn ? projectFilter : null, store.projects), [store.threads, store.projects, projectFilter, filterOn]);
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    // V0.46: a quiet project's threads leave the inbox and its counts, unless the filter picks it or a search is typed.
    const base = hideQuiet(searchable, filterOn || !!q, Date.now() + store.clockOffset);
    if (!q) return base;
    const out: Record<string, ThreadDTO> = {};
    for (const t of Object.values(base)) {
      if (`${t.title} ${t.project} ${t.pr ? "#" + t.pr.number : ""}`.toLowerCase().includes(q)) out[t.id] = t;
    }
    return out;
  }, [searchable, filterOn, query, quietClock, store.clockOffset]);
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
    registerServiceWorker((id) => {
      rememberOpen(sessionStore(), id);
      setPendingOpen(id);
    });
    // A tap that arrived just before a reload for a new build (V0.15).
    const carried = takeOpen(sessionStore());
    if (carried) {
      rememberOpen(sessionStore(), carried);
      setPendingOpen(carried);
    }
    watchInstall((can) => setCanInstall(can));
    // Reconcile with the host at start and whenever the app comes back to the foreground.
    void syncPushStatus(api).then(setPushSt);
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "visible") void syncPushStatus(api).then(setPushSt);
    });
    // V0.38: stream messages are applied together, once per frame (a short timer while the page is
    // hidden), so a burst of updates is one render, not one per message.
    let queue: ((s: Store) => Store)[] = [];
    let scheduled = false;
    const flush = () => {
      scheduled = false;
      const fns = queue;
      queue = [];
      if (fns.length) setStore((s) => fns.reduce((acc, f) => f(acc), s));
    };
    const batch = (fn: (s: Store) => Store) => {
      queue.push(fn);
      if (scheduled) return;
      scheduled = true;
      if (document.hidden || typeof requestAnimationFrame !== "function") setTimeout(flush, 250);
      else requestAnimationFrame(flush);
    };
    api.start({
      state: (dto) => {
        flush();
        setStore((s) => applySnapshot(s, dto, Date.now()));
      },
      thread: (t) => {
        if (shouldClearNotification(t.attention.state)) clearThreadNotification(t.id);
        batch((s) => upsertThread(s, t));
      },
      // V0.38: a patch against our copy; when it does not fit, ask for the whole thread.
      threadPatch: (p) => {
        const st = (p.patch as { attention?: { state?: string } }).attention?.state;
        if (st && shouldClearNotification(st)) clearThreadNotification(p.id);
        batch((s) => {
          const r = applyThreadPatch(s, p);
          if (r.needFull) queueMicrotask(() => void api.thread(p.id).then((t) => t && setStore((x) => upsertThread(x, t))));
          return r.store;
        });
      },
      event: (e) => batch((s) => liveEvent(s, e)),
      project: (p) => batch((s) => upsertProject(s, p)),
      deploy: (p) => batch((s) => setDeploy(s, p)),
      conn: (c) => batch((s) => setConn(s, c)),
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

  // Presence (V0.20): tell the host which thread is shown while visible.
  const beatRef = useRef<BeatState>({ last: "", at: 0 });
  useEffect(() => {
    const device = deviceId();
    const tick = () => {
      const target = presenceTarget(openId, document.visibilityState);
      const now = Date.now();
      if (shouldBeat(beatRef.current, target, now)) {
        beatRef.current = { last: target, at: now };
        void api.presence(target, device);
      }
    };
    tick();
    const iv = setInterval(tick, 5_000);
    document.addEventListener("visibilitychange", tick);
    window.addEventListener("pagehide", tick);
    return () => {
      clearInterval(iv);
      document.removeEventListener("visibilitychange", tick);
      window.removeEventListener("pagehide", tick);
    };
  }, [openId]);
  useEffect(() => {
    if (!openId || !store.loaded) return;
    let cancelled = false;
    const after = lastSeq(store, openId);
    const known = !!store.events[openId];
    const id = openId;
    void (async () => {
      // V0.38: the device's copy draws at once; the host sends only what is newer.
      const stored = !known && !api.mock ? await cache.events(id) : null;
      if (cancelled) return;
      if (stored?.length) setStore((s) => (s.events[id] ? s : mergeEvents(s, id, stored, true)));
      const from = known ? after : stored?.length ? stored[stored.length - 1].seq : 0;
      try {
        const evs = await api.events(id, from, PAGE);
        if (cancelled) return;
        if (!known && !stored?.length) setEarlier((m) => ({ ...m, [id]: evs.length >= PAGE }));
        if (!known && stored?.length) {
          const merged = mergeEventLists(stored, evs, PAGE, evs.length >= PAGE);
          if (evs.length >= PAGE) setEarlier((m) => ({ ...m, [id]: true }));
          setStore((s) => mergeEvents(s, id, merged, true));
        } else setStore((s) => mergeEvents(s, id, evs, !known));
      } catch {
        if (!cancelled && !known && !stored?.length) setStore((s) => mergeEvents(s, id, [], true));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [openId, store.loaded, store.conn === "live"]);

  // V0.38: keep the open thread's events and the state on the device, when the browser is idle.
  const openEvents = openId ? store.events[openId] : undefined;
  useEffect(() => {
    if (!openId || !openEvents || api.mock) return;
    const id = openId;
    const t = setTimeout(() => idle(() => void cache.saveEvents(id, openEvents.slice(-PAGE))), 2000);
    return () => clearTimeout(t);
  }, [openId, openEvents]);
  useEffect(() => {
    if (!store.loaded || api.mock) return;
    const t = setTimeout(
      () =>
        idle(() =>
          void cache.saveState({ now: new Date().toISOString(), user: store.user, host: store.host, threads: Object.values(store.threads), actions: store.actions, profiles: store.profiles, projects: store.projects } as StateDTO),
        ),
      2000,
    );
    return () => clearTimeout(t);
  }, [store.threads, store.projects, store.loaded]);

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
    const ms = toast.undo ? (phoneRef.current ? PHONE_UNDO_MS : UNDO_MS) : 4000;
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
    if (!ids.length) return 0;
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
    return ok.length;
  }, []);

  const doUndo = useCallback(async () => {
    const cmds = undoCommands(undo, Date.now());
    if (!cmds) return;
    setUndo(null);
    const res = await Promise.all(cmds.map((c) => api.command(c.action, c.threadId, c.args)));
    const bad = res.find((r) => !r.ok);
    if (bad) showError(`Undo failed: ${bad.error ?? ""}`);
    else setToast({ text: "Undone", undo: false, at: Date.now() });
    // U8: Undo of a settle or archive that left the thread reopens it.
    const back = reopenRef.current;
    reopenRef.current = null;
    if (!bad && back && phoneRef.current && undo?.threadIds.includes(back)) navGoRef.current({ screen: { name: "thread", id: back }, sheet: null });
  }, [undo]);
  const reopenRef = useRef<string | null>(null);
  const navGoRef = useRef<(n: Nav) => void>(() => {});

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

  // ---- right-click menu (V0.31) ----
  const [ctx, setCtx] = useState<{ x: number; y: number; items: MenuEntry[]; target: MenuTarget } | null>(null);
  useEffect(() => {
    const on = (e: MouseEvent) => {
      const selection = String(window.getSelection?.() ?? "").trim();
      const target = targetAt(e.target as Element | null, selection, threadsRef.current);
      const items = menuFor(target);
      if (items === null) return; // text fields keep the browser menu (spelling, Paste)
      e.preventDefault();
      if (phone || !items.length) return;
      setCtx({ x: e.clientX, y: e.clientY, items, target });
    };
    document.addEventListener("contextmenu", on);
    return () => document.removeEventListener("contextmenu", on);
  }, [phone]);
  const copyText = (text: string) => {
    void navigator.clipboard?.writeText(text).then(
      () => setToast({ text: "Copied", undo: false, at: Date.now() }),
      () => showError("Could not copy"),
    );
  };
  const MENU_ACTION: Record<string, string> = { unsettle: "thread.unsettle", unsnooze: "thread.unsnooze", markSeen: "thread.markSeen", unarchive: "thread.unarchive", interrupt: "thread.interrupt", dismissPR: "thread.dismissPR" };
  const pickMenu = (it: MenuItem, target: MenuTarget) => {
    setCtx(null);
    if (target.kind === "thread") {
      const id = target.t.id;
      if (it.id === "open") openThread(id, "open");
      else if (it.id === "settle") settle(id);
      else if (it.id === "archive") archive([id]);
      else if (it.id === "snooze") openSnooze([id]);
      else if (it.id === "markUnseen") markUnseen(id);
      else if (it.id === "project") openPicker([id]);
      else if (it.id === "copyLink") copyText(`${location.origin}/?open=${id}`);
      else if (it.id === "liveView") void runAction("thread.setView", [id], { on: !target.t.liveView });
      else if (MENU_ACTION[it.id]) void runAction(MENU_ACTION[it.id], [id]);
      return;
    }
    if (it.id === "copy" && (target.kind === "text" || target.kind === "link")) copyText(target.kind === "text" ? target.selection : (target.selection ?? ""));
    else if (it.id === "openLink" && target.kind === "link") window.open(target.href, "_blank", "noopener");
    else if (it.id === "copyLink" && target.kind === "link") copyText(target.href);
    else if (it.id === "openImage" && target.kind === "image") window.open(target.src, "_blank", "noopener");
  };
  const ctxMenu = ctx && <ContextMenu x={ctx.x} y={ctx.y} items={ctx.items} onPick={(it) => pickMenu(it, ctx.target)} onClose={() => setCtx(null)} />;

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

  const jump = (k: SectionKey) => {
    if (!sec[k].length) return; // an empty section is not a place to go
    if (!listOn) {
      setListPrefState(true); // jumping to a section shows the list (V0.40)
      setListPref(true);
    }
    setSectionOpen((o) => ({ ...o, [k]: true }));
    const first = sec[k][0];
    document.getElementById(`sec-${k}`)?.scrollIntoView({ block: "start" });
    if (first) {
      setSel(first.id);
      setZone("list");
    }
  };

  const send = async (args: SendArgs): Promise<boolean> => {
    if (!view) return false;
    const r = await api.command("thread.send", view.id, { ...args });
    if (!r.ok) showError(r.error ?? "Could not send");
    return r.ok;
  };

  // ---- projects (V0.23) ----
  const projectCommand = async (action: string, args: Record<string, unknown>, threadId?: string): Promise<string | null> => {
    const r = await api.command(action, threadId, args);
    return r.ok ? null : (r.error ?? "That did not work");
  };
  const projectActions = {
    update: (id: string, patch: ProjectPatch) => projectCommand("project.update", { id, ...patch }),
    /** V0.42: a folder from the picker; the host fills in its own name. */
    addFolder: (id: string, path: string) => projectCommand("project.addSource", { id, source: { kind: "folder", path } }),
    removeSource: (id: string, index: number) => projectCommand("project.removeSource", { id, index }),
    setMain: (id: string, index: number) => projectCommand("project.setMain", { id, index }),
    /** New project (V0.30): with threadIds, the host files one thread in the same write; more are filed after. */
    create: async (name: string, sources: ProjectSource[], threadIds: string[] = []): Promise<{ error: string | null; id?: string }> => {
      const args: Record<string, unknown> = { name };
      if (sources.length) args.sources = sources; // a folder without a host: the host fills in its own name
      if (threadIds.length === 1) args.threadId = threadIds[0];
      const r = await api.command("project.create", undefined, args);
      if (!r.ok) return { error: r.error ?? "That did not work" };
      const id = r.result?.id;
      if (threadIds.length > 1 && id) await Promise.all(threadIds.map((t) => projectCommand("thread.setProject", { projectId: id }, t)));
      setToast({ text: threadIds.length ? `Made project ${name} and filed ${threadIds.length === 1 ? "this thread" : `${threadIds.length} threads`}` : `Made project ${name}`, undo: false, at: Date.now() });
      return { error: null, id };
    },
    /** File threads into a project; null: No project. */
    move: async (ids: string[], projectId: string | null): Promise<string | null> => {
      const errs = (await Promise.all(ids.map((id) => projectCommand("thread.setProject", { projectId: projectId ?? "" }, id)))).filter(Boolean);
      const where = projectId ? (store.projects.find((p) => p.id === projectId)?.name ?? "the project") : null;
      if (!errs.length) setToast({ text: ids.length === 1 ? (where ? `Filed in ${where}` : "No project now") : where ? `Filed ${ids.length} threads in ${where}` : `${ids.length} threads have no project now`, undo: false, at: Date.now() });
      return errs[0] ?? null;
    },
  };
  // V0.42: the host folder picker, a dialog on the desktop and a sheet on the phone, over whatever asked for it.
  const [folderPick, setFolderPick] = useState<(PickOptions & { at: number; resolve(p: string | null): void }) | null>(null);
  const folderPickRef = useRef(folderPick);
  folderPickRef.current = folderPick;
  const closeFolderPick = (path: string | null) => {
    folderPickRef.current?.resolve(path);
    setFolderPick(null);
  };
  const folderPicker: FolderPick = {
    pick: (o = {}) =>
      new Promise((resolve) => {
        folderPickRef.current?.resolve(null);
        setFolderPick({ ...o, at: Date.now(), resolve });
      }),
  };
  const folderPickView = folderPick
    ? phone ? (
        <div class="m-scrim fscrim" onClick={(e) => e.target === e.currentTarget && Date.now() - folderPick.at > 500 && closeFolderPick(null)}>
          <div class="m-sheet fsheet" role="dialog" aria-modal="true" aria-label={folderPick.title ?? "Pick a folder"}>
            <div class="m-grab" />
            <div class="fsh">
              <span class="m-shh">{folderPick.title ?? "Pick a folder"}</span>
              <button class="m-fchg" onClick={() => closeFolderPick(null)}>
                Cancel
              </button>
            </div>
            <FolderPicker fs={api} first={folderPick.first} onPick={closeFolderPick} autoFocus={false} />
          </div>
        </div>
      ) : (
        <div class="fmodal">
          <Modal title={folderPick.title ?? "Pick a folder"} onClose={() => closeFolderPick(null)} wide>
            <div class="db">
              <FolderPicker fs={api} first={folderPick.first} onPick={closeFolderPick} />
            </div>
          </Modal>
        </div>
      )
    : null;

  const openProjectScreen = (id: string) => {
    if (phoneRef.current) navGo({ screen: { name: "project", id }, sheet: null });
    else {
      setProjectId(id);
      setDialog("project");
    }
  };
  const directory: Directory = {
    threads: store.threads,
    projects: store.projects,
    openThread: (id) => (phoneRef.current ? navGo({ screen: { name: "thread", id }, sheet: null }) : openThread(id, "open")),
    openProject: openProjectScreen,
    pickProject: (id, startNew) => (phoneRef.current ? navGo({ ...navRef.current, sheet: { kind: "project", ids: [id], startNew } }) : openPicker([id], startNew)),
    host: store.host,
  };

  const create = async (a: NewThreadArgs): Promise<string | null> => {
    // V0.42: no cwd starts in the project's main folder, or in a fresh scratch folder; the host files the project.
    const args: Record<string, unknown> = { text: a.text, profile: a.profile, model: a.model };
    if (a.cwd) args.cwd = a.cwd;
    if (a.projectId) args.projectId = a.projectId;
    if (a.attachments?.length) args.attachments = a.attachments;
    if (a.title) args.title = a.title;
    if (a.view) args.view = true;
    const r = await api.command("thread.create", undefined, args);
    if (!r.ok) return r.error ?? "Could not start the thread";
    setDialog(null);
    try {
      localStorage.setItem(LAST_PROJECT_KEY, a.projectId ?? "");
    } catch {}
    if (r.threadId) {
      const id = r.threadId;
      // The upsert may arrive after the reply; open once it is there.
      const tryOpen = (n: number) => {
        if (threadsRef.current[id] || n > 20) {
          if (phoneRef.current) navReplace({ screen: { name: "thread", id }, sheet: null });
          else openThread(id);
        }
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
    // A focused button (the project chip, the suggestion chip) takes Enter and Space itself.
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

  useEffect(() => {
    if (!pendingOpen || !store.threads[pendingOpen]) return;
    if (phone) navGo({ screen: { name: "thread", id: pendingOpen }, sheet: null });
    else openThread(pendingOpen, "open");
    takeOpen(sessionStore()); // on screen now: done
    setPendingOpen(null);
  }, [pendingOpen, store.threads]);

  const pwaControls = (
    <PwaControls
      push={pushSt}
      pushReason={pushReason}
      showUnsupported={env.ios && !env.standalone}
      canInstall={canInstall && !isStandalone()}
      onBell={() => setDialog("push")}
      onInstall={() => void promptInstall().then((ok) => ok && setCanInstall(false))}
    />
  );

  // ---- phone navigation ----
  const navGo = (n: Nav) => {
    history.pushState(n, "", urlFor(n, location.search));
    // A sheet opens over the screen; only a new screen slides (V0.32).
    if (n.screen !== nav.screen && JSON.stringify(n.screen) !== JSON.stringify(nav.screen)) navTransition("fwd", () => setNav(n));
    else setNav(n);
  };
  navGoRef.current = navGo;
  const navRef = useRef(nav);
  navRef.current = nav;
  /** U8: after settling or archiving the open thread on a phone, go back to the inbox (pop, not push). */
  const leaveOpenThread = (action: "settle" | "archive", ids: string[]) => {
    const s = navRef.current.screen;
    if (s.name !== "thread" || ids.length !== 1 || ids[0] !== s.id) return;
    if (!leavesThread(action, threadsRef.current[s.id])) return;
    reopenRef.current = s.id;
    navBack();
  };
  const navReplace = (n: Nav) => {
    history.replaceState(n, "", urlFor(n, location.search));
    setNav(n);
  };
  const navBack = () => {
    if (history.state && typeof history.state === "object" && "screen" in history.state && history.length > 1) history.back();
    else {
      const t = backTarget(nav);
      if (t) navReplace(t);
    }
  };
  // Build the history once: a deep link (a push) gets the inbox under it, so Back never leaves the app.
  const navInit = useRef(false);
  useEffect(() => {
    if (!phone || navInit.current) return;
    navInit.current = true;
    const entries = initialEntries(location.search);
    history.replaceState(entries[0], "", urlFor(entries[0], location.search));
    for (const e of entries.slice(1)) history.pushState(e, "", urlFor(e, location.search));
    setNav(entries[entries.length - 1]);
    const onPop = (e: PopStateEvent) => {
      const n = navFromHistory(e.state, location.search);
      if (JSON.stringify(n.screen) !== JSON.stringify(navRef.current.screen)) navTransition("back", () => setNav(n));
      else setNav(n);
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, [phone]);
  // The open thread follows the screen.
  useEffect(() => {
    if (!phone) return;
    const s = nav.screen;
    if (s.name === "thread") {
      if (view?.id !== s.id || view.mode !== "open") openThread(s.id, "open");
    } else if (view) closePeek();
  }, [phone, nav.screen.name === "thread" ? nav.screen.id : nav.screen.name]);

  const phoneActions: PhoneActions = {
    settle: (ids, bulk) => void runAction("thread.settle", ids, bulk ? { now: true } : undefined).then((n) => n && leaveOpenThread("settle", ids)),
    snooze: (ids, until) => void runAction("thread.snooze", ids, { until: until.toISOString() }),
    archive: (ids) => void runAction("thread.archive", ids).then((n) => n && leaveOpenThread("archive", ids)),
    markUnseen: (id) => markUnseen(id),
    retry: (id) => void runAction("thread.retry", [id]),
    interrupt: (id) => void runAction("thread.interrupt", [id]),
    dismissPR: (id) => void runAction("thread.dismissPR", [id]),
    setView: (id, on) => void runAction("thread.setView", [id], { on }),
    setModel: (id, model) => void runAction("thread.setModel", [id], { model }),
    send: async (id, args) => {
      const r = await api.command("thread.send", id, { ...args });
      if (!r.ok) showError(r.error ?? "Could not send");
      return r.ok;
    },
    cancelMessage: async (id, messageId) => (await runAction("thread.cancelMessage", [id], { messageId })) > 0,
    submitAsk: async (d) => {
      const cs = cards[d.id];
      const answers = cs ? answersOf(cs, d) : null;
      if (!answers || !view) return false;
      return (await runAction("thread.answer", [view.id], { decisionId: d.id, answers })) > 0;
    },
    allow: async (d, ok) => (view ? (await runAction("thread.answer", [view.id], { decisionId: d.id, allow: ok })) > 0 : false),
    create,
    undo: () => void doUndo(),
  };

  const pushDialog = (onClose: () => void) => (
    <PushDialog
      status={pushSt}
      reason={pushReason}
      onClose={onClose}
      onOn={async () => {
        const r = await enablePush(api);
        setPushSt(r.status);
        return r.error ?? null;
      }}
      onOff={async () => {
        const r = await disablePush(api);
        setPushSt(r.status);
        return r.error ?? null;
      }}
      onTest={async () => {
        const r = await api.pushTest();
        return r.ok ? null : (r.error ?? "The test was not sent");
      }}
    />
  );

  if (phone) {
    return (
      <DirectoryContext.Provider value={directory}>
        <FolderPickContext.Provider value={folderPicker}>
        <Boundary name="Fyr">
        <DeployBanner p={store.deploy} threads={store.threads} conn={store.conn} phone />
        <PreviewViewer />
        <PhoneApp
          nav={nav}
          go={navGo}
          replace={navReplace}
          back={navBack}
          threads={store.threads}
          sec={sec}
          searchable={searchable}
          now={now}
          host={store.host || (api.mock ? "mock" : "local")}
          conn={store.conn}
          thread={nav.screen.name === "thread" ? store.threads[nav.screen.id] : undefined}
          events={nav.screen.name === "thread" ? store.events[nav.screen.id] : undefined}
          onEarlier={view && earlier[view.id] ? () => loadEarlier(view.id) : undefined}
          onAtEnd={(b) => setTracker((tr) => setAtEnd(tr, b))}
          profiles={store.profiles}
          projects={store.projects}
          card={card}
          onCard={(fn) => decision && setCards((c) => (c[decision.id] ? { ...c, [decision.id]: fn(c[decision.id]) } : c))}
          sectionOpen={sectionOpen}
          setSectionOpen={setSectionOpen}
          expanded={expanded}
          onExpand={(k) => setExpanded((x) => ({ ...x, [k]: true }))}
          picked={picked}
          setPicked={setPicked}
          selecting={selecting}
          setSelecting={setSelecting}
          push={pushSt}
          pushReason={pushReason}
          ios={env.ios}
          standalone={env.standalone}
          canInstall={canInstall && !isStandalone()}
          onInstall={() => void promptInstall().then((ok) => ok && setCanInstall(false))}
          theme={theme}
          setTheme={setTheme}
          toast={toast}
          undoKind={undo?.kind ?? null}
          undoIds={undo?.threadIds ?? []}
          undoLive={undoLive(undo, Date.now())}
          actions={phoneActions}
          projectFilter={projectFilter}
          onProjectFilter={setProjectFilter}
          projectActions={projectActions}
          overlay={folderPickView}
        />
        {nav.sheet?.kind === "push" && pushDialog(navBack)}
        {nav.sheet?.kind === "adopt" && (
          <AdoptDialog
            load={() => api.t3Candidates()}
            now={now}
            onClose={navBack}
            onAdopt={async (ids) => {
              const e = await adopt(ids);
              if (!e) navBack();
              return e;
            }}
          />
        )}
        </Boundary>
        </FolderPickContext.Provider>
      </DirectoryContext.Provider>
    );
  }

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
      <DeployBanner p={store.deploy} threads={store.threads} conn={store.conn} />
      {ctxMenu}
      <PreviewViewer />
      <DirectoryContext.Provider value={directory}>
      <FolderPickContext.Provider value={folderPicker}>
      <div class={`${view ? "app open" : "app"}${listOn ? "" : " nolist"}`}>
        <Rail sec={sec} active={activeSection} conn={store.conn} host={store.host || (api.mock ? "mock" : "local")} onJump={jump} extra={pwaControls} listOn={listOn} onToggleList={toggleList} />
        <Boundary name="The inbox">
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
          projects={store.projects}
          projectFilter={projectFilter}
          onProjectFilter={setProjectFilter}
          extra={pwaControls}
          onFocusZone={() => setZone("list")}
        />
        </Boundary>
        {viewThread && view ? (
          <Boundary name="This thread" resetKey={view.id}>
          <ThreadPane
            thread={viewThread}
            mode={view.mode}
            events={store.events[view.id]}
            now={now}
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
            onMoveProject={() => openPicker([view.id])}
            onSend={send}
            onAtEnd={(b) => setTracker((tr) => setAtEnd(tr, b))}
            onFocusZone={() => {
              setZone("thread");
              if (view.mode === "peek") openThread(view.id, "open");
            }}
          />
          </Boundary>
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
          <button class="kh details" onClick={() => setToolDetails(!toolDetailsOn)} aria-pressed={toolDetailsOn} title="Show tool details: every tool group open, every machine line in full">
            tool details: {toolDetailsOn ? "on" : "off"}
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
      {dialog === "new" && <NewThreadDialog profiles={store.profiles} projects={store.projects} host={store.host} projectFilter={projectFilter} onClose={() => setDialog(null)} onCreate={create} onNewProject={(name, sources) => projectActions.create(name, sources)} />}
      {dialog === "project" && projectId && store.projects.find((p) => p.id === projectId) && (
        <Modal title="Project" onClose={() => setDialog(null)} wide>
          <div class="db">
            <ProjectEditor
              project={store.projects.find((p) => p.id === projectId)!}
              profiles={store.profiles}
              host={store.host}
              now={now}
              onSave={(patch) => projectActions.update(projectId, patch)}
              onAddFolder={(v) => projectActions.addFolder(projectId, v)}
              onRemoveSource={(i) => projectActions.removeSource(projectId, i)}
              onSetMain={(i) => projectActions.setMain(projectId, i)}
            />
          </div>
        </Modal>
      )}
      {dialog === "picker" && (
        <Modal title={pickIds.length > 1 ? `Project for ${pickIds.length} threads` : "Project"} onClose={() => setDialog(null)} wide>
          <div class="db">
            <ProjectPicker
              projects={store.projects}
              threads={pickIds.map((id) => store.threads[id]).filter(Boolean)}
              host={store.host}
              startNew={pickNew}
              onPick={async (p) => {
                const e = await projectActions.move(pickIds, p?.id ?? null);
                if (e) showError(e);
                else setDialog(null);
              }}
              onCreate={async (name, sources) => {
                const r = await projectActions.create(name, sources, pickIds);
                if (!r.error) setDialog(null);
                return r.error;
              }}
              onEdit={(p) => p.id && openProjectScreen(p.id)}
            />
          </div>
        </Modal>
      )}
      {dialog === "adopt" && <AdoptDialog load={() => api.t3Candidates()} now={now} onClose={() => setDialog(null)} onAdopt={adopt} />}
      {dialog === "push" && pushDialog(() => setDialog(null))}
      {dialog === "snooze" && <SnoozeMenu presets={snoozePresets(new Date())} count={snoozeTargets.length} onPick={snooze} onClose={() => setDialog(null)} />}
      {folderPickView}
      </FolderPickContext.Provider>
      </DirectoryContext.Provider>
    </div>
  );
}

function tgtThread(s: Store, zone: Zone, view: { id: string } | null, sel: string | null): ThreadDTO | undefined {
  const id = zone === "thread" && view ? view.id : sel;
  return id ? s.threads[id] : undefined;
}

render(<App />, document.getElementById("app")!);
