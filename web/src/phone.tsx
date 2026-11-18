// The phone layout (760 px or less), from design/mobile (README and mobile.html).
// Screens: Inbox, Thread, New, Search; sheets slide up over a screen and are history entries.
// Desktop components are reused where the README says they carry over (the thread tab body, AgentsStrip,
// image paste); thread commands, undo and toasts are the desktop's too (#41);
// everything else here uses its own m- classes so the desktop CSS never applies.

import { haptic } from "./native.ts";
import { useApi } from "./api-context.ts";
import { useDraft } from "./drafts.ts";
import { Fragment, type ComponentChildren, type VNode } from "preact";
import { SlotQueueSection, useSlotQueue } from "./slot-queue-ui.tsx";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Decision, FyrEvent, Profile, Project, Thread } from "./types.ts";
import { LoadSheetBody, LoadStrip, useLoad } from "./load-ui.tsx";
import { stripHosts } from "./load.ts";
import { SECTIONS, isAvailable, isPinned, unpinned, matchesQuery, sections, selectAllIds, sweptUnread, unavailableReason, undoVerb, type SectionKey, type Sections, type Store } from "./store.ts";
import type { ThreadCommands } from "./commands.ts";
import { currentDecision } from "./decode.ts";
import { BrandMark, StateIcon, stateClass } from "./icons.tsx";
import { rowMeta, rowState, stateLabel } from "./rows.ts";
import { shortModel } from "./format.ts";
import { snoozeChoices, snoozeCommand, snoozedHint, type SnoozeChoice } from "./snooze.ts";
import { effortsFor, offLabel, startEffort } from "./providers.ts";
import { AgentsStrip, EffectBody, ThreadTabBody, sendFrom } from "./thread.tsx";
import { UndeclaredStrip } from "./undeclared-ui.tsx";
import { LeftoverStrip } from "./leftovers-ui.tsx";
import { describeEffect } from "./effects.ts";
import { ChipStrip, PhonePickers, useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { SlashList, useSlash } from "./slash-ui.tsx";
import { AiSearchPanel, useAiSearch } from "./ai-search-ui.tsx";
import { ThreadOffers } from "./offers-ui.tsx";
import { changeChip, headerPr } from "./offers.ts";
import { SETTLE_DONE_HINT, seenToggle, settleDoneEntry } from "./menu.ts";
import { prChip } from "./pr.ts";
import { ChangesBoard } from "./board-ui.tsx";
import { needsCount } from "./board.ts";
import { TAB_LABEL, defaultTab, threadTabs } from "./view.ts";
import { HandoverForm } from "./handover-ui.tsx";
import { NewProjectStep, ProjectEditor, linkingFor, ProjectIcon, ProjectList, QuietGate, QuietHold, ProjectPicker, ProjectTag, StartedBy, type ProjectPatch, type QuietUnlock } from "./project-ui.tsx";
import { colorVar, findProject, projectName, projectOf, recordOf } from "./projects.ts";
import { folderHint, type NewThreadArgs } from "./new-thread.ts";
import { useNewThreadForm } from "./new-thread-form.ts";
import { setToolDetails, toolDetails } from "./prefs.ts";
import { answerOf, goTo, pickStay, chooseOther, questionsOf, setOther, type CardState } from "./question.ts";
import { ADVANCE_MS, activeSection, allowCommit, askCommit, stepLabel, stripCount, type Choice } from "./mobile.ts";
import { LONG_PRESS_MS, idle, longPressCancelled, settleLabel, swipeAvail, swipeEnd, swipeMove, swipeOffset, swipeStart, type Swipe, type SwipeAvail } from "./gesture.ts";
import type { Nav, Sheet } from "./nav.ts";
import { hostsNeed, settingsNeed, type PageRef } from "./settings-nav.ts";
import { useProvidersNeed } from "./settings-ui.tsx";
import { HostField } from "./host-pick-ui.tsx";
import { SavedMark, SetContext, useSaved } from "./settings-rows.tsx";
import { AgentStatus } from "./agent-status.tsx";
import { FilterEmpty, FilterRow, SECTION_LIMIT, isOpen, type FilterView } from "./inbox.tsx";
import { HeldUpdates, HideClock, NeedsGroupHead, NeedsRowBody, OneRowBody, PHONE_LOOK, PIN_ICON, SectionTitle, hideUse, sectionHead } from "./row-ui.tsx";
import { needsHeads } from "./rows.ts";
import { AUTO_HIDE_SECTIONS, type HideAt } from "./inbox-autohide.ts";
import { GUEST_TEXT, drawnRows, type Guest } from "./selection.ts";
import { PwaControls } from "./pwa-ui.tsx";
import type { PushStatus } from "./push.ts";
import { QuotaList } from "./quota-ui.tsx";
import { HostMark } from "./host-mark-ui.tsx";
import { hostLabel, hostOf, hostTag, offlineLine, profilesOfHost, threadProvider, type HostInfo } from "./hosts.ts";

// ---- icons ----
const I = {
  back: (
    <svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M15 5l-7 7 7 7" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  ),
  more: (
    <svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="5" cy="12" r="1.8" fill="currentColor" />
      <circle cx="12" cy="12" r="1.8" fill="currentColor" />
      <circle cx="19" cy="12" r="1.8" fill="currentColor" />
    </svg>
  ),
  close: (
    <svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M6 6l12 12M18 6L6 18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
    </svg>
  ),
  inbox: (
    <svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M3.5 13.5 6 5h12l2.5 8.5V19H3.5Z M3.5 13.5h5l1.5 2.5h4l1.5-2.5h5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" />
    </svg>
  ),
  search: (
    <svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="10.5" cy="10.5" r="6" fill="none" stroke="currentColor" stroke-width="1.8" />
      <path d="M15 15l5 5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </svg>
  ),
  plus: (
    <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
    </svg>
  ),
  send: (
    <svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M12 19V6M6 11l6-6 6 6" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  ),
  stop: (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
      <rect x="3" y="3" width="10" height="10" rx="2" fill="currentColor" />
    </svg>
  ),
  paste: (
    <svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true">
      <rect x="5" y="4.5" width="14" height="16" rx="2.5" fill="none" stroke="currentColor" stroke-width="1.7" />
      <rect x="9" y="2.8" width="6" height="3.6" rx="1.2" fill="none" stroke="currentColor" stroke-width="1.7" />
    </svg>
  ),
  photos: (
    <svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true">
      <rect x="3.5" y="5" width="17" height="14" rx="2.5" fill="none" stroke="currentColor" stroke-width="1.7" />
      <path d="M3.5 16l5-4.5 4 3.5 3-2.5 4.5 3.5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" />
      <circle cx="15.5" cy="9.5" r="1.5" fill="currentColor" />
    </svg>
  ),
  down: (
    <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M6 9l6 6 6-6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  ),
  up: (
    <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M6 15l6-6 6 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  ),
  right: (
    <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M9 6l6 6-6 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  ),
  undo: (
    <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M9 7 4.5 11.5 9 16M5 11.5h9a5 5 0 0 1 0 10h-2" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  ),
};

// ---- props from the app shell ----

/** The phone's place (design/mobile: screens and sheets are history entries) and how to move. */
export interface PhoneNav {
  at: Nav;
  go(n: Nav): void;
  replace(n: Nav): void;
  back(): void;
}

/** The thread on screen: its timeline, the question card's state, and the answers (they need the card). */
export interface PhoneOpen {
  thread: Thread | undefined;
  /** V0.114: the quiet thread the screen asked for, held behind the gate until the 30 s hold */
  gated?: Thread;
  events: FyrEvent[] | undefined;
  onEarlier?: () => void;
  onAtEnd(atEnd: boolean): void;
  card: CardState | null;
  onCard(fn: (cs: CardState) => CardState): void;
  submitAsk(d: Decision): Promise<boolean>;
  allow(d: Decision, ok: boolean): Promise<boolean>;
}

/** The inbox list's own state: open and expanded sections, selection mode and the picked rows. */
export interface PhoneList {
  sectionOpen: Partial<Record<SectionKey, boolean>>;
  /** A header's tap. */
  toggleSection(k: SectionKey): void;
  /** V0.186: the section named in the strip: it opens and stays open. */
  nameSection(k: SectionKey): void;
  /** V0.196: the sections that close by themselves, with their deadline (ms) */
  hideAt: HideAt;
  sectionUsed(k: SectionKey): void;
  expanded: Partial<Record<SectionKey, boolean>>;
  onExpand(k: SectionKey): void;
  picked: Set<string>;
  setPicked(s: Set<string>): void;
  selecting: boolean;
  setSelecting(b: boolean): void;
}

/** This device: push and install (V0.119: the theme is in Settings → This device). */
export interface PhoneDevice {
  push: PushStatus;
  pushReason: string | null;
  ios: boolean;
  standalone: boolean;
  canInstall: boolean;
  onInstall(): void;
}

/** What the phone does differently from a plain thread command: U8 leaves the open thread after a settle or
 *  archive, Mark unseen restarts the seen timer, and New starts a thread. Every other command is `commands.run`. */
export interface PhoneActions {
  /** `unreadKept`: the unread threads a Select all left out, for the toast (V0.189) */
  settle(ids: string[], bulk?: boolean, unreadKept?: number): void;
  /** V0.203: arms "settle when done", or cancels it when armed */
  settleWhenDone(id: string): void;
  archive(ids: string[]): void;
  markUnseen(id: string): void;
  create(a: NewThreadArgs): Promise<string | null>;
}

export interface PhoneProps {
  nav: PhoneNav;
  /** threads, projects, profiles, hosts and the connection */
  store: Store;
  /** the inbox's sections; what search finds: the project filter's threads, quiet ones only while the unlock runs
   *  (V0.114); all: the same with quiet ones, to count the hits the lock hides; the one unlock of quiet projects;
   *  V0.192: rows is what the list draws (sec, with the current thread when the filters hide it), guest that row */
  inbox: { sec: Sections; rows: Sections; guest: Guest | null; /** V0.your-move */ updates: number; showUpdates(): void; searchable: Record<string, Thread>; all: Record<string, Thread>; unlock: QuietUnlock };
  now: number;
  /** #41: the same thread commands, undo and toasts as the desktop */
  commands: ThreadCommands;
  open: PhoneOpen;
  list: PhoneList;
  device: PhoneDevice;
  /** V0.207: every project, also those this device hides (`store` leaves them out) */
  allProjects: Project[];
  /** the project filter (V0.23), for the menu's Filter by project sheet */
  filter: { id: string | null; set(id: string | null): void };
  /** V0.134: the host and project chips and the empty state, as the desktop shows them */
  filters: FilterView;
  projectActions: {
    update(id: string, patch: ProjectPatch): Promise<string | null>;
    setFolder(id: string, path: string): Promise<string | null>;
    /** V0.132: on `host`, else the threads' host */
    create(name: string, folder: string, threadIds?: string[], host?: string): Promise<{ error: string | null; id?: string }>;
    /** null: No project */
    move(ids: string[], projectId: string | null): Promise<string | null>;
    /** V0.58 */
    delete(id: string): Promise<string | null>;
    /** Decision 0034 */
    link(id: string, other: string): Promise<string | null>;
    unlink(id: string): Promise<string | null>;
    add(id: string, host: string, folder: string): Promise<{ error: string | null; id?: string }>;
  };
  actions: PhoneActions;
  /** V0.119: a Settings screen (the app builds it with its Api): the nav (null) or a page, under this header */
  settings?(at: PageRef | null, header: (title: ComponentChildren, end?: ComponentChildren) => ComponentChildren): ComponentChildren;
  /** V0.42: the folder picker sheet, over everything */
  overlay?: ComponentChildren;
  /** V0.118: the update status, the first line of the screen below the safe area, in the layout (never over a control) */
  status?: ComponentChildren;
}

/** The host's name in the header (the mock says so). */
function useHostName(p: PhoneProps): string {
  return hostLabel(p.store.host, useApi().mock);
}

const STORAGE_IOS = "fyr.iosRowDismissed";

function store(key: string, value: string | null) {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    /* storage blocked */
  }
}
function load(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

// ---- shell ----

export function PhoneApp(p: PhoneProps) {
  const s = p.nav.at.screen;
  const sheetThread = p.nav.at.sheet?.ids?.length === 1 ? p.store.threads[p.nav.at.sheet.ids[0]] : undefined;
  const bottomBar = !p.list.selecting && (s.name === "inbox" || s.name === "search");
  const api = useApi(); // V0.98 (3): the board visit
  return (
    <div class={`m-app${bottomBar ? " has-bar" : ""}`}>
      {p.status}
      {s.name === "inbox" && <MInbox {...p} />}
      {s.name === "search" && <MSearch {...p} />}
      {s.name === "thread" && p.open.thread && <MThread {...p} t={p.open.thread} tab={defaultTab(p.open.thread, s.tab)} />}
      {s.name === "thread" && !p.open.thread && p.open.gated && (
        <div class="m-screen">
          <MHeader onBack={p.nav.back} title="Quiet hours" />
          <QuietGate project={recordOf(p.store.projects, p.open.gated!.projectId)} now={p.now} unlock={p.inbox.unlock} what="thread" />
        </div>
      )}
      {s.name === "thread" && !p.open.thread && !p.open.gated && (
        <div class="m-screen">
          <MHeader onBack={p.nav.back} title="Thread" />
          <div class="m-empty">{Object.keys(p.store.threads).length ? "This thread is not here anymore." : "Loading…"}</div>
        </div>
      )}
      {s.name === "new" && <MNew {...p} />}
      {s.name === "project" && <MProject {...p} id={s.id} />}
      {s.name === "changes" && (
        <div class="m-screen m-changes">
          <MHeader onBack={p.nav.back} title="Changes" />
          <div class="m-nbody">
            <ChangesBoard phone threads={p.store.threads} projects={p.store.projects} visit={api.boardVisit ? () => api.boardVisit!() : undefined} onRun={(id, a, args) => void p.commands.run(a, [id], args)} onOpenThread={(id) => p.nav.go({ screen: { name: "thread", id }, sheet: null })} />
          </div>
        </div>
      )}
      {s.name === "settings" && <div class="m-screen m-settings">{p.settings?.(s.page ? { page: s.page, ...(s.sub ? { sub: s.sub } : {}), ...(s.host ? { host: s.host } : {}) } : null, (title, end) => <MHeader onBack={p.nav.back} title={title} right={end} />)}</div>}
      {bottomBar && <MBottomBar {...p} />}
      <MToast {...p} bottomBar={bottomBar} />
      {p.nav.at.sheet && <MSheets {...p} sheet={p.nav.at.sheet} t={sheetThread} />}
      {p.overlay}
    </div>
  );
}

function MHeader({ onBack, title, right, close }: { onBack(): void; title: ComponentChildren; right?: ComponentChildren; close?: boolean }) {
  return (
    <div class="m-nh">
      <button class="b44" onClick={onBack} aria-label={close ? "Close" : "Back"}>
        {close ? I.close : I.back}
      </button>
      <div class="m-nt ell">{title}</div>
      <span class="sp" />
      {right}
    </div>
  );
}

function MToast(p: PhoneProps & { bottomBar: boolean }) {
  const { toast, undo } = p.commands;
  if (!toast) return null;
  let text = toast.text;
  if (toast.undo && undo && undo.threadIds.length === 1) {
    const verb = undoVerb(undo.kind);
    const title = p.store.threads[undo.threadIds[0]]?.title;
    if (title) text = `${verb} “${title}”`;
  }
  return (
    <div class={`m-toast${toast.error ? " err" : ""}${p.bottomBar ? " over-bar" : ""}`} role="status">
      <span class="ell">{text}</span>
      {toast.undo && p.commands.canUndo() && (
        <button class="tb" onClick={() => void p.commands.doUndo()}>
          {I.undo}
          Undo
        </button>
      )}
    </div>
  );
}

function MBottomBar(p: PhoneProps) {
  const n = p.inbox.sec.needs.length;
  const s = p.nav.at.screen.name;
  return (
    <nav class="m-bn" aria-label="Main">
      <button class={`m-bi${s === "inbox" ? " on" : ""}`} onClick={() => (s === "inbox" ? document.querySelector(".m-list")?.scrollTo({ top: 0 }) : p.nav.replace({ screen: { name: "inbox" }, sheet: null }))}>
        {I.inbox}
        {n > 0 && <i class="m-bdg">{stripCount(n)}</i>}
        Inbox
      </button>
      <button class="m-bnew" onClick={() => p.nav.go({ screen: { name: "new" }, sheet: null })}>
        {I.plus}
        New
      </button>
      <button class={`m-bi${s === "search" ? " on" : ""}`} onClick={() => s !== "search" && p.nav.go({ screen: { name: "search" }, sheet: null })}>
        {I.search}
        Search
      </button>
    </nav>
  );
}

// ---- inbox ----

function MInbox(p: PhoneProps) {
  const queue = useSlotQueue();
  const hostName = useHostName(p);
  const listRef = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState<SectionKey | null>("needs");
  const [iosDismissed, setIosDismissed] = useState(() => load(STORAGE_IOS) === "1");
  const showIos = p.device.ios && !p.device.standalone && !iosDismissed;

  const onScroll = () => {
    const el = listRef.current;
    if (!el) return;
    const tops = SECTIONS.filter((s) => document.getElementById(`m-sec-${s.key}`)).map((s) => ({ key: s.key, top: document.getElementById(`m-sec-${s.key}`)!.offsetTop - el.offsetTop }));
    setActive(activeSection(tops, el.scrollTop));
    // V0.196: a touch screen has no pointing, and a drag turns into a scroll: scrolling a closing section is using it.
    const box = el.getBoundingClientRect();
    for (const k of AUTO_HIDE_SECTIONS) {
      if (p.list.hideAt[k] === undefined) continue;
      const r = document.getElementById(`m-sec-${k}`)?.getBoundingClientRect();
      if (r && r.top < box.bottom && r.bottom > box.top) p.list.sectionUsed(k);
    }
  };

  const jump = (k: SectionKey) => {
    p.list.nameSection(k);
    requestAnimationFrame(() => {
      const el = listRef.current;
      const h = document.getElementById(`m-sec-${k}`);
      if (el && h) el.scrollTo({ top: h.offsetTop - el.offsetTop, behavior: "smooth" });
      setActive(k);
    });
  };

  const all = SECTIONS.flatMap((s) => p.inbox.sec[s.key]);
  // V0.189: Select all is a sweep, so it leaves unread rows out and the settle says how many.
  const [swept, setSwept] = useState(false);
  useEffect(() => {
    if (!p.list.selecting) setSwept(false);
  }, [p.list.selecting]);
  const unreadLeft = swept ? sweptUnread(all, p.list.picked) : 0;
  const strip = stripHosts(useLoad(), p.store.hosts);
  // V0.162: the button shows while a merged thread can settle; the bulk settle leaves pinned ones out and counts them.
  const merged = p.inbox.sec.seen.filter((t) => t.pr?.state === "MERGED" && (isPinned(t) || isAvailable(t, "thread.settle")));
  const canSettleMerged = merged.some((t) => !isPinned(t));

  return (
    <div class="m-screen">
      {p.list.selecting ? (
        <div class="m-ih sel">
          <button class="b44" onClick={() => (p.list.setSelecting(false), p.list.setPicked(new Set()))} aria-label="Cancel selection">
            {I.close}
          </button>
          <span class="m-nt">{p.list.picked.size} selected</span>
          <span class="sp" />
          <button class="m-tbtn" onClick={() => (p.list.setPicked(new Set(selectAllIds(all))), setSwept(true))}>
            Select all
          </button>
        </div>
      ) : (
        <div class="m-ih">
          {/* V0.103, V0.113: the rail's logo (V0.93) alone, as on the desktop rail; its alt is the name. */}
          <span class="m-brand">
            <BrandMark size={28} />
          </span>
          {/* V0.166: with several hosts the load strip names each one, so the connection keeps only its dot (the name on
              hover and for the screen reader): a 360 px header has no room for both. */}
          <span class={`m-conn ${p.store.conn}${strip.length > 1 ? " dot" : ""}`} title={`${hostName}: ${p.store.conn}`}>
            <i />
            <span class="ell">{hostName}</span>
          </span>
          <span class="sp" />
          {/* V0.166: each host's load and heavy jobs, as on the desktop rail; a tap opens the details */}
          <LoadStrip hosts={strip} onOpen={() => p.nav.go({ ...p.nav.at, sheet: { kind: "load" } })} />
          <PwaControls
            push={p.device.push}
            pushReason={p.device.pushReason}
            showUnsupported={false}
            canInstall={p.device.canInstall}
            onBell={() => p.nav.go({ screen: { name: "settings", page: "device" }, sheet: null })}
            onInstall={p.device.onInstall}
          />
          <button class="b44" onClick={() => p.nav.go({ ...p.nav.at, sheet: { kind: "menu" } })} aria-label="More">
            {I.more}
          </button>
        </div>
      )}
      {!p.list.selecting && (
        <div class="m-strip" role="tablist" aria-label="Sections">
          {SECTIONS.map((s) => {
            const n = p.inbox.sec[s.key].length;
            if (s.transient && n === 0) return null; // V0.137: Waiting for a slot only while threads wait
            return (
              <button class={`m-seg${active === s.key ? " on" : ""}${s.key === "needs" && n > 0 ? " att" : ""}`} onClick={() => jump(s.key)} title={`${s.label}: ${n}`}>
                <span class="c">
                  <StateIcon state={s.state} size={14} />
                  <span class="n">{stripCount(n)}</span>
                </span>
                <span class="l">{s.short}</span>
              </button>
            );
          })}
        </div>
      )}
      {!p.list.selecting && <FilterRow f={p.filters} now={p.now} unlock={p.inbox.unlock} />}
      {p.store.conn === "offline" && (
        <div class="m-cstrip">
          <span class="ell">Reconnecting to {hostName}…</span>
        </div>
      )}
      {showIos && !p.list.selecting && (
        <div class="m-iosrow">
          <span class="ell">Add Fyr to the Home Screen to get notifications.</span>
          <button class="m-tbtn" onClick={() => p.nav.go({ screen: { name: "settings", page: "device" }, sheet: null })}>
            How
          </button>
          <button class="b44" aria-label="Dismiss" onClick={() => (store(STORAGE_IOS, "1"), setIosDismissed(true))}>
            {I.close}
          </button>
        </div>
      )}
      <div class="m-list" ref={listRef} onScroll={onScroll}>
        <HeldUpdates n={p.inbox.updates} show={p.inbox.showUpdates} />
        {!p.list.selecting && <FilterEmpty f={p.filters} cls="m-fempty" />}
        {p.inbox.rows.pinned.length > 0 && (
          // V0.104: the desktop's pinned block, above every section and always open.
          <section id="m-sec-pinned">
            <div class="m-sec">
              <SectionTitle label="Pinned" icon={PIN_ICON} n={p.inbox.sec.pinned.length} shown={stripCount(p.inbox.sec.pinned.length)} />
            </div>
            {p.inbox.rows.pinned.map((t) => (
              <MRow key={t.id} t={t} p={p} needs={t.attention.section === "needs"} />
            ))}
          </section>
        )}
        {SECTIONS.map((s) => {
          if (s.key === "slots")
            return (
              <SlotQueueSection
                phone
                queue={queue}
                threads={p.inbox.rows.slots}
                n={p.inbox.sec.slots.length}
                row={(t, a) => (
                  <div key={t.id} ref={a.attrs.ref} class={a.attrs.class || undefined} data-id={t.id}>
                    <MRow t={t} p={p} needs={false} lead={a.lead} />
                  </div>
                )}
              />
            );
          const list = unpinned(p.inbox.sec[s.key]);
          const rows = unpinned(p.inbox.rows[s.key]); // V0.192: `list` counts, `rows` draws
          const open = isOpen(s.key, p.list.sectionOpen) && rows.length > 0;
          const limit = p.list.expanded[s.key] ? rows.length : SECTION_LIMIT[s.key];
          const shown = drawnRows(rows, open, limit, p.inbox.guest);
          const hint = s.key === "working" ? workingHint(list) : s.key === "snoozed" ? snoozedHint(list, p.now) : s.key === "settled" ? "last 24 h" : "";
          const heads = s.key === "needs" ? needsHeads(shown) : {};
          const hideAt = open ? p.list.hideAt[s.key] : undefined;
          return (
            <section id={`m-sec-${s.key}`} {...hideUse(hideAt !== undefined, () => p.list.sectionUsed(s.key))}>
              <div
                class={`m-sec${s.key === "seen" && canSettleMerged && !p.list.selecting ? " hasbtn" : ""}`}
                {...sectionHead({ n: list.length, open, toggle: () => p.list.toggleSection(s.key) })}
              >
                <SectionTitle label={s.label} icon={<StateIcon state={s.state} size={16} />} n={list.length} shown={stripCount(list.length)} open={open} attention={s.key === "needs"} />
                {hideAt !== undefined && <HideClock at={hideAt} />}
                {s.key === "seen" && canSettleMerged && !p.list.selecting ? (
                  <button
                    class="m-sbtn"
                    onClick={(e) => {
                      e.stopPropagation();
                      p.actions.settle(
                        merged.map((t) => t.id),
                        true,
                      );
                    }}
                  >
                    Settle merged
                  </button>
                ) : (
                  hint && <span class="h ell">{hint}</span>
                )}
              </div>
              {shown.map((t) => (
                <Fragment key={t.id}>
                  {heads[t.id] && <NeedsGroupHead label={heads[t.id]} />}
                  <MRow t={t} p={p} needs={s.key === "needs"} />
                </Fragment>
              ))}
              {open && rows.length > shown.length && (
                <button class="m-more" onClick={() => p.list.onExpand(s.key)}>
                  Show all {rows.length.toLocaleString()}
                </button>
              )}
            </section>
          );
        })}
      </div>
      {p.list.selecting && (
        <div class="m-selbar">
          <button class="m-bt p" disabled={!p.list.picked.size && !unreadLeft} onClick={() => (p.actions.settle([...p.list.picked], true, unreadLeft), p.list.setSelecting(false), p.list.setPicked(new Set()))}>
            Settle {p.list.picked.size || ""}
          </button>
          <button class="m-bt" disabled={!p.list.picked.size} onClick={() => p.nav.go({ ...p.nav.at, sheet: { kind: "snooze", ids: [...p.list.picked] } })}>
            Snooze…
          </button>
          <button class="b44" disabled={!p.list.picked.size} aria-label="More actions" onClick={() => p.nav.go({ ...p.nav.at, sheet: { kind: "actions", ids: [...p.list.picked] } })}>
            {I.more}
          </button>
        </div>
      )}
    </div>
  );
}

function workingHint(list: Thread[]): string {
  const waiting = list.filter((t) => t.attention.state === "waiting").length;
  return waiting ? `${waiting} waiting` : "";
}

/** V0.54, V0.170: an offline host says so on a row, in words (it is state). Which host a row is on is its mark at the
 *  title's start (row-ui.tsx), as on the desktop, so the words for an online host are gone. */
function MHost({ t }: { t: Thread }) {
  return t.hostOffline ? <><b class="m-off">{t.host || "host"} offline</b> · </> : null;
}

/** A row with swipes (right settles, left snoozes), long press (actions) and selection. */
function MRow({ t, p, needs, lead: own }: { t: Thread; p: PhoneProps; needs: boolean; /** V0.137: the queue's "#2" */ lead?: VNode }) {
  const avail: SwipeAvail = useMemo(() => swipeAvail(t), [t]);
  const [sw, setSw] = useState<Swipe>(idle);
  const swRef = useRef<Swipe>(idle);
  const [hint, setHint] = useState<string | null>(null);
  const rowRef = useRef<HTMLDivElement>(null);
  const lp = useRef<{ timer: ReturnType<typeof setTimeout> | null; x: number; y: number; fired: boolean }>({ timer: null, x: 0, y: 0, fired: false });
  const suppressClick = useRef(false);

  const set = (s: Swipe) => {
    swRef.current = s;
    setSw(s);
  };
  const clearLp = () => {
    if (lp.current.timer) clearTimeout(lp.current.timer);
    lp.current.timer = null;
  };
  useEffect(() => () => clearLp(), []);

  const onDown = (e: PointerEvent) => {
    if (e.pointerType === "mouse" && e.button !== 0) return;
    set(p.list.selecting ? idle : swipeStart(e.clientX, e.clientY));
    lp.current = { timer: null, x: e.clientX, y: e.clientY, fired: false };
    if (!p.list.selecting) {
      lp.current.timer = setTimeout(() => {
        lp.current.fired = true;
        suppressClick.current = true;
        set(idle);
        if (navigator.vibrate) navigator.vibrate(8);
        p.nav.go({ ...p.nav.at, sheet: { kind: "actions", ids: [t.id] } });
      }, LONG_PRESS_MS);
    }
  };
  const onMove = (e: PointerEvent) => {
    if (lp.current.timer && longPressCancelled(lp.current.x, lp.current.y, e.clientX, e.clientY)) clearLp();
    const cur = swRef.current;
    const next = swipeMove(cur, e.clientX, e.clientY);
    if (next.phase === "swipe" && cur.phase !== "swipe") {
      clearLp();
      try {
        rowRef.current?.setPointerCapture(e.pointerId);
      } catch {
        /* capture unsupported */
      }
    }
    if (next !== cur) set(next);
  };
  const onUp = () => {
    clearLp();
    const cur = swRef.current;
    if (cur.phase === "swipe") {
      suppressClick.current = true;
      const r = swipeEnd(cur, rowRef.current?.offsetWidth ?? 390, avail);
      if (r.kind === "settle") (haptic("firm"), p.actions.settle([t.id]));
      else if (r.kind === "snooze") (haptic(), p.nav.go({ ...p.nav.at, sheet: { kind: "snooze", ids: [t.id] } }));
      else if (r.reason) {
        setHint(r.reason);
        setTimeout(() => setHint(null), 1400);
      }
    }
    set(idle);
  };
  const onClick = () => {
    if (suppressClick.current) {
      suppressClick.current = false;
      return;
    }
    if (p.list.selecting) {
      const n = new Set(p.list.picked);
      if (n.has(t.id)) n.delete(t.id);
      else n.add(t.id);
      p.list.setPicked(n);
      return;
    }
    p.nav.go({ screen: { name: "thread", id: t.id }, sheet: null });
  };

  const off = swipeOffset(sw, avail);
  const side = off > 0 ? avail.right : off < 0 ? avail.left : null;
  const st = t.attention.state;
  const picked = p.list.picked.has(t.id);
  const lead = p.list.selecting ? <i class={`m-ck${picked ? " on" : ""}`} /> : own;
  const pre = <MHost t={t} />;
  // V0.109: no row is marked here. The phone shows the inbox or one thread, never both, so there is no open thread
  // beside the list for a mark to follow; the desktop marks the open thread's row (app.tsx, selection.ts).
  const guest = p.inbox.guest?.id === t.id ? p.inbox.guest.why : undefined; // V0.192: as on the desktop
  const body = needs ? <NeedsRowBody t={t} now={p.now} look={PHONE_LOOK} lead={lead} pre={pre} guest={guest} /> : <OneRowBody t={t} now={p.now} look={PHONE_LOOK} lead={lead} pre={pre} guest={guest} />;
  return (
    <div class={`m-swp${picked ? " picked" : ""}${sw.phase === "swipe" ? " moving" : ""}`}>
      {side && (
        <div class={`m-under ${off > 0 ? "l" : "r"} ${!side.ok ? "no" : off > 0 ? "set" : "snz"}`}>
          {side.ok && off > 0 && <StateIcon state="settled" size={16} />}
          <span class="ell">{side.label}</span>
          {side.ok && off < 0 && <StateIcon state="snoozed" size={16} />}
        </div>
      )}
      {hint && <div class="m-under l no show">{hint}</div>}
      <div
        ref={rowRef}
        data-id={t.id}
        class="m-mv"
        style={off ? { transform: `translateX(${off}px)` } : undefined}
        onPointerDown={onDown}
        onPointerMove={onMove}
        onPointerUp={onUp}
        onPointerCancel={() => (clearLp(), set(idle))}
        onClick={onClick}
        onContextMenu={(e) => e.preventDefault()}
        role="button"
        aria-label={`${t.title}, ${stateLabel(st)}${guest ? `, ${GUEST_TEXT[guest].tag}` : ""}`}
      >
        {body}
      </div>
    </div>
  );
}

// ---- search ----

function MSearch(p: PhoneProps) {
  const [q, setQ] = useState("");
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => ref.current?.focus(), []);
  // V0.46: search finds the threads of a project in its quiet hours too; V0.114: only while the unlock runs.
  const all = useMemo(() => {
    const sec = sections(p.inbox.searchable);
    return SECTIONS.flatMap((s) => sec[s.key]);
  }, [p.inbox.searchable]);
  const needle = q.trim().toLowerCase();
  const hits = needle ? all.filter((t) => matchesQuery(t, needle)).slice(0, 200) : [];
  const quietHits = needle ? Object.values(p.inbox.all).filter((t) => !p.inbox.searchable[t.id] && matchesQuery(t, needle)).length : 0;
  const ai = useAiSearch(q, all);
  return (
    <div class="m-screen">
      <div class="m-ih m-sh">
        <form
          class="m-sfield"
          role="search"
          onSubmit={(e) => {
            e.preventDefault();
            ref.current?.blur(); // the keyboard's Search key: the answer needs the room the keyboard takes
            ai.run();
          }}
        >
          {I.search}
          <input ref={ref} type="search" enterKeyHint="search" value={q} placeholder="Search threads" aria-label="Search threads" onInput={(e) => setQ((e.target as HTMLInputElement).value)} />
          {q && (
            <button
              type="button"
              class="m-sclear"
              aria-label="Clear the search"
              onClick={() => {
                setQ("");
                ref.current?.focus();
              }}
            >
              {I.close}
            </button>
          )}
        </form>
      </div>
      <div class="m-list">
        {quietHits > 0 && <QuietGate now={p.now} unlock={p.inbox.unlock} what="search" />}
        <AiSearchPanel ai={ai} onOpen={(id) => p.nav.go({ screen: { name: "thread", id }, sheet: null })} />
        {needle && hits.length === 0 && !quietHits && <div class="m-empty">No title or project has “{q.trim()}”.</div>}
        {!needle && <div class="m-empty">Find a thread by its title, project or PR number, or describe it and ask AI.</div>}
        {hits.map((t) => (
          <MRow key={t.id} t={t} p={p} needs={false} />
        ))}
      </div>
    </div>
  );
}

// ---- thread ----

function MThread(p: PhoneProps & { t: Thread; tab: string }) {
  const t = p.t;
  const hostName = useHostName(p);
  const decision = currentDecision(t);
  const [collapsed, setCollapsed] = useState(false);
  useEffect(() => setCollapsed(false), [decision?.id]);
  const cardOpen = !!decision && !collapsed;
  const st = t.attention.state;
  const setTab = (tab: string) => p.nav.replace({ screen: { name: "thread", id: t.id, tab }, sheet: null });
  const [nextOffer, setNextOffer] = useState<Thread | null>(null);
  useEffect(() => {
    if (!nextOffer) return;
    const id = setTimeout(() => setNextOffer(null), 5000);
    return () => clearTimeout(id);
  }, [nextOffer]);

  const afterAnswer = (ok: boolean) => {
    if (!ok) return;
    const next = p.inbox.sec.needs.find((x) => x.id !== t.id);
    if (next) setNextOffer(next);
  };

  return (
    <div class={`m-screen m-thread${cardOpen ? " carding" : ""}`}>
      <div class="m-tth">
        <div class="m-tr1">
          <button class="b44" onClick={p.nav.back} aria-label="Back to the inbox">
            {I.back}
          </button>
          <div class="m-ttw">
            <div class="m-tt">{t.title || "Untitled"}</div>
            {t.parent && (
              <div class="m-from">
                <StartedBy parentId={t.parent} />
              </div>
            )}
          </div>
          <button class="b44" onClick={() => p.nav.go({ ...p.nav.at, sheet: { kind: "thread-menu", ids: [t.id] } })} aria-label="Thread actions">
            {I.more}
          </button>
        </div>
        <div class="m-chips">
          <MProjectChip t={t} projects={p.store.projects} onClick={() => p.nav.go({ ...p.nav.at, sheet: { kind: "project", ids: [t.id] } })} />
          <button class="m-chipsd" onClick={() => p.nav.go({ ...p.nav.at, sheet: { kind: "details", ids: [t.id] } })} aria-label="Thread details">
          <span class={`m-chip st ${stateClass(st)}`}>
            <span class={t.running ? "spin icw" : "icw"}>
              <StateIcon state={st} size={14} />
            </span>
            <span>{stateLabel(st)}</span>
          </span>
          {hostTag(t, p.store.hosts) && !t.hostOffline && (
            <span class="m-chip m m-hostc">
              <HostMark name={t.host!} />
              <span>{t.host}</span>
            </span>
          )}
          {t.branch && (
            <span class="m-chip m brn">
              <span>{t.branch}</span>
            </span>
          )}
          {(() => {
            const pv = threadProvider(t, p.store.hosts, p.store.profiles); // V0.206: as the desktop header shows it
            return pv.name ? (
              <span class="m-chip pvc" title={pv.title}>
                <span class="pdot" style={pv.color ? { background: pv.color } : undefined} />
                <span>{pv.name}</span>
              </span>
            ) : null;
          })()}
          {(() => {
            const pr = headerPr(t); // V0.98 (6): the newest open change's PR, never a done one
            if (!pr) return null;
            const l = prChip(pr);
            return <span class={`m-chip prc ${l.cls}`} title={l.title}>{l.text}</span>;
          })()}
          {(() => {
            const c = changeChip(t); // V0.59 (8)
            return c ? <span class={`m-chip prc ${c.cls}`} title={c.title}>{c.text}</span> : null;
          })()}
          </button>
        </div>
        <ThreadOffers t={t} onRun={(a, args) => void p.commands.run(a, [t.id], args)} />
        {t.hostOffline && (
          <div class="m-offl" role="status" title="The front shows this thread from its last copy; commands wait until the host is back">
            {offlineLine(t)}
          </div>
        )}
        {!cardOpen && (
          <div class="m-tabs" role="tablist">
            {threadTabs(t).map((k) => (
              <button class={p.tab === k ? "on" : ""} onClick={() => setTab(k)}>
                {TAB_LABEL[k]}
              </button>
            ))}
          </div>
        )}
      </div>
      {!cardOpen && <UndeclaredStrip id={t.id} />}
      {!cardOpen && <LeftoverStrip id={t.id} now={p.now} />}
      {t.tasks.length > 0 && !cardOpen && <AgentsStrip key={t.id} tasks={t.tasks} now={p.now} />}
      {cardOpen ? (
        <div class="m-peekc ell">{t.summary ? `Claude: ${t.summary}` : `${stateLabel(st)} · ${t.project}`}</div>
      ) : (
        <ThreadTabBody
          phone
          t={t}
          tab={p.tab}
          setTab={setTab}
          events={p.open.events}
          now={p.now}
          onAtEnd={p.open.onAtEnd}
          onEarlier={p.open.onEarlier}
          run={(a, args) => p.commands.run(a, [t.id], args)}
          send={(args) => p.commands.send(t.id, args)}
        />
      )}
      {decision && decision.kind === "ask" && p.open.card && p.open.card.decisionId === decision.id && (
        <MAsk d={decision} card={p.open.card} onCard={p.open.onCard} collapsed={collapsed} onCollapse={() => setCollapsed(!collapsed)} submit={() => p.open.submitAsk(decision).then(afterAnswer)} />
      )}
      {decision && decision.kind !== "ask" && (
        <MAllow d={decision} t={t} host={hostName} collapsed={collapsed} onCollapse={() => setCollapsed(!collapsed)} commit={(ok) => p.open.allow(decision, ok).then(afterAnswer)} />
      )}
      {!decision && nextOffer && (
        <div class="m-abar">
          <button class="m-bt p" onClick={() => (setNextOffer(null), p.nav.replace({ screen: { name: "thread", id: nextOffer.id }, sheet: null }))}>
            <span class="ell">Next: {nextOffer.title} ›</span>
          </button>
        </div>
      )}
      {!decision && !nextOffer && <MComposer key={t.id} t={t} p={p} tab={p.tab} setTab={setTab} />}
    </div>
  );
}

function MComposer({ t, p, tab, setTab }: { t: Thread; p: PhoneProps; tab: string; setTab(tab: string): void }) {
  const [text, setText] = useDraft(t.id);
  const att = useAttachments(t.hostId);
  const ta = useRef<HTMLTextAreaElement>(null);
  const reason = unavailableReason(t, "thread.send");
  const plan = sendArgs(text, att.chips);
  const empty = !text.trim() && !att.chips.length;
  const stop = t.running && empty;
  // V0.79: `/` at the start lists the agent's commands and skills; a tap picks.
  const slash = useSlash(reason ? null : { host: t.hostId ?? "", threadId: t.id }, text, setText, ta);
  useLayoutEffect(() => {
    const el = ta.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 22 * 5 + 22) + "px";
  }, [text]);
  const send = async () => {
    if (reason || !plan.ok) return;
    if (await sendFrom(tab, plan.args, (a) => p.commands.send(t.id, a), setTab)) {
      haptic();
      setText("");
      att.clear();
    }
  };
  return (
    <div class="m-cmpw" onDrop={att.onDrop} onDragOver={att.onDragOver}>
      <AgentStatus phone t={t} now={p.now} onRestart={() => void p.commands.run("thread.restart", [t.id])} />
      <ChipStrip chips={att.chips} onRemove={att.remove} />
      <SlashList s={slash} phone />
      <div class={`m-cmp${reason ? " off" : ""}`}>
        <PhonePickers onFiles={att.add} disabled={!!reason} />
        <textarea
          ref={ta}
          class="in"
          rows={1}
          value={text}
          disabled={!!reason}
          placeholder={reason ?? (t.running ? "Steer the run…" : "Message Claude…")}
          onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
          onKeyDown={(e) => void slash.onKeyDown(e)}
          onPaste={att.onPaste}
        />
        {stop ? (
          <button class="m-send" aria-label="Stop the run" title={unavailableReason(t, "thread.interrupt") ?? "Stop"} disabled={!isAvailable(t, "thread.interrupt")} onClick={() => void p.commands.run("thread.interrupt", [t.id])}>
            {I.stop}
          </button>
        ) : (
          <button class="m-send" aria-label="Send" title={plan.ok ? "Send" : plan.reason} disabled={!!reason || !plan.ok} onClick={() => void send()}>
            {I.send}
          </button>
        )}
      </div>
    </div>
  );
}

// ---- decisions ----

function CardHead({ children, collapsed, onCollapse }: { children: ComponentChildren; collapsed: boolean; onCollapse(): void }) {
  return (
    <div class="m-ah" onClick={onCollapse}>
      <StateIcon state="asking" size={14} />
      {children}
      <span class="sp" />
      <button class="b44" aria-label={collapsed ? "Show the question" : "Show the conversation"} onClick={(e) => (e.stopPropagation(), onCollapse())}>
        {collapsed ? I.up : I.down}
      </button>
    </div>
  );
}

function MAsk({ d, card, onCard, collapsed, onCollapse, submit }: { d: Decision; card: CardState; onCard(fn: (cs: CardState) => CardState): void; collapsed: boolean; onCollapse(): void; submit(): void }) {
  const qs = questionsOf(d);
  const q = qs[card.index];
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const qRef = useRef<HTMLDivElement>(null);
  const [clamped, setClamped] = useState(false);
  const [readAll, setReadAll] = useState(false);
  const otherRef = useRef<HTMLInputElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const [below, setBelow] = useState(0);
  /** Options under the fold get a "↓ N more options" band above the Answer bar (README, Decisions). */
  const measure = () => {
    const body = bodyRef.current;
    if (!body) return setBelow(0);
    const bottom = body.getBoundingClientRect().bottom;
    setBelow([...body.querySelectorAll(".m-op")].filter((el) => el.getBoundingClientRect().top > bottom - 24).length);
  };
  useLayoutEffect(measure, [card.index, readAll, collapsed, card.otherOpen]);
  useEffect(() => () => {
    if (timer.current) clearTimeout(timer.current);
  }, []);
  useLayoutEffect(() => {
    setReadAll(false);
  }, [card.index]);
  useLayoutEffect(() => {
    const el = qRef.current;
    setClamped(!!el && el.scrollHeight > el.clientHeight + 2);
  }, [card.index, readAll, q?.question]);
  useEffect(() => {
    if (card.otherOpen) otherRef.current?.focus();
  }, [card.otherOpen, card.index]);
  if (!q) return null;
  const step = stepLabel(card.index, qs.length);
  const commit = askCommit(card, d);
  const picks = card.picks[card.index] ?? [];
  const otherOn = card.other[card.index] !== null || card.otherOpen;

  const tap = (i: number) => {
    const idx = card.index;
    onCard((cs) => pickStay(cs, d, i));
    if (!q.multiSelect && idx < qs.length - 1) {
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => onCard((cs) => (cs.index === idx ? goTo(cs, d, idx + 1) : cs)), ADVANCE_MS);
    }
  };
  const onCommit = () => {
    if (commit.act === "submit") submit();
    else if (commit.act === "next") onCard((cs) => goTo(cs, d, cs.index + 1));
  };

  return (
    <>
      <div class={`m-ask${collapsed ? " collapsed" : ""}`}>
        <CardHead collapsed={collapsed} onCollapse={onCollapse}>
          <b>{step.text}</b>
          {step.dots && qs.length > 1 && (
            <span class="m-dots">
              {qs.map((_, i) => (
                <i class={i === card.index ? "on" : answerOf(card, d, i) !== null ? "done" : ""} />
              ))}
            </span>
          )}
          {q.multiSelect && <span class="mt">multi-select</span>}
        </CardHead>
        {!collapsed && (
          <div class="m-abody" ref={bodyRef} onScroll={measure}>
            {card.index === 0 && <div class="m-asker">{d.agent ? `Asked by subagent “${d.agent}”` : "Asked by the thread"}</div>}
            {qs.slice(0, card.index).map((pq, i) => (
              <div class="m-prev">
                <b>{i + 1}</b>
                <span class="ell">{answerOf(card, d, i) ?? "Not answered"}</span>
                <button class="ed" onClick={() => onCard((cs) => goTo(cs, d, i))} aria-label={`Change the answer to: ${pq.question}`}>
                  Change
                </button>
              </div>
            ))}
            <div ref={qRef} class={`m-q${readAll ? "" : " clamp"}`}>
              {q.question}
            </div>
            {(clamped || readAll) && (
              <button class="m-rd" onClick={() => setReadAll(!readAll)}>
                {readAll ? "Show less" : "Read all"}
              </button>
            )}
            <div class="m-opts" role={q.multiSelect ? "group" : "radiogroup"}>
              {q.options.map((o, i) => {
                const on = picks.includes(o.label);
                return (
                  <button class={`m-op${q.multiSelect ? " multi" : ""}${on ? " on" : ""}`} role={q.multiSelect ? "checkbox" : "radio"} aria-checked={on} onClick={() => tap(i)}>
                    <i class="mk" />
                    <span class="ot">
                      <span class="a">{o.label}</span>
                      {o.description && <span class="d">{o.description}</span>}
                    </span>
                  </button>
                );
              })}
              <button class={`m-op${q.multiSelect ? " multi" : ""}${otherOn ? " on" : ""}`} role={q.multiSelect ? "checkbox" : "radio"} aria-checked={otherOn} onClick={() => onCard((cs) => chooseOther(cs, d))}>
                <i class="mk" />
                <span class="ot">
                  <span class="a">Other…</span>
                  {!otherOn && <span class="d">Answer in your own words.</span>}
                </span>
              </button>
              {otherOn && (
                <input
                  ref={otherRef}
                  class="m-otherf"
                  value={card.other[card.index] ?? ""}
                  placeholder="Your answer"
                  enterKeyHint={commit.act === "next" ? "next" : "send"}
                  onInput={(e) => {
                    const v = (e.target as HTMLInputElement).value;
                    onCard((cs) => setOther({ ...cs, otherOpen: true }, d, v));
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      onCommit();
                    }
                  }}
                />
              )}
            </div>
          </div>
        )}
        {!collapsed && below > 0 && (
          <button class="m-below" onClick={() => bodyRef.current?.scrollBy({ top: bodyRef.current.clientHeight * 0.8, behavior: "smooth" })}>
            ↓ {below} more {below === 1 ? "option" : "options"}
          </button>
        )}
      </div>
      <div class="m-abar">
        <button class="m-bt p" disabled={!commit.enabled} onClick={onCommit}>
          {commit.label}
        </button>
      </div>
    </>
  );
}

