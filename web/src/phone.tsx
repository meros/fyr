// The phone layout (760 px or less), from design/mobile (README and mobile.html).
// Screens: Inbox, Thread, New, Search; sheets slide up over a screen and are history entries.
// Desktop components are reused where the README says they carry over (the thread tab body, AgentsStrip,
// image paste); thread commands, undo and toasts are the desktop's too (#41);
// everything else here uses its own m- classes so the desktop CSS never applies.

import { storedGet, storedSet } from "./prefs.ts";
import { MNew, MSheets } from "./phone-sheets.tsx";
import { MAsk, MAllow } from "./phone-cards.tsx";
import { I } from "./phone-icons.tsx";
import { haptic } from "./native.ts";
import { Pop, SlideMark, springAway, springTo, x3d } from "./motion-ui.tsx";
import { addSample, releaseVelocity, type Sample, SNAP_SPRING } from "./motion.ts";
import { useApi } from "./api-context.ts";
import { useDraft } from "./drafts.ts";
import { Fragment, type ComponentChildren, type VNode } from "preact";
import { SlotQueueSection, useSlotQueue } from "./slot-queue-ui.tsx";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Decision, FyrEvent, Project, Thread } from "./types.ts";
import type { HostId } from "./hostid.ts";
import { LoadStrip, useLoad } from "./load-ui.tsx";
import { HomeScreen } from "./home-ui.tsx";
import { stripHosts } from "./host-load.ts";
import { SECTIONS, connLabel, connWords, isAvailable, isPinned, unpinned, matchesQuery, sections, selectAllIds, sweptUnread, unavailableReason, undoVerb, type SectionKey, type Sections, type View } from "./store.ts";
import type { ThreadCommands } from "./commands.ts";
import { currentDecision } from "./decode.ts";
import { BrandMark, StateIcon, stateClass } from "./icons.tsx";
import { stateLabel } from "./rows.ts";
import { snoozedHint } from "./snooze.ts";
import { AgentPill, showOnly, threadAgent } from "./agent-ui.tsx";
import { AgentsStrip, ThreadTabBody, sendFrom } from "./thread.tsx";
import { UndeclaredStrip } from "./undeclared-ui.tsx";
import { LeftoverStrip } from "./leftovers-ui.tsx";
import { InvariantCards } from "./invariants-ui.tsx";
import { ChipStrip, PhonePickers, useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { createSubmitGuard } from "./send-guard.ts";
import { SlashList, useSlash } from "./slash-ui.tsx";
import { AiSearchPanel, useAiSearch } from "./ai-search-ui.tsx";
import { changeChip, headerPrs, settleLine, threadPrs } from "./offers.ts";
import { WakeChip } from "./wake-ui.tsx";
import { hasMultipleRepos, pickPr, prChip, shortRepo } from "./pr.ts";
import { ChangesBoard } from "./board-ui.tsx";
import { tabLabel, defaultTab, showsDecision, threadTabs } from "./view.ts";
import { DockRun, showCard } from "./runcard-ui.tsx";
import { dockRun } from "./runcard.ts";
import { HandoverLinkBar } from "./handover-ui.tsx";
import { ProjectEditor, linkingFor, ProjectIcon, QuietGate, StartedBy, type ProjectPatch, type QuietUnlock } from "./project-ui.tsx";
import { colorVar, findProject, projectName, projectOf, recordOf } from "./projects.ts";
import { type NewThreadArgs } from "./new-thread.ts";
import { type CardState } from "./question.ts";
import { activeSection, stripCount } from "./mobile.ts";
import { LONG_PRESS_MS, idle, longPressCancelled, swipeAvail, swipeArmed, swipeEnd, swipeMove, swipeOffset, swipeStart, type Swipe, type SwipeAvail } from "./gesture.ts";
import type { Nav } from "./nav.ts";
import { type PageRef } from "./settings-nav.ts";
import { SetupInboxCard } from "./setup-ui.tsx";
import { SavedMark, SetContext, useSaved } from "./settings-rows.tsx";
import { AgentStatus } from "./agent-status.tsx";
import { FilterEmpty, FilterRow, SECTION_LIMIT, isOpen, type FilterView } from "./inbox.tsx";
import { HideClock, NeedsGroupHead, NeedsRowBody, OneRowBody, PHONE_LOOK, PIN_ICON, SectionTitle, hideUse, sectionHead } from "./row-ui.tsx";
import { needsHeads } from "./rows.ts";
import { AUTO_HIDE_SECTIONS, type HideAt } from "./inbox-autohide.ts";
import { GUEST_TEXT, drawnRows, type Guest } from "./selection.ts";
import { PwaControls } from "./pwa-ui.tsx";
import type { PushStatus } from "./push.ts";
import { QuotaWakeActions, ThreadQuotaFigure, useThreadQuota, useThreadQuotaCard } from "./quota-ui.tsx";
import { CardView } from "./card-ui.tsx";
import { ArrivedBar } from "./arrived-bar.tsx";
import { type AnswerMove, arrive } from "./arrived.ts";
import { HostMark } from "./host-mark-ui.tsx";
import { hostTag, offlineLine, profilesOf, threadProvider, type HostProfiles } from "./hosts.ts";


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
      {!cardOpen && <HandoverLinkBar t={t} phone />}
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
  const [sending, setSending] = useState(false);
  const guard = useRef(createSubmitGuard(setSending)).current;
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
  const send = () => {
    if (reason || !plan.ok || guard.busy()) return;
    const args = plan.args;
    void guard.run(async () => {
      const ok = await sendFrom(tab, args, (a) => p.commands.send(t.id, a), setTab);
      if (ok) {
        haptic();
        setText("");
        att.clear();
      }
      return ok;
    });
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
          placeholder={reason ?? (t.running ? "Steer the run…" : "Message…")}
          onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
          onKeyDown={(e) => void slash.onKeyDown(e)}
          onPaste={att.onPaste}
        />
        {stop ? (
          <button class="m-send" aria-label="Stop the run" title={unavailableReason(t, "thread.interrupt") ?? "Stop"} disabled={!isAvailable(t, "thread.interrupt")} onClick={() => void p.commands.run("thread.interrupt", [t.id])}>
            {I.stop}
          </button>
        ) : (
          <button class="m-send" aria-label="Send" title={sending ? "Sending…" : (plan.ok ? "Send" : plan.reason)} disabled={sending || !!reason || !plan.ok} onClick={send}>
            {I.send}
          </button>
        )}
      </div>
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
