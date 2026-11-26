// The phone layout (760 px or less), from design/mobile (README and mobile.html).
// Screens: Inbox, Thread, New, Search; sheets slide up over a screen and are history entries.
// Desktop components are reused where the README says they carry over (the thread tab body, AgentsStrip,
// image paste); thread commands, undo and toasts are the desktop's too (#41);
// everything else here uses its own m- classes so the desktop CSS never applies.

import { storedGet, storedSet } from "./prefs.ts";
import { MSheets } from "./phone-sheets.tsx";
import { MNew } from "./phone-new.tsx";
import { MAsk, MAllow } from "./phone-cards.tsx";
import { I } from "./phone-icons.tsx";
import { Pop, SlideMark } from "./motion-ui.tsx";
import { useApi } from "./api-context.ts";
import { Fragment, type ComponentChildren } from "preact";
import { SlotQueueSection, useSlotQueue } from "./slot-queue-ui.tsx";
import { useEffect, useRef, useState } from "preact/hooks";
import type { Decision, FyrEvent, Project, Thread } from "./types.ts";
import type { HostId } from "./hostid.ts";
import { LoadStrip, useLoad } from "./load-ui.tsx";
import { HomeScreen } from "./home-ui.tsx";
import { stripHosts } from "./host-load.ts";
import { SECTIONS, connLabel, connWords, mergedToSettle, unpinned, matchesQuery, selectAllIds, sweptUnread, undoVerb, type SectionKey, type Sections, type View } from "./store.ts";
import type { ThreadCommands } from "./commands.ts";
import { currentDecision } from "./decode.ts";
import { BrandMark, StateIcon } from "./icons.tsx";
import { stateChip } from "./rows.ts";
import { StateChip } from "./state-chip.tsx";
import { snoozedHint, type SnoozeCmd } from "./snooze.ts";
import { AgentPill, showOnly, threadAgent } from "./agent-ui.tsx";
import { UsageChip } from "./usage-ui.tsx";
import { ThreadTabBody } from "./thread.tsx";
import { UndeclaredStrip } from "./undeclared-ui.tsx";
import { LeftoverStrip } from "./leftovers-ui.tsx";
import { InvariantCards } from "./invariants-ui.tsx";
import { AiSearchPanel, useAiSearch } from "./ai-search-ui.tsx";
import { changeChip, headerPrs, settleLine, threadPrs } from "./offers.ts";
import { WakeChip } from "./wake-ui.tsx";
import { WaitsStrip } from "./waits-ui.tsx";
import { hasMultipleRepos, pickPr, prChip, shortRepo } from "./pr.ts";
import { ChangesBoard } from "./board-ui.tsx";
import { tabLabel, defaultTab, showsDecision, threadTabs } from "./view.ts";
import { DockRun, showCard } from "./runcard-ui.tsx";
import { dockRun } from "./runcard.ts";
import { HandoverLinkBar } from "./handover-ui.tsx";
import { ProjectIcon, StartedBy } from "./project-ui.tsx";
import { colorVar, findProject, projectName, projectOf, recordOf } from "./projects.ts";
import { type NewThreadArgs } from "./new-thread.ts";
import { type CardState } from "./question.ts";
import { activeSection, stripCount } from "./mobile.ts";
import { showsBottomBar, type Nav } from "./nav.ts";
import { type PageRef } from "./settings-nav.ts";
import { SetupInboxCard } from "./setup-ui.tsx";
import { SavedMark, SetContext, useSaved } from "./settings-rows.tsx";
import { FilterEmpty, FilterRow, SECTION_LIMIT, isOpen, type FilterView } from "./inbox.tsx";
import { HideClock, NeedsGroupHead, PIN_ICON, SectionTitle, hideUse, sectionHead } from "./row-ui.tsx";
import { needsHeads } from "./rows.ts";
import { AUTO_HIDE_SECTIONS, type HideAt } from "./inbox-autohide.ts";
import { drawnRows, type Guest } from "./selection.ts";
import { PwaControls } from "./pwa-ui.tsx";
import type { PushStatus } from "./push.ts";
import { QuotaWakeActions, ThreadQuotaFigure, useThreadQuota, useThreadQuotaCard } from "./quota-ui.tsx";
import { CardView } from "./card-ui.tsx";
import { ArrivedBar } from "./arrived-bar.tsx";
import { type AnswerMove, arrive } from "./arrived.ts";
import { HostMark } from "./host-mark-ui.tsx";
import { hostTag, offlineLine, profilesOf, threadProvider, type HostProfiles } from "./hosts.ts";
import { type ProjectPatch, ProjectEditor, archiveFor } from "./project-editor.tsx";
import { shownHereFor } from "./settings-ui.tsx";
import type { Hidden } from "./hidden.ts";
import type { ArchiveInfo } from "./archived.ts";
import { linkingFor } from "./project-linking.tsx";
import { QuietGate } from "./quiet-ui.tsx";
import { AgentsStrip } from "./agents-strip.tsx";
import { PhoneComposer, sendFrom } from "./composer.tsx";
import { writingIn } from "./drafts.ts";
import { workingHint, MRow } from "./phone-row.tsx";
import { FolderChip } from "./folder-ui.tsx";
import { PreviewsChip } from "./preview-ui.tsx";
import { useOpenedWithView } from "./view-ui.tsx";