function MAllow({ d, t, host, collapsed, onCollapse, commit }: { d: Decision; t: Thread; host: string; collapsed: boolean; onCollapse(): void; commit(ok: boolean): void }) {
  const [choice, setChoice] = useState<Choice>(null);
  useEffect(() => setChoice(null), [d.id]);
  const approval = d.kind === "approval";
  const x = approval ? null : describeEffect(d, t.pr);
  const c = allowCommit(d, choice);
  const cmd = approval && typeof d.input?.command === "string" ? (d.input.command as string) : null;
  const rest = approval && d.input ? Object.fromEntries(Object.entries(d.input).filter(([k]) => k !== "command" && k !== "cwd")) : {};
  const cwd = approval && typeof d.input?.cwd === "string" ? (d.input.cwd as string) : t.cwd;
  const tile = (value: Choice, label: string, desc: string) => (
    <button class={`m-op${choice === value ? " on" : ""}`} role="radio" aria-checked={choice === value} onClick={() => setChoice(value)}>
      <i class="mk" />
      <span class="ot">
        <span class="a">{label}</span>
        {desc && <span class="d">{desc}</span>}
      </span>
    </button>
  );
  return (
    <>
      <div class={`m-ask${collapsed ? " collapsed" : ""}`}>
        <CardHead collapsed={collapsed} onCollapse={onCollapse}>
          <b>{x ? x.label : "Approval"}</b>
        </CardHead>
        {!collapsed && (
          <div class="m-abody">
            {approval && <div class="m-asker">{d.agent ? `Asked by subagent “${d.agent}”` : "Asked by the thread"}</div>}
            <div class="m-q">{x ? x.title : `Allow ${d.tool ?? "this tool"}?`}</div>
            {approval ? (
              <>
                {cmd && <pre class="m-pre">{cmd}</pre>}
                {Object.keys(rest).length > 0 && <pre class="m-pre">{JSON.stringify(rest, null, 1)}</pre>}
                <div class="m-kv ell">
                  in {cwd} · on {host}
                </div>
              </>
            ) : (
              x && <EffectBody d={d} x={x} cls="m-eff" />
            )}
            <div class="m-opts" role="radiogroup">
              {tile("allow", x ? x.yes : "Allow", x ? "" : "Run it once.")}
              {tile("deny", x ? x.no : "Deny", x ? "Nothing happens; the agent is told." : "The agent is told no and continues.")}
            </div>
          </div>
        )}
      </div>
      <div class="m-abar">
        <button class="m-bt p" disabled={!c.enabled} onClick={() => choice && (haptic("firm"), commit(choice === "allow"))}>
          {c.label}
        </button>
      </div>
    </>
  );
}

