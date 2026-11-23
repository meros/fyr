// The shell's state: the store, the filters, search and quiet hours, the sections and rows the list draws, the open
// thread, the dialogs. The first hook of the App in app.tsx; the others take what it returns.

import { storedGet, storedSet } from "./prefs.ts";
import { type Zone, type Dialog, readTheme, usePhone, useViewportVars, useStaticPage, params } from "./app-util.tsx";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Thread } from "./types.ts";
import { useApi } from "./api-context.ts";
import { useThreadCommands } from "./commands.ts";
import {
  SECTIONS,
  flatOrder,
  initialStore,
  matchesQuery,
  sections,
  UNDO_MS,
  type Arrival,
  type SectionKey,
  type Store,
  viewOf,
  profilesByHost,
  hostOfThread,
} from "./store.ts";
import { placeOf, steadyThreads, stillRows, trackArrivals, type FinishHold, type RowPlace } from "./still-list.ts";
import { useListAnchor } from "./still-list-ui.ts";
import { SECTION_OPEN_KEY, guestOf, hiddenWhy, markedRow, parseSectionOpen } from "./selection.ts";
import { initialTracker, type SeenTracker } from "./seen.ts";
import { type AnswerMove, arrivalOnOpen } from "./arrived.ts";
import { type CardState } from "./question.ts";
import { parsePage, type PageRef } from "./settings-nav.ts";
import { SECTION_LIMIT, isOpen, type FilterView, type Theme } from "./inbox.tsx";
import { useSlotQueue } from "./slot-queue-ui.tsx";
import { pushEnv } from "./pwa.ts";
import { unsupportedReason, type PushStatus } from "./push.ts";
import { FILTER_KEY, findProject } from "./projects.ts";
import { HIDDEN_KEY, applyHidden, hiddenCount, parseHidden, type Hidden } from "./hidden.ts";
import { HOSTS_OFF_KEY, activeFilters, applyFilters, clickHost, filterEmpty, hostKey, parseHostsOff, type FilterSel } from "./filters.ts";
import { admitThread, hideQuiet, nextQuietEnd, quietLocked, reachable } from "./quiet.ts";
import { initialEntries, type Nav } from "./nav.ts";
import { PHONE_UNDO_MS } from "./mobile.ts";
import { useQuietUnlock } from "./quiet-ui.tsx";
import { useToolDetails } from "./tool-calls-ui.tsx";
import { useConfirmHost } from "./confirm-ui.tsx";


