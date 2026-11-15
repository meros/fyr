// The phone layout (760 px or less), from design/mobile (README and mobile.html).
// Screens: Inbox, Thread, New, Search; sheets slide up over a screen and are history entries.
// Desktop components are reused where the README says they carry over (Conversation, AgentsStrip,
// PrTab, image paste); everything else here uses its own m- classes so the desktop CSS never applies.

import { haptic } from "./native.ts";
import { useDraft } from "./drafts.ts";
import type { ComponentChildren } from "preact";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Decision, FyrEvent, Profile, Project, ThreadDTO } from "./types.ts";
import { SECTIONS, isAvailable, matchesQuery, sections, unavailableReason, undoVerb, type Conn, type SectionKey, type Sections, type UndoKind } from "./store.ts";
import { quietNote } from "./quiet.ts";
import { StateIcon, stateClass } from "./icons.tsx";
import { rowMeta, rowState, stateLabel } from "./rows.ts";
import { relTime, shortModel, untilLabel, whenLabel } from "./format.ts";
import { effortsFor, startEffort } from "./providers.ts";
import { AgentsStrip, Conversation, EffectBody, PrTab, type SendArgs } from "./thread.tsx";
import { describeEffect } from "./effects.ts";
import { ChipStrip, PhonePickers, useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { Filmstrip } from "./preview-ui.tsx";
import { LiveViewPanel, ViewChat } from "./view-ui.tsx";
import { ChangeList, ThreadOffers, viewAction } from "./offers-ui.tsx";
import { changeChip } from "./offers.ts";
import { prChip } from "./pr.ts";
import { ChangesBoard } from "./board-ui.tsx";
import { needsCount } from "./board.ts";
import { defaultTab, liveViewOf } from "./view.ts";
import { HandoverForm } from "./handover-ui.tsx";
import { ChangesView } from "./changes-ui.tsx";
import { CiNotesScope } from "./ci-notes-ui.tsx";
import { NewProjectStep, ProjectEditor, ProjectIcon, ProjectList, ProjectPicker, ProjectTag, StartedBy, ThreadProject, type ProjectPatch } from "./project-ui.tsx";
import { colorVar, localProject, newThreadFolder, projectChip, projectDefaults, projectName, projectOf, startProject } from "./projects.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { changeOptions, folderHint, lastProject } from "./dialogs.tsx";
import { setToolDetails, toolDetails } from "./prefs.ts";
import { previewsOf } from "./conversation.ts";
import { answerOf, goTo, pickStay, chooseOther, questionsOf, setOther, type CardState } from "./question.ts";
import { ADVANCE_MS, activeSection, allowCommit, askCommit, newDefaults, parseLastUsed, phoneSnoozePresets, stepLabel, stripCount, type Choice } from "./mobile.ts";
import { LONG_PRESS_MS, idle, longPressCancelled, settleLabel, swipeAvail, swipeEnd, swipeMove, swipeOffset, swipeStart, type Swipe, type SwipeAvail } from "./gesture.ts";
import type { Nav, Sheet } from "./nav.ts";
import { groupTasks, liveLine, workingLine } from "./tasks.ts";
import { COLLAPSED_BY_DEFAULT, SECTION_LIMIT, isOpen, type Theme } from "./inbox.tsx";
import { PwaControls } from "./pwa-ui.tsx";
import type { PushStatus } from "./push.ts";
import type { NewThreadArgs } from "./dialogs.tsx";
import { QuotaList } from "./quota-ui.tsx";
import { hostForProject, hostTag, offlineLine, profilesOn, type HostInfo } from "./hosts.ts";

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

export interface PhoneActions {
  /** V0.60: any thread command, for example action.run */
  run(id: string, action: string, args?: Record<string, unknown>): void;
  settle(ids: string[], bulk?: boolean): void;
  snooze(ids: string[], until: Date): void;
  archive(ids: string[]): void;
  markUnseen(id: string): void;
  retry(id: string): void;
  interrupt(id: string): void;
  /** V0.36: put away a merged or closed PR */
  dismissPR(id: string): void;
  /** V0.37: turn the live view on or off */
  setView(id: string, on: boolean): void;
  setModel(id: string, model: string): void;
  /** V0.50 */
  setEffort(id: string, effort: string): void;
  /** V0.45: hand the thread over to another profile; resolves to an error text, or null */
  handover(id: string, profile: string, model: string): Promise<string | null>;
  send(id: string, args: SendArgs): Promise<boolean>;
  /** V0.33: withdraw a message the agent has not read */
  cancelMessage(id: string, messageId: string): Promise<boolean>;
  submitAsk(d: Decision): Promise<boolean>;
  allow(d: Decision, ok: boolean): Promise<boolean>;
  create(a: NewThreadArgs): Promise<string | null>;
  undo(): void;
}

export interface PhoneProps {
  nav: Nav;
  go(n: Nav): void;
  replace(n: Nav): void;
  back(): void;
  threads: Record<string, ThreadDTO>;
  sec: Sections;
  now: number;
  host: string;
  conn: Conn;
  thread: ThreadDTO | undefined;
  events: FyrEvent[] | undefined;
  onEarlier?: () => void;
  onAtEnd(atEnd: boolean): void;
  profiles: Profile[];
  /** V0.51/V0.54: the front's hosts; with more than one, rows and headers name each thread's host */
  hosts?: HostInfo[];
  projects: Project[];
  card: CardState | null;
  onCard(fn: (cs: CardState) => CardState): void;
  sectionOpen: Partial<Record<SectionKey, boolean>>;
  setSectionOpen(fn: (o: Partial<Record<SectionKey, boolean>>) => Partial<Record<SectionKey, boolean>>): void;
  expanded: Partial<Record<SectionKey, boolean>>;
  onExpand(k: SectionKey): void;
  picked: Set<string>;
  setPicked(s: Set<string>): void;
  selecting: boolean;
  setSelecting(b: boolean): void;
  push: PushStatus;
  pushReason: string | null;
  ios: boolean;
  standalone: boolean;
  canInstall: boolean;
  onInstall(): void;
  theme: Theme;
  setTheme(t: Theme): void;
  /** V0.50: the Settings body (the app builds it with its Api) */
  settings?: ComponentChildren;
  toast: { text: string; undo: boolean; error?: boolean } | null;
  undoKind: UndoKind | null;
  undoIds: string[];
  undoLive: boolean;
  actions: PhoneActions;
  /** what search finds: the project filter's threads, quiet ones included (V0.46) */
  searchable: Record<string, ThreadDTO>;
  /** projects (V0.23) */
  projectFilter: string | null;
  onProjectFilter(id: string | null): void;
  /** V0.42: the folder picker sheet, over everything */
  overlay?: ComponentChildren;
  projectActions: {
    update(id: string, patch: ProjectPatch): Promise<string | null>;
    setFolder(id: string, path: string): Promise<string | null>;
    create(name: string, folder: string, threadIds?: string[]): Promise<{ error: string | null; id?: string }>;
    /** null: No project */
    move(ids: string[], projectId: string | null): Promise<string | null>;
    /** V0.58 */
    delete(id: string): Promise<string | null>;
  };
}

const STORAGE_LAST = "fyr.newDefaults";
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
  const s = p.nav.screen;
  const sheetThread = p.nav.sheet?.ids?.length === 1 ? p.threads[p.nav.sheet.ids[0]] : undefined;
  const bottomBar = !p.selecting && (s.name === "inbox" || s.name === "search");
  return (
    <div class={`m-app${bottomBar ? " has-bar" : ""}`}>
      {s.name === "inbox" && <MInbox {...p} />}
      {s.name === "search" && <MSearch {...p} />}
      {s.name === "thread" && p.thread && <MThread {...p} t={p.thread} tab={defaultTab(p.thread, s.tab)} />}
      {s.name === "thread" && !p.thread && (
        <div class="m-screen">
          <MHeader onBack={p.back} title="Thread" />
          <div class="m-empty">{Object.keys(p.threads).length ? "This thread is not here anymore." : "Loading…"}</div>
        </div>
      )}
      {s.name === "new" && <MNew {...p} />}
      {s.name === "project" && <MProject {...p} id={s.id} />}
      {s.name === "changes" && (
        <div class="m-screen m-changes">
          <MHeader onBack={p.back} title="Changes" />
          <div class="m-nbody">
            <ChangesBoard phone threads={p.threads} projects={p.projects} onRun={(id, a, args) => p.actions.run(id, a, args)} onOpenThread={(id) => p.go({ screen: { name: "thread", id }, sheet: null })} />
          </div>
        </div>
      )}
      {s.name === "settings" && (
        <div class="m-screen m-settings">
          <MHeader onBack={p.back} title="Settings" />
          <div class="m-nbody">{p.settings}</div>
        </div>
      )}
      {bottomBar && <MBottomBar {...p} />}
      <MToast {...p} bottomBar={bottomBar} />
      {p.nav.sheet && <MSheets {...p} sheet={p.nav.sheet} t={sheetThread} />}
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
  if (!p.toast) return null;
  let text = p.toast.text;
  if (p.toast.undo && p.undoKind && p.undoIds.length === 1) {
    const verb = undoVerb(p.undoKind);
    const title = p.threads[p.undoIds[0]]?.title;
    if (title) text = `${verb} “${title}”`;
  }
  return (
    <div class={`m-toast${p.toast.error ? " err" : ""}${p.bottomBar ? " over-bar" : ""}`} role="status">
      <span class="ell">{text}</span>
      {p.toast.undo && p.undoLive && (
        <button class="tb" onClick={p.actions.undo}>
          {I.undo}
          Undo
        </button>
      )}
    </div>
  );
}

function MBottomBar(p: PhoneProps) {
  const n = p.sec.needs.length;
  const s = p.nav.screen.name;
  return (
    <nav class="m-bn" aria-label="Main">
      <button class={`m-bi${s === "inbox" ? " on" : ""}`} onClick={() => (s === "inbox" ? document.querySelector(".m-list")?.scrollTo({ top: 0 }) : p.replace({ screen: { name: "inbox" }, sheet: null }))}>
        {I.inbox}
        {n > 0 && <i class="m-bdg">{stripCount(n)}</i>}
        Inbox
      </button>
      <button class="m-bnew" onClick={() => p.go({ screen: { name: "new" }, sheet: null })}>
        {I.plus}
        New
      </button>
      <button class={`m-bi${s === "search" ? " on" : ""}`} onClick={() => s !== "search" && p.go({ screen: { name: "search" }, sheet: null })}>
        {I.search}
        Search
      </button>
    </nav>
  );
}

// ---- inbox ----

function MInbox(p: PhoneProps) {
  const listRef = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState<SectionKey | null>("needs");
  const [iosDismissed, setIosDismissed] = useState(() => load(STORAGE_IOS) === "1");
  const showIos = p.ios && !p.standalone && !iosDismissed;

  const onScroll = () => {
    const el = listRef.current;
    if (!el) return;
    const tops = SECTIONS.map((s) => ({ key: s.key, top: (document.getElementById(`m-sec-${s.key}`)?.offsetTop ?? 0) - el.offsetTop }));
    setActive(activeSection(tops, el.scrollTop));
  };

  const jump = (k: SectionKey) => {
    p.setSectionOpen((o) => ({ ...o, [k]: true }));
    requestAnimationFrame(() => {
      const el = listRef.current;
      const h = document.getElementById(`m-sec-${k}`);
      if (el && h) el.scrollTo({ top: h.offsetTop - el.offsetTop, behavior: "smooth" });
      setActive(k);
    });
  };

  const all = SECTIONS.flatMap((s) => p.sec[s.key]);
  const merged = p.sec.seen.filter((t) => t.pr?.state === "MERGED" && isAvailable(t, "thread.settle"));

  return (
    <div class="m-screen">
      {p.selecting ? (
        <div class="m-ih sel">
          <button class="b44" onClick={() => (p.setSelecting(false), p.setPicked(new Set()))} aria-label="Cancel selection">
            {I.close}
          </button>
          <span class="m-nt">{p.picked.size} selected</span>
          <span class="sp" />
          <button class="m-tbtn" onClick={() => p.setPicked(new Set(all.map((t) => t.id)))}>
            Select all
          </button>
        </div>
      ) : (
        <div class="m-ih">
          <span class="m-brand">Fyr</span>
          <span class={`m-conn ${p.conn}`} title={p.conn}>
            <i />
            <span class="ell">{p.host}</span>
          </span>
          <span class="sp" />
          <PwaControls
            push={p.push}
            pushReason={p.pushReason}
            showUnsupported={false}
            canInstall={p.canInstall}
            onBell={() => p.go({ ...p.nav, sheet: { kind: "push" } })}
            onInstall={p.onInstall}
          />
          <button class="b44" onClick={() => p.go({ ...p.nav, sheet: { kind: "menu" } })} aria-label="More">
            {I.more}
          </button>
        </div>
      )}
      {!p.selecting && (
        <div class="m-strip" role="tablist" aria-label="Sections">
          {SECTIONS.map((s) => {
            const n = p.sec[s.key].length;
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
      {p.projectFilter && !p.selecting && (() => {
        const fp = p.projects.find((x) => x.id === p.projectFilter);
        return fp ? (
          <div class="m-pfline" style={{ "--pc": colorVar(fp.color, fp.name) }}>
            <button class="m-pfn" onClick={() => p.go({ ...p.nav, sheet: { kind: "filter" } })}>
              <ProjectIcon project={fp} name={fp.name} size={20} />
              <span class="ell">Only {fp.name}</span>
              {quietNote(fp, p.now) && <span class="m-pfq">{quietNote(fp, p.now)}</span>}
            </button>
            <button class="m-tbtn" onClick={() => p.onProjectFilter(null)}>
              Clear
            </button>
          </div>
        ) : null;
      })()}
      {p.conn === "offline" && (
        <div class="m-cstrip">
          <span class="ell">Reconnecting to {p.host}…</span>
        </div>
      )}
      {showIos && !p.selecting && (
        <div class="m-iosrow">
          <span class="ell">Add Fyr to the Home Screen to get notifications.</span>
          <button class="m-tbtn" onClick={() => p.go({ ...p.nav, sheet: { kind: "push" } })}>
            How
          </button>
          <button class="b44" aria-label="Dismiss" onClick={() => (store(STORAGE_IOS, "1"), setIosDismissed(true))}>
            {I.close}
          </button>
        </div>
      )}
      <div class="m-list" ref={listRef} onScroll={onScroll}>
        {SECTIONS.map((s) => {
          const list = p.sec[s.key];
          const open = isOpen(s.key, p.sectionOpen) && list.length > 0;
          const limit = p.expanded[s.key] ? list.length : SECTION_LIMIT[s.key];
          const hint = s.key === "working" ? workingHint(list) : s.key === "snoozed" ? snoozedHint(list, p.now) : s.key === "settled" ? "last 24 h" : "";
          const collapsible = list.length > 0 && (COLLAPSED_BY_DEFAULT.includes(s.key) || !open);
          return (
            <section id={`m-sec-${s.key}`}>
              <div
                class={`m-sec${collapsible ? (open ? " exp" : " col") : ""}${s.key === "seen" && merged.length && !p.selecting ? " hasbtn" : ""}`}
                onClick={() => list.length && p.setSectionOpen((o) => ({ ...o, [s.key]: !isOpen(s.key, o) }))}
              >
                {s.label}
                <span class="n">{stripCount(list.length)}</span>
                {s.key === "seen" && merged.length > 0 && !p.selecting ? (
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
              {open &&
                list.slice(0, limit).map((t) => <MRow key={t.id} t={t} p={p} needs={s.key === "needs"} />)}
              {open && list.length > limit && (
                <button class="m-more" onClick={() => p.onExpand(s.key)}>
                  Show all {list.length.toLocaleString()}
                </button>
              )}
            </section>
          );
        })}
      </div>
      {p.selecting && (
        <div class="m-selbar">
          <button class="m-bt p" disabled={!p.picked.size} onClick={() => (p.actions.settle([...p.picked], true), p.setSelecting(false), p.setPicked(new Set()))}>
            Settle {p.picked.size || ""}
          </button>
          <button class="m-bt" disabled={!p.picked.size} onClick={() => p.go({ ...p.nav, sheet: { kind: "snooze", ids: [...p.picked] } })}>
            Snooze…
          </button>
          <button class="b44" disabled={!p.picked.size} aria-label="More actions" onClick={() => p.go({ ...p.nav, sheet: { kind: "actions", ids: [...p.picked] } })}>
            {I.more}
          </button>
        </div>
      )}
    </div>
  );
}

function workingHint(list: ThreadDTO[]): string {
  const waiting = list.filter((t) => t.attention.state === "waiting").length;
  return waiting ? `${waiting} waiting` : "";
}
function snoozedHint(list: ThreadDTO[], now: number): string {
  const next = list
    .map((t) => t.view?.snoozeUntil)
    .filter((x): x is string => !!x)
    .sort()[0];
  return next ? `next: ${untilLabel(next, now)}` : "none";
}

function oneMeta(t: ThreadDTO, now: number): string {
  if (t.attention.section === "working") {
    const subs = t.subagents && !groupTasks(t.tasks).running.length ? `${t.subagents} sub · ` : "";
    return subs + workingLine(t);
  }
  if (t.attention.state === "snoozed") return `until ${untilLabel(t.view?.snoozeUntil, now)}`;
  return t.project;
}

/** V0.54: a row names the thread's host when the front has more than one; an offline host says so on any front. */
function MHost({ t, hosts }: { t: ThreadDTO; hosts?: HostInfo[] }) {
  if (t.hostOffline) return <><b class="m-off">{t.host || "host"} offline</b> · </>;
  const h = hostTag(t, hosts);
  return h ? <><span class="m-hostn">{h}</span> · </> : null;
}

function MPr({ t, short }: { t: ThreadDTO; short?: boolean }) {
  if (!t.pr) return <span />;
  const l = prChip(t.pr, short);
  return (
    <span class={`m-pr ${l.cls}`} title={l.title}>
      {l.text}
    </span>
  );
}

/** A row with swipes (right settles, left snoozes), long press (actions) and selection. */
function MRow({ t, p, needs }: { t: ThreadDTO; p: PhoneProps; needs: boolean }) {
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
    set(p.selecting ? idle : swipeStart(e.clientX, e.clientY));
    lp.current = { timer: null, x: e.clientX, y: e.clientY, fired: false };
    if (!p.selecting) {
      lp.current.timer = setTimeout(() => {
        lp.current.fired = true;
        suppressClick.current = true;
        set(idle);
        if (navigator.vibrate) navigator.vibrate(8);
        p.go({ ...p.nav, sheet: { kind: "actions", ids: [t.id] } });
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
      else if (r.kind === "snooze") (haptic(), p.go({ ...p.nav, sheet: { kind: "snooze", ids: [t.id] } }));
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
    if (p.selecting) {
      const n = new Set(p.picked);
      if (n.has(t.id)) n.delete(t.id);
      else n.add(t.id);
      p.setPicked(n);
      return;
    }
    p.go({ screen: { name: "thread", id: t.id }, sheet: null });
  };

  const off = swipeOffset(sw, avail);
  const side = off > 0 ? avail.right : off < 0 ? avail.left : null;
  const st = t.attention.state;
  const picked = p.picked.has(t.id);
  const body = needs ? (
    <div class={`m-rw${st === "done" ? " dn" : ""}`}>
      {p.selecting ? <i class={`m-ck${picked ? " on" : ""}`} /> : <span class="m-tile"><StateIcon state={st} /></span>}
      <span class="t ell">{t.title || "Untitled"}</span>
      <span class="age">{relTime(t.activityAt ?? t.updatedAt, p.now)}</span>
      <span class="m ell">
        <b class={stateClass(st)}>{rowState(t)}</b> · <MHost t={t} hosts={p.hosts} />{t.parent && <><StartedBy parentId={t.parent} link={false} /> · </>}<ThreadProject t={t} /> · {rowMeta(t)}
      </span>
      <MPr t={t} />
    </div>
  ) : (
    <div class="m-one">
      {p.selecting ? <i class={`m-ck${picked ? " on" : ""}`} /> : <StateIcon state={st} />}
      <span class="t">
        <span class="tn ell">{t.title || "Untitled"}</span>
        <i class="ell">
          <MHost t={t} hosts={p.hosts} />
          {t.parent && (
            <>
              <StartedBy parentId={t.parent} link={false} /> ·{" "}
            </>
          )}
          {t.attention.section !== "working" && t.attention.state !== "snoozed" ? <ThreadProject t={t} /> : oneMeta(t, p.now)}
        </i>
      </span>
      {t.attention.section !== "working" ? <MPr t={t} short /> : <span />}
      <span class="age">
        {t.attention.section === "working"
          ? relTime(t.runStartedAt ?? groupTasks(t.tasks).running.map((x) => x.startedAt).filter(Boolean).sort()[0], p.now)
          : whenLabel(t.activityAt ?? t.updatedAt, p.now)}
      </span>
    </div>
  );
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
        class="m-mv"
        style={off ? { transform: `translateX(${off}px)` } : undefined}
        onPointerDown={onDown}
        onPointerMove={onMove}
        onPointerUp={onUp}
        onPointerCancel={() => (clearLp(), set(idle))}
        onClick={onClick}
        onContextMenu={(e) => e.preventDefault()}
        role="button"
        aria-label={`${t.title}, ${stateLabel(st)}`}
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
  // V0.46: search finds the threads of a project in its quiet hours too.
  const all = useMemo(() => {
    const sec = sections(p.searchable);
    return SECTIONS.flatMap((s) => sec[s.key]);
  }, [p.searchable]);
  const needle = q.trim().toLowerCase();
  const hits = needle ? all.filter((t) => matchesQuery(t, needle)).slice(0, 200) : [];
  return (
    <div class="m-screen">
      <div class="m-ih">
        <label class="m-sfield">
          {I.search}
          <input ref={ref} type="search" value={q} placeholder="Search threads" aria-label="Search threads" onInput={(e) => setQ((e.target as HTMLInputElement).value)} />
        </label>
      </div>
      <div class="m-list">
        {needle && hits.length === 0 && <div class="m-empty">No thread matches “{q}”.</div>}
        {!needle && <div class="m-empty">Titles, projects and PR numbers.</div>}
        {hits.map((t) => (
          <MRow key={t.id} t={t} p={p} needs={false} />
        ))}
      </div>
    </div>
  );
}

// ---- thread ----

function MThread(p: PhoneProps & { t: ThreadDTO; tab: string }) {
  const t = p.t;
  const decision = t.decisions?.[0] ?? null;
  const [collapsed, setCollapsed] = useState(false);
  useEffect(() => setCollapsed(false), [decision?.id]);
  const cardOpen = !!decision && !collapsed;
  const previews = useMemo(() => previewsOf(p.events ?? [], t.host), [p.events, t.host]);
  const st = t.attention.state;
  const setTab = (tab: string) => p.replace({ screen: { name: "thread", id: t.id, tab }, sheet: null });
  const [nextOffer, setNextOffer] = useState<ThreadDTO | null>(null);
  useEffect(() => {
    if (!nextOffer) return;
    const id = setTimeout(() => setNextOffer(null), 5000);
    return () => clearTimeout(id);
  }, [nextOffer]);

  const afterAnswer = (ok: boolean) => {
    if (!ok) return;
    const next = p.sec.needs.find((x) => x.id !== t.id);
    if (next) setNextOffer(next);
  };

  return (
    <div class={`m-screen m-thread${cardOpen ? " carding" : ""}`}>
      <div class="m-tth">
        <div class="m-tr1">
          <button class="b44" onClick={p.back} aria-label="Back to the inbox">
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
          <button class="b44" onClick={() => p.go({ ...p.nav, sheet: { kind: "thread-menu", ids: [t.id] } })} aria-label="Thread actions">
            {I.more}
          </button>
        </div>
        <div class="m-chips">
          <MProjectChip t={t} projects={p.projects} onClick={() => p.go({ ...p.nav, sheet: { kind: "project", ids: [t.id] } })} />
          <button class="m-chipsd" onClick={() => p.go({ ...p.nav, sheet: { kind: "details", ids: [t.id] } })} aria-label="Thread details">
          <span class={`m-chip st ${stateClass(st)}`}>
            <span class={t.running ? "spin icw" : "icw"}>
              <StateIcon state={st} size={14} />
            </span>
            <span>{stateLabel(st)}</span>
          </span>
          {hostTag(t, p.hosts) && !t.hostOffline && (
            <span class="m-chip m m-hostc">
              <span>{t.host}</span>
            </span>
          )}
          {t.branch && (
            <span class="m-chip m brn">
              <span>{t.branch}</span>
            </span>
          )}
          {t.pr && <span class={`m-chip prc ${prChip(t.pr).cls}`} title={prChip(t.pr).title}>{prChip(t.pr).text}</span>}
          {(() => {
            const c = changeChip(t); // V0.59 (8)
            return c ? <span class={`m-chip prc ${c.cls}`} title={c.title}>{c.text}</span> : null;
          })()}
          </button>
        </div>
        <ThreadOffers t={t} onRun={(a, args) => p.actions.run(t.id, a, args)} />
        {t.hostOffline && (
          <div class="m-offl" role="status" title="The front shows this thread from its last copy; commands wait until the host is back">
            {offlineLine(t)}
          </div>
        )}
        {!cardOpen && (
          <div class="m-tabs" role="tablist">
            {liveViewOf(t) && (
              <button class={p.tab === "view" ? "on" : ""} onClick={() => setTab("view")}>
                View
              </button>
            )}
            <button class={p.tab === "conv" ? "on" : ""} onClick={() => setTab("conv")}>
              Conversation
            </button>
            <button class={p.tab === "changes" ? "on" : ""} onClick={() => setTab("changes")}>
              Changes
            </button>
            {t.pr && (
              <button class={p.tab === "pr" ? "on" : ""} onClick={() => setTab("pr")}>
                PR
              </button>
            )}
          </div>
        )}
      </div>
      {t.tasks && t.tasks.length > 0 && !cardOpen && <AgentsStrip key={t.id} tasks={t.tasks} now={p.now} />}
      {!cardOpen && p.tab === "view" && <LiveViewPanel full t={t} now={p.now} onPress={(text) => p.actions.send(t.id, { text, from: "view" })} onAction={viewAction((a, args) => p.actions.run(t.id, a, args))} />}
      {!cardOpen && p.tab === "view" && <ViewChat t={t} events={p.events} now={p.now} onOpen={() => setTab("conv")} />}
      {!cardOpen && p.tab === "conv" && <Filmstrip cards={previews} collapsible />}
      {cardOpen ? (
        <div class="m-peekc ell">{t.summary ? `Claude: ${t.summary}` : `${stateLabel(st)} · ${t.project}`}</div>
      ) : p.tab === "conv" ? (
        <Conversation
          outbox={(t.outbox ?? []).map((o) => o.id)}
          events={p.events}
          onAtEnd={p.onAtEnd}
          threadId={t.id}
          onEarlier={p.onEarlier}
          cwd={t.cwd}
          host={t.host}
          delivery={{ cancel: (messageId) => p.actions.cancelMessage(t.id, messageId), resend: (args) => p.actions.send(t.id, args) }}
        />
      ) : p.tab === "changes" ? (
        <div class="chtab">
          <ChangeList t={t} onRun={(a, args) => p.actions.run(t.id, a, args)} />
          <CiNotesScope changes={t.changes}><ChangesView threadId={t.id} changes={t.changes} /></CiNotesScope>
        </div>
      ) : p.tab === "pr" ? (
        <PrTab t={t} now={p.now} phone onSend={(text) => p.actions.send(t.id, { text })} onDismiss={() => p.actions.dismissPR(t.id)} onChanges={() => setTab("changes")} />
      ) : null}
      {decision && decision.kind === "ask" && p.card && p.card.decisionId === decision.id && (
        <MAsk d={decision} card={p.card} onCard={p.onCard} collapsed={collapsed} onCollapse={() => setCollapsed(!collapsed)} submit={() => p.actions.submitAsk(decision).then(afterAnswer)} />
      )}
      {decision && decision.kind !== "ask" && (
        <MAllow d={decision} t={t} host={p.host} collapsed={collapsed} onCollapse={() => setCollapsed(!collapsed)} commit={(ok) => p.actions.allow(decision, ok).then(afterAnswer)} />
      )}
      {!decision && nextOffer && (
        <div class="m-abar">
          <button class="m-bt p" onClick={() => (setNextOffer(null), p.replace({ screen: { name: "thread", id: nextOffer.id }, sheet: null }))}>
            <span class="ell">Next: {nextOffer.title} ›</span>
          </button>
        </div>
      )}
      {!decision && !nextOffer && <MComposer key={t.id} t={t} p={p} onView={p.tab === "view"} />}
    </div>
  );
}

function MComposer({ t, p, onView }: { t: ThreadDTO; p: PhoneProps; onView?: boolean }) {
  const [text, setText] = useDraft(t.id);
  const att = useAttachments(t.host);
  const ta = useRef<HTMLTextAreaElement>(null);
  const reason = unavailableReason(t, "thread.send");
  const plan = sendArgs(text, att.chips);
  const empty = !text.trim() && !att.chips.length;
  const stop = t.running && empty;
  useLayoutEffect(() => {
    const el = ta.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 22 * 5 + 22) + "px";
  }, [text]);
  const send = async () => {
    if (reason || !plan.ok) return;
    if (await p.actions.send(t.id, onView ? { ...plan.args, from: "view-typed" } : plan.args)) {
      haptic();
      setText("");
      att.clear();
    }
  };
  return (
    <div class="m-cmpw" onDrop={att.onDrop} onDragOver={att.onDragOver}>
      {t.running && (
        <div class="sys m-status">
          <span class="spin icw">
            <StateIcon state="working" size={14} />
          </span>
          <span class="ell">{liveLine(t, p.now)}</span>
        </div>
      )}
      <ChipStrip chips={att.chips} onRemove={att.remove} />
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
          onPaste={att.onPaste}
        />
        {stop ? (
          <button class="m-send" aria-label="Stop the run" title={unavailableReason(t, "thread.interrupt") ?? "Stop"} disabled={!isAvailable(t, "thread.interrupt")} onClick={() => p.actions.interrupt(t.id)}>
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

function MAllow({ d, t, host, collapsed, onCollapse, commit }: { d: Decision; t: ThreadDTO; host: string; collapsed: boolean; onCollapse(): void; commit(ok: boolean): void }) {
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

/** SPEC 0.5 V0.30: Project first; the folder follows from the project's folder on this host, else it is asked. */
function MNew(p: PhoneProps) {
  const last = useMemo(() => parseLastUsed(load(STORAGE_LAST)), []);
  const defs = useMemo(() => newDefaults(last, p.projects, p.profiles), [p.projects, p.profiles]);
  const [made, setMade] = useState<Project | null>(null);
  const projects = made && !p.projects.some((x) => x.id === made.id) ? [...p.projects, made] : p.projects;
  // undefined: No project; null: not chosen yet (the projects may still be loading).
  const [projectId, setProjectId] = useState<string | undefined | null>(null);
  // V0.42: a folder picked for this thread; "" follows the project (its folder, else a scratch folder).
  const [picked, setPicked] = useState("");
  const picker = useFolderPick();
  const [combo, setCombo] = useState(`${defs.profile}\u0000${defs.model}`);
  const [picking, setPicking] = useState(false);
  const [creating, setCreating] = useState<string | null>(null);
  // V0.63: the project being made in the New project step, as it is typed.
  const [draft, setDraft] = useState<{ name: string; folder: string } | null>(null);
  const making = creating !== null ? (draft ?? { name: creating, folder: "" }) : null;
  const [text, setText] = useState("");
  const [liveView, setLiveView] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const ta = useRef<HTMLTextAreaElement>(null);
  const cur = projectId ? projects.find((x) => x.id === projectId) : undefined;
  // V0.54: a thread starts on its project's host, with that host's profiles; its files upload there.
  const target = hostForProject(cur?.host, p.hosts);
  const profiles = profilesOn(target, p.hosts, p.profiles);
  const att = useAttachments(target);
  const folder = newThreadFolder(picked, making ? localProject("", making.name, making.folder) : cur);
  const chip = projectChip(cur, making);
  const choose = (pr: Project | null) => {
    setPicking(false);
    setCreating(null);
    setDraft(null);
    setProjectId(pr?.id);
    setPicked("");
    const there = profilesOn(hostForProject(pr?.host, p.hosts), p.hosts, p.profiles);
    const d = pr ? projectDefaults(pr, there) : {};
    if (d.profile && d.model) setCombo(`${d.profile}\u0000${d.model}`);
    else if (!there.some((x) => x.id === combo.split("\u0000")[0])) {
      const np = there.find((x) => !x.unavailable);
      if (np) setCombo(`${np.id}\u0000${np.defaultModel ?? np.models[0] ?? ""}`);
    }
  };
  useEffect(() => {
    if (projectId === null && p.projects.length) {
      const id = startProject(p.projects, p.projectFilter, lastProject());
      const pr = p.projects.find((x) => x.id === id);
      if (pr) choose(pr);
      else setProjectId(undefined);
    }
    if (!combo.split("\u0000")[0] && defs.profile) setCombo(`${defs.profile}\u0000${defs.model}`);
  }, [p.projects.length, defs.profile]);
  const [profile, model] = combo.split("\u0000");
  const [effortPick, setEffort] = useState("");
  const prof = profiles.find((x) => x.id === profile);
  const efforts = effortsFor(prof, model);
  const effort = startEffort(prof, model, effortPick);
  const plan = sendArgs(text, att.chips);
  const ready = plan.ok;

  const paste = async () => {
    setErr(null);
    try {
      const items = await navigator.clipboard.read();
      const files: File[] = [];
      for (const it of items) {
        const type = it.types.find((x) => x.startsWith("image/"));
        if (type) files.push(new File([await it.getType(type)], "pasted", { type }));
      }
      if (!files.length) return setErr("The clipboard has no image.");
      att.add(files);
    } catch {
      ta.current?.focus();
      setErr("Paste into the text field: press and hold it, then Paste.");
    }
  };

  const start = async () => {
    if (!ready || busy) return;
    setBusy(true);
    // V0.63: Start with the New project step open makes the project first, then files the thread in it.
    let pid = projectId ?? undefined;
    if (making) {
      if (!making.name.trim()) return (setBusy(false), setErr("A project needs a name."));
      const r = await p.projectActions.create(making.name.trim(), making.folder);
      if (r.error || !r.id) return (setBusy(false), setErr(r.error ?? "The project was not made."));
      const np = localProject(r.id, making.name.trim(), making.folder);
      setMade(np);
      choose(np);
      pid = r.id;
    }
    store(STORAGE_LAST, JSON.stringify({ cwd: folder.cwd ?? "", profile, model }));
    const e = await p.actions.create({ cwd: making && !picked ? making.folder || undefined : folder.cwd, text: plan.ok ? plan.args.text : text, attachments: plan.ok ? plan.args.attachments : undefined, profile, model, effort: effortPick && effort ? effort : undefined, projectId: pid, view: liveView || undefined });
    setBusy(false);
    if (e) setErr(e);
  };

  return (
    <div class="m-screen m-new">
      <MHeader onBack={p.back} title="New thread" close />
      <div class="m-nbody" onDrop={att.onDrop} onDragOver={att.onDragOver}>
        <div class="m-defs">
          <div class="m-dl">Project</div>
          <button class="m-dchip m-ppick" onClick={() => (setPicking(!picking), setCreating(null), setDraft(null))} aria-expanded={picking} style={cur && !chip.isNew ? { "--pc": colorVar(cur.color, cur.name) } : undefined}>
            {chip.isNew ? <span class="pi all">+</span> : cur ? <ProjectIcon project={cur} name={cur.name} size={28} /> : <span class="pi all" />}
            <span class="plt">
              <span class="ell pln">{chip.name}</span>
              <span class="ell pls2">{chip.sub}</span>
            </span>
            {picking ? I.up : I.down}
          </button>
          {picking && creating === null && (
            <div class="m-pickpanel">
              <ProjectList
                projects={projects}
                selectedId={projectId ?? null}
                allLabel="No project"
                newRow={{ fromThread: false, sub: "A name, and a folder if you like", onNew: (q) => setCreating(q.trim()) }}
                onPick={(pr) => choose(pr)}
              />
            </div>
          )}
          {creating !== null && (
            <div class="m-pickpanel">
              <NewProjectStep
                initialName={creating}
                initialFolder=""
                fromThread={false}
                onDraft={(name, folder) => setDraft({ name, folder })}
                onCancel={() => (setCreating(null), setDraft(null))}
                onCreate={async (name, folder) => {
                  const r = await p.projectActions.create(name, folder);
                  if (r.error) return r.error;
                  if (r.id) {
                    const np = localProject(r.id, name, folder);
                    setMade(np);
                    choose(np);
                  }
                  return null;
                }}
              />
            </div>
          )}
          <div class="m-dl">Folder</div>
          <div class={`m-dchip m-fline${folder.scratch ? " scratch" : ""}`}>
            <span class="ell">{folder.label}</span>
            <button class="m-fchg" onClick={async () => {
              const path = await picker.pick(changeOptions(cur, folder.cwd ?? ""));
              if (path) setPicked(path);
            }}>
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
        <textarea
          ref={ta}
          class="m-ta"
          rows={3}
          value={text}
          placeholder={att.chips.length ? "Add a note (optional)" : "What should Claude do? Optional with a screenshot."}
          onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
          onPaste={att.onPaste}
        />
        <div class="m-defs">
          <label class="m-dchip">
            <span class="ell">
              {profiles.find((x) => x.id === profile)?.name ?? profile} · {shortModel(model)}
            </span>
            {I.down}
            <select value={combo} aria-label="Profile and model" onChange={(e) => setCombo((e.target as HTMLSelectElement).value)}>
              {profiles.flatMap((pr) =>
                pr.models.map((m) => (
                  <option value={`${pr.id}\u0000${m}`}>
                    {pr.name} · {shortModel(m)}
                  </option>
                )),
              )}
            </select>
          </label>
          {efforts.length > 0 && (
            <label class="m-dchip">
              <span class="ell">Effort · {effort || "default"}</span>
              {I.down}
              <select value={effort} aria-label="Effort" onChange={(e) => setEffort((e.target as HTMLSelectElement).value)}>
                {!effort && <option value="">CLI default</option>}
                {efforts.map((x) => (
                  <option value={x}>{x}</option>
                ))}
              </select>
            </label>
          )}
          <label class="m-dchip m-lvc">
            <input type="checkbox" checked={liveView} onChange={(e) => setLiveView((e.target as HTMLInputElement).checked)} />
            <span class="ell">Live view</span>
          </label>
          <QuotaList profiles={profiles} />
        </div>
        {err && <div class="err prose">{err}</div>}
      </div>
      <div class="m-abar">
        <button class="m-bt p" disabled={!ready || busy} onClick={() => void start()}>
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

function MSheets(p: PhoneProps & { sheet: Sheet; t: ThreadDTO | undefined }) {
  const close = p.back;
  const { sheet, t } = p;
  const ids = sheet.ids ?? [];
  const after = (fn: () => void) => () => {
    close();
    setTimeout(fn, 0);
  };
  if (sheet.kind === "snooze") {
    return <SnoozeSheet ids={ids} title={ids.length === 1 ? p.threads[ids[0]]?.title : undefined} onClose={close} onPick={(d) => (close(), p.actions.snooze(ids, d), p.setSelecting(false), p.setPicked(new Set()))} />;
  }
  if (sheet.kind === "menu") {
    return (
      <MSheet onClose={close} label="More">
        <Item label="Select threads" icon={<i class="m-ck" />} onClick={after(() => p.setSelecting(true))} />
        <Item label="Changes" sub="Every change across threads, by stage" right={needsCount(p.threads) ? `${needsCount(p.threads)} need you` : undefined} onClick={() => p.replace({ screen: { name: "changes" }, sheet: null })} />
        <Item label="Settings" sub="Providers, notifications, appearance" onClick={() => p.replace({ screen: { name: "settings" }, sheet: null })} />
        <Item label="Filter by project…" right={p.projectFilter ? p.projects.find((x) => x.id === p.projectFilter)?.name : "all"} onClick={() => p.replace({ ...p.nav, sheet: { kind: "filter" } })} />
        <Item label="Adopt from T3" onClick={() => p.replace({ ...p.nav, sheet: { kind: "adopt" } })} />
        <Item label="Notifications" right={p.push} onClick={() => p.replace({ ...p.nav, sheet: { kind: "push" } })} />
        <Item label="Show tool details" sub="Every tool group open, every machine line in full" right={toolDetails() ? "on" : "off"} onClick={after(() => setToolDetails(!toolDetails()))} />
        {(["system", "light", "dark"] as Theme[]).map((th) => (
          <Item label={`Theme: ${th === "system" ? "auto" : th}`} right={p.theme === th ? "✓" : ""} onClick={() => p.setTheme(th)} />
        ))}
      </MSheet>
    );
  }
  if (sheet.kind === "filter") {
    return (
      <MSheet onClose={close} label="Filter by project">
        <div class="m-shh">Show one project</div>
        <ProjectList
          projects={p.projects.filter((x) => (x.threads ?? 0) > 0 || x.id === p.projectFilter)}
          selectedId={p.projectFilter}
          allLabel="All projects"
          onPick={(pr) => (p.onProjectFilter(pr?.id ?? null), close())}
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
            projects={p.projects}
            threads={ids.map((id) => p.threads[id]).filter(Boolean)}
            onPick={async (pr) => {
              const e = await p.projectActions.move(ids, pr?.id ?? null);
              if (!e) close();
            }}
            onCreate={async (name, folder) => {
              const r = await p.projectActions.create(name, folder, ids);
              if (!r.error) close();
              return r.error;
            }}
            onEdit={(pr) => pr.id && p.replace({ screen: { name: "project", id: pr.id }, sheet: null })}
          />
        </div>
      </MSheet>
    );
  }
  if (sheet.kind === "actions" && ids.length > 1) {
    return (
      <MSheet onClose={close} label="Selected threads">
        <div class="m-shh">{ids.length} selected</div>
        <Item label={`Archive ${ids.length}`} icon={I.close} onClick={after(() => (p.actions.archive(ids), p.setSelecting(false), p.setPicked(new Set())))} />
      </MSheet>
    );
  }
  if (!t) return null;
  const reason = (a: string) => unavailableReason(t, a);
  const avail = swipeAvail(t);
  if (sheet.kind === "actions" || sheet.kind === "thread-menu") {
    const peek = sheet.kind === "actions";
    const d = t.decisions?.[0];
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
        {peek && <Item label="Open thread" icon={I.right} right="tap" onClick={() => p.replace({ screen: { name: "thread", id: t.id }, sheet: null })} />}
        {t.attention.state === "failed" && <Item label="Retry" sub={reason("thread.retry")} disabled={!!reason("thread.retry")} onClick={after(() => p.actions.retry(t.id))} />}
        <Item
          label={avail.right.ok ? settleLabel(t) : "Settle"}
          icon={<StateIcon state="settled" size={16} />}
          sub={avail.right.ok ? undefined : t.attention.state === "asking" ? "Answer the question first" : reason("thread.settle")}
          disabled={!avail.right.ok}
          right={peek ? "swipe →" : undefined}
          onClick={after(() => p.actions.settle([t.id]))}
        />
        <Item
          label="Snooze…"
          icon={<StateIcon state="snoozed" size={16} />}
          sub={reason("thread.snooze")}
          disabled={!!reason("thread.snooze")}
          right={peek ? "← swipe" : undefined}
          onClick={() => p.replace({ ...p.nav, sheet: { kind: "snooze", ids: [t.id] } })}
        />
        <Item label="Project…" icon={<ProjectIcon project={projectOf(t, p.projects)} name={projectName(t, p.projects)} size={18} />} right={projectName(t, p.projects)} onClick={() => p.replace({ ...p.nav, sheet: { kind: "project", ids: [t.id] } })} />
        <Item label="Live view" sub="The agent keeps its state pinned on top" right={t.liveView ? "on" : "off"} disabled={!!reason("thread.setView")} onClick={after(() => p.actions.setView(t.id, !t.liveView))} />
        <Item label="Hand over…" sub={reason("thread.handover") ?? "Continue this thread with another profile"} disabled={!!reason("thread.handover")} onClick={() => p.replace({ ...p.nav, sheet: { kind: "handover", ids: [t.id] } })} />
        <Item label="Mark unseen" icon={<StateIcon state="seen" size={16} />} sub={reason("thread.markUnseen")} disabled={!!reason("thread.markUnseen")} onClick={after(() => p.actions.markUnseen(t.id))} />
        {peek && <Item label="Select" icon={<i class="m-ck" />} onClick={after(() => (p.setSelecting(true), p.setPicked(new Set([t.id]))))} />}
        {!peek && <Item label="Details" icon={I.right} onClick={() => p.replace({ ...p.nav, sheet: { kind: "details", ids: [t.id] } })} />}
        <Item label="Archive" icon={I.close} sub={reason("thread.archive")} disabled={!!reason("thread.archive")} onClick={after(() => p.actions.archive([t.id]))} />
      </MSheet>
    );
  }
  if (sheet.kind === "handover" && t) {
    return (
      <MSheet onClose={close} label="Hand over">
        <div class="m-shh">Hand over</div>
        <HandoverForm phone t={t} profiles={p.profiles} onClose={close} onConfirm={(pr, m) => p.actions.handover(t.id, pr, m)} />
      </MSheet>
    );
  }
  if (sheet.kind === "details") {
    const prof = p.profiles.find((x) => x.id === t.profile);
    const canModel = (prof?.models.length ?? 0) > 1 && !reason("thread.setModel");
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
              const pr = projectOf(t, p.projects);
              return (
                <span class="m-nopj">
                  <button class="m-plink" onClick={() => p.replace({ ...p.nav, sheet: { kind: "project", ids: [t.id] } })} aria-label={`Project: ${projectName(t, p.projects)}. Change`}>
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
              <select class="m-msel" value={t.model} onChange={(e) => p.actions.setModel(t.id, (e.target as HTMLSelectElement).value)}>
                {prof!.models.map((m) => (
                  <option value={m}>{shortModel(m)}</option>
                ))}
              </select>
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
                <select class="m-msel" value={startEffort(prof, t.model, t.effort)} onChange={(e) => p.actions.setEffort(t.id, (e.target as HTMLSelectElement).value)}>
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

function SnoozeSheet({ ids, title, onClose, onPick }: { ids: string[]; title?: string; onClose(): void; onPick(d: Date): void }) {
  const presets = phoneSnoozePresets(new Date());
  const [custom, setCustom] = useState(false);
  const [val, setVal] = useState("");
  return (
    <MSheet onClose={onClose} label="Snooze">
      <div class="m-shh ell">{ids.length > 1 ? `Snooze ${ids.length} threads` : `Snooze “${title ?? ""}”`}</div>
      {presets.map((s) => (
        <Item label={s.label} icon={<StateIcon state="snoozed" size={16} />} right={s.when} onClick={() => onPick(s.until)} />
      ))}
      {custom ? (
        <div class="m-custom">
          <input type="datetime-local" value={val} onInput={(e) => setVal((e.target as HTMLInputElement).value)} aria-label="Snooze until" />
          <button class="m-bt p" disabled={!val || new Date(val).getTime() <= Date.now()} onClick={() => onPick(new Date(val))}>
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
function MProjectChip({ t, projects, onClick }: { t: ThreadDTO; projects: Project[]; onClick(): void }) {
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

/** The project screen (V0.55): name, folder, quiet hours, colour, icon and defaults. */
function MProject(p: PhoneProps & { id: string }) {
  const pr = p.projects.find((x) => x.id === p.id);
  return (
    <div class="m-screen m-project">
      <MHeader onBack={p.back} title="Project" />
      <div class="m-nbody">
        {pr ? (
          <ProjectEditor
            project={pr}
            profiles={p.profiles}
            now={p.now}
            onSave={(patch) => p.projectActions.update(pr.id!, patch)}
            onSetFolder={(v) => p.projectActions.setFolder(pr.id!, v)}
            onDelete={async () => {
              const e = await p.projectActions.delete(pr.id!);
              if (!e) p.back();
              return e;
            }}
          />
        ) : (
          <div class="m-empty">{p.projects.length ? "This project is not here anymore." : "Loading…"}</div>
        )}
      </div>
    </div>
  );
}
