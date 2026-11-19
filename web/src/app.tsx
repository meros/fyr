// The app shell: state, commands and the keyboard.

import { render } from "preact";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Decision, Thread } from "./types.ts";
import { createApi } from "./api.ts";
import { currentDecision } from "./decode.ts";
import { ApiContext, useApi } from "./api-context.ts";
import { useThreadCommands } from "./commands.ts";
import {
  bulkSettle,
  bulkSettleText,
  SECTIONS,
  applySnapshot,
  flatOrder,
  initialStore,
  isAvailable,
  liveEvent,
  matchesQuery,
  mergeEvents,
  resumeSeq,
  resumed,
  sectionOf,
  isPinned,
  sections,
  setConn,
  stateForCache,
  upsertThread,
  upsertProject,
  setHosts,
  setProjectOrder,
  setDeploy,
  setStall,
  UNDO_MS,
  type Arrival,
  type SectionKey,
  type Sections,
  type Store,
  applyThreadPatch,
  moveHostInStore,
  resolveRef,
} from "./store.ts";
import { migrateStorage, moveHostKeys, moveRef, rotations } from "./storage-migrate.ts";
import { placeOf, steadyThreads, stillRows, trackArrivals, type FinishHold, type RowPlace } from "./still-list.ts";
import { useListAnchor } from "./still-list-ui.ts";
import { SECTION_OPEN_KEY, afterSettle, cutSection, guestOf, hiddenWhy, markedRow, parseSectionOpen, rowAfter, stepRow } from "./selection.ts";
import { closeView, initialTracker, markSent, markedUnseen, openView, setAtEnd, setWindowFocus, shouldMarkSeen, type SeenTracker } from "./seen.ts";
import { answerThenNext, answersOf, cardKey, goTo, initCard, setOther, type CardState } from "./question.ts";
import { keyHints } from "./keys.ts";
import { escapeTarget, parsePage, type PageRef } from "./settings-nav.ts";
import { Settings, type SettingsDevice, type SettingsProps } from "./settings-ui.tsx";
import { ChangesBoard } from "./board-ui.tsx";
import { needsCount } from "./board.ts";
import { Inbox, Rail, SECTION_LIMIT, isOpen, type FilterView, type Theme } from "./inbox.tsx";
import { useSlotQueue } from "./slot-queue-ui.tsx";
import { UndContext, type UndNav } from "./undeclared-ctx.tsx";
import { ThreadPane, useToolDetails, type SendArgs } from "./thread.tsx";
import { HandoverDialog } from "./handover-ui.tsx";
import { listPref, listShown, setListPref, setToolDetails } from "./prefs.ts";
import { ListResizer } from "./list-resize-ui.tsx";
import { PreviewViewer } from "./preview-ui.tsx";
import { AdoptDialog, Modal, NewThreadDialog, SnoozeMenu } from "./dialogs.tsx";
import { snoozeChoiceForKey, snoozeChoices, snoozeCommand, snoozeKeyCount, type SnoozeChoice } from "./snooze.ts";
import type { NewThreadArgs } from "./new-thread.ts";
import { clearThreadNotification, disablePush, syncPushHidden, syncPushPrefs, syncPushStatus, testPush, enablePush, isStandalone, promptInstall, pushEnv, registerServiceWorker, watchInstall } from "./pwa.ts";
import { PUSH_PREFS_KEY, parsePushPrefs, rememberOpen, takeOpen, unsupportedReason, type PushPrefs, type PushStatus } from "./push.ts";
import { PwaControls } from "./pwa-ui.tsx";
import { PhoneApp, type PhoneActions } from "./phone.tsx";
import { ConfirmProvider, useConfirmHost } from "./confirm-ui.tsx";
import { DirectoryContext, ProjectPicker, QuietGate, useQuietUnlock, type Directory, type ProjectPatch } from "./project-ui.tsx";
import { FILTER_KEY, expandOrder, findProject, memberOn, moveToast, orderProjects, projectRecords, recordOf, splitSharedPatch } from "./projects.ts";
import { HIDDEN_KEY, NOTHING_HIDDEN, applyHidden, hiddenCount, parseHidden, pushHiddenOf, type Hidden, type PushHidden } from "./hidden.ts";
import { HOSTS_OFF_KEY, OLD_HOST_FILTER_KEY, activeFilters, applyFilters, clickHost, filterEmpty, hostKey, parseHostsOff, type FilterSel } from "./filters.ts";
import { admitThread, hideQuiet, nextQuietEnd, quietLocked, reachable } from "./quiet.ts";
import { FolderPickContext, FolderPicker, type FolderPick, type FsApi, type PickOptions } from "./folder-picker.tsx";
import { backTarget, initialEntries, leavesThread, navFromHistory, urlFor, type Nav } from "./nav.ts";
import { PHONE_UNDO_MS } from "./mobile.ts";
import { hostIdByName, hostOfflineNote, multiHost } from "./hosts.ts";
import { HostsContext, ProfilesContext } from "./host-pick-ui.tsx";
import { HostMarksContext } from "./host-mark-ui.tsx";
import { VIEWPORT_VV_EVENTS, VIEWPORT_WINDOW_EVENTS, rereadLater, viewportVars } from "./viewport.ts";
import { deviceId, presenceTarget, shouldBeat, type BeatState } from "./presence.ts";
import { AUTO_HIDE_MS, AUTO_HIDE_SECTIONS, hideClocks, hideStep, type HideAt } from "./inbox-autohide.ts";
import { DeployBanner } from "./deploy-ui.tsx";
import { StallBanner } from "./stall-ui.tsx";
import { restartNowAsk } from "./deploy.ts";
import { cache, mergeEventLists } from "./cache.ts";
import { navTransition } from "./native.ts";
import { BottomSheet, installPress, PHONE_QUERY, Pop } from "./motion-ui.tsx";
import { SETTLE_DONE_TOAST, menuFor, targetAt, type MenuEntry, type MenuItem, type MenuTarget } from "./menu.ts";
import { ContextMenu } from "./menu-ui.tsx";
import { Boundary } from "./boundary.tsx";
import { HomeScreen } from "./home-ui.tsx";
import { isDeskEntry, leaveDeskThread, popStep, recordOpen, type OpenCause } from "./desk-history.ts";

/** Run when the browser is idle (V0.38: storing never competes with input). */
const idle = (fn: () => void) => ((globalThis as { requestIdleCallback?: (f: () => void, o?: object) => void }).requestIdleCallback ?? ((f: () => void) => setTimeout(f, 0)))(fn, { timeout: 3000 });

/** V0.status-home: the keys the status screen names at its foot. */
const HOME_KEYS: [string, string][] = [
  ["j/k", "pick"],
  ["Enter", "open"],
  ["c", "new thread"],
  ["?", "keys"],
];

const sessionStore = () => {
  try {
    return typeof sessionStorage !== "undefined" ? sessionStorage : null;
  } catch {
    return null;
  }
};


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
 * V0.165: the app's chrome does not zoom. iOS Safari ignores user-scalable=no in the viewport, so its gesture events
 * are stopped here too; previews zoom inside their own viewer, from pointer events, which this does not touch.
 */