// ---- new thread ----

/** SPEC 0.5 V0.30: Project first; the folder follows from the project's folder on this host, else it is asked.
 *  #41: the state is useNewThreadForm's, shared with the desktop dialog. */
function MNew(p: PhoneProps) {
  const n = useNewThreadForm({ onAddProject: p.projectActions.add, projects: p.store.projects, profiles: p.store.profiles, hosts: p.store.hosts, projectFilter: p.filter.id, clockOffset: p.store.clockOffset, onCreate: p.actions.create, onNewProject: (name, folder, host) => p.projectActions.create(name, folder, [], host) });
  const { form: f, cur, chip, folder, profiles, att } = n;
  const ta = useRef<HTMLTextAreaElement>(null);
  const ready = n.plan.ok;
  // V0.79: `/` at the start lists the commands seen for this provider and folder.
  const slash = useSlash(n.commandSource, f.text, (text) => n.set({ text }), ta);

  const paste = async () => {
    n.setErr(null);
    try {
      const items = await navigator.clipboard.read();
      const files: File[] = [];
      for (const it of items) {
        const type = it.types.find((x) => x.startsWith("image/"));
        if (type) files.push(new File([await it.getType(type)], "pasted", { type }));
      }
      if (!files.length) return n.setErr("The clipboard has no image.");
      att.add(files);
    } catch {
      ta.current?.focus();
      n.setErr("Paste into the text field: press and hold it, then Paste.");
    }
  };

  return (
    <div class="m-screen m-new">
      <MHeader onBack={p.nav.back} title="New thread" close />
      <div class="m-nbody" onDrop={att.onDrop} onDragOver={att.onDragOver}>
        <div class="m-defs">
          {/* V0.139: host first, as on the desktop */}
          <HostField value={n.host} onPick={n.chooseHost} what="The thread runs there. The projects, folders and providers below are that host's." />
          <div class="m-dl">Project</div>
          <button class="m-dchip m-ppick" onClick={n.togglePicking} aria-expanded={f.picking} style={cur && !chip.isNew ? { "--pc": colorVar(cur.color, cur.name) } : undefined}>
            {chip.isNew ? <span class="pi all">+</span> : cur ? <ProjectIcon project={cur} name={cur.name} size={28} /> : <span class="pi all" />}
            <span class="plt">
              <span class="ell pln">{chip.name}</span>
              <span class="ell pls2">{chip.sub}</span>
            </span>
            {f.picking ? I.up : I.down}
          </button>
          {f.picking && f.creating === null && (
            <div class="m-pickpanel">
              <ProjectList
                projects={n.projects}
                selectedId={f.projectId ?? null}
                allLabel="No project"
                newRow={{ fromThread: false, sub: "A name, and a folder if you like", onNew: n.startCreating }}
                onPick={n.choose}
                extra={<QuietHold {...n.quiet} />}
              />
            </div>
          )}
          {n.missing && (
            <button class="btn" onClick={() => void n.addHere()}>
              Add {n.missing.name} on {n.host}…
            </button>
          )}
          {f.creating !== null && (
            <div class="m-pickpanel">
              <NewProjectStep initialName={f.creating} initialFolder="" fromThread={false} host={n.host} onDraft={n.setDraft} onCancel={n.cancelCreating} onCreate={n.createProject} />
            </div>
          )}
          <div class="m-dl">Folder</div>
          <div class={`m-dchip m-fline${folder.scratch ? " scratch" : ""}`}>
            <span class="ell">{folder.label}</span>
            <button class="m-fchg" onClick={() => void n.changeFolder()}>
              Change
            </button>
          </div>
          <div class="m-fhint">{folderHint(cur, folder)}</div>
        </div>
        <div class="m-capr">
          <button class="m-capb" onClick={() => void paste()}>
            {I.paste}
            Paste
          </button>
          <PhonePickers onFiles={att.add} big />
        </div>
        <ChipStrip chips={att.chips} onRemove={att.remove} />
        <SlashList s={slash} phone />
        <textarea
          ref={ta}
          class="m-ta"
          rows={3}
          value={f.text}
          placeholder={att.chips.length ? "Add a note (optional)" : "What should Claude do? Optional with a screenshot."}
          onInput={(e) => n.set({ text: (e.target as HTMLTextAreaElement).value })}
          onKeyDown={(e) => void slash.onKeyDown(e)}
          onPaste={att.onPaste}
        />
        <div class="m-defs">
          <label class="m-dchip">
            <span class="ell">
              {profiles.find((x) => x.id === f.profile)?.name ?? f.profile} · {shortModel(f.model)}
            </span>
            {I.down}
            <select
              value={`${f.profile}\u0000${f.model}`}
              aria-label="Provider and model"
              onChange={(e) => {
                const [profile, model] = (e.target as HTMLSelectElement).value.split("\u0000");
                n.set({ profile, model });
              }}
            >
              {profiles.flatMap((pr) =>
                pr.models.map((m) => (
                  <option value={`${pr.id}\u0000${m}`}>
                    {pr.name} · {shortModel(m)}
                  </option>
                )),
              )}
            </select>
          </label>
          {n.efforts.length > 0 && (
            <label class="m-dchip">
              <span class="ell">Effort · {n.effort || "default"}</span>
              {I.down}
              <select value={n.effort} aria-label="Effort" onChange={(e) => n.set({ effortPick: (e.target as HTMLSelectElement).value })}>
                {!n.effort && <option value="">CLI default</option>}
                {n.efforts.map((x) => (
                  <option value={x}>{x}</option>
                ))}
              </select>
            </label>
          )}
          <label class="m-dchip m-lvc">
            <input type="checkbox" checked={f.liveView} onChange={(e) => n.set({ liveView: (e.target as HTMLInputElement).checked })} />
            <span class="ell">Live view</span>
          </label>
          <QuotaList profiles={profiles} />
        </div>
        {n.err && <div class="err prose">{n.err}</div>}
      </div>
      <div class="m-abar">
        <button class="m-bt p" disabled={!ready || n.busy} onClick={() => void n.submit()}>
          Start
        </button>
      </div>
    </div>
  );
}