// ---- props from the app shell ----

/** The phone's place (design/mobile: screens and sheets are history entries) and how to move. */
interface PhoneNav {
  at: Nav;
  go(n: Nav): void;
  replace(n: Nav): void;
  back(): void;
}

/** The thread on screen: its timeline, the question card's state, and the answers (they need the card). */
interface PhoneOpen {
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

/** V0.app-shell-tidy: the app's one search (app-state.tsx), as the desktop's search box reads it. */
interface PhoneSearch {
  query: string;
  setQuery(q: string): void;
  /** what search may reach, in the list's order: the filters' threads, no quiet ones (V0.114) */
  list: Thread[];
  /** the hits the quiet lock leaves out, so the search says why and offers the hold */
  quietHits: number;
}

/** The inbox list's own state: open and expanded sections, selection mode and the picked rows. */
interface PhoneList {
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
interface PhoneDevice {
  push: PushStatus;
  pushReason: string | null;
  ios: boolean;
  standalone: boolean;
  canInstall: boolean;
  onInstall(): void;
}

/** What the phone does differently from a plain thread command: U8 leaves the open thread after a settle, archive
 *  or snooze the host took (V0.app-shell-tidy), Mark unseen restarts the seen timer, and New starts a thread. Every
 *  other command is `commands.run`. */
export interface PhoneActions {
  /** `unreadKept`: the unread threads a Select all left out, for the toast (V0.189) */
  settle(ids: string[], bulk?: boolean, unreadKept?: number): void;
  /** V0.203: arms "settle when done", or cancels it when armed */
  settleWhenDone(id: string): void;
  archive(ids: string[]): void;
  /** a snooze choice's command (`snoozeCommand`) */
  snooze(c: SnoozeCmd): void;
  markUnseen(id: string): void;
  create(a: NewThreadArgs): Promise<string | null>;
}

export interface PhoneProps {
  nav: PhoneNav;
  /** threads, projects, hosts and the connection: the store as the screens read it (store.ts viewOf) */
  store: View;
  /** slice 2b: each host's profiles by install id; a thread's and a project's are its own host's */
  profiles: HostProfiles;
  /** the inbox's sections; the search; where a quiet project is lifted (V0.quiet-lift, its page); V0.192: rows is what the list draws (sec,
   *  with the current thread when the filters hide it), guest that row */
  inbox: { sec: Sections; rows: Sections; guest: Guest | null; search: PhoneSearch; openProject(id: string): void };
  now: number;
  /** #41: the same thread commands, undo and toasts as the desktop */
  commands: ThreadCommands;
  open: PhoneOpen;
  list: PhoneList;
  device: PhoneDevice;
  /** V0.207: every project, also those this device hides (`store` leaves them out) */
  allProjects: Project[];
  /** V0.207, V0.settings-ia: what this device hides, for the project screen's Show on this device (as the desktop's) */
  shownHere?: { hidden: Hidden; set(h: Hidden): void };
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
    /** V0.project-archive */
    archive(id: string, on: boolean): Promise<string | null>;
    archiveInfo(id: string): ArchiveInfo;
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


// ---- shell ----

export function PhoneApp(p: PhoneProps) {
  const s = p.nav.at.screen;
  const sheetThread = p.nav.at.sheet?.ids?.length === 1 ? p.store.threads[p.nav.at.sheet.ids[0]] : undefined;
  const bottomBar = showsBottomBar(p.nav.at, p.list.selecting);
  const api = useApi(); // V0.98 (3): the board visit
  const openedWithView = useOpenedWithView(s.name === "thread" ? p.open.thread : null); // V0.no-interrupt-rest (1)
  return (
    <div class={`m-app${bottomBar ? " has-bar" : ""}`}>
      {p.status}
      {s.name === "inbox" && <MInbox {...p} />}
      {s.name === "search" && <MSearch {...p} />}
      {s.name === "thread" && p.open.thread && <MThread {...p} t={p.open.thread} tab={defaultTab(p.open.thread, s.tab, openedWithView)} />}
      {s.name === "thread" && !p.open.thread && p.open.gated && (
        <div class="m-screen">
          <MHeader onBack={p.nav.back} title="Quiet hours" />
          <QuietGate project={recordOf(p.store.projects, p.open.gated!.projectId)} now={p.now} what="thread" onProject={p.inbox.openProject} />
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
      {s.name === "settings" && <div class="m-screen m-settings">{p.settings?.(s.page ? { page: s.page, ...(s.sub ? { sub: s.sub } : {}), ...(s.host ? { host: s.host } : {}), ...(s.focus ? { focus: s.focus } : {}) } : null, (title, end) => <MHeader onBack={p.nav.back} title={title} right={end} />)}</div>}
      {bottomBar && <MBottomBar {...p} />}
      <MToast {...p} bottomBar={bottomBar} />
      {p.nav.at.sheet && <MSheets {...p} sheet={p.nav.at.sheet} t={sheetThread} />}
      {p.overlay}
    </div>
  );
}

export function MHeader({ onBack, title, right, close }: { onBack(): void; title: ComponentChildren; right?: ComponentChildren; close?: boolean }) {
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
  const [iosDismissed, setIosDismissed] = useState(() => storedGet(STORAGE_IOS) === "1");
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
  const { ids: mergedIds, any: canSettleMerged } = mergedToSettle(p.inbox.sec.seen);

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
      {!p.list.selecting && <FilterRow f={p.filters} now={p.now} openProject={p.inbox.openProject} />}
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
          <button class="b44" aria-label="Dismiss" onClick={() => (storedSet(STORAGE_IOS, "1"), setIosDismissed(true))}>
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
            <div class="m-sec" data-mk="sec:pinned">
              <SectionTitle label="Pinned" icon={PIN_ICON} n={p.inbox.sec.pinned.length} shown={stripCount(p.inbox.sec.pinned.length)} mk="pinned" />
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
                data-mk={`sec:${s.key}`}
                {...sectionHead({ n: list.length, open, toggle: () => p.list.toggleSection(s.key) })}
              >
                <SectionTitle label={s.label} icon={<StateIcon state={s.state} size={16} />} n={list.length} shown={stripCount(list.length)} open={open} attention={s.key === "needs"} mk={s.key} />
                {hideAt !== undefined && <HideClock at={hideAt} />}
                {s.key === "seen" && canSettleMerged && !p.list.selecting ? (
                  <button
                    class="m-sbtn"
                    onClick={(e) => {
                      e.stopPropagation();
                      p.actions.settle(mergedIds, true);
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
                <button class="m-more" data-mk={`more:${s.key}`} onClick={() => p.list.onExpand(s.key)}>
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


function MSearch(p: PhoneProps) {
  // The query is the app's (the desktop's search box), so the list and quietHits are computed once, in app-state.tsx.
  // The phone has no box on the inbox: the query lives while this screen does and is cleared when it goes, so the
  // inbox it goes back to is not filtered.
  const { query: q, setQuery: setQ, list: all, quietHits } = p.inbox.search;
  const ref = useRef<HTMLInputElement>(null);
  /* eslint-disable react-hooks/exhaustive-deps -- focuses the field when the search opens and clears the query when it closes, once each; setQ is the search's setter, passed down */
  useEffect(() => {
    ref.current?.focus();
    return () => setQ("");
  }, []);
  /* eslint-enable react-hooks/exhaustive-deps */
  const needle = q.trim().toLowerCase();
  const hits = needle ? all.filter((t) => matchesQuery(t, needle)).slice(0, 200) : [];
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
        {quietHits > 0 && <QuietGate now={p.now} what="search" />}
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
  // V0.no-interrupt (2): a question or approval that arrives while the owner writes a message comes folded, and the
  // composer stays under it with the focus and the keyboard; one that is there when the thread opens comes open. Decided
  // in the render the decision arrives in, so the field is never unmounted under the owner's fingers.
  const key = `${t.id}/${decision?.id ?? ""}`;
  const [fold, setFold] = useState(() => ({ key, collapsed: false, held: false }));
  let cur = fold;
  if (fold.key !== key) {
    const held = !!decision && fold.key.startsWith(`${t.id}/`) && writingIn(t.id);
    cur = { key, collapsed: held, held };
    setFold(cur);
  }
  const collapsed = cur.collapsed;
  const setCollapsed = (v: boolean) => setFold({ ...cur, collapsed: v });
  const [selectedPr, setSelectedPr] = useState<{ id: string; url: string } | null>(null);
  useEffect(() => setSelectedPr(null), [t.id]);
  const cardOpen = !!decision && !collapsed;
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
          <StateChip t={t} phone />
          {hostTag(t, p.store.hostList) && !t.hostOffline && (
            <span class="m-chip m m-hostc">
              <HostMark name={t.host!} />
              <span>{t.host}</span>
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
          {/* V0.usage-ledger: the desktop header's token chip */}
          <UsageChip u={t.usage} phone />
          {(() => {
            const c = changeChip(t); // V0.59 (8)
            return c ? <span class={`m-chip prc ${c.cls}`} title={c.title}>{c.text}</span> : null;
          })()}
          {(() => {
            const l = settleLine(t); // as the desktop header shows it
            return l ? <span class="m-chip"><span>{l}</span></span> : null;
          })()}
          </button>
          {/* V0.folder-chip: the desktop header's folder chip (it replaced the branch chip on both fronts). It stands after
              the Details button, not beside the host as on the desktop: that button takes every tap on the chips inside it,
              and this one's tap opens its own card with the path and Copy path. */}
          <FolderChip t={t} phone />
          {/* V0.filmstrip-fold: the desktop header's previews chip, beside the folder chip and outside the Details button */}
          <PreviewsChip phone threadRef={t.id} host={t.hostId} events={p.open.events} onShow={() => setTab("conv")} />
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
              <button key={k} class={p.tab === k ? "on" : ""} role="tab" aria-selected={p.tab === k} onClick={() => setTab(k)}>
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
      {!cardOpen && <HandoverLinkBar t={t} phone />}
      {!cardOpen && <UndeclaredStrip id={t.id} />}
      {!cardOpen && <LeftoverStrip id={t.id} now={p.now} />}
      {t.tasks.length > 0 && !cardOpen && <AgentsStrip key={t.id} tasks={t.tasks} now={p.now} events={p.open.events} cwd={t.cwd} />}
      {cardOpen ? (
        <div class="m-peekc ell">{t.summary ? `Claude: ${t.summary}` : `${stateChip(t).word} · ${t.project}`}</div>
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
      {!decision && !nextOffer && <WaitsStrip phone t={t} onCancel={(rule) => void p.commands.run("thread.cancelWait", [t.id], { rule })} onSnooze={(rule) => p.actions.snooze({ action: "thread.snoozeUntilWait", ids: [t.id], args: { rule } })} />}
      {(!decision || (collapsed && cur.held)) && !nextOffer && (
        <PhoneComposer
          key={t.id}
          t={t}
          now={p.now}
          onSend={(a) => sendFrom(p.tab, a, (x) => p.commands.send(t.id, x), setTab)}
          onStop={() => void p.commands.run("thread.interrupt", [t.id])}
          onRestart={() => void p.commands.run("thread.restart", [t.id])}
        />
      )}
    </div>
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
            archive={archiveFor(pr.id!, p.projectActions, (id) => p.nav.go({ screen: { name: "thread", id }, sheet: null }))}
            shownHere={p.shownHere && shownHereFor(group ?? pr, p.shownHere.hidden, p.shownHere.set)}
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
export function ThreadQuotaRow(p: {
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