function useStaticPage() {
  useEffect(() => {
    const no = (e: Event) => e.preventDefault();
    for (const t of ["gesturestart", "gesturechange"]) document.addEventListener(t, no, { passive: false });
    return () => {
      for (const t of ["gesturestart", "gesturechange"]) document.removeEventListener(t, no);
    };
  }, []);
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

const hostApi = await createApi(); // #42: the mock Api is a chunk loaded only in mock mode
const params = new URLSearchParams(location.search);

type Dialog = "new" | "adopt" | "snooze" | "picker" | "handover" | "settings" | "changes" | null;
type Zone = "list" | "thread";

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
  // #41: every component reaches the host through ApiContext; a test can provide a fake Api.
  const api = useApi();
  const [fullStore, setStore] = useState<Store>(initialStore);
  // V0.207: what this device hides is gone from `store`, so every list on both fronts follows. Settings and the cache
  // read `fullStore`.
  const [deviceHidden, setHiddenState] = useState<Hidden>(() => {
    try {
      return parseHidden(localStorage.getItem(HIDDEN_KEY));
    } catch {
      return NOTHING_HIDDEN;
    }
  });
  const setHidden = (h: Hidden) => {
    setHiddenState(h);
    try {
      if (hiddenCount(h)) localStorage.setItem(HIDDEN_KEY, JSON.stringify(h));
      else localStorage.removeItem(HIDDEN_KEY);
    } catch {
      /* storage blocked */
    }
  };
  const store = useMemo(() => applyHidden(fullStore, deviceHidden), [fullStore, deviceHidden]);
  // V0.109: the list keeps no cursor of its own. The marked row is the open thread; with nothing open, the last one.
  const [lastOpen, setLastOpen] = useState<string | null>(null);
  const [view, setView] = useState<{ id: string; mode: "open" | "peek" } | null>(null);
  const sel = markedRow(view?.id ?? null, lastOpen);
  const [zone, setZone] = useState<Zone>("list");
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [dialog, setDialog] = useState<Dialog>(null);
  const [snoozeTargets, setSnoozeTargets] = useState<string[]>([]);
  // V0.45: the thread the handover dialog is for
  const [handoverId, setHandoverId] = useState<string | null>(null);
  const [tracker, setTracker] = useState<SeenTracker>(initialTracker);
  const [cards, setCards] = useState<Record<string, CardState>>({});
  // V0.186: the owner's folds, kept per device; a reload (a new build does one by itself) must not undo them.
  const [sectionOpen, setSectionOpen] = useState<Partial<Record<SectionKey, boolean>>>(() => {
    try {
      return parseSectionOpen(localStorage.getItem(SECTION_OPEN_KEY));
    } catch {
      return {};
    }
  });
  useEffect(() => {
    try {
      localStorage.setItem(SECTION_OPEN_KEY, JSON.stringify(sectionOpen));
    } catch {
      /* private mode: the folds last until the reload */
    }
  }, [sectionOpen]);
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
  useStaticPage();
  // U8: Undo of a settle or archive that left the open thread on a phone reopens it.
  const reopenRef = useRef<string | null>(null);
  const navGoRef = useRef<(n: Nav) => void>(() => {});
  // V0.207: the row the owner just marked seen or unseen goes where that puts it: it no longer keeps its place.
  const [freed, setFreed] = useState<string | null>(null);
  useEffect(() => setFreed(null), [sel]);
  const cmds = useThreadCommands({
    onRun: (action, ids) => {
      if ((action === "thread.markSeen" || action === "thread.markUnseen") && ids.length === 1) setFreed(ids[0]);
    },
    undoMs: phone ? PHONE_UNDO_MS : UNDO_MS,
    hostOf: (id) => threadsRef.current[id]?.hostId ?? "",
    onUndone: (entry, ok) => {
      const back = reopenRef.current;
      reopenRef.current = null;
      if (ok && back && phoneRef.current && entry.threadIds.includes(back)) navGoRef.current({ screen: { name: "thread", id: back }, sheet: null });
    },
  });
  const { toast } = cmds;
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
  // V0.169: the hosts turned off, remembered per device beside the project filter. V0.134's one host is dropped.
  const [hostsOff, setHostsOffState] = useState<string[]>(() => {
    try {
      localStorage.removeItem(OLD_HOST_FILTER_KEY);
      return parseHostsOff(localStorage.getItem(HOSTS_OFF_KEY));
    } catch {
      return [];
    }
  });
  const setHostsOff = (keys: string[]) => {
    setHostsOffState(keys);
    try {
      if (keys.length) localStorage.setItem(HOSTS_OFF_KEY, JSON.stringify(keys));
      else localStorage.removeItem(HOSTS_OFF_KEY);
    } catch {
      /* storage blocked */
    }
  };
  // V0.119: the page the desktop Settings dialog shows (`?dialog=settings&page=providers/<id>` for screenshots).
  const [settingsAt, setSettingsAt] = useState<PageRef>(() => parsePage(params.get("page")));
  const openSettings = (at: PageRef) => {
    setSettingsAt(at);
    setDialog("settings");
  };
  // The project picker (V0.30): the threads it files.
  const [pickIds, setPickIds] = useState<string[]>([]);
  const openPicker = (ids: string[]) => {
    setPickIds(ids);
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
  // Decision 0034: the filter may name any member of a linked project; the list knows the project by one id.
  const filterProject = findProject(store.projects, projectFilter);
  const filterId = filterProject?.id ?? projectFilter;
  const filterOn = !!filterProject;
  // V0.114: one unlock of quiet projects on this device for every path (New thread, the filter, search, the gate).
  const unlock = useQuietUnlock(store.clockOffset);
  // V0.207: a hidden host or project is no filter value: it would leave a chip behind.
  const filterSel = useMemo<FilterSel>(() => {
    const gone = (key: string) => fullStore.hosts.some((h) => hostKey(h) === key) && !store.hosts.some((h) => hostKey(h) === key);
    const projectGone = !!findProject(fullStore.projects, projectFilter) && !filterOn;
    return { hostsOff: hostsOff.filter((n) => !gone(n)), project: projectGone ? null : projectFilter || null };
  }, [hostsOff, projectFilter, fullStore.hosts, fullStore.projects, store.hosts, filterOn]);
  // V0.finish-debounce: a thread that just stopped working stays in Moving a few seconds before the list shows it
  // finished, so a run that starts again at once does not blink. The thread view reads store.threads, live.
  const [holdClock, setHoldClock] = useState(0);
  const holdRef = useRef<{ holds: Record<string, FinishHold>; last: Record<string, Thread> | null }>({ holds: {}, last: null });
  const steady = useMemo(() => {
    const r = steadyThreads(holdRef.current.holds, holdRef.current.last, store.threads, Date.now());
    holdRef.current = { holds: r.holds, last: store.threads };
    return r;
  }, [store.threads, holdClock]);
  useEffect(() => {
    if (steady.next === null) return;
    const t = setTimeout(() => setHoldClock((n) => n + 1), Math.max(0, steady.next - Date.now()) + 50);
    return () => clearTimeout(t);
  }, [steady.next]);
  const listThreads = steady.threads;
  // V0.134: host AND project.
  const filtered = useMemo(() => applyFilters(listThreads, filterSel, store.projects), [listThreads, store.projects, filterSel]);
  // What search and the filter may reach: quiet threads only while the unlock runs (V0.114; V0.46 (4) showed them).
  const searchable = useMemo(() => reachable(filtered, Date.now() + store.clockOffset, unlock.showUntil), [filtered, unlock.showUntil, quietClock, store.clockOffset]);
  // V0.ai-search: what AI search reads, in the list's order.
  const searchList = useMemo(() => {
    const sec = sections(searchable);
    return SECTIONS.flatMap((s) => sec[s.key]);
  }, [searchable]);
  // V0.114: search hits the lock leaves out, so the search says why and offers the hold (desktop).
  const quietHits = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return 0;
    return Object.values(filtered).filter((t) => !searchable[t.id] && matchesQuery(t, q)).length;
  }, [filtered, searchable, query]);
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    // V0.46: a quiet project's threads leave the inbox and its counts, unless the filter picks it or a search is typed
    // (and, V0.114, the unlock runs: searchable holds them only then).
    const base = hideQuiet(searchable, filterOn || !!q, Date.now() + store.clockOffset);
    if (!q) return base;
    const out: Record<string, Thread> = {};
    for (const t of Object.values(base)) {
      if (matchesQuery(t, q)) out[t.id] = t;
    }
    return out;
  }, [searchable, filterOn, query, quietClock, store.clockOffset]);
  const queue = useSlotQueue(); // V0.137
  // V0.207: `sec` is the truth, for every count; the list draws `rows` and j/k walks `drawn`, which keep their place.
  // V0.stable-groups: when each thread came into its group; a new row goes last and nothing else moves one.
  const arrivalsRef = useRef<Record<string, Arrival>>({});
  const arrivals = useMemo(() => (arrivalsRef.current = trackArrivals(arrivalsRef.current, listThreads, Date.now() + store.clockOffset)), [listThreads]);
  const sec = useMemo(() => sections(shown, queue, arrivals), [shown, queue, arrivals]);
  // V0.134, V0.169: the two rows (only values with threads in view; a selected one stays) and the empty state, on both
  // fronts.
  const filterView = useMemo<FilterView>(() => {
    const chips = activeFilters({ threads: store.threads, projects: store.projects, hosts: store.hosts, sel: filterSel, open: sectionOpen, now: Date.now() + store.clockOffset, showUntil: unlock.showUntil });
    const locked = !!filterSel.project && chips.projects.some((c) => c.selected && quietLocked(c.project, Date.now() + store.clockOffset, unlock.showUntil));
    return {
      ...chips,
      sel: filterSel,
      toggleHost: (key) => setHostsOff(clickHost(filterSel.hostsOff, chips.hosts, key)),
      setProject: setProjectFilter,
      clear: () => (setHostsOff([]), setProjectFilter(null)),
      // V0.114: a locked quiet project shows its gate instead.
      empty: locked ? null : filterEmpty(hideQuiet(searchable, filterOn, Date.now() + store.clockOffset), filterSel, sectionOpen),
    };
  }, [store.threads, store.projects, store.hosts, filterSel, sectionOpen, unlock.showUntil, searchable, filterOn, quietClock, store.clockOffset]);
  const limits = useMemo(() => {
    const l: Partial<Record<SectionKey, number>> = {};
    for (const s of SECTIONS) l[s.key] = !isOpen(s.key, sectionOpen) ? 0 : expanded[s.key] ? Infinity : SECTION_LIMIT[s.key];
    return l;
  }, [expanded, sectionOpen]);
  // V0.114: a quiet thread opens only through the hold, whatever path opened it (a link, back and forward, a notice,
  // a board card, ?open=, a push tap). The thread already open when its window starts stays open (V0.46 (6)).
  const admittedRef = useRef<string | null>(null);
  const wantId = phone ? (nav.screen.name === "thread" ? nav.screen.id : null) : (view?.id ?? null);
  const gate = admitThread(admittedRef.current, wantId, wantId ? store.threads[wantId] : undefined, Date.now() + store.clockOffset, unlock.showUntil);
  const gatedRef = useRef(false);
  gatedRef.current = gate.gated;
  admittedRef.current = gate.admitted;
  const gatedThread = gate.gated && wantId ? store.threads[wantId] : undefined;
  // V0.192: the current thread punches through what hides it: the filters and quiet hours add its row in its own place
  // (`rows`), a folded section draws it alone (`guest`). `sec` stays what the filters let through, for every count.
  const current = sel ? listThreads[sel] : undefined;
  // V0.212: not on the phone. The list and the thread are never on screen together there, so the row marks nothing.
  const hidden = phone ? null : hiddenWhy(current, { shown, filtered, query, allowed: !!current && (gate.admitted === current.id || !!reachable({ [current.id]: current }, Date.now() + store.clockOffset, unlock.showUntil)[current.id]) });
  const listed = useMemo(() => (hidden && current ? { ...shown, [current.id]: current } : shown), [shown, hidden, current]);
  const liveRows = useMemo(() => (listed === shown ? sec : sections(listed, queue, arrivals)), [listed, shown, sec, queue, arrivals]);
  // V0.open-row-only: the open row keeps the place it had on the screen when it was opened, until another thread is
  // current. Every other row is where the live list has it. V0.phone-row-moves: not on the phone, where the list and the
  // thread are never on screen together, so back on the list the row is where the live list has it.
  const drawnRef = useRef<Sections | null>(null);
  const placeRef = useRef<RowPlace | null>(null);
  if (placeRef.current?.id !== sel) placeRef.current = (drawnRef.current && placeOf(drawnRef.current, sel)) ?? placeOf(liveRows, sel);
  const place = phone || placeRef.current?.id === freed ? null : placeRef.current;
  const rows = useMemo(() => stillRows(liveRows, place), [liveRows, place]);
  drawnRef.current = rows;
  const drawn = useMemo(() => stillRows(sec, place), [sec, place]);
  const guest = useMemo(() => guestOf(rows, sel, hidden, sectionOpen), [rows, sel, hidden, sectionOpen]);
  const order = useMemo(() => flatOrder(drawn, limits), [drawn, limits]);
  useListAnchor(useMemo(() => flatOrder(rows, limits).join(" "), [rows, limits]), sel);
  const allRows = useMemo(() => flatOrder(rows), [rows]); // V0.186: with the rows not drawn, for the guest's place
  const viewThread = view && !gate.gated ? store.threads[view.id] : undefined;

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
        clearThreadNotification(t.id, t.attention.state);
        batch((s) => upsertThread(s, t));
      },
      // V0.38: a patch against our copy; when it does not fit, ask for the whole thread.
      threadPatch: (p) => {
        const st = (p.patch as { attention?: { state?: string } }).attention?.state;
        if (st) clearThreadNotification(p.id, st);
        batch((s) => {
          const r = applyThreadPatch(s, p);
          if (r.needFull) queueMicrotask(() => void api.thread(s.threads[p.id]?.hostId ?? "", p.id).then((t) => t && setStore((x) => upsertThread(x, t))));
          return r.store;
        });
      },
      event: (e) => batch((s) => liveEvent(s, e)),
      project: (p) => batch((s) => upsertProject(s, p)),
      projectOrder: (ids) => batch((s) => setProjectOrder(s, ids)),
      deploy: (p) => batch((s) => setDeploy(s, p)),
      stall: (raw) => batch((s) => setStall(s, raw)),
      hosts: (hs) => batch((s) => setHosts(s, hs)),
      hostMarks: (m) => batch((s) => ({ ...s, hostMarks: m })), // V0.171
      conn: (c) => batch((s) => setConn(s, c)),
    });
    const iv = setInterval(() => setTick(Date.now()), 15_000);
    return () => clearInterval(iv);
  }, []);

  // A link names a record by the host's own id; once the records are here it names the ref (decision 0031 C12).
  useEffect(() => {
    if (!store.loaded) return;
    const s = nav.screen;
    if (s.name === "thread") {
      const r = resolveRef(Object.values(store.threads), s.id);
      if (r !== s.id) navReplace({ ...nav, screen: { ...s, id: r } });
    } else if (s.name === "project") {
      const r = resolveRef(projectRecords(store.projects), s.id);
      if (r !== s.id) navReplace({ ...nav, screen: { ...s, id: r } });
    }
  }, [store.loaded, nav.screen.name, "id" in nav.screen ? nav.screen.id : ""]);

  // V0.client-refs (decision 0031 4.2): what this device kept by the hosts' own ids and names moves to refs and install
  // ids, once, when the first state lists the hosts and their records. The filters read again what moved.
  const rereadFilters = () => {
    try {
      setProjectFilterState(localStorage.getItem(FILTER_KEY));
      setHostsOffState(parseHostsOff(localStorage.getItem(HOSTS_OFF_KEY)));
    } catch {
      /* storage blocked */
    }
  };
  useEffect(() => {
    if (!fullStore.loaded) return;
    let kv: Storage | null = null;
    try {
      kv = localStorage;
    } catch {
      return;
    }
    if (migrateStorage(kv, { hosts: fullStore.hosts, threads: Object.values(fullStore.threads), projects: projectRecords(fullStore.projects) })) rereadFilters();
  }, [fullStore.loaded]);
  // Decision 0031 2.1, 2.5: a host listed with a new id and the one it had (idWas): what the device keeps under the old
  // id moves to the new one (the keys, the stored timelines, the store, the thread on screen).
  const rotationKey = rotations(fullStore.hosts).map((r) => `${r.was}>${r.now}`).join(",");
  useEffect(() => {
    if (!rotationKey) return;
    for (const { was, now } of rotations(fullStore.hosts)) {
      let kv: Storage | null = null;
      try {
        kv = localStorage;
      } catch {
        /* storage blocked */
      }
      if (moveHostKeys(kv, was, now)) rereadFilters();
      void cache.moveHost(was, now);
      setStore((s) => moveHostInStore(s, was, now));
      setView((v) => (v && moveRef(v.id, was, now) !== v.id ? { ...v, id: moveRef(v.id, was, now) } : v));
      setLastOpen((id) => (id ? moveRef(id, was, now) : id));
      const sc = nav.screen;
      if ((sc.name === "thread" || sc.name === "project") && moveRef(sc.id, was, now) !== sc.id) navReplace({ ...nav, screen: { ...sc, id: moveRef(sc.id, was, now) } });
    }
  }, [rotationKey]);

  // Initial selection and ?open= for screenshots.
  const didInit = useRef(false);
  useEffect(() => {
    if (!store.loaded || didInit.current) return;
    didInit.current = true;
    const want = params.get("open") ? resolveRef(Object.values(store.threads), params.get("open")!) : null;
    const first = want && store.threads[want] ? want : null;
    // On a phone the inbox comes first; V0.status-home: the desktop starts on the status screen. Both open a thread only
    // when asked (?open=).
    if (first) {
      const phone = typeof window.matchMedia === "function" && window.matchMedia(PHONE_QUERY).matches;
      if (!phone) recordOpen(first, "auto");
      setLastOpen(first);
      setView({ id: first, mode: "open" });
      setZone("thread");
      setTracker((tr) => openView(tr, first, "open", Date.now(), document.hasFocus()));
    }
    const d = params.get("dialog");
    if (d === "new" || d === "adopt" || d === "settings" || d === "changes") setDialog(d);
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
    void api.eventsBefore(store.threads[id]?.hostId ?? "", id, first, PAGE).then((evs) => {
      setEarlier((m) => ({ ...m, [id]: evs.length >= PAGE }));
      setStore((s) => mergeEvents(s, id, evs));
    });
  };

  // Load the open thread's events; after a reconnect fetch what was missed.
  const openId = view && !gate.gated ? view.id : null;

  // Presence (V0.20): tell the host which thread is shown while visible.
  const beatRef = useRef<BeatState>({ last: "", at: 0 });
  useEffect(() => {
    const device = deviceId();
    const tick = () => {
      const target = presenceTarget(openId, document.visibilityState);
      const now = Date.now();
      if (shouldBeat(beatRef.current, target, now)) {
        beatRef.current = { last: target, at: now };
        void api.presence(threadsRef.current[target]?.hostId ?? "", target, device);
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
    // After a reconnect, from where the timeline stood when the stream dropped (review 2026-11-15).
    const after = resumeSeq(store, openId);
    const known = !!store.events[openId];
    const id = openId;
    void (async () => {
      // V0.38: the device's copy draws at once; the host sends only what is newer.
      const stored = !known && !api.mock ? await cache.events(id) : null;
      if (cancelled) return;
      if (stored?.length) setStore((s) => (s.events[id] ? s : mergeEvents(s, id, stored, true)));
      const from = known ? after : stored?.length ? stored[stored.length - 1].seq : 0;
      try {
        const evs = await api.events(threadsRef.current[id]?.hostId ?? "", id, from, PAGE);
        if (cancelled) return;
        if (!known && !stored?.length) setEarlier((m) => ({ ...m, [id]: evs.length >= PAGE }));
        if (!known && stored?.length) {
          const merged = mergeEventLists(stored, evs, PAGE, evs.length >= PAGE);
          if (evs.length >= PAGE) setEarlier((m) => ({ ...m, [id]: true }));
          setStore((s) => mergeEvents(s, id, merged, true));
        } else setStore((s) => resumed(mergeEvents(s, id, evs, !known), id, from));
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
          void cache.saveState(stateForCache(fullStore, new Date().toISOString())),
        ),
      2000,
    );
    return () => clearTimeout(t);
  }, [fullStore.threads, fullStore.projects, fullStore.hosts, fullStore.deploy, fullStore.loaded]);

  // V0.207: the front leaves this device out of the pushes of what it hides. Sent again when push turns on and on a
  // reconnect, so a change made offline arrives.
  const pushHidden = useMemo(() => JSON.stringify(pushHiddenOf(fullStore.hosts, fullStore.projects, deviceHidden)), [fullStore.hosts, fullStore.projects, deviceHidden]);
  useEffect(() => {
    if (pushSt !== "on" || !fullStore.loaded || fullStore.conn === "offline" || api.mock) return;
    void syncPushHidden(api, JSON.parse(pushHidden) as PushHidden);
  }, [pushHidden, pushSt, fullStore.loaded, fullStore.conn]);

  // V0.notification-polish: what this device is told about, kept in this browser and sent to the front like the hidden
  // list: again when push turns on and on a reconnect.
  const [pushPrefs, setPushPrefsState] = useState<PushPrefs>(() => {
    try {
      return parsePushPrefs(localStorage.getItem(PUSH_PREFS_KEY));
    } catch {
      return parsePushPrefs(null);
    }
  });
  const setPushPrefs = (p: PushPrefs) => {
    setPushPrefsState(p);
    try {
      localStorage.setItem(PUSH_PREFS_KEY, JSON.stringify(p));
    } catch {
      /* storage denied: the choice holds until a reload */
    }
  };
  useEffect(() => {
    if (pushSt !== "on" || !fullStore.loaded || fullStore.conn === "offline" || api.mock) return;
    void syncPushPrefs(api, pushPrefs);
  }, [pushPrefs.finished, pushSt, fullStore.loaded, fullStore.conn]);

  // Decision card state follows the open decision.
  const decision: Decision | null = viewThread ? currentDecision(viewThread) : null;
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
      // V0.114: a thread behind the quiet gate is not on screen, so it is not seen.
      if (t && !gatedRef.current && shouldMarkSeen(tr, t, Date.now())) {
        setTracker((x) => markSent(x, t));
        // upTo: only what this view showed counts as seen (API v0, SPEC 6.4).
        void api.command(t.hostId ?? "", "thread.markSeen", t.id, { upTo: t.activityAt });
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

  // Track whether focus is in a text field (for the key-hint bar).
  useEffect(() => {
    const f = () => setInText(isTextTarget(document.activeElement));
    const out = () => setTimeout(f, 0);
    document.addEventListener("focusin", f);
    document.addEventListener("focusout", out);
    return () => {
      document.removeEventListener("focusin", f);
      document.removeEventListener("focusout", out);
    };
  }, []);

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
    const next = rowAfter(order, sel, ids, allRows);
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
    const to = afterSettle(order, sel, ids, allRows);
    if (to === "stay") return;
    if (to === "home") return view ? closeThread() : setLastOpen(null);
    const z = zone;
    if (view) openThread(to, view.mode);
    else setLastOpen(to);
    setZone(z);
  };

  /** V0.203: settles now; on a running thread the host stops the run, and the toast says so. */
  const settle = (id: string) => {
    const label = store.threads[id]?.running ? "Settled; run stopped" : undefined;
    leaveSettled([id]);
    void cmds.run("thread.settle", [id], undefined, label ? { label: () => label } : undefined);
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
    const { ids: take } = bulkSettle(ids.map((id) => store.threads[id]).filter((t): t is Thread => !!t), sweep);
    leaveSettled(take);
    setPicked(new Set());
    void settleBulk(ids, { sweep });
  };

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

  // ---- right-click menu (V0.31) ----
  const [ctx, setCtx] = useState<{ x: number; y: number; items: MenuEntry[]; target: MenuTarget; anchor: Element | null } | null>(null);
  useEffect(() => {
    const on = (e: MouseEvent) => {
      const selection = String(window.getSelection?.() ?? "").trim();
      const target = targetAt(e.target as Element | null, selection, threadsRef.current);
      const items = menuFor(target);
      if (items === null) return; // text fields keep the browser menu (spelling, Paste)
      e.preventDefault();
      if (phone || !items.length) return;
      setCtx({ x: e.clientX, y: e.clientY, items, target, anchor: (e.target as Element | null)?.closest?.("[data-id]") ?? (e.target as Element | null) });
    };
    document.addEventListener("contextmenu", on);
    return () => document.removeEventListener("contextmenu", on);
  }, [phone]);
  const copyText = (text: string) => {
    void navigator.clipboard?.writeText(text).then(
      () => cmds.notify("Copied"),
      () => cmds.showError("Could not copy"),
    );
  };
  const MENU_ACTION: Record<string, string> = { pin: "thread.pin", unpin: "thread.unpin", unsettle: "thread.unsettle", unsnooze: "thread.unsnooze", markSeen: "thread.markSeen", unarchive: "thread.unarchive", interrupt: "thread.interrupt", dismissPR: "thread.dismissPR" };
  const pickMenu = (it: MenuItem, target: MenuTarget) => {
    setCtx(null);
    if (target.kind === "thread") {
      const id = target.t.id;
      if (it.id === "open") openThread(id, "open");
      else if (it.id === "settle") settle(id);
      else if (it.id === "settleWhenDone") settleWhenDone(id);
      else if (it.id === "archive") archive([id]);
      else if (it.id === "snooze") openSnooze([id]);
      else if (it.id === "markUnseen") markUnseen(id);
      else if (it.id === "project") openPicker([id]);
      else if (it.id === "copyLink") copyText(`${location.origin}/?open=${id}`);
      else if (it.id === "liveView") void cmds.run("thread.setView", [id], { on: !target.t.liveView });
      else if (it.id === "handover") (setHandoverId(id), setDialog("handover"));
      else if (it.id === "regenTitle") void cmds.regenerateTitle(id);
      else if (MENU_ACTION[it.id]) void cmds.run(MENU_ACTION[it.id], [id]);
      return;
    }
    if (it.id === "copy" && (target.kind === "text" || target.kind === "link")) copyText(target.kind === "text" ? target.selection : (target.selection ?? ""));
    else if (it.id === "openLink" && target.kind === "link") window.open(target.href, "_blank", "noopener");
    else if (it.id === "copyLink" && target.kind === "link") copyText(target.href);
    else if (it.id === "openImage" && target.kind === "image") window.open(target.src, "_blank", "noopener");
  };
  // A thread's menu follows the thread while it is open (its state can change under the pointer); the action goes to
  // the row's own thread id, the same settle() as the thread's button and `e`.
  const ctxTarget: MenuTarget | null = ctx && (ctx.target.kind === "thread" ? (store.threads[ctx.target.t.id] ? { kind: "thread", t: store.threads[ctx.target.t.id] } : null) : ctx.target);
  const ctxMenu = ctx && ctxTarget && (
    <ContextMenu x={ctx.x} y={ctx.y} items={ctx.target.kind === "thread" ? (menuFor(ctxTarget) ?? []) : ctx.items} anchor={ctx.anchor} onPick={(it) => pickMenu(it, ctxTarget)} onClose={() => setCtx(null)} />
  );

  const submitAsk = (d: Decision) => {
    const cs = cards[d.id];
    if (!cs || !view) return;
    const answers = answersOf(cs, d);
    if (!answers) return;
    const id = view.id;
    void answerThenNext(() => cmds.run("thread.answer", [id], { decisionId: d.id, answers }), () => nextNeeding(1, id));
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
    void cmds.run("thread.answer", [view.id], { decisionId: d.id, allow: ok });
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

  // V0.48, V0.196: Snoozed and Settled opened from their header close again after 10 s, on both fronts, unless the
  // current thread is in them.
  const [hideAt, setHideAt] = useState<HideAt>({});
  const [ahTick, setAhTick] = useState(0);
  const activeForHide: SectionKey | null = sel && store.threads[sel] ? sectionOf(store.threads[sel]) : null;
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
  }, [hideAt, sectionOpen, activeForHide, ahTick]);
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
    if (!drawn[k].length) return; // an empty section is not a place to go
    if (!listOn) {
      setListPrefState(true); // jumping to a section shows the list (V0.40)
      setListPref(true);
    }
    nameSection(k);
    const first = drawn[k][0];
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
  useEffect(() => {
    const k = cutSection(rows, view?.id ?? null, limits);
    if (k) setExpanded((x) => ({ ...x, [k]: true }));
  }, [view?.id, openSection, !!(view && listed[view.id]), limits]);

  // Keep the mark visible: when it changes, moves to another section or the pinned block, or is drawn again.
  const selPlace = sel && listed[sel] ? `${placeOf(rows, sel)?.section ?? sectionOf(listed[sel])}:${isPinned(listed[sel])}:${order.includes(sel)}:${guest?.why ?? ""}` : "";
  useEffect(() => {
    if (!sel) return;
    const id = CSS.escape(sel);
    document.querySelector(`.list [data-id="${id}"]`)?.scrollIntoView({ block: "nearest" });
  }, [sel, selPlace, listOn]);

  const send = async (args: SendArgs): Promise<boolean> => (view ? cmds.send(view.id, args) : false);

  // ---- projects (V0.23) ----
  const projectCommand = async (action: string, args: Record<string, unknown>, threadId?: string): Promise<string | null> => {
    // A project command goes to the project's host, a thread's to the thread's (decision 0031).
    const pid = typeof args.id === "string" ? args.id : typeof args.projectId === "string" ? args.projectId : "";
    // V0.207: from the full store, because Settings also edits what this device hides.
    const host = (threadId ? fullStore.threads[threadId]?.hostId : recordOf(fullStore.projects, pid)?.hostId) ?? "";
    const r = await api.command(host, action, threadId, args);
    return r.ok ? null : (r.error ?? "That did not work");
  };
  const projectActions = {
    /** Decision 0034: on a linked project the shared values go to every host through the front; the rest is this host's. */
    update: async (id: string, patch: ProjectPatch): Promise<string | null> => {
      const link = findProject(fullStore.projects, id)?.members ? recordOf(fullStore.projects, id)?.link : undefined; // V0.207: Settings edits hidden ones too
      if (!link) return projectCommand("project.update", { id, ...patch });
      const { shared, own } = splitSharedPatch(patch as Record<string, unknown>);
      if (Object.keys(shared).length) {
        const r = await api.frontCommand("front.projectShared", { link, ...shared });
        if (!r.ok) return r.error ?? "That did not work";
        const failed = (r.result as { failed?: { error?: string }[] } | undefined)?.failed;
        if (failed?.[0]?.error) return failed[0].error;
      }
      return Object.keys(own).length ? projectCommand("project.update", { id, ...own }) : null;
    },
    /** V0.55: the project's one folder, from the picker. */
    setFolder: (id: string, path: string) => projectCommand("project.update", { id, folder: path }),
    /** New project (V0.30): with threadIds, the host files one thread in the same write; more are filed after. */
    create: async (name: string, folder: string, threadIds: string[] = [], host?: string): Promise<{ error: string | null; id?: string }> => {
      const args: Record<string, unknown> = { name };
      // V0.132: on the host picked, else the threads' host (a project made from threads lives where they run).
      const on = host ? hostIdByName(fullStore.hosts, host) : threadIds.length ? (store.threads[threadIds[0]]?.hostId ?? "") : hostIdByName(store.hosts);
      if (folder) args.folder = folder;
      if (threadIds.length === 1) args.threadId = threadIds[0];
      const r = await api.command(on, "project.create", undefined, args);
      if (!r.ok) return { error: r.error ?? "That did not work" };
      const id = r.result?.id;
      if (threadIds.length > 1 && id) await Promise.all(threadIds.map((t) => projectCommand("thread.setProject", { projectId: id }, t)));
      cmds.notify(threadIds.length ? `Made project ${name} and filed ${threadIds.length === 1 ? "this thread" : `${threadIds.length} threads`}` : `Made project ${name}`);
      return { error: null, id };
    },
    /** V0.58: delete a project; its threads match again on the host. */
    delete: async (id: string): Promise<string | null> => {
      const name = recordOf(fullStore.projects, id)?.name ?? "the project";
      const e = await projectCommand("project.delete", { id });
      if (!e) cmds.notify(`Deleted ${name}`);
      return e;
    },
    /** V0.121: the owner's one project order. It shows at once on this device; a refusal puts the last order back. */
    order: async (ids: string[]): Promise<string | null> => {
      const before = store.projectOrder;
      const all = expandOrder(ids, store.projects); // decision 0034: a linked project's members move together
      setStore((s) => setProjectOrder(s, all));
      const r = await api.frontCommand("front.projectOrder", { ids: all });
      if (r.ok) return null;
      setStore((s) => (before ? setProjectOrder(s, before) : { ...s, projectOrder: null, projects: orderProjects(s.projects, null) }));
      return r.error ?? "That did not work";
    },
    /** File threads into a project; null: No project. Shows the result or the error as a toast. */
    move: async (ids: string[], projectId: string | null): Promise<string | null> => {
      // Decision 0034: a thread is filed in its own host's record of a linked project.
      const pr = findProject(store.projects, projectId);
      const errs = (await Promise.all(ids.map((id) => projectCommand("thread.setProject", { projectId: memberOn(pr, store.threads[id]?.hostId)?.id ?? projectId ?? "" }, id)))).filter(Boolean);
      const where = projectId ? (pr?.name ?? "the project") : null;
      // Both shells show the result here, the error too (review 2026-11-15).
      const e = errs[0] ?? null;
      const m = moveToast(ids.length, where, e);
      cmds.notify(m.text, m.error);
      return e;
    },
    /** Decision 0034: make two projects on different hosts one. A host that could not store it is the error. */
    link: async (id: string, other: string): Promise<string | null> => {
      const r = await api.frontCommand("front.projectLink", { refs: [id, other] });
      if (!r.ok) return r.error ?? "That did not work";
      const failed = (r.result as { failed?: { error?: string }[] } | undefined)?.failed;
      return failed?.[0]?.error ?? null;
    },
    /** Decision 0034: the project gets a member on another host, in a folder there. */
    add: async (id: string, host: string, folder: string): Promise<{ error: string | null; id?: string }> => {
      const r = await api.frontCommand("front.projectAdd", { ref: id, host, folder });
      if (!r.ok) return { error: r.error ?? "That did not work" };
      return { error: null, id: (r.result as { id?: string } | undefined)?.id };
    },
    /** Decision 0034: this host's record leaves the link, with its values and threads. */
    unlink: async (id: string): Promise<string | null> => {
      const r = await api.frontCommand("front.projectUnlink", { ref: id });
      return r.ok ? null : (r.error ?? "That did not work");
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
  // V0.68: Fyr's own confirmation, a sheet on the phone and a dialog on the desktop.
  const confirmHost = useConfirmHost(phone);
  // V0.132: the picker reads the folders of the host it was opened for; an offline host says so, and no other host's
  // folders stand in.
  const pickHost = folderPick?.host ?? "";
  const pickId = hostIdByName(pickHost ? fullStore.hosts : store.hosts, pickHost); // V0.207: Settings may name a hidden host
  const pickFs: FsApi = { fsList: (p) => api.fsList(pickId, p), fsRecent: () => api.fsRecent(pickId), fsRepos: () => api.fsRepos(pickId), fsMkdir: (a, b) => api.fsMkdir(pickId, a, b) };
  const pickTitle = `${folderPick?.title ?? "Pick a folder"}${pickHost && multiHost(fullStore.hosts) ? ` on ${pickHost}` : ""}`;
  const pickNote = hostOfflineNote(pickHost, fullStore.hosts, now);
  const pickBody = (autoFocus: boolean) =>
    pickNote ? (
      <div class="note fnote" role="alert">
        {pickNote}
      </div>
    ) : (
      <FolderPicker key={pickHost} fs={pickFs} first={folderPick?.first} onPick={closeFolderPick} autoFocus={autoFocus} />
    );
  // V0.132: the hosts a new project or thread can live on (HostField, NewProjectStep).
  const hostsCtx = useMemo(() => ({ hosts: store.hosts, now }), [store.hosts, Math.floor(now / 60_000)]);
  const allHostsCtx = useMemo(() => ({ ...hostsCtx, hosts: fullStore.hosts }), [hostsCtx, fullStore.hosts]); // V0.207: Settings
  const folderPickView = folderPick
    ? phone ? (
        <BottomSheet onClose={() => closeFolderPick(null)} label={pickTitle} cls="fsheet" scrimCls="fscrim">
          <div class="fsh">
            <span class="m-shh">{pickTitle}</span>
            <button class="m-fchg" onClick={() => closeFolderPick(null)}>
              Cancel
            </button>
          </div>
          {pickBody(false)}
        </BottomSheet>
      ) : (
        <div class="fmodal">
          <Modal title={pickTitle} onClose={() => closeFolderPick(null)} wide>
            <div class="db">
              {pickBody(true)}
            </div>
          </Modal>
        </div>
      )
    : null;

  /** V0.119: a project's page. Desktop: Settings at projects/<id> (the separate Project dialog is gone); phone: the
   *  project screen. */
  const openProjectScreen = (id: string) => {
    if (phoneRef.current) navGo({ screen: { name: "project", id }, sheet: null });
    else openSettings({ page: "projects", sub: id });
  };
  const directory: Directory = {
    threads: store.threads,
    projects: store.projects,
    openThread: (id) => (phoneRef.current ? navGo({ screen: { name: "thread", id }, sheet: null }) : openThread(id, "open")),
    openProject: openProjectScreen,
    pickProject: (id) => (phoneRef.current ? navGo({ ...navRef.current, sheet: { kind: "project", ids: [id] } }) : openPicker([id])),
    projectFilter: filterOn ? filterId : null,
  };

  const create = async (a: NewThreadArgs): Promise<string | null> => {
    // V0.42: no cwd starts in the project's folder, or in a fresh scratch folder; the host files the project.
    const args: Record<string, unknown> = { text: a.text, profile: a.profile, model: a.model };
    if (a.cwd) args.cwd = a.cwd;
    if (a.projectId) args.projectId = a.projectId;
    if (a.attachments?.length) args.attachments = a.attachments;
    if (a.title) args.title = a.title;
    if (a.view) args.view = true;
    if (a.effort) args.effort = a.effort;
    // V0.132: the host picked; else the project's; else the first, as the picker's default (the call names it).
    const on = a.host ? hostIdByName(store.hosts, a.host) : a.projectId ? (recordOf(store.projects, a.projectId)?.hostId ?? "") : hostIdByName(store.hosts);
    const r = await api.command(on, "thread.create", undefined, args);
    if (!r.ok) return r.error ?? "Could not start the thread";
    setDialog(null);
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

  const adopt = async (ids: string[], host?: string): Promise<string | null> => {
    const r = await api.command(hostIdByName(store.hosts, host), "t3.adopt", undefined, { t3Ids: ids }); // V0.139
    if (!r.ok) return r.error ?? "Could not adopt";
    setDialog(null);
    cmds.notify(ids.length === 1 ? "Adopted 1 thread" : `Adopted ${ids.length} threads`);
    return null;
  };

  // V0.83: Restart now on the update banner: Fyr's confirmation first, then the deploy stops waiting and switches.
  const restartNow = async () => {
    const p = store.deploy;
    if (!p || !(await confirmHost.ask(restartNowAsk(p, store.threads)))) return;
    const r = await api.command(p.host ?? "", "host.deployNow"); // decision 0031: the host whose update it is
    if (!r.ok) cmds.notify(r.error ?? "Fyr could not restart now");
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
        const to = stepRow(allRows, order, sel, dir);
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
  const reloadProfiles = () => void api.profiles().then((ps) => ps.length && setStore((s) => ({ ...s, profiles: ps })));
  // V0.119: the rail's bell opens Settings at This device (the Notifications dialog is gone).
  const pwaControls = (
    <PwaControls
      push={pushSt}
      pushReason={pushReason}
      showUnsupported={env.ios && !env.standalone}
      canInstall={canInstall && !isStandalone()}
      onBell={() => openSettings({ page: "device" })}
      onInstall={() => void promptInstall().then((ok) => ok && setCanInstall(false))}
    />
  );
  /** V0.119: This device, in Settings on the desktop and the phone. */
  const settingsDevice: SettingsDevice = {
    push: pushSt,
    pushReason,
    setPush: async (on) => {
      const r = on ? await enablePush(api) : await disablePush(api);
      setPushSt(r.status);
      return r.error ?? null;
    },
    testPush: async () => {
      const r = await testPush(api);
      return r.ok ? null : (r.error ?? "The test was not sent");
    },
    pushPrefs,
    setPushPrefs,
    theme,
    setTheme,
    hidden: deviceHidden,
    setHidden,
    canInstall: canInstall && !isStandalone(),
    onInstall: () => void promptInstall().then((ok) => ok && setCanInstall(false)),
  };
  /** The Settings pages, the same on the desktop (a dialog) and the phone (screens). */
  const settingsFor = (at: PageRef | null, go: (r: PageRef | null) => void, phoneHeader?: SettingsProps["header"]) => (
    <HostsContext.Provider value={allHostsCtx}>
    <Settings
      api={api}
      front={store.host}
      hosts={fullStore.hosts}
      user={store.user}
      phone={!!phoneHeader}
      at={at}
      go={go}
      header={phoneHeader}
      offline={store.conn === "offline"}
      onChanged={reloadProfiles}
      clockKnown={store.loaded}
      device={settingsDevice}
      projects={fullStore.projects}
      profiles={store.profiles}
      now={now}
      projectActions={{
        update: projectActions.update,
        setFolder: projectActions.setFolder,
        delete: projectActions.delete,
        create: async (name, folder, host) => (await projectActions.create(name, folder, [], host)).error,
        order: projectActions.order,
        link: projectActions.link,
        unlink: projectActions.unlink,
        add: async (id, host, folder) => (await projectActions.add(id, host, folder)).error,
      }}
    />
    </HostsContext.Provider>
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
  const leaveOpenThread = (ids: string[]) => {
    const s = navRef.current.screen;
    if (s.name !== "thread" || ids.length !== 1 || ids[0] !== s.id) return;
    if (!leavesThread(threadsRef.current[s.id])) return;
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
      // V0.native-feel: after the system's own swipe back the screen has already slid; a second slide would replay it.
      const slid = (e as PopStateEvent & { hasUAVisualTransition?: boolean }).hasUAVisualTransition;
      if (!slid && JSON.stringify(n.screen) !== JSON.stringify(navRef.current.screen)) navTransition("back", () => setNav(n));
      else setNav(n);
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, [phone]);
  // V0.145: Back and Forward on the desktop (the mouse's side buttons, Alt+Left and Alt+Right, the browser's arrows)
  // open the thread of the entry, with the tab it had (ThreadPane reads it from the URL). A thread archived or deleted
  // since is skipped in the same direction. The browser handles the side buttons itself, in a tab and in the installed
  // window alike, so Fyr listens to popstate only.
  const viewRef = useRef(view);
  viewRef.current = view;
  useEffect(() => {
    if (phone) return;
    if (isDeskEntry(history.state)) deskAtRef.current = history.state.desk;
    const onPop = (e: PopStateEvent) => {
      const gone = (id: string) => {
        const t = threadsRef.current[id];
        return !t || t.attention.state === "archived";
      };
      const step = popStep(e.state, deskAtRef.current, viewRef.current?.id ?? null, gone);
      if (isDeskEntry(e.state)) deskAtRef.current = e.state.desk;
      if (!step) return;
      if ("skip" in step) history.go(step.skip);
      else openThread(step.open, "open", "auto");
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
    settle: (ids, bulk, unreadKept) =>
      bulk
        ? void settleBulk(ids, { unreadKept }).then((done) => done.length && leaveOpenThread(done))
        : void cmds.run("thread.settle", ids, undefined, ids.length === 1 && store.threads[ids[0]]?.running ? { label: () => "Settled; run stopped" } : undefined).then((n) => n && leaveOpenThread(ids)),
    settleWhenDone,
    archive: (ids) => void cmds.run("thread.archive", ids).then((n) => n && leaveOpenThread(ids)),
    markUnseen: (id) => markUnseen(id),
    create,
  };

  /** V0.119 phone: a Settings page is a screen; a project is its own screen; going to the level above goes Back. */
  const phoneSettingsGo = (r: PageRef | null) => {
    if (r?.page === "projects" && r.sub) return navGo({ screen: { name: "project", id: r.sub }, sheet: null });
    const screen: Nav["screen"] = r ? { name: "settings", page: r.page, ...(r.sub ? { sub: r.sub } : {}), ...(r.host ? { host: r.host } : {}) } : { name: "settings" };
    const up = backTarget(navRef.current);
    if (up && JSON.stringify(up.screen) === JSON.stringify(screen)) navBack();
    else navGo({ screen, sheet: null });
  };

  // V0.164: undeclared work's components find a thread's title and go to a thread or to Settings through this, on both fronts.
  const undNav: UndNav = {
    lookup: (id) => ({ title: store.threads[id]?.title || id, project: store.threads[id]?.project }),
    now: () => Date.now() + store.clockOffset,
    openThread: (id) => {
      if (!phoneRef.current) setDialog(null); // the desktop's Settings is a dialog over the list
      directory.openThread(id);
    },
    openSettings: (at) => (phoneRef.current ? phoneSettingsGo(at) : openSettings(at)),
  };

  if (phone) {
    return (
      <DirectoryContext.Provider value={directory}>
        <FolderPickContext.Provider value={folderPicker}>
        <UndContext.Provider value={undNav}>
<HostsContext.Provider value={hostsCtx}>
<HostMarksContext.Provider value={store.hostMarks}>
        <ConfirmProvider value={confirmHost.ask}>
        <ProfilesContext.Provider value={store.profiles}>
        <Boundary name="Fyr">
        <PreviewViewer />
        <PhoneApp
          nav={{ at: nav, go: navGo, replace: navReplace, back: navBack }}
          store={store}
          allProjects={fullStore.projects}
          inbox={{ sec, rows, guest, searchable, all: filtered, unlock }}
          now={now}
          commands={cmds}
          open={{
            thread: nav.screen.name === "thread" && !gate.gated ? store.threads[nav.screen.id] : undefined,
            gated: gatedThread,
            events: nav.screen.name === "thread" ? store.events[nav.screen.id] : undefined,
            onEarlier: view && earlier[view.id] ? () => loadEarlier(view.id) : undefined,
            onAtEnd: (b) => setTracker((tr) => setAtEnd(tr, b)),
            card,
            onCard: (fn) => decision && setCards((c) => (c[decision.id] ? { ...c, [decision.id]: fn(c[decision.id]) } : c)),
            submitAsk: async (d) => {
              const cs = cards[d.id];
              const answers = cs ? answersOf(cs, d) : null;
              if (!answers || !view) return false;
              return (await cmds.run("thread.answer", [view.id], { decisionId: d.id, answers })) > 0;
            },
            allow: async (d, ok) => (view ? (await cmds.run("thread.answer", [view.id], { decisionId: d.id, allow: ok })) > 0 : false),
          }}
          list={{ sectionOpen, toggleSection, nameSection, hideAt: hideShown, sectionUsed, expanded, onExpand: (k) => setExpanded((x) => ({ ...x, [k]: true })), picked, setPicked, selecting, setSelecting }}
          device={{
            push: pushSt,
            pushReason,
            ios: env.ios,
            standalone: env.standalone,
            canInstall: canInstall && !isStandalone(),
            onInstall: () => void promptInstall().then((ok) => ok && setCanInstall(false)),
          }}
          settings={(at, header) => settingsFor(at, phoneSettingsGo, header)}
          actions={phoneActions}
          filter={{ id: filterId, set: setProjectFilter }}
          filters={filterView}
          projectActions={projectActions}
          status={
            <>
              <StallBanner stalls={store.stalls} multiHost={store.hosts.length > 1} phone />
              <DeployBanner p={store.deploy} threads={store.threads} conn={store.conn} phone onRestartNow={() => void restartNow()} />
            </>
          }
          overlay={
            <>
              {folderPickView}
              {confirmHost.view}
            </>
          }
        />
        {nav.sheet?.kind === "adopt" && (
          <AdoptDialog
            load={(h) => api.t3Candidates(hostIdByName(store.hosts, h))}
            now={now}
            onClose={navBack}
            onAdopt={async (ids, h) => {
              const e = await adopt(ids, h);
              if (!e) navBack();
              return e;
            }}
          />
        )}
        </Boundary>
        </ProfilesContext.Provider>
        </ConfirmProvider>
        </HostMarksContext.Provider>
</HostsContext.Provider>
</UndContext.Provider>
        </FolderPickContext.Provider>
      </DirectoryContext.Provider>
    );
  }

  const activeSection: SectionKey | null = sel && store.threads[sel] ? sectionOf(store.threads[sel]) : null;
  const hints = keyHints({
    dialog,
    snoozeCount: dialog === "snooze" ? snoozeKeyCount(snoozeList()) : undefined,
    zone,
    hasSelection: !!sel,
    picked: picked.size,
    view: view?.mode ?? null,
    card: decision?.kind ?? null,
    cardMulti: !!(decision?.kind === "ask" && card && decision.questions?.[card.index]?.multiSelect),
    running: !!viewThread?.running,
    failed: (tgtThread(store, zone, view, sel)?.attention.state ?? "") === "failed",
    undo: !!toast?.undo && cmds.canUndo(),
    inText,
  });

  return (
    <div class="shell">
      <StallBanner stalls={store.stalls} multiHost={store.hosts.length > 1} />
      <DeployBanner p={store.deploy} threads={store.threads} conn={store.conn} onRestartNow={() => void restartNow()} />
      {ctxMenu}
      <PreviewViewer />
      <DirectoryContext.Provider value={directory}>
      <FolderPickContext.Provider value={folderPicker}>
        <UndContext.Provider value={undNav}>
<HostsContext.Provider value={hostsCtx}>
<HostMarksContext.Provider value={store.hostMarks}>
      <ConfirmProvider value={confirmHost.ask}>
      <ProfilesContext.Provider value={store.profiles}>
      <div class={`${view ? "app open" : "app"}${listOn ? "" : " nolist"}`}>
        <Rail sec={sec} active={activeSection} profiles={store.profiles} conn={store.conn} onJump={jump} extra={pwaControls} listOn={listOn} onToggleList={toggleList} onSettings={() => setDialog("settings")} onChanges={() => setDialog("changes")} changesNeeds={needsCount(store.threads)} />
        <Boundary name="The inbox">
        <Inbox
          sec={sec}
          rows={rows}
          guest={guest}
          now={now}
          selected={sel}
          picked={picked}
          open={sectionOpen}
          expanded={expanded}
          query={query}
          searchRef={searchRef}
          searchable={searchList}
          onQuery={setQuery}
          onToggle={toggleSection}
          autoHideAt={hideShown}
          onSectionActivity={sectionUsed}
          onClick={(id) => openThread(id, "open")}
          onBackground={closeThread}
          onExpand={(k) => setExpanded((x) => ({ ...x, [k]: true }))}
          onNew={() => setDialog("new")}
          onAdopt={() => setDialog("adopt")}
          filter={filterView}
          unlock={unlock}
          quietHits={quietHits}
          extra={pwaControls}
          onFocusZone={() => setZone("list")}
          // V0.184: desktop only. The phone shows the list full screen with no pane beside it, so it has no edge to drag.
          resizer={<ListResizer win={winW} />}
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
            onSubmitAsk={submitAsk}
            onRun={(a, args) => {
              if (a === "thread.settle") return settle(view.id);
              if (a === "thread.archive") return archive([view.id]);
              void cmds.run(a, [view.id], args);
            }}
            onSnooze={() => openSnooze([view.id])}
            onSettleWhenDone={() => settleWhenDone(view.id)}
            onMoveProject={() => openPicker([view.id])}
            onHandover={() => (setHandoverId(view.id), setDialog("handover"))}
            onRegenerateTitle={() => void cmds.regenerateTitle(view.id)}
            onSend={send}
            onAtEnd={(b) => setTracker((tr) => setAtEnd(tr, b))}
            onFocusZone={() => {
              setZone("thread");
              if (view.mode === "peek") openThread(view.id, "open");
            }}
          />
          </Boundary>
        ) : gatedThread ? (
          <section class="th empty-th">
            <QuietGate project={recordOf(store.projects, gatedThread.projectId)} now={Date.now() + store.clockOffset} unlock={unlock} what="thread" />
          </section>
        ) : store.loaded ? (
          // V0.status-home: no thread open: the status screen.
          <section class="th">
            <Boundary name="Status">
              <HomeScreen sec={sec} projects={store.projects} profiles={store.profiles} hosts={store.hosts} now={now} loaded={store.loaded} onOpen={(id) => openThread(id, "open")} onAllNeeds={() => jump("needs")} keys={HOME_KEYS} />
            </Boundary>
          </section>
        ) : (
          <section class="th empty-th">
            <div class="sys">{store.conn === "offline" ? "Fyr is not reachable. Retrying…" : "Connecting…"}</div>
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
        <Pop kind="toast" class={`toast${toast.error ? " err" : ""}`} role="status">
          <span class="ell">{toast.text}</span>
          {toast.undo && cmds.canUndo() && (
            <button class="btn sm" onClick={() => void cmds.doUndo()}>
              Undo <kbd>⌘Z</kbd>
            </button>
          )}
        </Pop>
      )}
      {dialog === "new" && <NewThreadDialog onAddProject={projectActions.add} profiles={store.profiles} hosts={store.hosts} projects={store.projects} projectFilter={filterId} clockOffset={store.clockOffset} onClose={() => setDialog(null)} onCreate={create} onNewProject={(name, folder, host) => projectActions.create(name, folder, [], host)} />}
      {dialog === "picker" && (
        <Modal title={pickIds.length > 1 ? `Project for ${pickIds.length} threads` : "Project"} onClose={() => setDialog(null)} wide>
          <div class="db">
            <ProjectPicker
              projects={store.projects}
              threads={pickIds.map((id) => store.threads[id]).filter(Boolean)}
              onPick={async (p) => {
                const e = await projectActions.move(pickIds, p?.id ?? null);
                if (!e) setDialog(null);
              }}
              onCreate={async (name, folder) => {
                const r = await projectActions.create(name, folder, pickIds);
                if (!r.error) setDialog(null);
                return r.error;
              }}
              onEdit={(p) => p.id && openProjectScreen(p.id)}
            />
          </div>
        </Modal>
      )}
      {dialog === "handover" && handoverId && store.threads[handoverId] && (
        <HandoverDialog t={store.threads[handoverId]} profiles={store.profiles} onClose={() => setDialog(null)} onConfirm={(pr, m) => cmds.handover(handoverId, pr, m)} />
      )}
      {dialog === "adopt" && <AdoptDialog load={(h) => api.t3Candidates(hostIdByName(store.hosts, h))} now={now} onClose={() => setDialog(null)} onAdopt={adopt} />}
      {dialog === "changes" && (
        <Modal title="Changes" wide onClose={() => setDialog(null)}>
          <div class="db">
            <ChangesBoard
              threads={store.threads}
              projects={store.projects}
              onRun={(id, a, args) => void cmds.run(a, [id], args)}
              visit={api.boardVisit ? () => api.boardVisit!() : undefined}
              onOpenThread={(id) => {
                setDialog(null);
                openThread(id, "open");
              }}
            />
          </div>
        </Modal>
      )}
      {dialog === "settings" && (
        <Modal title="Settings" wide cls="settings" onClose={() => setDialog(null)}>
          {settingsFor(settingsAt, (r) => setSettingsAt(r ?? { page: "providers" }))}
        </Modal>
      )}
      {dialog === "snooze" && <SnoozeMenu presets={snoozeList()} count={snoozeTargets.length} onPick={snooze} onClose={() => setDialog(null)} />}
      </ProfilesContext.Provider>
      {folderPickView}
      {confirmHost.view}
      </ConfirmProvider>
      </HostMarksContext.Provider>
</HostsContext.Provider>
</UndContext.Provider>
        </FolderPickContext.Provider>
      </DirectoryContext.Provider>
    </div>
  );
}

function tgtThread(s: Store, zone: Zone, view: { id: string } | null, sel: string | null): Thread | undefined {
  const id = zone === "thread" && view ? view.id : sel;
  return id ? s.threads[id] : undefined;
}

installPress();
render(
  <ApiContext.Provider value={hostApi}>
    <App />
  </ApiContext.Provider>,
  document.getElementById("app")!,
);