// ---- sheets ----

function MSheet({ onClose, children, label }: { onClose(): void; children: ComponentChildren; label: string }) {
  // A long press opens the sheet under the finger; the release must not land on the scrim and close it.
  const opened = useRef(Date.now());
  return (
    <div class="m-scrim" onClick={(e) => e.target === e.currentTarget && Date.now() - opened.current > 500 && onClose()}>
      <div class="m-sheet" role="dialog" aria-modal="true" aria-label={label}>
        <div class="m-grab" />
        {children}
      </div>
    </div>
  );
}

function Item({ icon, label, sub, right, disabled, onClick }: { icon?: ComponentChildren; label: string; sub?: string; right?: string; disabled?: boolean; onClick?(): void }) {
  return (
    <button class={`m-mi${disabled ? " dis" : ""}`} disabled={disabled} onClick={onClick}>
      <span>{icon}</span>
      <span class="ell">
        {label}
        {sub && <small class="ell">{sub}</small>}
      </span>
      {right && <span class="w">{right}</span>}
    </button>
  );
}

/** The More sheet's Settings row: what it holds, and at its right what needs the owner ("1 needs a login"). */
function MSettingsItem({ hosts, self, onClick }: { hosts: HostInfo[]; self: string; onClick(): void }) {
  // V0.125: the hosts that need the owner add to the count, as the Hosts badge does on the Settings screen. V0.127:
  // the providers of every host count.
  const need = settingsNeed(useProvidersNeed(useApi(), hosts, self), hostsNeed(hosts));
  return <Item label="Settings" sub="Providers, automation, projects, hosts, this device" right={need || undefined} onClick={onClick} />;
}