export function useAppState() {
  // #41: every component reaches the host through ApiContext; a test can provide a fake Api.
  const api = useApi();
  const [rawStore, setStore] = useState<Store>(initialStore);
  // Slice 2b: the store keeps each host's state; the lists the screens draw are selectors over the hosts (store.ts viewOf).
  const fullStore = useMemo(() => viewOf(rawStore), [rawStore]);
  const rawRef = useRef(rawStore);
  rawRef.current = rawStore;
  /** The host whose state has the thread (hidden ones too: Settings and an undo act on them); none for one not kept. */
  const hostOfId = (id: string) => hostOfThread(rawRef.current, id);
  // V0.207: what this device hides is gone from `store`, so every list on both fronts follows. Settings and the cache
  // read `fullStore`.
  const [deviceHidden, setHiddenState] = useState<Hidden>(() => parseHidden(storedGet(HIDDEN_KEY)));
  const setHidden = (h: Hidden) => {
    setHiddenState(h);
    storedSet(HIDDEN_KEY, hiddenCount(h) ? JSON.stringify(h) : null);
  };
  const store = useMemo(() => applyHidden(fullStore, deviceHidden), [fullStore, deviceHidden]);
  const threadsRef = useRef(store.threads);
  threadsRef.current = store.threads;
  /** Slice 2b: each host's profiles by install id; a thread's or a project's are its own host's. */
  const byHost = profilesByHost(rawStore);
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
  const [sectionOpen, setSectionOpen] = useState<Partial<Record<SectionKey, boolean>>>(() => parseSectionOpen(storedGet(SECTION_OPEN_KEY)));
  useEffect(() => {
    storedSet(SECTION_OPEN_KEY, JSON.stringify(sectionOpen)); // blocked storage: the folds last until the reload
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
  // V0.68: Fyr's own confirmation, a sheet on the phone and a dialog on the desktop; here so every hook can ask.
  const confirmHost = useConfirmHost(phone);
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
    hostIdOf: (id) => hostOfId(id),
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
  const [projectFilter, setProjectFilterState] = useState<string | null>(() => storedGet(FILTER_KEY));
  const setProjectFilter = (id: string | null) => {
    setProjectFilterState(id);
    storedSet(FILTER_KEY, id || null);
  };
  // V0.169: the hosts turned off, remembered per device beside the project filter.
  const [hostsOff, setHostsOffState] = useState<string[]>(() => parseHostsOff(storedGet(HOSTS_OFF_KEY)));
  const setHostsOff = (keys: string[]) => {
    setHostsOffState(keys);
    storedSet(HOSTS_OFF_KEY, keys.length ? JSON.stringify(keys) : null);
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
    const gone = (key: string) => fullStore.hostList.some((h) => hostKey(h) === key) && !store.hostList.some((h) => hostKey(h) === key);
    const projectGone = !!findProject(fullStore.projects, projectFilter) && !filterOn;
    return { hostsOff: hostsOff.filter((n) => !gone(n)), project: projectGone ? null : projectFilter || null };
  }, [hostsOff, projectFilter, fullStore.hostList, fullStore.projects, store.hostList, filterOn]);
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
  // V0.207: `sec` is the truth, for every count; the list draws `rows`, j/k walks `walkRows` (V0.open-row-glides).
  // V0.stable-groups: when each thread came into its group; a new row goes last and nothing else moves one.
  const arrivalsRef = useRef<Record<string, Arrival>>({});
  const arrivals = useMemo(() => (arrivalsRef.current = trackArrivals(arrivalsRef.current, listThreads, Date.now() + store.clockOffset)), [listThreads]);
  const sec = useMemo(() => sections(shown, queue, arrivals), [shown, queue, arrivals]);
  // V0.134, V0.169: the two rows (only values with threads in view; a selected one stays) and the empty state, on both
  // fronts.
  const filterView = useMemo<FilterView>(() => {
    const chips = activeFilters({ threads: store.threads, projects: store.projects, hosts: store.hostList, sel: filterSel, open: sectionOpen, now: Date.now() + store.clockOffset, showUntil: unlock.showUntil });
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
  }, [store.threads, store.projects, store.hostList, filterSel, sectionOpen, unlock.showUntil, searchable, filterOn, quietClock, store.clockOffset]);
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
  // V0.arrived-from: after an answer moved the owner on, the next thread says where they came from until they leave it.
  const [arrival, setArrival] = useState<AnswerMove | null>(null);
  useEffect(() => setArrival((a) => arrivalOnOpen(a, wantId)), [wantId]);
  const arrivedHere = arrival && arrival.to === wantId ? arrival : null;
  // V0.192: the current thread punches through what hides it: the filters and quiet hours add its row in its own place
  // (`rows`), a folded section draws it alone (`guest`). `sec` stays what the filters let through, for every count.
  const current = sel ? listThreads[sel] : undefined;
  // V0.212: not on the phone. The list and the thread are never on screen together there, so the row marks nothing.
  const hidden = phone ? null : hiddenWhy(current, { shown, filtered, query, allowed: !!current && (gate.admitted === current.id || !!reachable({ [current.id]: current }, Date.now() + store.clockOffset, unlock.showUntil)[current.id]) });
  const listed = useMemo(() => (hidden && current ? { ...shown, [current.id]: current } : shown), [shown, hidden, current]);
  const rows = useMemo(() => (listed === shown ? sec : sections(listed, queue, arrivals)), [listed, shown, sec, queue, arrivals]);
  // V0.open-row-glides: every row, the open one too, is where the live list has it; the open row glides there
  // (`useListAnchor`). What stays is where the owner was: j/k, the next row after a settle and the step past a row that
  // leaves walk the list with the open row back in the place it had when it was opened (`stillRows`), until another
  // thread is current or the owner marks it seen or unseen by hand. V0.phone-row-moves: not on the phone, where the list
  // and the thread are never on screen together.
  const placeRef = useRef<RowPlace | null>(null);
  if (placeRef.current?.id !== sel) placeRef.current = placeOf(rows, sel);
  const place = phone || placeRef.current?.id === freed ? null : placeRef.current;
  // V0.phone-no-guest: not on the phone, which marks no row at all (V0.109 (5)): nothing is current there, so a folded
  // section draws no row for it either.
  const guest = useMemo(() => guestOf(rows, sel, hidden, sectionOpen, phone), [rows, sel, hidden, sectionOpen, phone]);
  const order = useMemo(() => flatOrder(sec, limits), [sec, limits]);
  useListAnchor(useMemo(() => flatOrder(rows, limits).join(" "), [rows, limits]), sel);
  const allRows = useMemo(() => flatOrder(rows), [rows]); // V0.186: with the rows not drawn, for the guest's place
  const walkRows = useMemo(() => (place ? flatOrder(stillRows(rows, place)) : allRows), [rows, place, allRows]);
  const viewThread = view && !gate.gated ? store.threads[view.id] : undefined;


  return { api, rawStore, setStore, fullStore, hostOfId, deviceHidden, setHidden, store, threadsRef, byHost, setLastOpen, view, setView, sel, zone, setZone, picked, setPicked, dialog, setDialog, snoozeTargets, setSnoozeTargets, handoverId, setHandoverId, tracker, setTracker, cards, setCards, sectionOpen, setSectionOpen, query, setQuery, help, setHelp, searchRef, expanded, setExpanded, theme, setTheme, tick, setTick, inText, setInText, toolDetailsOn, phone, phoneRef, reopenRef, navGoRef, cmds, toast, nav, setNav, selecting, setSelecting, pushSt, setPushSt, canInstall, setCanInstall, pendingOpen, setPendingOpen, env, pushReason, now, projectFilter, setProjectFilterState, setProjectFilter, setHostsOffState, settingsAt, setSettingsAt, openSettings, pickIds, openPicker, filterId, filterOn, unlock, filtered, searchable, searchList, quietHits, queue, sec, filterView, limits, gate, gatedRef, gatedThread, arrival, setArrival, arrivedHere, hidden, listed, rows, guest, order, walkRows, viewThread, confirmHost };
}
