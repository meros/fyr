// The phone layout (760 px or less), from design/mobile (README and mobile.html).
// Screens: Inbox, Thread, New, Search; sheets slide up over a screen and are history entries.
// Desktop components are reused where the README says they carry over (the thread tab body, AgentsStrip,
// image paste); thread commands, undo and toasts are the desktop's too (#41);
// everything else here uses its own m- classes so the desktop CSS never applies.

import { haptic } from "./native.ts";
import { BottomSheet, Pop, SlideMark, springAway, springTo, x3d } from "./motion-ui.tsx";
import { addSample, releaseVelocity, type Sample, SNAP_SPRING } from "./motion.ts";
import { useApi } from "./api-context.ts";
import { useDraft } from "./drafts.ts";
import { Fragment, type ComponentChildren, type VNode } from "preact";
import { SlotQueueSection, useSlotQueue } from "./slot-queue-ui.tsx";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Decision, FyrEvent, Project, Thread } from "./types.ts";
import type { HostId } from "./hostid.ts";
import { LoadSheetBody, LoadStrip, useLoad } from "./load-ui.tsx";
import { HomeScreen } from "./home-ui.tsx";
import { stripHosts } from "./load.ts";
import { SECTIONS, connLabel, connWords, isAvailable, isPinned, unpinned, matchesQuery, sections, selectAllIds, sweptUnread, unavailableReason, undoVerb, type SectionKey, type Sections, type View } from "./store.ts";
import type { ThreadCommands } from "./commands.ts";
import { currentDecision } from "./decode.ts";
import { BrandMark, StateIcon, stateClass } from "./icons.tsx";
import { rowMeta, rowState, stateLabel } from "./rows.ts";
import { snoozeChoices, snoozeCommand, snoozedHint, type SnoozeChoice } from "./snooze.ts";
import { AgentPill, newThreadAgent, showOnly, threadAgent } from "./agent-ui.tsx";
import { AgentsStrip, EffectBody, ThreadTabBody, sendFrom } from "./thread.tsx";
import { UndeclaredStrip } from "./undeclared-ui.tsx";
import { LeftoverStrip } from "./leftovers-ui.tsx";
import { InvariantCards } from "./invariants-ui.tsx";
import { describeEffect } from "./effects.ts";
import { ChipStrip, PhonePickers, useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { SlashList, useSlash } from "./slash-ui.tsx";
import { AiSearchPanel, useAiSearch } from "./ai-search-ui.tsx";
import { changeChip, headerPrs, prForEffect, settleLine, threadPrs } from "./offers.ts";
import { WakeChip } from "./wake-ui.tsx";
import { actionsFor, runAction, type ActionHandlers, type ThreadAction } from "./actions.ts";
import { hasMultipleRepos, pickPr, prChip, rowPrChip, shortRepo } from "./pr.ts";
import { ChangesBoard } from "./board-ui.tsx";
import { needsCount } from "./board.ts";
import { tabLabel, defaultTab, showsDecision, threadTabs } from "./view.ts";
import { DockRun, showCard } from "./runcard-ui.tsx";
import { dockRun } from "./runcard.ts";
import { HandoverForm } from "./handover-ui.tsx";
import { NewProjectStep, ProjectEditor, linkingFor, ProjectIcon, ProjectList, QuietGate, QuietHold, ProjectPicker, ProjectTag, StartedBy, type ProjectPatch, type QuietUnlock } from "./project-ui.tsx";
import { colorVar, findProject, projectName, projectOf, recordOf } from "./projects.ts";
import { folderHint, type NewThreadArgs } from "./new-thread.ts";
import { useNewThreadForm } from "./new-thread-form.ts";
import { setToolDetails, toolDetails } from "./prefs.ts";
import { answerOf, back, goTo, pickStay, chooseOther, questionsOf, setOther, settledOf, skip, type CardState } from "./question.ts";
import { ADVANCE_MS, activeSection, allowCommit, askCommit, stepLabel, stripCount, type Choice } from "./mobile.ts";
import { LONG_PRESS_MS, idle, longPressCancelled, swipeAvail, swipeArmed, swipeEnd, swipeMove, swipeOffset, swipeStart, type Swipe, type SwipeAvail } from "./gesture.ts";
import type { Nav, Sheet } from "./nav.ts";
import { hostsNeed, settingsNeed, type PageRef } from "./settings-nav.ts";
import { SetupInboxCard } from "./setup-ui.tsx";
import { useProvidersNeed } from "./settings-ui.tsx";
import { HostField } from "./host-pick-ui.tsx";
import { SavedMark, SetContext, useSaved } from "./settings-rows.tsx";
import { AgentStatus } from "./agent-status.tsx";
import { FilterEmpty, FilterRow, SECTION_LIMIT, isOpen, type FilterView } from "./inbox.tsx";
import { HideClock, NeedsGroupHead, NeedsRowBody, OneRowBody, PHONE_LOOK, PIN_ICON, SectionTitle, hideUse, sectionHead } from "./row-ui.tsx";
import { needsHeads } from "./rows.ts";
import { AUTO_HIDE_SECTIONS, type HideAt } from "./inbox-autohide.ts";
import { GUEST_TEXT, drawnRows, type Guest } from "./selection.ts";
import { PwaControls } from "./pwa-ui.tsx";
import type { PushStatus } from "./push.ts";
import { QuotaList, QuotaWakeActions, ThreadQuotaFigure, useThreadQuota, useThreadQuotaCard } from "./quota-ui.tsx";
import { CardView } from "./card-ui.tsx";
import { ArrivedBar } from "./arrived-bar.tsx";
import { type AnswerMove, arrive } from "./arrived.ts";
import { HostMark } from "./host-mark-ui.tsx";
import { hostTag, offlineLine, profilesOf, threadProvider, type HostInfo, type HostProfiles } from "./hosts.ts";

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
  /** V0.ask-cancel: close the question card without an answer */
  cancelAsk(d: Decision): Promise<boolean>;
  allow(d: Decision, ok: boolean): Promise<boolean>;
  /** V0.arrived-from: the answer that moved the owner to this thread, as on the desktop */
  arrival: AnswerMove | null;
  setArrival(a: AnswerMove | null): void;
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
  /** threads, projects, hosts and the connection: the store as the screens read it (store.ts viewOf) */
  store: View;
  /** slice 2b: each host's profiles by install id; a thread's and a project's are its own host's */
  profiles: HostProfiles;
  /** the inbox's sections; what search finds: the project filter's threads, quiet ones only while the unlock runs
   *  (V0.114); all: the same with quiet ones, to count the hits the lock hides; the one unlock of quiet projects;
   *  V0.192: rows is what the list draws (sec, with the current thread when the filters hide it), guest that row */
  inbox: { sec: Sections; rows: Sections; guest: Guest | null; searchable: Record<string, Thread>; all: Record<string, Thread>; unlock: QuietUnlock };
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
    create(name: string, folder: string, threadIds?: string[], host?: HostId): Promise<{ error: string | null; id?: string }>;
    /** null: No project */
    move(ids: string[], projectId: string | null): Promise<string | null>;
    /** V0.58 */
    delete(id: string): Promise<string | null>;
    /** Decision 0034 */
    link(id: string, other: string): Promise<string | null>;
    unlink(id: string): Promise<string | null>;
    add(id: string, host: HostId, folder: string): Promise<{ error: string | null; id?: string }>;
  };
  actions: PhoneActions;
  /** V0.119: a Settings screen (the app builds it with its Api): the nav (null) or a page, under this header */
  settings?(at: PageRef | null, header: (title: ComponentChildren, end?: ComponentChildren) => ComponentChildren): ComponentChildren;
  /** V0.42: the folder picker sheet, over everything */
  overlay?: ComponentChildren;
  /** V0.118: the update status, the first line of the screen below the safe area, in the layout (never over a control) */
  status?: ComponentChildren;
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
      {s.name === "status" && (
        // V0.status-home: the status screen, the same component the desktop shows with no thread open.
        <div class="m-screen m-status">
          <MHeader onBack={p.nav.back} title="Status" />
          <div class="m-nbody">
            <HomeScreen phone sec={p.inbox.sec} projects={p.store.projects} profiles={p.profiles} hosts={p.store.hostList} now={p.now} loaded={p.store.loaded} onOpen={(id) => p.nav.go({ screen: { name: "thread", id }, sheet: null })} onAllNeeds={p.nav.back} />
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
    <Pop kind="toast" class={`m-toast${toast.error ? " err" : ""}${p.bottomBar ? " over-bar" : ""}`} role="status">
      <span class="ell">{text}</span>
      {toast.undo && p.commands.canUndo() && (
        <button class="tb" onClick={() => void p.commands.doUndo()}>
          {I.undo}
          Undo
        </button>
      )}
    </Pop>
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
  const strip = stripHosts(useLoad(), p.store.hostList);
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
          {/* V0.status-home: the logo opens the Status screen. */}
          <button class="m-brand b44" onClick={() => p.nav.go({ screen: { name: "status" }, sheet: null })} aria-label="Status">
            <BrandMark size={28} />
          </button>
          {/* V0.166: with several hosts the load strip names each one, so the connection keeps only its dot (the name on
              hover and for the screen reader): a 360 px header has no room for both. */}
          {/* Decision 0031 2.6: the connection in the same words as the desktop rail's foot, through every front. */}
          <span class={`m-conn ${p.store.conn}${strip.length > 1 ? " dot" : ""}`} title={connWords(p.store.conn)}>
            <i />
            <span class="ell">{connLabel(p.store.conn)}</span>
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
          <SlideMark at={active ?? ""} />
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
          <span class="ell">Reconnecting…</span>
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
        {!p.list.selecting && (
          <SetupInboxCard
            hosts={p.store.hostList}
            projectCount={p.store.projects.length}
            phone
            onOpen={() => p.nav.go({ screen: { name: "settings", page: "setup" }, sheet: null })}
          />
        )}
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
                open={isOpen("slots", p.list.sectionOpen)}
                onToggle={() => p.list.toggleSection("slots")}
                guest={p.inbox.guest?.id ?? null}
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
              {s.key === "needs" && <InvariantCards phone now={p.now} onOpen={(id) => p.nav.go({ screen: { name: "thread", id }, sheet: null })} />}
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

/** A row with swipes (right settles, left snoozes), long press (actions) and selection. V0.native-feel: the row follows
 *  the finger without a render, a flick commits, and it leaves or returns on a spring that starts at the finger's speed. */
function MRow({ t, p, needs, lead: own }: { t: Thread; p: PhoneProps; needs: boolean; /** V0.137: the queue's "#2" */ lead?: VNode }) {
  const avail: SwipeAvail = useMemo(() => swipeAvail(t), [t]);
  const availRef = useRef(avail);
  availRef.current = avail;
  const swRef = useRef<Swipe>(idle);
  const [under, setUnderState] = useState<{ dir: 1 | -1; armed: boolean } | null>(null);
  const underRef = useRef(under);
  const [moving, setMoving] = useState(false);
  const [hint, setHint] = useState<string | null>(null);
  const rowRef = useRef<HTMLDivElement>(null);
  const boxRef = useRef<HTMLDivElement>(null);
  const samples = useRef<Sample[]>([]);
  const leaving = useRef<{ gone: Animation; timer: ReturnType<typeof setTimeout> } | null>(null);
  const lp = useRef<{ timer: ReturnType<typeof setTimeout> | null; x: number; y: number; fired: boolean }>({ timer: null, x: 0, y: 0, fired: false });
  const suppressClick = useRef(false);

  const setUnder = (u: { dir: 1 | -1; armed: boolean } | null) => {
    const c = underRef.current;
    if (c?.dir === u?.dir && c?.armed === u?.armed) return;
    if (u?.armed && !c?.armed) haptic(); // the tick that says "let go now"
    underRef.current = u;
    setUnderState(u);
  };
  /** The row under the finger: a style write per move, a render only when what is under it changes. */
  const paint = (s: Swipe) => {
    swRef.current = s;
    const el = rowRef.current;
    if (!el) return;
    const off = swipeOffset(s, availRef.current);
    el.style.transform = off ? x3d(off) : "";
    setUnder(off ? { dir: off > 0 ? 1 : -1, armed: swipeArmed(s, el.offsetWidth, availRef.current) } : null);
  };
  const clearLp = () => {
    if (lp.current.timer) clearTimeout(lp.current.timer);
    lp.current.timer = null;
  };
  useEffect(() => () => clearLp(), []);
  /** A row that flew off is shown again: its thread changed section while the row stayed mounted (the list's hold,
   *  an undo), or nothing happened to it at all (a refused command). */
  const comeBack = () => {
    const l = leaving.current;
    if (!l) return;
    leaving.current = null;
    clearTimeout(l.timer);
    l.gone.cancel();
    if (rowRef.current) rowRef.current.style.transform = "";
    setUnder(null);
  };
  useLayoutEffect(comeBack, [t.attention.section]);
  useEffect(() => () => clearTimeout(leaving.current?.timer), []);

  const onDown = (e: PointerEvent) => {
    if (leaving.current || (e.pointerType === "mouse" && e.button !== 0)) return;
    rowRef.current?.getAnimations().forEach((a) => a.cancel());
    swRef.current = p.list.selecting ? idle : swipeStart(e.clientX, e.clientY);
    samples.current = [];
    lp.current = { timer: null, x: e.clientX, y: e.clientY, fired: false };
    if (!p.list.selecting) {
      lp.current.timer = setTimeout(() => {
        lp.current.fired = true;
        suppressClick.current = true;
        paint(idle);
        haptic();
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
      setMoving(true);
      try {
        rowRef.current?.setPointerCapture(e.pointerId);
      } catch {
        /* capture unsupported */
      }
    }
    if (next.phase === "swipe") {
      samples.current = addSample(samples.current, e.timeStamp, e.clientX);
      paint(next);
    } else swRef.current = next;
  };
  /** Back to rest from where the finger left it; what is under the row stays until the row covers it. */
  const snapBack = (from: number, v: number) => {
    const el = rowRef.current;
    const a = el && from ? springTo(el, from, 0, v, SNAP_SPRING, x3d) : null;
    if (el && !a) el.style.transform = "";
    const done = () => setUnder(null);
    if (a) a.finished.then(done, done);
    else done();
  };
  /** The row flies off in its direction, the gap closes, and only then the thread settles, so nothing jumps. */
  const flyOff = (from: number, v: number, then: () => void) => {
    const el = rowRef.current;
    const box = boxRef.current;
    const fly = el && springAway(el, from, el.offsetWidth + 24, Math.max(v, 0), x3d);
    if (!el || !box || !fly) {
      then();
      if (el) el.style.transform = "";
      setUnder(null);
      return;
    }
    const collapse = () => {
      const h = box.offsetHeight;
      const cs = getComputedStyle(box);
      const gone = box.animate([{ height: `${h}px`, marginTop: cs.marginTop, marginBottom: cs.marginBottom, opacity: 1 }, { height: "0px", marginTop: "0px", marginBottom: "0px", opacity: 0 }], { duration: 200, easing: "cubic-bezier(.32,.72,0,1)", fill: "forwards" });
      leaving.current = { gone, timer: setTimeout(comeBack, 4000) };
      gone.finished.then(then, () => {});
    };
    fly.finished.then(collapse, collapse);
  };
  const onUp = (e: PointerEvent) => {
    clearLp();
    const cur = swRef.current;
    swRef.current = idle;
    if (cur.phase !== "swipe") return;
    setMoving(false);
    suppressClick.current = true;
    const el = rowRef.current;
    const off = swipeOffset(cur, avail);
    const v = releaseVelocity(samples.current, e.timeStamp);
    const r = swipeEnd(cur, el?.offsetWidth ?? 390, avail, v);
    if (r.kind === "settle") {
      haptic("firm");
      flyOff(off, v, () => p.actions.settle([t.id]));
      return;
    }
    snapBack(off, r.kind === "none" && !r.reason ? v : 0);
    if (r.kind === "snooze") (haptic(), p.nav.go({ ...p.nav.at, sheet: { kind: "snooze", ids: [t.id] } }));
    else if (r.reason) {
      setHint(r.reason);
      setTimeout(() => setHint(null), 1400);
    }
  };
  const onCancel = () => {
    clearLp();
    const cur = swRef.current;
    swRef.current = idle;
    setMoving(false);
    snapBack(swipeOffset(cur, avail), 0);
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

  const side = under ? (under.dir > 0 ? avail.right : avail.left) : null;
  const st = t.attention.state;
  const picked = p.list.picked.has(t.id);
  const lead = p.list.selecting ? <i class={`m-ck${picked ? " on" : ""}`} /> : own;
  const pre = <MHost t={t} />;
  // V0.109: no row is marked here. The phone shows the inbox or one thread, never both, so there is no open thread
  // beside the list for a mark to follow; the desktop marks the open thread's row (app.tsx, selection.ts).
  const guest = p.inbox.guest?.id === t.id ? p.inbox.guest.why : undefined; // V0.192: as on the desktop
  const body = needs ? <NeedsRowBody t={t} now={p.now} look={PHONE_LOOK} lead={lead} pre={pre} guest={guest} /> : <OneRowBody t={t} now={p.now} look={PHONE_LOOK} lead={lead} pre={pre} guest={guest} />;
  return (
    <div ref={boxRef} class={`m-swp${picked ? " picked" : ""}${moving ? " moving" : ""}`}>
      {under && side && (
        <div class={`m-under ${under.dir > 0 ? "l" : "r"} ${!side.ok ? "no" : under.dir > 0 ? "set" : "snz"}${under.armed ? " armed" : ""}`}>
          {side.ok && under.dir > 0 && <StateIcon state="settled" size={16} />}
          <span class="ell">{side.label}</span>
          {side.ok && under.dir < 0 && <StateIcon state="snoozed" size={16} />}
        </div>
      )}
      {hint && <div class="m-under l no show">{hint}</div>}
      <div
        ref={rowRef}
        data-id={t.id}
        class="m-mv"
        onPointerDown={onDown}
        onPointerMove={onMove}
        onPointerUp={onUp}
        onPointerCancel={onCancel}
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
  const decision = currentDecision(t);
  const dock = dockRun(t);
  const [collapsed, setCollapsed] = useState(false);
  const [selectedPr, setSelectedPr] = useState<{ id: string; url: string } | null>(null);
  useEffect(() => setCollapsed(false), [decision?.id]);
  useEffect(() => setSelectedPr(null), [t.id]);
  const cardOpen = !!decision && !collapsed;
  const st = t.attention.state;
  const setTab = (tab: string) => p.nav.replace({ screen: { name: "thread", id: t.id, tab }, sheet: null });
  const allPrs = threadPrs(t);
  const openPrs = headerPrs(t);
  const activePr = pickPr(allPrs, selectedPr?.id === t.id ? selectedPr.url : undefined, t.pr?.url);
  const openPr = (url: string) => {
    setSelectedPr({ id: t.id, url });
    setTab("pr");
  };
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
          {/* V0.quota-in-thread: what is left on this thread's provider; one figure here, every window in Details */}
          <ThreadQuotaFigure profile={t.profile} host={t.host} name={threadProvider(t, p.profiles).name} onOpen={() => p.nav.go({ ...p.nav.at, sheet: { kind: "details", ids: [t.id] } })} />
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
          {hostTag(t, p.store.hostList) && !t.hostOffline && (
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
            const pv = threadProvider(t, p.profiles); // V0.206: as the desktop header shows it
            // V0.agent-pill: the desktop header's pill. It only shows here: the chip row is one button that opens Details,
            // where the same pill picks (a select inside a button would take the tap from it).
            if (!pv.name) return null;
            const pf = profilesOf(t, p.profiles).find((x) => x.id === t.profile);
            return <AgentPill size="chip" phone segs={threadAgent(t, pf, pv, "", () => {}).map(showOnly)} />;
          })()}
          {(() => {
            const c = changeChip(t); // V0.59 (8)
            return c ? <span class={`m-chip prc ${c.cls}`} title={c.title}>{c.text}</span> : null;
          })()}
          {(() => {
            const l = settleLine(t); // as the desktop header shows it
            return l ? <span class="m-chip"><span>{l}</span></span> : null;
          })()}
          </button>
          {/* V0.multi-pr: every open change's PR as a tappable chip outside Details, jumping straight to that PR in the PR tab. */}
          {(() => {
            const multiRepo = hasMultipleRepos(openPrs);
            return openPrs.map((pr) => {
              const l = prChip(pr);
              const repo = multiRepo ? shortRepo(pr.repo) : "";
              const on = p.tab === "pr" && activePr?.url === pr.url;
              return (
                <button key={pr.url} type="button" class="m-pjb m-prb" title={l.title} onClick={() => openPr(pr.url)}>
                  <span class={`m-chip prc ${l.cls}${on ? " on" : ""}`}>{repo ? `${repo} ${l.text}` : l.text}</span>
                </button>
              );
            });
          })()}
          {/* V0.wakeups-via-watch: the desktop header's wake chip. Outside the Details button, which takes every tap on the
              chips row, so a tap here cancels the wake-up as on the desktop. */}
          <WakeChip t={t} now={p.now} cls="m-chip" button="m-pjb m-wakeb" onCancel={(rule) => p.commands.run("thread.cancelWake", [t.id], { rule })} />
        </div>
        {t.hostOffline && (
          <div class="m-offl" role="status" title="The host is offline. This is the last copy Fyr saw; what you send waits until it is back.">
            {offlineLine(t)}
          </div>
        )}
        {!cardOpen && (
          <div class="m-tabs" role="tablist">
            <SlideMark under at={p.tab} />
            {threadTabs(t).map((k) => (
              <button class={p.tab === k ? "on" : ""} onClick={() => setTab(k)}>
                {tabLabel(t, k)}
              </button>
            ))}
          </div>
        )}
      </div>
      {/* V0.arrived-from: shown with the card open too, since the card is what the owner answers next */}
      {p.open.arrival && p.open.arrival.to === t.id && (
        <ArrivedBar
          phone
          a={p.open.arrival}
          onBack={() => {
            const from = p.open.arrival!.from;
            p.open.setArrival(null);
            p.nav.replace({ screen: { name: "thread", id: from }, sheet: null });
          }}
          onDismiss={() => p.open.setArrival(null)}
        />
      )}
      {!cardOpen && <UndeclaredStrip id={t.id} />}
      {!cardOpen && <LeftoverStrip id={t.id} now={p.now} />}
      {t.tasks.length > 0 && !cardOpen && <AgentsStrip key={t.id} tasks={t.tasks} now={p.now} events={p.open.events} cwd={t.cwd} />}
      {cardOpen ? (
        <div class="m-peekc ell">{t.summary ? `Claude: ${t.summary}` : `${stateLabel(st)} · ${t.project}`}</div>
      ) : (
        <ThreadTabBody
          phone
          t={t}
          tab={p.tab}
          setTab={setTab}
          prs={allPrs}
          activePr={activePr}
          onSelectPr={(url) => setSelectedPr({ id: t.id, url })}
          events={p.open.events}
          now={p.now}
          onAtEnd={p.open.onAtEnd}
          onEarlier={p.open.onEarlier}
          // V0.settle-request: the Settle chip settles as the phone's Settle does (U8: back to the list)
          run={(a, args) => (a === "thread.settle" ? p.actions.settle([t.id]) : p.commands.run(a, [t.id], args))}
          send={(args) => p.commands.send(t.id, args)}
        />
      )}
      {decision && decision.kind === "ask" && p.open.card && p.open.card.decisionId === decision.id && (
        <MAsk d={decision} card={p.open.card} onCard={p.open.onCard} collapsed={collapsed} onCollapse={() => setCollapsed(!collapsed)} submit={() => p.open.submitAsk(decision).then(afterAnswer)} cancel={() => void p.open.cancelAsk(decision)} />
      )}
      {decision && decision.kind !== "ask" && (
        <MAllow d={decision} t={t} host={t.host ?? ""} collapsed={collapsed} onCollapse={() => setCollapsed(!collapsed)} commit={(ok) => p.open.allow(decision, ok).then(afterAnswer)} />
      )}
      {!decision && nextOffer && (
        <div class="m-abar">
          <button
            class="m-bt p"
            onClick={() => {
              setNextOffer(null);
              p.open.setArrival(arrive({ id: t.id, title: t.title }, nextOffer.id, p.inbox.sec.needs.map((x) => x.id)));
              p.nav.replace({ screen: { name: "thread", id: nextOffer.id }, sheet: null });
            }}
          >
            <span class="ell">Next: {nextOffer.title} ›</span>
          </button>
        </div>
      )}
      {/* V0.next-step-dock: a run request waits in the dock above the composer, as on the desktop (DockRun) */}
      {!decision && dock && showsDecision(p.tab) && <DockRun o={dock.o} t={t} host={t.host || undefined} more={dock.waiting - 1} onRun={(a, args) => void p.commands.run(a, [t.id], args)} onShow={(id) => showCard(id, p.tab, setTab)} />}
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
  const slash = useSlash(reason ? null : { host: t.hostId, threadId: t.id }, text, setText, ta);
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

function CardHead({ children, collapsed, onCollapse, onCancel }: { children: ComponentChildren; collapsed: boolean; onCollapse(): void; onCancel?(): void }) {
  return (
    <div class="m-ah" onClick={onCollapse}>
      <StateIcon state="asking" size={14} />
      {children}
      <span class="sp" />
      {onCancel && (
        <button class="b44 m-ahx" aria-label="Cancel the question" title="Cancel the question" onClick={(e) => (e.stopPropagation(), onCancel())}>
          {I.close}
        </button>
      )}
      <button class="b44" aria-label={collapsed ? "Show the question" : "Show the conversation"} onClick={(e) => (e.stopPropagation(), onCollapse())}>
        {collapsed ? I.up : I.down}
      </button>
    </div>
  );
}

function MAsk({ d, card, onCard, collapsed, onCollapse, submit, cancel }: { d: Decision; card: CardState; onCard(fn: (cs: CardState) => CardState): void; collapsed: boolean; onCollapse(): void; submit(): void; cancel(): void }) {
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
  const passed = settledOf(card, d, card.index) && !picks.length && !otherOn;

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
    else if (commit.act === "cancel") cancel();
    else if (commit.act === "next") onCard((cs) => goTo(cs, d, cs.index + 1));
  };

  return (
    <>
      <div class={`m-ask${collapsed ? " collapsed" : ""}`}>
        <CardHead collapsed={collapsed} onCollapse={onCollapse} onCancel={cancel}>
          <b>{step.text}</b>
          {step.dots && qs.length > 1 && (
            <span class="m-dots">
              {qs.map((_, i) => (
                <i class={i === card.index ? "on" : settledOf(card, d, i) ? "done" : ""} />
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
                <span class="ell">{answerOf(card, d, i) ?? (card.skipped[i] ? "Skipped" : "Not answered")}</span>
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
            {/* V0.ask-cancel: Back to the question before, and Skip to leave this one for the agent */}
            <div class="m-asknav">
              {card.index > 0 && (
                <button class="m-navb" onClick={() => onCard((cs) => back(cs, d))}>
                  {I.back}
                  Back
                </button>
              )}
              <button class={`m-navb${passed ? " on" : ""}`} onClick={() => onCard((cs) => skip(cs, d))}>
                {passed ? "Unskip" : "Skip this question"}
              </button>
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
  const x = approval ? null : describeEffect(d, prForEffect(t, d));
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
  const n = useNewThreadForm({ onAddProject: p.projectActions.add, projects: p.store.projects, profiles: p.profiles, hosts: p.store.hostList, projectFilter: p.filter.id, clockOffset: p.store.clockOffset, onCreate: p.actions.create, onNewProject: (name, folder, host) => p.projectActions.create(name, folder, [], host) });
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
          {/* V0.agent-pill on V0.model-choice: the desktop dialog's pill, at a finger's size */}
          <AgentPill size="field" phone segs={newThreadAgent({ ...f, effort: n.effort, efforts: n.efforts }, profiles, n.pickModel, (x) => n.set({ effortPick: x }))} />
          <label class="m-dchip m-lvc">
            <input type="checkbox" checked={f.liveView} onChange={(e) => n.set({ liveView: (e.target as HTMLInputElement).checked })} />
            <span class="ell">Live view</span>
          </label>
          {n.note && <div class="m-mnote" role="status">{n.note}</div>}
          <QuotaList profiles={p.profiles} />
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
  return (
    <BottomSheet onClose={onClose} label={label}>
      {children}
    </BottomSheet>
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
function MSettingsItem({ hosts, onClick }: { hosts: HostInfo[]; onClick(): void }) {
  // V0.125: the hosts that need the owner add to the count, as the Hosts badge does on the Settings screen. V0.127:
  // the providers of every host count.
  const need = settingsNeed(useProvidersNeed(useApi(), hosts), hostsNeed(hosts));
  return <Item label="Settings" sub="Providers, automation, projects, hosts, this device" right={need || undefined} onClick={onClick} />;
}

/** V0.166: the host load sheet: what the desktop rail's tooltips say, as text, and the provider quotas. */
function MLoadSheet({ hosts, profiles, onClose }: { hosts: HostInfo[]; profiles: HostProfiles; onClose(): void }) {
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
  if (sheet.kind === "load") return <MLoadSheet hosts={p.store.hostList} profiles={p.profiles} onClose={close} />;
  if (sheet.kind === "menu") {
    // V0.119: Notifications and Theme live in Settings → This device now (the owner's choice); Settings says what
    // needs the owner, as the Providers badge does.
    return (
      <MSheet onClose={close} label="More">
        <Item label="Select threads" icon={<i class="m-ck" />} onClick={after(() => p.list.setSelecting(true))} />
        <Item label="Status" sub="What needs you, what runs, what happened" onClick={() => p.nav.replace({ screen: { name: "status" }, sheet: null })} />
        <Item label="Changes" sub="Every change across threads, by stage" right={needsCount(p.store.threads) ? `${needsCount(p.store.threads)} need you` : undefined} onClick={() => p.nav.replace({ screen: { name: "changes" }, sheet: null })} />
        <MSettingsItem hosts={p.store.hostList} onClick={() => p.nav.replace({ screen: { name: "settings" }, sheet: null })} />
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
  if (sheet.kind === "actions" || sheet.kind === "thread-menu") {
    const peek = sheet.kind === "actions";
    const d = currentDecision(t);
    // V0.one-action-list: the same actions as the desktop's row menu (peek) and its header ⋯ menu (thread sheet),
    // run by the same dispatch. A sheet opens over this one (replace); a command closes the sheet first.
    const handlers: ActionHandlers = {
      command: (command, id, args) => void p.commands.run(command, [id], args),
      sheet: (kind, id) => p.nav.replace({ ...p.nav.at, sheet: { kind, ids: [id] } }),
      navigate: (id) => p.nav.replace({ screen: { name: "thread", id }, sheet: null }),
      copy: (text) =>
        void navigator.clipboard?.writeText(text).then(
          () => p.commands.notify("Copied"),
          () => p.commands.showError("Could not copy"),
        ),
      select: (id) => (p.list.setSelecting(true), p.list.setPicked(new Set([id]))),
      own: { settle: (id) => p.actions.settle([id]), settleWhenDone: p.actions.settleWhenDone, archive: (id) => p.actions.archive([id]), markUnseen: p.actions.markUnseen, regenTitle: (id) => void p.commands.regenerateTitle(id) },
    };
    const pick = (a: ThreadAction) => (a.kind === "sheet" || a.kind === "navigate" ? () => runAction(a, t.id, handlers) : after(() => runAction(a, t.id, handlers, location.origin)));
    // The phone's own look of an entry: an icon, and at the right what a swipe or tap does (touch only) or the project.
    // No key hints: the phone has no keyboard.
    const look = (a: ThreadAction): { icon?: ComponentChildren; right?: string } => {
      switch (a.id) {
        case "open":
          return { icon: I.right, right: "tap" };
        case "settle":
          return { icon: <StateIcon state="settled" size={16} />, right: peek ? "swipe →" : undefined };
        case "unsettle":
        case "settleWhenDone":
          return { icon: <StateIcon state="settled" size={16} /> };
        case "snooze":
          return { icon: <StateIcon state="snoozed" size={16} />, right: peek ? "← swipe" : undefined };
        case "unsnooze":
          return { icon: <StateIcon state="snoozed" size={16} /> };
        case "pin":
        case "unpin":
          return { icon: PIN_ICON };
        case "markSeen":
          return { icon: <StateIcon state="seen" size={16} /> };
        case "markUnseen":
          return { icon: <StateIcon state="done" size={16} /> };
        case "project":
          return { icon: <ProjectIcon project={projectOf(t, p.store.projects)} name={projectName(t, p.store.projects)} size={18} />, right: projectName(t, p.store.projects) };
        case "archive":
        case "unarchive":
          return { icon: I.close };
        case "details":
          return { icon: I.right };
        case "select":
          return { icon: <i class="m-ck" /> };
        default:
          return {};
      }
    };
    return (
      <MSheet onClose={close} label={t.title}>
        {peek && (
          <div class="m-pk">
            <div class="m-pkh">
              <StateIcon state={t.attention.state} />
              <div class="m-pkt">{t.title}</div>
            </div>
            <div class="m-pkm ell">
              {(() => {
                const open = headerPrs(t);
                const chip = rowPrChip(open.length ? open : threadPrs(t));
                return [t.project, t.branch, chip?.text ?? null].filter(Boolean).join(" · ");
              })()}
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
        {actionsFor("phone", t, peek ? "row" : "thread").map((a) => {
          const l = look(a);
          return <Item key={a.id} label={a.label} icon={l.icon} sub={a.disabled ?? a.hint} disabled={!!a.disabled} right={l.right} onClick={pick(a)} />;
        })}
      </MSheet>
    );
  }
  if (sheet.kind === "handover" && t) {
    return (
      <MSheet onClose={close} label="Hand over">
        <div class="m-shh">Hand over</div>
        <HandoverForm phone t={t} profiles={profilesOf(t, p.profiles)} onClose={close} onConfirm={(pr, m, e) => p.commands.handover(t.id, pr, m, e)} />
      </MSheet>
    );
  }
  if (sheet.kind === "details") {
    const prof = profilesOf(t, p.profiles).find((x) => x.id === t.profile); // V0.139: its host's
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
          <ThreadQuotaRow
            profile={t.profile}
            host={t.host}
            name={threadProvider(t, p.profiles).name}
            wakes={t.wakes}
            wakeDisabled={reason("thread.wakeOnQuota")}
            onWake={(window) => void p.commands.run("thread.wakeOnQuota", [t.id], { window })}
            onCancelWake={(rule) => void p.commands.run("thread.cancelWake", [t.id], { rule })}
          />
          {row("Folder", t.cwd)}
          {t.branch && row("Branch", t.branch)}
          {/* V0.agent-pill: the desktop header's pill, at a finger's size; full width in Details so provider, model and effort never squeeze */}
          <div class="m-kvrow m-agrow">
            <span class="k">Agent</span>
            <AgentPill size="field" phone segs={threadAgent(t, prof, threadProvider(t, p.profiles), reason("thread.setModel"), (a) => void p.commands.run("thread.setModel", [t.id], a))} />
          </div>
          {(() => {
            const prs = threadPrs(t);
            if (!prs.length) return null;
            return row(
              prs.length > 1 ? "PRs" : "PR",
              <span class="m-kvprs">
                {prs.map((pr) => (
                  <a key={pr.url} href={pr.url} target="_blank" rel="noopener noreferrer">
                    {pr.repo ? `${pr.repo}#${pr.number}` : `#${pr.number}`}
                    {prs.length > 1 ? ` · ${prChip(pr).text.replace(/^#\d+\s*/, "")}` : ""}
                  </a>
                ))}
              </span>,
            );
          })()}
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
      {offline && <div class="m-ban">No connection to Fyr. Settings show what was last read and cannot be changed until it is back.</div>}
      <div class="m-nbody">
        <div class={`set phone${offline ? " ro" : ""}`}>
        {pr ? (
          <ProjectEditor
            project={pr}
            linking={group && linkingFor(group, p.allProjects, { ...p.projectActions, add: async (id, host, folder) => (await p.projectActions.add(id, host, folder)).error }, (id) => p.nav.replace({ screen: { name: "project", id }, sheet: null }))}
            profiles={profilesOf(pr, p.profiles)}
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

/** V0.quota-in-thread, V0.quota-reset-wake: the thread's provider quota in its Details sheet: what the desktop chip's
 *  hover says, as text, with the same wake-on-reset buttons. */
function ThreadQuotaRow(p: {
  profile: string;
  host?: string;
  name: string;
  wakes?: Thread["wakes"];
  wakeDisabled?: string;
  onWake(window: string): void;
  onCancelWake(rule: string): void;
}) {
  const c = useThreadQuotaCard(p.profile, p.host, p.name);
  const q = useThreadQuota(p.profile, p.host);
  if (!c) return null;
  return (
    <div class="m-kvrow m-qrow">
      <span class="k">Quota</span>
      <span class="v">
        <CardView c={c} head={false} />
        {q && <QuotaWakeActions q={q} wakes={p.wakes} wakeDisabled={p.wakeDisabled} onWake={p.onWake} onCancelWake={p.onCancelWake} />}
      </span>
    </div>
  );
}