/** V0.166: the host load sheet: what the desktop rail's tooltips say, as text, and the provider quotas. */
function MLoadSheet({ hosts, profiles, onClose }: { hosts: HostInfo[]; profiles: Profile[]; onClose(): void }) {
  const list = stripHosts(useLoad(), hosts);
  return (
    <MSheet onClose={onClose} label="Host load">
      <div class="m-shh">Host load</div>
      <LoadSheetBody hosts={list} quotas={<QuotaList profiles={profiles} />} />
    </MSheet>
  );
}

function MSheets(p: PhoneProps & { sheet: Sheet; t: Thread | undefined }) {
  const close = p.nav.back;
  const { sheet, t } = p;
  const ids = sheet.ids ?? [];
  const after = (fn: () => void) => () => {
    close();
    setTimeout(fn, 0);
  };
  if (sheet.kind === "snooze") {
    return <SnoozeSheet ids={ids} title={ids.length === 1 ? p.store.threads[ids[0]]?.title : undefined} onClose={close} threads={ids.flatMap((id) => p.store.threads[id] ?? [])} onPick={(pick) => {
      const c = snoozeCommand(pick, ids);
      close();
      void p.commands.run(c.action, c.ids, c.args);
      p.list.setSelecting(false);
      p.list.setPicked(new Set());
    }} />;
  }
  if (sheet.kind === "load") return <MLoadSheet hosts={p.store.hosts} profiles={p.store.profiles} onClose={close} />;
  if (sheet.kind === "menu") {
    // V0.119: Notifications and Theme live in Settings → This device now (the owner's choice); Settings says what
    // needs the owner, as the Providers badge does.
    return (
      <MSheet onClose={close} label="More">
        <Item label="Select threads" icon={<i class="m-ck" />} onClick={after(() => p.list.setSelecting(true))} />
        <Item label="Changes" sub="Every change across threads, by stage" right={needsCount(p.store.threads) ? `${needsCount(p.store.threads)} need you` : undefined} onClick={() => p.nav.replace({ screen: { name: "changes" }, sheet: null })} />
        <MSettingsItem hosts={p.store.hosts} self={p.store.host} onClick={() => p.nav.replace({ screen: { name: "settings" }, sheet: null })} />
        <Item label="Filter by project…" right={p.filter.id ? p.store.projects.find((x) => x.id === p.filter.id)?.name : "all"} onClick={() => p.nav.replace({ ...p.nav.at, sheet: { kind: "filter" } })} />
        <Item label="Adopt from T3" onClick={() => p.nav.replace({ ...p.nav.at, sheet: { kind: "adopt" } })} />
        <Item label="Show tool details" sub="Every tool group open, every machine line in full" right={toolDetails() ? "on" : "off"} onClick={after(() => setToolDetails(!toolDetails()))} />
      </MSheet>
    );
  }
  if (sheet.kind === "filter") {
    return (
      <MSheet onClose={close} label="Filter by project">
        <div class="m-shh">Show one project</div>
        <ProjectList
          projects={p.store.projects.filter((x) => (x.threads ?? 0) > 0 || x.id === p.filter.id)}
          selectedId={p.filter.id}
          allLabel="All projects"
          onPick={(pr) => (p.filter.set(pr?.id ?? null), close())}
        />
      </MSheet>
    );
  }
  if (sheet.kind === "project") {
    // SPEC 0.5 V0.30: the one picker: file, unfile, or New project from this thread.
    return (
      <MSheet onClose={close} label="Project">
        <div class="m-shh">{ids.length > 1 ? `Project for ${ids.length} threads` : "Project"}</div>
        <div class="m-pform">
          <ProjectPicker
            projects={p.store.projects}
            threads={ids.map((id) => p.store.threads[id]).filter(Boolean)}
            onPick={async (pr) => {
              const e = await p.projectActions.move(ids, pr?.id ?? null);
              if (!e) close();
            }}
            onCreate={async (name, folder) => {
              const r = await p.projectActions.create(name, folder, ids);
              if (!r.error) close();
              return r.error;
            }}
            onEdit={(pr) => pr.id && p.nav.replace({ screen: { name: "project", id: pr.id }, sheet: null })}
          />
        </div>
      </MSheet>
    );
  }
  if (sheet.kind === "actions" && ids.length > 1) {
    return (
      <MSheet onClose={close} label="Selected threads">
        <div class="m-shh">{ids.length} selected</div>
        <Item label={`Archive ${ids.length}`} icon={I.close} onClick={after(() => (p.actions.archive(ids), p.list.setSelecting(false), p.list.setPicked(new Set())))} />
      </MSheet>
    );
  }
  if (!t) return null;
  const reason = (a: string) => unavailableReason(t, a);
  const avail = swipeAvail(t);
  const mark = seenToggle(t);
  const done = settleDoneEntry(t);
  if (sheet.kind === "actions" || sheet.kind === "thread-menu") {
    const peek = sheet.kind === "actions";
    const d = currentDecision(t);
    return (
      <MSheet onClose={close} label={t.title}>
        {peek && (
          <div class="m-pk">
            <div class="m-pkh">
              <StateIcon state={t.attention.state} />
              <div class="m-pkt">{t.title}</div>
            </div>
            <div class="m-pkm ell">
              {[t.project, t.branch, t.pr ? prChip(t.pr).text : null].filter(Boolean).join(" · ")}
            </div>
            {t.summary && <div class="m-pkb prose">Claude: {t.summary}</div>}
            {d && (
              <div class="m-pkq">
                <b class="s-ask">{rowState(t)}</b> · {rowMeta(t)}
              </div>
            )}
            <div class="m-pkn">Peek does not mark the thread seen.</div>
          </div>
        )}
        {!peek && <div class="m-shh ell">{t.title}</div>}
        {peek && <Item label="Open thread" icon={I.right} right="tap" onClick={() => p.nav.replace({ screen: { name: "thread", id: t.id }, sheet: null })} />}
        {t.attention.state === "failed" && <Item label="Retry" sub={reason("thread.retry")} disabled={!!reason("thread.retry")} onClick={after(() => void p.commands.run("thread.retry", [t.id]))} />}
        <Item
          label={avail.right.ok ? settleLabel(t) : "Settle"}
          icon={<StateIcon state="settled" size={16} />}
          sub={avail.right.ok ? undefined : t.attention.state === "asking" ? "Answer the question first" : reason("thread.settle")}
          disabled={!avail.right.ok}
          right={peek ? "swipe →" : undefined}
          onClick={after(() => p.actions.settle([t.id]))}
        />
        {/* V0.203: the same entry as the desktop menus (settleDoneEntry) */}
        <Item label={done.label} icon={<StateIcon state="settled" size={16} />} sub={reason(done.action) ?? (t.settleWhenDone ? undefined : SETTLE_DONE_HINT)} disabled={!!reason(done.action)} onClick={after(() => p.actions.settleWhenDone(t.id))} />
        <Item
          label="Snooze…"
          icon={<StateIcon state="snoozed" size={16} />}
          sub={reason("thread.snooze")}
          disabled={!!reason("thread.snooze")}
          right={peek ? "← swipe" : undefined}
          onClick={() => p.nav.replace({ ...p.nav.at, sheet: { kind: "snooze", ids: [t.id] } })}
        />
        {t.pinnedAt ? (
          <Item label="Unpin" icon={PIN_ICON} sub={reason("thread.unpin")} disabled={!!reason("thread.unpin")} onClick={after(() => void p.commands.run("thread.unpin", [t.id]))} />
        ) : (
          <Item label="Pin" icon={PIN_ICON} sub={reason("thread.pin") ?? "Keep it on top of the list"} disabled={!!reason("thread.pin")} onClick={after(() => void p.commands.run("thread.pin", [t.id]))} />
        )}
        <Item label="Project…" icon={<ProjectIcon project={projectOf(t, p.store.projects)} name={projectName(t, p.store.projects)} size={18} />} right={projectName(t, p.store.projects)} onClick={() => p.nav.replace({ ...p.nav.at, sheet: { kind: "project", ids: [t.id] } })} />
        <Item label="Live view" sub="The agent keeps its state pinned on top" right={t.liveView ? "on" : "off"} disabled={!!reason("thread.setView")} onClick={after(() => void p.commands.run("thread.setView", [t.id], { on: !t.liveView }))} />
        <Item label="Hand over…" sub={reason("thread.handover") ?? "Continue this thread with another provider"} disabled={!!reason("thread.handover")} onClick={() => p.nav.replace({ ...p.nav.at, sheet: { kind: "handover", ids: [t.id] } })} />
        <Item label="Regenerate title" sub={reason("thread.regenerateTitle") ?? "Name the thread from its conversation so far"} disabled={!!reason("thread.regenerateTitle")} onClick={after(() => void p.commands.regenerateTitle(t.id))} />
        {/* V0.189: the desktop menu's entry (seenToggle): an unread thread offers Mark seen */}
        <Item label={mark.label} icon={<StateIcon state={mark.id === "markSeen" ? "seen" : "done"} size={16} />} sub={reason(mark.action)} disabled={!!reason(mark.action)} onClick={after(() => (mark.id === "markSeen" ? void p.commands.run(mark.action, [t.id]) : p.actions.markUnseen(t.id)))} />
        {peek && <Item label="Select" icon={<i class="m-ck" />} onClick={after(() => (p.list.setSelecting(true), p.list.setPicked(new Set([t.id]))))} />}
        {!peek && <Item label="Details" icon={I.right} onClick={() => p.nav.replace({ ...p.nav.at, sheet: { kind: "details", ids: [t.id] } })} />}
        <Item label="Archive" icon={I.close} sub={reason("thread.archive")} disabled={!!reason("thread.archive")} onClick={after(() => p.actions.archive([t.id]))} />
      </MSheet>
    );
  }
  if (sheet.kind === "handover" && t) {
    return (
      <MSheet onClose={close} label="Hand over">
        <div class="m-shh">Hand over</div>
        <HandoverForm phone t={t} profiles={p.store.profiles} onClose={close} onConfirm={(pr, m) => p.commands.handover(t.id, pr, m)} />
      </MSheet>
    );
  }
  if (sheet.kind === "details") {
    const prof = profilesOfHost(hostOf(t, p.store.hosts), p.store.hosts, p.store.profiles).find((x) => x.id === t.profile); // V0.139: its host's
    // V0.120 (5): a model the owner turned off shows as "(off)" until another is picked; the desktop's chip does the same.
    const modelOff = !!prof && !!t.model && !prof.models.includes(t.model);
    const canModel = ((prof?.models.length ?? 0) > 1 || (modelOff && (prof?.models.length ?? 0) > 0)) && !reason("thread.setModel");
    const row = (k: string, v: ComponentChildren) => (
      <div class="m-kvrow">
        <span class="k">{k}</span>
        <span class="v prose">{v}</span>
      </div>
    );
    return (
      <MSheet onClose={close} label="Details">
        <div class="m-shh prose">{t.title}</div>
        <div class="m-kvs">
          {row("State", `${stateLabel(t.attention.state)}${t.attention.reason ? " · " + t.attention.reason : ""}`)}
          {row(
            "Project",
            (() => {
              const pr = projectOf(t, p.store.projects);
              return (
                <span class="m-nopj">
                  <button class="m-plink" onClick={() => p.nav.replace({ ...p.nav.at, sheet: { kind: "project", ids: [t.id] } })} aria-label={`Project: ${projectName(t, p.store.projects)}. Change`}>
                    {pr ? <ProjectTag project={pr} name={pr.name} /> : <span>No project</span>}
                    <span class="m-plc">Change</span>
                  </button>
                </span>
              );
            })(),
          )}
          {row("Folder", t.cwd)}
          {t.branch && row("Branch", t.branch)}
          {row(
            "Model",
            canModel ? (
              <select class="m-msel" value={t.model} onChange={(e) => void p.commands.run("thread.setModel", [t.id], { model: (e.target as HTMLSelectElement).value })}>
                {modelOff && <option value={t.model}>{offLabel(t.model)}</option>}
                {prof!.models.map((m) => (
                  <option value={m}>{shortModel(m)}</option>
                ))}
              </select>
            ) : modelOff ? (
              offLabel(t.model)
            ) : (
              shortModel(t.model)
            ),
          )}
          {effortsFor(prof, t.model).length > 0 &&
            row(
              "Effort",
              reason("thread.setModel") ? (
                startEffort(prof, t.model, t.effort) || "default"
              ) : (
                <select class="m-msel" value={startEffort(prof, t.model, t.effort)} onChange={(e) => void p.commands.run("thread.setModel", [t.id], { effort: (e.target as HTMLSelectElement).value })}>
                  {!startEffort(prof, t.model, t.effort) && <option value="">CLI default</option>}
                  {effortsFor(prof, t.model).map((x) => (
                    <option value={x}>{x}</option>
                  ))}
                </select>
              ),
            )}
          {row("Provider", prof?.name ?? t.profile)}
          {t.pr &&
            row(
              "PR",
              <a href={t.pr.url} target="_blank" rel="noopener noreferrer">
                {t.pr.repo ? `${t.pr.repo}#${t.pr.number}` : `#${t.pr.number}`}
              </a>,
            )}
        </div>
      </MSheet>
    );
  }
  return null;
}

function SnoozeSheet({ ids, title, threads, onClose, onPick }: { ids: string[]; title?: string; threads: Thread[]; onClose(): void; onPick(c: Pick<SnoozeChoice, "until" | "done" | "ids">): void }) {
  const presets = snoozeChoices(new Date(), threads);
  const [custom, setCustom] = useState(false);
  const [val, setVal] = useState("");
  return (
    <MSheet onClose={onClose} label="Snooze">
      <div class="m-shh ell">{ids.length > 1 ? `Snooze ${ids.length} threads` : `Snooze “${title ?? ""}”`}</div>
      {presets.map((s) => (
        <Item label={s.label} sub={s.off} disabled={!!s.off} icon={<StateIcon state="snoozed" size={16} />} right={s.off ? undefined : s.when} onClick={() => onPick(s)} />
      ))}
      {custom ? (
        <div class="m-custom">
          <input type="datetime-local" value={val} onInput={(e) => setVal((e.target as HTMLInputElement).value)} aria-label="Snooze until" />
          <button class="m-bt p" disabled={!val || new Date(val).getTime() <= Date.now()} onClick={() => onPick({ until: new Date(val) })}>
            Snooze
          </button>
        </div>
      ) : (
        <Item label="Pick a date and time…" onClick={() => setCustom(true)} />
      )}
      <div class="m-shf">
        <button class="m-bt" onClick={onClose}>
          Cancel
        </button>
      </div>
    </MSheet>
  );
}

// ---- projects (V0.23) ----

/** The thread header's project chip (V0.30): a tap opens the picker. */
function MProjectChip({ t, projects, onClick }: { t: Thread; projects: Project[]; onClick(): void }) {
  const pr = projectOf(t, projects);
  return (
    <button class="m-pjb" onClick={onClick} aria-label={`Project: ${projectName(t, projects)}. Change`}>
      <span class={`m-chip pj${pr ? " proj" : " nopj"}`} style={pr ? { "--pc": colorVar(pr.color, pr.name) } : undefined}>
        {pr && <ProjectIcon project={pr} name={pr.name} size={16} />}
        <span>{projectName(t, projects)}</span>
      </span>
    </button>
  );
}

/** The project screen (V0.55; V0.119: the same page as Settings → Projects → a project, titled with its name; Saved at
 *  the right of the header, because the phone's header carries the title). */
function MProject(p: PhoneProps & { id: string }) {
  // V0.207: Settings lists a project this device hides, so its page opens too.
  const pr = recordOf(p.allProjects, p.id);
  const group = findProject(p.allProjects, p.id);
  const sv = useSaved();
  const offline = p.store.conn === "offline";
  return (
    <SetContext.Provider value={{ readOnly: offline, phone: true, saved: sv.mark }}>
    <div class="m-screen m-project">
      <MHeader onBack={p.nav.back} title={pr?.name ?? "Project"} right={<SavedMark shown={sv.shown} />} />
      {offline && <div class="m-ban">No connection to {p.store.host || "the host"}. Settings show what was last read and cannot be changed until it is back.</div>}
      <div class="m-nbody">
        <div class={`set phone${offline ? " ro" : ""}`}>
        {pr ? (
          <ProjectEditor
            project={pr}
            linking={group && linkingFor(group, p.allProjects, { ...p.projectActions, add: async (id, host, folder) => (await p.projectActions.add(id, host, folder)).error }, (id) => p.nav.replace({ screen: { name: "project", id }, sheet: null }))}
            profiles={p.store.profiles}
            now={p.now}
            onSave={(patch) => p.projectActions.update(pr.id!, patch)}
            onSetFolder={(v) => p.projectActions.setFolder(pr.id!, v)}
            onDelete={async () => {
              const e = await p.projectActions.delete(pr.id!);
              if (!e) p.nav.back();
              return e;
            }}
          />
        ) : (
          <div class="m-empty">{p.allProjects.length ? "This project is not here anymore." : "Loading…"}</div>
        )}
        </div>
      </div>
    </div>
    </SetContext.Provider>
  );
}
