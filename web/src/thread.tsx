// Thread pane: header with chips and tabs, conversation, docked decision card and composer
// (the rail direction (decision 0006)).

import { ArrivedBar } from "./arrived-bar.tsx";
import type { AnswerMove } from "./arrived.ts";
import { PIN_ICON } from "./row-ui.tsx";
import { copySource, copyToClipboard } from "./copy.ts";
import { useDraft } from "./drafts.ts";
import { AgentStatus } from "./agent-status.tsx";
import { describeApproval, type ApprovalField, type ApprovalText } from "./approvals.ts";
import { describeEffect, type EffectText } from "./effects.ts";
import { Boundary, CrashProbe, crashProbe } from "./boundary.tsx";
import { useApi } from "./api-context.ts";
import { useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import { createContext } from "preact";
import { SlideMark, reducedMotion } from "./motion-ui.tsx";
import type { Decision, FyrEvent, PullRequest, TaskDTO, Thread } from "./types.ts";
import { agentPrompt, buildConversation, findCall, pace, type Entry, deliveryLabel, capCalls, cutOutput, friendlyName, fullInput, groupStats, groupSummary, groupTone, previewsOf, segments, type Item, type ToolCall, type SubStep, runLabel, fullOutputOf } from "./conversation.ts";
import { renderMarkdown } from "./markdown.ts";
import { currentDecision } from "./decode.ts";
import { StateIcon, stateClass } from "./icons.tsx";
import { stateLabel } from "./rows.ts";
import { clockTime, duration, relTime } from "./format.ts";
import { AgentPill, threadAgent } from "./agent-ui.tsx";
import { dividerLine, rungDone } from "./handover.ts";
import { HandoverLinkBar } from "./handover-ui.tsx";
import { ref } from "./hostfeed.ts";
import { back, pick, progressText, questionsOf, goTo, settledOf, skip, type CardState, nextOpen, submitState } from "./question.ts";
import { isAvailable, unavailableReason } from "./store.ts";
import { deskThreadMenu, type ThreadAction } from "./actions.ts";
import { hostChip, offlineLine, profilesOf, threadProvider, type HostProfiles } from "./hosts.ts";
import type { HostId } from "./hostid.ts";
import { ThreadQuotaChip } from "./quota-ui.tsx";
import { ProfilesContext, useHosts } from "./host-pick-ui.tsx";
import { HostMark } from "./host-mark-ui.tsx";
import { onToolDetails, toolDetails } from "./prefs.ts";
import { FromThreadMessage, ProjectIcon, StartedBy, useDirectory } from "./project-ui.tsx";
import { colorVar, projectName, projectOf } from "./projects.ts";
import { ChipStrip, ClipPicker, SentAttachments, useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { SlashList, useSlash } from "./slash-ui.tsx";
import type { ComponentChildren } from "preact";
import { Filmstrip, PreviewCardView, openMarkdownImage } from "./preview-ui.tsx";
import { LiveViewPanel } from "./view-ui.tsx";
import { rememberTab } from "./desk-history.ts";
import { tabLabel, composerAnswers, defaultTab, showsDecision, threadTabs } from "./view.ts";
import type { PreviewCard } from "./previews.ts";
import { agentsIcon, agentsSummary, endedLabel, groupTasks, taskLine, taskStats } from "./tasks.ts";
import { ChangesView } from "./changes-ui.tsx";
import { CiNotesScope } from "./ci-notes-ui.tsx";
import { ChangeList, useViewAction } from "./offers-ui.tsx";
import { DockRun, NextSteps, ProseCommand, RunCardAt, RunCards, showCard } from "./runcard-ui.tsx";
import { cardShows, cardsKey, dockRun, placeCards, requests, splitProse, waitingRuns } from "./runcard.ts";
import { changeChip, headerPrs, prForEffect, settleLine, threadPrs } from "./offers.ts";
import { WakeChip } from "./wake-ui.tsx";
import { hasMultipleRepos, pickPr, prChip, shortRepo } from "./pr.ts";
import { PrView } from "./pr-ui.tsx";
import { UndeclaredStrip } from "./undeclared-ui.tsx";
import { LeftoverStrip } from "./leftovers-ui.tsx";
import { UndIcon } from "./undeclared-ctx.tsx";

export interface AnswerField {
  index: number;
  text: string;
  setText(text: string): void;
  /** Enter: take the text as this question's answer, then submit or move on */
  submit(): void;
}

export interface ThreadPaneProps {
  /** U15: send the answers of the open question card */
  onSubmitAsk?(d: Decision): void;
  /** V0.ask-cancel: close the open question card without an answer */
  onCancelAsk?(d: Decision): void;
  thread: Thread;
  mode: "open" | "peek";
  events: FyrEvent[] | undefined;
  /** slice 2b: each host's profiles; the thread's provider is its own host's */
  profiles: HostProfiles;
  card: CardState | null;
  answerField: AnswerField | null;
  onCard(cs: CardState): void;
  onAllow(d: Decision, allow: boolean): void;
  onRun(action: string, args?: Record<string, unknown>): void;
  onSnooze(): void;
  onSend(args: SendArgs): Promise<boolean>;
  onAtEnd(atEnd: boolean): void;
  onFocusZone(): void;
  /** Loads older events; absent when the start of the thread is loaded. */
  onEarlier?: () => void;
  /** server-adjusted clock, for elapsed times */
  now: number;
  /** Back to the inbox: the desktop closes the thread pane with it (open or peek). */
  onBack?: () => void;
  /** V0.one-action-list: runs an entry of the ⋯ menu (actions.ts, the same dispatch as the row menu); absent: no ⋯ menu. */
  onAction?(a: ThreadAction): void;
  /** V0.arrived-from: an answer moved the owner here; the bar names the thread answered and leads back to it */
  arrived?: { a: AnswerMove; onBack(): void; onDismiss(): void };
}

/** A deep link `/?open=<id>&tab=pr` opens that tab on the desktop too. */
function urlTab(id: string): string | undefined {
  if (typeof location === "undefined") return undefined;
  const q = new URLSearchParams(location.search);
  const tab = q.get("tab");
  return q.get("open") === id && tab && ["view", "conv", "changes", "pr"].includes(tab) ? tab : undefined;
}

export function ThreadPane(p: ThreadPaneProps) {
  const t = p.thread;
  // #41: the tab is derived as on the phone: the one picked for this thread (or the deep link's), made valid by
  // defaultTab (no View without a view, no PR tab without a PR).
  const [pick, setPick] = useState<{ id: string; tab: string } | null>(null);
  const [selectedPr, setSelectedPr] = useState<{ id: string; url: string } | null>(null);
  const tab = defaultTab(t, pick?.id === t.id ? pick.tab : urlTab(t.id));
  const setTab = (tab: string) => {
    setPick({ id: t.id, tab });
    rememberTab(t.id, tab); // V0.145: Back to this thread shows this tab
  };
  useEffect(() => {
    setPick(null);
    setSelectedPr(null);
  }, [t.id]); // another thread opens on its own default
  const allPrs = threadPrs(t);
  const openPrs = headerPrs(t);
  const activePr = pickPr(allPrs, selectedPr?.id === t.id ? selectedPr.url : undefined, t.pr?.url);
  const openPr = (url: string) => {
    setSelectedPr({ id: t.id, url });
    setTab("pr");
  };
  const hs = useHosts();
  // V0.139: the thread's provider is its host's (the same id on another host is another provider).
  const profile = profilesOf(t, p.profiles).find((x) => x.id === t.profile);
  const provider = threadProvider(t, p.profiles);
  const hc = hostChip(t.hostId, hs.hosts, hs.now);
  const decision = currentDecision(t);
  const dock = dockRun(t);
  const st = t.attention.state;
  const settleReason = unavailableReason(t, "thread.settle");
  const snoozeReason = unavailableReason(t, "thread.snooze");
  return (
    <section class="th" onMouseDown={p.onFocusZone}>
      <div class="thh">
        <div class="tr">
          {p.onBack && (
            <button class="btn back" onClick={p.onBack} aria-label="Back to the inbox">
              ‹
            </button>
          )}
          <div class="tt" title={t.title}>
            {t.title || "Untitled"}
          </div>
          <div class="acts">
            {st === "failed" && (
              <button class="btn" disabled={!isAvailable(t, "thread.retry")} title={unavailableReason(t, "thread.retry") ?? "Retry (r)"} onClick={() => p.onRun("thread.retry")}>
                Retry <kbd>r</kbd>
              </button>
            )}
            {(() => {
              // V0.104: the owner's pin; the phone has it in the thread's ⋯ sheet.
              const act = t.pinnedAt ? "thread.unpin" : "thread.pin";
              const why = unavailableReason(t, act);
              return (
                <button class={`btn pinb${t.pinnedAt ? " on" : ""}`} aria-pressed={!!t.pinnedAt} aria-label={t.pinnedAt ? "Unpin" : "Pin"} disabled={!!why} title={why ?? (t.pinnedAt ? "Unpin" : "Pin: keep it on top of the list")} onClick={() => p.onRun(act)}>
                  {PIN_ICON}
                </button>
              );
            })()}
            <button class="btn" disabled={!!snoozeReason} title={snoozeReason ?? "Snooze (s)"} onClick={p.onSnooze}>
              Snooze <kbd>s</kbd>
            </button>
            <button class="btn" disabled={!!settleReason} title={settleReason ?? (t.running ? "Settle now and stop the run (e)" : "Settle now (e)")} onClick={() => p.onRun("thread.settle")}>
              Settle <kbd>e</kbd>
            </button>
            {/* V0.one-action-list: the buttons above are DESK_HEADER_BUTTONS; the ⋯ menu holds the rest of the thread's actions */}
            {p.onAction && <ThreadMenu items={deskThreadMenu(t)} onPick={p.onAction} />}
          </div>
        </div>
        <div class="chips">
          <span class={`chip st ${stateClass(st)}`} title={t.attention.reason || stateLabel(st)}>
            <span class={t.running ? "spin" : "icw"}>
              <StateIcon state={st} size={14} />
            </span>
            <span style="color:var(--ink)">{stateLabel(st)}</span>
          </span>
          {p.mode === "peek" && (
            <span class="chip peek">
              <span>Peek · ⏎ opens</span>
            </span>
          )}
          <HeaderProjectChip t={t} profileName={profile?.name} />
          {t.parent && (
            <span class="chip fromc">
              <StartedBy parentId={t.parent} />
            </span>
          )}
          {t.hostOffline ? (
            <span class="chip m hostc" title="The host is offline. This is the last copy Fyr saw; what you send waits until it is back.">
              {hc && <HostMark name={hc.name} />}
              <span>{offlineLine(t)}</span>
            </span>
          ) : (
            // V0.139: the host it runs on, as the phone's header shows it; V0.170: with its mark.
            hc && (
              <span class="chip m hostc" title={`Runs on ${hc.name}`}>
                <HostMark name={hc.name} />
                <span>{hc.name}</span>
              </span>
            )
          )}
          {t.branch && (
            <span class="chip m brn" title={t.branch}>
              <span>{t.branch}</span>
            </span>
          )}
          {/* V0.agent-pill: provider, model and effort in one pill; the model and the effort pick in place */}
          <AgentPill size="chip" segs={threadAgent(t, profile, provider, unavailableReason(t, "thread.setModel"), (a) => p.onRun("thread.setModel", a))} />
          {/* V0.quota-in-thread, V0.quota-reset-wake: what is left on the account this thread uses, where the owner works; click to arm a wake when a window resets */}
          <ThreadQuotaChip
            profile={t.profile}
            host={t.host}
            name={provider.name}
            wakes={t.wakes}
            wakeDisabled={unavailableReason(t, "thread.wakeOnQuota") ?? undefined}
            onWake={(window) => p.onRun("thread.wakeOnQuota", { window })}
            onCancelWake={(rule) => p.onRun("thread.cancelWake", { rule })}
          />
          {/* V0.98 (6), V0.multi-pr: every open change's PR as a chip; clicking jumps to that PR in the PR tab. */}
          {(() => {
            const multiRepo = hasMultipleRepos(openPrs);
            return openPrs.map((pr) => (
              <PrChip
                key={pr.url}
                pr={pr}
                repoPrefix={multiRepo ? shortRepo(pr.repo) : ""}
                active={tab === "pr" && activePr?.url === pr.url}
                onOpen={() => openPr(pr.url)}
                onDismiss={isAvailable(t, "thread.dismissPR") && pr.state !== "OPEN" ? () => p.onRun("thread.dismissPR", { pr: pr.url }) : undefined}
              />
            ));
          })()}
          {(() => {
            const c = changeChip(t); // V0.59 (8)
            return c ? (
              <span class={`chip prc ${c.cls}`} title={c.title} onClick={() => setTab("changes")}>
                <span>{c.text}</span>
              </span>
            ) : null;
          })()}
          {(() => {
            const l = settleLine(t);
            return l ? (
              <span class="chip">
                <span>{l}</span>
              </span>
            ) : null;
          })()}
          {/* V0.wakeups-via-watch: the agent's wake-up, in the settle chip's slot and style; a press cancels it */}
          <WakeChip t={t} now={p.now} cls="chip" onCancel={(rule) => p.onRun("thread.cancelWake", { rule })} />
        </div>
        <div class="tabs" role="tablist">
          <SlideMark under at={tab} />
          {threadTabs(t).map((k) => (
            <span class={tab === k ? "on" : ""} role="tab" onClick={() => setTab(k)}>
              {tabLabel(t, k)}
            </span>
          ))}
        </div>
      </div>
      {p.arrived && <ArrivedBar a={p.arrived.a} onBack={p.arrived.onBack} onDismiss={p.arrived.onDismiss} />}
      <HandoverLinkBar t={t} />
      <UndeclaredStrip id={t.id} />
      <LeftoverStrip id={t.id} now={p.now} />
      {t.tasks.length > 0 && <AgentsStrip key={t.id} tasks={t.tasks} now={p.now} events={p.events} cwd={t.cwd} />}
      <ThreadTabBody
        t={t}
        tab={tab}
        setTab={setTab}
        prs={allPrs}
        activePr={activePr}
        onSelectPr={(url) => setSelectedPr({ id: t.id, url })}
        events={p.events}
        now={p.now}
        onAtEnd={p.onAtEnd}
        onEarlier={p.onEarlier}
        run={p.onRun}
        send={p.onSend}
      />
      {/* V0.next-step-dock: what waits on the owner stands in one dock; a question or approval first, then a Run card */}
      {showsDecision(tab) && !decision && dock && <DockRun o={dock.o} t={t} host={t.host || undefined} more={dock.waiting - 1} onRun={p.onRun} onShow={(id) => showCard(id, tab, setTab)} />}
      {showsDecision(tab) && decision && <DecisionCard d={decision} more={t.decisions.length - 1 + waitingRuns(t)} card={p.card} onCard={p.onCard} onAllow={p.onAllow} onSubmitAsk={p.onSubmitAsk} onCancelAsk={p.onCancelAsk} pr={prForEffect(t, decision)} cwd={t.cwd} host={t.host || undefined} />}
      <Composer key={t.id} t={t} now={p.now} onSend={(a) => sendFrom(tab, a, p.onSend, setTab)} onInterrupt={() => p.onRun("thread.interrupt")} onRestart={() => p.onRun("thread.restart")} answer={composerAnswers(tab) ? p.answerField : null} />
    </section>
  );
}

export interface ThreadTabBodyProps {
  t: Thread;
  /** from defaultTab */
  tab: string;
  setTab(tab: string): void;
  /** V0.multi-pr: non-dismissed PRs of the thread and the one shown in the PR tab */
  prs?: PullRequest[];
  activePr?: PullRequest | null;
  onSelectPr?(url: string): void;
  events: FyrEvent[] | undefined;
  now: number;
  onAtEnd(atEnd: boolean): void;
  /** Loads older events; absent when the start of the thread is loaded. */
  onEarlier?: () => void;
  /** a thread command on this thread */
  run(action: string, args?: Record<string, unknown>): unknown;
  send(args: SendArgs): Promise<boolean>;
  phone?: boolean;
}

/** #41: what a thread tab shows, the same on desktop and phone: View (V0.37), Conversation with its filmstrip
 *  (V0.19), Changes (V0.25, V0.59) and PR (V0.43, V0.multi-pr). */
export function ThreadTabBody({ t, tab, setTab, prs, activePr, onSelectPr, events, now, onAtEnd, onEarlier, run, send, phone }: ThreadTabBodyProps) {
  const api = useApi();
  const previews = useMemo(() => previewsOf(events ?? [], t.hostId, api), [events, t.hostId]);
  const viewAction = useViewAction(run, t.cwd);
  const prList = prs ?? threadPrs(t);
  const currentPr = activePr ?? pickPr(prList, undefined, t.pr?.url);
  return (
    <>
      {tab === "view" && <LiveViewPanel full t={t} now={now} onPress={(text) => send({ text, from: "view" })} onAction={viewAction} />}
      {tab === "conv" && (
        <Conversation
          strip={<Filmstrip cards={previews} collapsible={phone} />}
          outbox={t.outbox.map((o) => o.id)}
          events={events}
          onAtEnd={onAtEnd}
          threadId={t.id}
          onEarlier={onEarlier}
          cwd={t.cwd}
          host={t.hostId}
          delivery={{ cancel: async (messageId) => run("thread.cancelMessage", { messageId }), resend: send }}
          cards={{ t, onRun: run }}
        />
      )}
      {tab === "changes" && (
        <div class="chtab">
          <ChangeList t={t} onRun={run} />
          <CiNotesScope changes={t.changes}>
            <ChangesView host={t.hostId} threadId={t.id} changes={t.changes} />
          </CiNotesScope>
        </div>
      )}
      {tab === "pr" && (
        <PrTab
          t={t}
          prs={prList}
          pr={currentPr}
          onSelect={(url) => onSelectPr?.(url)}
          now={now}
          phone={phone}
          onSend={(text) => send({ text })}
          onDismiss={(url) => run("thread.dismissPR", { pr: url })}
          onChanges={() => setTab("changes")}
        />
      )}
    </>
  );
}

/** The thread ⋯ menu (V0.26): opens on click or Enter, arrows move, Esc closes and returns focus. */
function ThreadMenu({ items, onPick }: { items: ThreadAction[]; onPick(a: ThreadAction): void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const btn = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!open) return;
    ref.current?.querySelector<HTMLElement>("[role=menuitem]")?.focus();
    const away = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", away);
    return () => document.removeEventListener("mousedown", away);
  }, [open]);
  const run = (a: ThreadAction) => () => {
    setOpen(false);
    onPick(a);
  };
  const onKey = (e: KeyboardEvent) => {
    // The menu owns its keys: the inbox shortcuts must not act while it has focus.
    e.stopPropagation();
    if (!open) return;
    const els = [...(ref.current?.querySelectorAll<HTMLElement>("[role=menuitem]") ?? [])];
    const i = els.indexOf(document.activeElement as HTMLElement);
    if (e.key === "Escape") {
      e.preventDefault();
      setOpen(false);
      btn.current?.focus();
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const n = els.length;
      els[(i + (e.key === "ArrowDown" ? 1 : n - 1) + n) % n]?.focus();
    } else if (e.key === "Tab") setOpen(false);
  };
  return (
    <div class="tmenu" ref={ref} onKeyDown={onKey}>
      <button ref={btn} class="btn tmenu-b" aria-haspopup="menu" aria-expanded={open} aria-label="More actions" title="More actions" onClick={() => setOpen(!open)}>
        ⋯
      </button>
      {open && (
        <div class="tmenu-l" role="menu" aria-label="Thread actions">
          {items.map((a) => (
            <button key={a.id} class="mi" role="menuitem" disabled={!!a.disabled} title={a.disabled ?? a.hint} onClick={run(a)}>
              {a.key ? <kbd>{a.key}</kbd> : <span />}
              <span class="ell">{a.label}</span>
              <span />
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

/** The project chip (V0.30): icon and name in the project colour, or "No project"; a click opens the picker. */
function HeaderProjectChip({ t, profileName }: { t: Thread; profileName?: string }) {
  const dir = useDirectory();
  const p = projectOf(t, dir.projects);
  const where = `${t.cwd}${profileName ? ` · ${profileName}` : ""}`;
  return (
    <button class={`chip pj${p ? " proj" : " nopj"}`} style={p ? { "--pc": colorVar(p.color, p.name) } : undefined} title={`${where} · Project… (p)`} aria-label={`Project: ${projectName(t, dir.projects)}. Change`} onClick={() => dir.pickProject(t.id)}>
      {p && <ProjectIcon project={p} name={p.name} size={16} />}
      <span>{projectName(t, dir.projects)}</span>
    </button>
  );
}

/** The PR tab (V0.43, V0.multi-pr). */
function PrTab({ t, prs, pr, onSelect, now, onSend, onDismiss, onChanges, phone }: { t: Thread; prs: PullRequest[]; pr: PullRequest | null; onSelect(url: string): void; now: number; onSend(text: string): Promise<boolean>; onDismiss(url: string): void; onChanges(): void; phone?: boolean }) {
  if (!pr) return <div class="pane-note">No pull request is linked to this thread.</div>;
  return <PrView key={t.id} t={t} prs={prs} pr={pr} onSelect={onSelect} now={now} onSend={onSend} onDismiss={onDismiss} onChanges={onChanges} phone={phone} />;
}

function PrChip({ pr, repoPrefix, active, onOpen, onDismiss }: { pr: PullRequest; repoPrefix?: string; active?: boolean; onOpen?: () => void; onDismiss?: () => void }) {
  const l = prChip(pr);
  const done = pr.state === "MERGED" || pr.state === "CLOSED" || pr.state === "MISSING";
  const label = repoPrefix ? `${repoPrefix} ${l.text}` : l.text;
  return (
    <span class="prw">
      <a
        class={`chip pr ${l.cls}${active ? " on" : ""}`}
        href={pr.url}
        target="_blank"
        rel="noopener noreferrer"
        title={l.title}
        onClick={(e) => {
          if (onOpen && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey && e.button === 0) {
            e.preventDefault();
            onOpen();
          }
        }}
      >
        <span>{label}</span>
      </a>
      {done && onDismiss && (
        <button class="chip prx" aria-label="Dismiss PR" title="Dismiss PR: the thread goes on" onClick={onDismiss}>
          ×
        </button>
      )}
    </span>
  );
}

// ---- agents strip (SPEC V0.16) ----

const TASK_ICON: Record<string, "working" | "settled" | "failed" | "seen"> = {
  running: "working",
  completed: "settled",
  failed: "failed",
  stopped: "seen",
};

/** V0.subagent-timeline: a row whose Agent call is in the loaded conversation opens that subagent's timeline under it. */
function TaskRow({ t, now, call, open, onOpen }: { t: TaskDTO; now: number; call?: ToolCall; open?: boolean; onOpen?: () => void }) {
  const line = taskLine(t);
  const row = (
    <div
      class={`tk ${t.status}${call ? " click" : ""}`}
      title={`${t.description}\n${line}`}
      {...(call ? { role: "button", tabIndex: 0, "aria-expanded": !!open, onClick: onOpen, onKeyDown: (e: KeyboardEvent) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), onOpen?.()) } : {})}
    >
      <StateIcon state={TASK_ICON[t.status] ?? "seen"} size={14} />
      <span class="tkt">
        <span class="tkd ell">{t.description || "Agent"}</span>
        <span class="tkl ell">{line}</span>
      </span>
      <span class="tks">{taskStats(t)}</span>
      <span class="tka">{relTime(t.startedAt, now)}</span>
    </div>
  );
  if (!call || !open) return row;
  return (
    <>
      {row}
      <div class="sub tksub">
        <SubTimeline c={call} />
      </div>
    </>
  );
}

/** Running tasks always show; ended ones fold behind "N finished" ("1 finished, 3 stopped"). On a phone the whole strip is one line until opened. */
export function AgentsStrip({ tasks, now, events, cwd }: { tasks: TaskDTO[]; now: number; events?: FyrEvent[]; cwd?: string }) {
  const [showEnded, setShowEnded] = useState(false);
  const [open, setOpen] = useState(false);
  const [opened, setOpened] = useState<string | null>(null);
  const g = groupTasks(tasks);
  // V0.subagent-timeline: the Agent calls the rows can open, from the loaded events (an earlier page may not have them).
  const wanted = tasks.map((t) => t.toolUseId ?? "").join(",");
  const calls = useMemo(() => {
    const m = new Map<string, ToolCall>();
    if (!wanted.replace(/,/g, "")) return m;
    const items = buildConversation(events ?? [], { cwd });
    for (const id of wanted.split(",")) {
      const c = id ? findCall(items, id) : undefined;
      if (c) m.set(id, c);
    }
    return m;
  }, [events, wanted, cwd]);
  const row = (t: TaskDTO) => {
    const call = t.toolUseId ? calls.get(t.toolUseId) : undefined;
    return <TaskRow t={t} now={now} key={t.id} call={call} open={opened === t.id} onOpen={() => setOpened(opened === t.id ? null : t.id)} />;
  };
  return (
    <div class={`agents${open ? " open" : ""}`}>
      <button class="agsum ell" onClick={() => setOpen(!open)} aria-expanded={open}>
        <StateIcon state={agentsIcon(tasks)} size={14} />
        <span class="ell">{agentsSummary(tasks)}</span>
        <span class="chev">{open ? "▾" : "▸"}</span>
      </button>
      <div class={`agrows${opened ? " deep" : ""}`}>
        {g.running.map(row)}
        {g.ended.length > 0 && (
          <button class="tkmore" onClick={() => setShowEnded(!showEnded)} aria-expanded={showEnded}>
            {endedLabel(g.ended)} {showEnded ? "▾" : "▸"}
          </button>
        )}
        {showEnded && g.ended.map(row)}
      </div>
    </div>
  );
}

// ---- conversation ----

/** "Show tool details" (V0.21): every group open and every machine line in full. */
export function useToolDetails(): boolean {
  const [on, setOn] = useState(toolDetails());
  useEffect(() => onToolDetails(setOn), []);
  return on;
}

/** SPEC 0.5 V0.33: what a message's Cancel and Send again do. */
export interface DeliveryActions {
  cancel(messageId: string): Promise<unknown>;
  resend(args: SendArgs): Promise<unknown>;
}
const DeliveryCtx = createContext<DeliveryActions | null>(null);

/** V0.54: the thread's host: its attachments and previews load from there. */
const HostContext = createContext<HostId | undefined>(undefined);

export function Conversation({ events, onAtEnd, threadId, onEarlier, cwd, delivery, outbox, host, strip, cards }: { events: FyrEvent[] | undefined; outbox?: string[]; onAtEnd(b: boolean): void; threadId: string; onEarlier?: () => void; cwd?: string; delivery?: DeliveryActions; host?: HostId; strip?: ComponentChildren; cards?: { t: Thread; onRun(action: string, args?: Record<string, unknown>): unknown } }) {
  const details = useToolDetails();
  const api = useApi();
  const outKey = outbox ? outbox.join(",") : null;
  const all = useMemo(() => buildConversation(events ?? [], { details, cwd, host, urls: api, outbox: outKey === null ? undefined : outKey ? outKey.split(",") : [] }), [events, details, cwd, host, outKey]);
  // V0.33: messages the agent has not read stand after "Agent is here".
  // V0.run-cards-in-conversation: a Run card stands where the agent asked, on desktop and phone.
  const openCards = cards ? requests(cards.t).filter((o) => cardShows(o, cards.t.runs)).map((o) => o.id).join("\n") : "";
  const [items, pending, looseCards] = useMemo(() => {
    const placed = placeCards(all.filter((i) => !(i.kind === "user" && i.pending)), openCards ? openCards.split("\n") : []);
    return [placed.items, all.filter((i) => i.kind === "user" && i.pending), placed.loose] as const;
  }, [all, openCards]);
  const followCards = cards ? cardsKey(cards.t) : "";
  const previews = useMemo(() => items.flatMap((it) => (it.kind === "preview" ? [it.card] : [])), [items]);
  // V0.41: outcome at full weight, the work quiet and folded.
  const entries = useMemo(() => pace(items, { details }), [items, details]);
  const ref = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  // V0.scroll-to-bottom: away from the end, an arrow takes you back there (desktop and phone).
  const [away, setAway] = useState(false);

  const check = () => {
    const el = ref.current;
    if (!el) return;
    const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    stick.current = atEnd;
    setAway(!atEnd);
    onAtEnd(atEnd);
  };

  const toEnd = () => {
    const el = ref.current;
    if (!el) return;
    stick.current = true;
    setAway(false);
    el.scrollTo({ top: el.scrollHeight, behavior: reducedMotion() ? "auto" : "smooth" });
  };

  useLayoutEffect(() => {
    stick.current = true;
    setAway(false);
  }, [threadId]);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    // V0.38: at the bottom, follow new content without reading the layout first (a read forces a
    // layout of the whole conversation on every update); away from the bottom, nothing moves.
    if (stick.current) {
      el.scrollTop = 1e9;
      setAway(false);
      onAtEnd(true);
    }
  }, [all, threadId, followCards]);

  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => {
      if (stick.current) el.scrollTop = el.scrollHeight;
      check();
    });
    ro.observe(el);
    // The content grows after the first paint (late markdown, images, folded work): follow it while at the bottom.
    if (el.firstElementChild) ro.observe(el.firstElementChild);
    return () => ro.disconnect();
  }, []);

  // V0.34: Copy on code blocks inside rendered markdown (their buttons are plain HTML).
  const onCodeCopy = async (e: MouseEvent) => {
    const b = (e.target as Element | null)?.closest?.("[data-cpy]") as HTMLButtonElement | null;
    if (!b) return;
    e.stopPropagation();
    const code = b.parentElement?.querySelector("pre")?.textContent ?? "";
    if (await copyToClipboard(copySource({ kind: "block", text: code }))) {
      b.textContent = "Copied";
      setTimeout(() => (b.textContent = "Copy"), 1200);
    }
  };
  return (
    <ThreadIdContext.Provider value={threadId}>
    <HostContext.Provider value={host}>
    <RunCards.Provider value={cards ?? null}>
    <div class="convo" ref={ref} onScroll={check} onClick={onCodeCopy}>
      <div class="convo-in">
        {/* V0.75 (2), #64: the filmstrip scrolls with the conversation, so a thread without previews has no empty band */}
        {strip && <div class="cv-strip">{strip}</div>}
        {events === undefined && <div class="sys">Loading…</div>}
        {events !== undefined && items.length === 0 && pending.length === 0 && <div class="sys">No messages yet.</div>}
        {onEarlier && events && events.length > 0 && (
          <button class="btn ghost earlier" onClick={onEarlier}>
            Show earlier messages
          </button>
        )}
        {crashProbe() && (
          <Boundary name="This message">
            <CrashProbe />
          </Boundary>
        )}
        <DeliveryCtx.Provider value={delivery ?? null}>
          {entries.map((en) => (
            <Boundary key={en.key} name="This message">
              <EntryView en={en} previews={previews} />
            </Boundary>
          ))}
          {looseCards.map((id) => (
            <Boundary key={`card-${id}`} name="This command">
              <div class="cv out">
                <RunCardAt id={id} />
              </div>
            </Boundary>
          ))}
          {/* V0.next-step-chips: the optional next steps, under the newest message */}
          <Boundary name="The next steps">
            <NextSteps />
          </Boundary>
          {/* V0.message-before-start: a new thread's first message waits alone (the start may wait for the machine); no line above nothing */}
          {pending.length > 0 && items.length > 0 && (
            <div class="here" role="separator" aria-label="The agent has read everything above this line">
              <span>Agent is here</span>
            </div>
          )}
          {pending.map((it) => (
            <Boundary key={it.key} name="This message">
              <div class="cv out own">
                <ItemView it={it} previews={previews} />
              </div>
            </Boundary>
          ))}
        </DeliveryCtx.Provider>
      </div>
      {away && (
        <div class="cv-end">
          <button type="button" class="cv-endb" aria-label="Scroll to the newest message" title="Scroll to the newest message" onClick={toEnd}>
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 5v14" /><path d="m19 12-7 7-7-7" /></svg>
          </button>
        </div>
      )}
    </div>
    </RunCards.Provider>
    </HostContext.Provider>
    </ThreadIdContext.Provider>
  );
}

/** V0.33: the quiet delivery word under a message, with Cancel while it waits and Send again once it is gone. */
function DeliveryLine({ it }: { it: Extract<Item, { kind: "user" }> }) {
  const acts = useContext(DeliveryCtx);
  const [busy, setBusy] = useState(false);
  const word = deliveryLabel(it);
  if (!word) return null;
  const gone = it.delivery === "cancelled" || it.delivery === "lost";
  return (
    <div class={`dl${it.from ? " from" : ""}`}>
      <span>{busy ? "Cancelling…" : word}</span>
      {it.delivery === "queued" && it.id && acts && (
        <button
          class="dlb"
          disabled={busy}
          title="Take this message back before the agent reads it"
          onClick={async () => {
            setBusy(true);
            try {
              await acts.cancel(it.id!);
            } finally {
              setBusy(false);
            }
          }}
        >
          Cancel
        </button>
      )}
      {gone && !it.from && acts && (
        <button class="dlb" onClick={() => void acts.resend(it.attachments.length ? { text: it.text, attachments: it.attachments.map((a) => a.id) } : { text: it.text })}>
          Send again
        </button>
      )}
    </div>
  );
}

/** Enter and Space activate a focused group or row (they are divs with role=button). */
function keyActivate(fn: () => void) {
  return (e: KeyboardEvent) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      e.stopPropagation();
      fn();
    }
  };
}

/** A quiet machine line: one line, the exact text one tap away. */
function QuietLine({ icon, line, full, fail, und }: { icon: "settled" | "failed" | "seen" | "working"; line: string; full?: string; fail?: boolean; und?: boolean }) {
  const [open, setOpen] = useState(false);
  const more = !!full && full.trim() !== line.trim();
  return (
    <div class={`ql${fail ? " fail" : ""}${und ? " und" : ""}`}>
      <div class={`qlh${more ? " click" : ""}`} role={more ? "button" : undefined} tabIndex={more ? 0 : undefined} aria-expanded={more ? open : undefined} onClick={more ? () => setOpen(!open) : undefined} onKeyDown={more ? keyActivate(() => setOpen(!open)) : undefined}>
        {und ? <UndIcon /> : <StateIcon state={icon} size={12} />}
        <span class="ell">{line}</span>
      </div>
      {open && full && <CopyPre cls="qlx" text={full} />}
    </div>
  );
}

/** The end of a turn: a small mark; a tap shows how long a failed turn ran. A failure keeps its text in one line.
 *  V0.quota-wait: a run the provider's quota stopped waits, so it wears the waiting mark, not the failed one. */
function RunMark({ it }: { it: Extract<Item, { kind: "run" }> }) {
  const [open, setOpen] = useState(false);
  const facts = it.durationMs !== undefined ? duration(it.durationMs) : "";
  const quota = it.failure?.class === "quota";
  const icon = quota ? "waiting" : it.failure ? "failed" : it.interrupted ? "seen" : "settled";
  const word = quota ? `Waiting for quota: ${it.failure?.message}` : it.failure ? `Failed (${it.failure.class}): ${it.failure.message}` : it.interrupted ? "Interrupted" : "Finished";
  if (!it.compact) {
    return (
      <div class={`sys prose${it.failure && !quota ? " fail" : ""}`}>
        <StateIcon state={icon} size={14} />
        <span>{word}</span>
        {facts && <span class="tnum">{facts}</span>}
      </div>
    );
  }
  // V0.21: words, never a bare symbol.
  const lbl = runLabel(it);
  return (
    <div class={`runmark${it.failure && !quota ? " fail" : ""}`}>
      <button class="rm" aria-expanded={open} title={[lbl.text, lbl.more].filter(Boolean).join(" · ")} onClick={() => lbl.more && setOpen(!open)}>
        {it.failure && <StateIcon state={icon} size={12} />}
        <span class="prose">{lbl.text}</span>
        {open && lbl.more && <span class="rmx tnum">{lbl.more}</span>}
      </button>
    </div>
  );
}

/** V0.34: a Copy button that says "Copied" for a moment. */
function CopyButton({ text, label = "Copy", cls = "" }: { text: string; label?: string; cls?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      class={`cpy ${cls}`}
      aria-label={label}
      title={label}
      onClick={async (e) => {
        e.stopPropagation();
        if (await copyToClipboard(text)) {
          setDone(true);
          setTimeout(() => setDone(false), 1200);
        }
      }}
    >
      {done ? "Copied" : "Copy"}
    </button>
  );
}

/** A preformatted block with Copy; `copy` is the full text when the shown one is cut. */
function CopyPre({ cls, text, copy }: { cls: string; text: string; copy?: string }) {
  return (
    <div class="codeb">
      <CopyButton text={copySource({ kind: "block", text: copy ?? text })} label="Copy" />
      <pre class={cls}>{text}</pre>
    </div>
  );
}

/** V0.41: one paced entry. The wrapper's class carries the rhythm: more room before an outcome, little between steps. */
function EntryView({ en, previews }: { en: Entry; previews: PreviewCard[] }) {
  if (en.kind === "fold") return <div class="cv proc"><Fold en={en} previews={previews} /></div>;
  const cls = en.role === "outcome" ? (en.item.kind === "user" ? "out own" : "out") : en.role === "end" ? "end" : "proc";
  return (
    <div class={`cv ${cls}`}>
      <ItemView it={en.item} previews={previews} process={en.role === "process"} />
    </div>
  );
}

/** #91: the icon of a group's tone; a recovered group keeps the failed shape and turns amber (.tl.rec in app.css). */
const TONE_ICON = { failed: "failed", recovered: "failed", running: "working", ok: "settled" } as const;

/** V0.41: the agent's steps between two outcomes as one quiet line; a tap shows every step in order. */
function Fold({ en, previews }: { en: Extract<Entry, { kind: "fold" }>; previews: PreviewCard[] }) {
  const [open, setOpen] = useState(false);
  const toggle = () => setOpen(!open);
  const time = en.durationMs !== null ? duration(en.durationMs) : "";
  return (
    <div class={`foldw${open ? " open" : ""}`}>
      <div class={`tl fold${en.tone === "recovered" ? " rec" : ""}`} role="button" tabIndex={0} aria-expanded={open} data-keys="self" title={en.text} onClick={toggle} onKeyDown={keyActivate(toggle)}>
        <StateIcon state={TONE_ICON[en.tone]} size={12} />
        <span class="tlt ell">
          {en.text}
          {en.failed > 0 && <b class="tlerr"> · {en.failed} failed</b>}
        </span>
        <span class="d">
          <span class="fst">
            {en.steps} steps{time ? " · " : ""}
          </span>
          {time} {open ? "▾" : "▸"}
        </span>
      </div>
      {open && (
        <div class="foldb">
          {en.items.map((it) => (
            <div class="cv proc" key={it.key}>
              <ItemView it={it} previews={previews} process />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function ItemView({ it, previews, process }: { it: Item; previews: PreviewCard[]; process?: boolean }) {
  const host = useContext(HostContext);
  const api = useApi();
  switch (it.kind) {
    case "runcard":
      return <RunCardAt id={it.action} />;
    case "user": {
      // V0.33: unread messages are dimmed; a cancelled or lost one is struck through.
      const cls = it.pending ? " pend" : it.delivery === "cancelled" || it.delivery === "lost" ? " gone" : "";
      if (it.from === "view")
        // V0.37: a press in the live view, with the View tag.
        return (
          <div class={`youw${cls}`}>
            <div class="you prose vmsg vchip" title="Pressed in the live view">
              <span class="vk">View</span>
              {it.text}
            </div>
            <DeliveryLine it={it} />
          </div>
        );
      if (it.from)
        return (
          <div class={`youw from${cls}`}>
            <FromThreadMessage fromId={it.from} text={it.text}>
              {it.attachments.length > 0 && <SentAttachments items={it.attachments} host={host} />}
            </FromThreadMessage>
            <DeliveryLine it={it} />
          </div>
        );
      return (
        <div class={`youw${cls}`}>
          <div class="you prose">
            {it.text}
            {it.attachments.length > 0 && <SentAttachments items={it.attachments} host={host} />}
            {it.images > 0 && !it.attachments.length && (
              <span class="imgchip">
                {it.images} {it.images === 1 ? "image" : "images"}
              </span>
            )}
          </div>
          <DeliveryLine it={it} />
        </div>
      );
    }
    case "assistant":
      if (it.quiet) return <QuietLine icon="seen" line={it.text.split("\n").find((x) => x.trim()) ?? ""} full={it.text} />;
      return (
        <div class={`msgw${process ? " proc" : ""}`}>
          {/* V0.149 (8): a command the message asks the owner to run is a card (Copy only), never a block that scrolls */}
          {splitProse(it.text).map((part, i) =>
            part.kind === "cmd" ? <ProseCommand key={i} text={part.text} /> : <div key={i} class="cl prose md" onClick={openMarkdownImage} dangerouslySetInnerHTML={{ __html: renderMarkdown(part.text, host, api.attachmentUrl) }} />,
          )}
          <CopyButton text={copySource({ kind: "message", text: it.text })} label="Copy message" cls="msgcp" />
        </div>
      );
    case "tools":
      return <ToolCard calls={it.calls} />;
    case "run":
      return <RunMark it={it} />;
    case "effect":
      return <QuietLine icon={it.ok ? "settled" : "failed"} line={it.line} full={`${it.action}\n\n${it.text}`} fail={!it.ok} />;
    case "note":
      return <QuietLine icon="seen" line={`Fyr: ${it.line}`} full={it.text} />;
    case "undeclared":
      return <QuietLine icon="seen" line={it.line} full={it.text} und />; // V0.164 (N9)
    case "preview":
      return <PreviewCardView card={it.card} all={previews} />;
    case "task":
      return <QuietLine icon={it.started ? "working" : TASK_ICON[it.status] ?? "settled"} line={it.text} fail={it.status === "failed"} />;
    case "answer":
      // V0.ask-cancel: a cancelled card says so, and its questions are one tap away.
      return it.cancelled ? <QuietLine icon="seen" line="Question cancelled" full={it.text} /> : <QuietLine icon="settled" line={`Answered: ${it.text}`} />;
    case "handover":
      return <HandoverDivider it={it} />;
  }
}

/** V0.45, V0.trans-host-handover: "Handed over from Claude to Claude (acme) · 17:20", and cross-host link when moved between hosts. */
function HandoverDivider({ it }: { it: Extract<Item, { kind: "handover" }> }) {
  const profiles = profilesOf({ hostId: useContext(HostContext) }, useContext(ProfilesContext)); // the thread's host's
  const dir = useDirectory();
  const link = it.toHostId && it.toThread
    ? { ref: ref(it.toHostId, it.toThread), label: `Open on ${it.toHost || "target"} →` }
    : it.fromHostId && it.fromThread
      ? { ref: ref(it.fromHostId, it.fromThread), label: `View source on ${it.fromHost || "source"}` }
      : null;
  return (
    <div class="hodiv" role="separator">
      <span class="hot">
        <span>{dividerLine(it, profiles, clockTime(it.atIso))}</span>
        <small title={it.note}>
          {rungDone(it.rung)}
          {it.note ? ` · ${it.note}` : ""}
        </small>
        {link && (
          <button type="button" class="ho-linkbtn" onClick={() => dir.openThread(link.ref)}>
            {link.label}
          </button>
        )}
      </span>
    </div>
  );
}

function callIcon(c: ToolCall) {
  return c.isError ? "failed" : c.done ? "settled" : "working";
}

/** One bordered card for consecutive tool calls: folded summary lines, with subagent rows as they were (V0.16). */
function ToolCard({ calls, nested }: { calls: ToolCall[]; nested?: boolean }) {
  return (
    <div class={nested ? "act nested" : "act"}>
      {segments(calls).map((s) => (s.kind === "agents" ? <AgentsRow calls={s.calls} /> : <FoldGroup calls={s.calls} />))}
    </div>
  );
}

/** "Read 3 files · ran 2 commands", total time, an error mark; a tap opens the rows. */
function FoldGroup({ calls }: { calls: ToolCall[] }) {
  const details = useToolDetails();
  const [open, setOpen] = useState<boolean | null>(null);
  const [all, setAll] = useState(false);
  const isOpen = open ?? details;
  const st = groupStats(calls);
  const showAll = () => setAll(true);
  const toggle = () => setOpen(!isOpen);
  const text = calls.length === 1 ? calls[0].label : groupSummary(calls);
  return (
    <>
      <div class={`tl grp${groupTone(st) === "recovered" ? " rec" : ""}`} role="button" tabIndex={0} aria-expanded={isOpen} data-keys="self" title={text} onClick={toggle} onKeyDown={keyActivate(toggle)}>
        <StateIcon state={TONE_ICON[groupTone(st)]} size={14} />
        <span class="tlt ell">
          {text}
          {st.failed > 0 && <b class="tlerr"> · {st.failed} failed</b>}
        </span>
        <span class="d">
          {st.durationMs !== null ? duration(st.durationMs) : ""} {isOpen ? "▾" : "▸"}
        </span>
      </div>
      {isOpen &&
        capCalls(calls, all).map((r) =>
          r.kind === "more" ? (
            <div class="tl row more" key="more" role="button" tabIndex={0} data-keys="self" title="Show every call" onClick={showAll} onKeyDown={keyActivate(showAll)}>
              <span class="tlm">⋯</span>
              <span class="tlt ell">{r.label}</span>
              <span class="d" />
            </div>
          ) : (
            <CallRow c={r.call} key={r.call.id} />
          ),
        )}
    </>
  );
}

export const ThreadIdContext = createContext("");

/** One call: its label; a tap shows the exact input and output (output cut at 200 lines). */
function CallRow({ c }: { c: ToolCall }) {
  const api = useApi();
  const [open, setOpen] = useState(false);
  const [all, setAll] = useState(false);
  const threadId = useContext(ThreadIdContext);
  const hostId = useContext(HostContext);
  const [whole, setWhole] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const loadWhole = async () => {
    if (!c.cutSeq || !hostId) return;
    setLoading(true);
    const ev = await api.fullEvent(hostId, threadId, c.cutSeq).catch(() => null);
    const text = ev ? fullOutputOf(ev, c.id) : null;
    setLoading(false);
    if (text !== null) {
      setWhole(text);
      setAll(true);
    }
  };
  const toggle = () => setOpen(!open);
  const full = whole ?? c.output;
  const out = all ? { text: full, hidden: 0 } : cutOutput(full);
  return (
    <div class={`tlr${open ? " open" : ""}`}>
      <div class="tl row" role="button" tabIndex={0} aria-expanded={open} data-keys="self" onClick={toggle} onKeyDown={keyActivate(toggle)}>
        <StateIcon state={callIcon(c)} size={14} />
        <span class={`tlt ell${c.isError ? " err" : ""}`}>{c.label}</span>
        <span class="d">{c.done ? duration(c.durationMs) : "running"}</span>
      </div>
      {open && (
        <div class="tld">
          <div class="tldh">
            {friendlyName(c.name)}
            {c.isError ? " · failed" : ""}
          </div>
          <CopyPre cls="tlin" text={fullInput(c.name, c.input)} />
          {c.output ? (
            <>
              <CopyPre cls={`tlout${c.isError ? " err" : ""}`} text={out.text} copy={c.output} />
              {c.cutSeq && whole === null && (
                <button class="btn sm tlall" disabled={loading} onClick={loadWhole}>
                  {loading ? "Loading…" : "Load the full output"}
                </button>
              )}
              {out.hidden > 0 && (
                <button class="btn sm tlall" onClick={() => setAll(true)}>
                  Show all ({out.hidden.toLocaleString()} more lines)
                </button>
              )}
            </>
          ) : (
            <div class="tldh">{c.done ? "No output" : "Running…"}</div>
          )}
        </div>
      )}
    </div>
  );
}

/** V0.subagent-timeline: the prompt a subagent was given: three lines, a tap shows it whole (desktop and phone alike). */
function SubPrompt({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  if (!text) return null;
  return (
    <div class={`subprompt prose${open ? " open" : ""}`} onClick={() => setOpen(!open)} title={open ? "Fold the prompt" : "Show the whole prompt"}>
      <span class="subpl">Prompt</span>
      {text}
    </div>
  );
}

/** V0.subagent-timeline: a subagent's prompt, then its texts and tool calls in order; in an Agent row and in the agents strip. */
function SubTimeline({ c }: { c: ToolCall }) {
  return (
    <>
      <SubPrompt text={agentPrompt(c)} />
      {c.sub.map((s: SubStep, i) =>
        s.kind === "calls" ? <ToolCard key={i} calls={s.calls} nested /> : <div key={i} class="subtext prose md" dangerouslySetInnerHTML={{ __html: renderMarkdown(s.text) }} />,
      )}
    </>
  );
}

function AgentsRow({ calls }: { calls: ToolCall[] }) {
  const [open, setOpen] = useState(false);
  const failed = calls.some((c) => c.isError);
  const done = calls.every((c) => c.done);
  const ms = done ? Math.max(...calls.map((c) => c.durationMs ?? 0)) : null;
  const desc = calls[0].description || calls[0].summary;
  const n = calls.length;
  return (
    <>
      <div class="al click" onClick={() => setOpen(!open)} title={calls.map((c) => c.description || c.summary).join("\n")}>
        <StateIcon state={failed ? "failed" : done ? "settled" : "working"} size={14} />
        <span class="nm ell">{calls[0].name}</span>
        <span class="s ell">
          {n} {n === 1 ? "subagent" : "subagents"} · {desc} {open ? "▾" : "▸"}
        </span>
        <span class="d">{ms !== null ? duration(ms) : ""}</span>
      </div>
      {open && (
        <div class="sub">
          {calls.map((c) => (
            <div class="subw">
              <div class="subh ell">
                {c.description || c.summary} · {c.subMessages} {c.subMessages === 1 ? "message" : "messages"}
              </div>
              <SubTimeline c={c} />
            </div>
          ))}
        </div>
      )}
    </>
  );
}

// ---- decision card ----

interface CardProps {
  d: Decision;
  more: number;
  card: CardState | null;
  onCard(cs: CardState): void;
  onAllow(d: Decision, allow: boolean): void;
  /** U15: send the answers of an ask card */
  onSubmitAsk?(d: Decision): void;
  /** V0.ask-cancel: close the card without an answer */
  onCancelAsk?(d: Decision): void;
  /** the thread's PR, to name it in a merge card */
  pr?: PullRequest | null;
  /** the thread's working directory and host, for readable approval cards */
  cwd?: string;
  host?: string;
}

function asker(d: Decision): string {
  return d.agent ? `asked by subagent “${d.agent}”` : "asked by this thread";
}

/** One sentence for an effect decision (V0.27), for rows and previews. */
/** The plain body of an effect card (V0.27): reason, what happens, details behind a toggle. */
export function EffectBody({ d, x, cls = "" }: { d: Decision; x: EffectText; cls?: string }) {
  return (
    <div class={`eff ${cls}`}>
      {x.why && <p class="eff-why">{x.why}</p>}
      <p class="eff-what">{x.happens}</p>
      <details class="eff-det">
        <summary>Details</summary>
        <div class="eff-who">{d.agent ? `Asked by subagent “${d.agent}”` : "Asked by this thread"} · the agent does not wait for the result</div>
        <pre>{x.details.action + "\n" + JSON.stringify(x.details.args, null, 2)}</pre>
      </details>
    </div>
  );
}

const LONG_FIELD_CHARS = 260;
const LONG_FIELD_LINES = 5;

function isLongField(f: ApprovalField): boolean {
  return f.value.length > LONG_FIELD_CHARS || f.value.split("\n").length > LONG_FIELD_LINES;
}

function ApprovalFieldRow({ f }: { f: ApprovalField }) {
  const [open, setOpen] = useState(false);
  const long = !f.mono && isLongField(f);
  return (
    <div class="appr-f">
      <div class="appr-k">{f.label}</div>
      {f.mono ? (
        <pre class="appr-v mono">{f.value}</pre>
      ) : (
        <>
          <div class={`appr-v prose${long && !open ? " clamp" : ""}`}>{f.value}</div>
          {long && (
            <button type="button" class="appr-more" onClick={() => setOpen(!open)}>
              {open ? "Show less" : "Show full text"}
            </button>
          )}
        </>
      )}
    </div>
  );
}

/** V0.readable-approvals: the human-readable body of an approval card, shared between desktop and phone.
 *  V0.tool-approval-judge: the sandbox badge and the judge's note stand above it on both fronts.
 *  The raw tool name and full JSON input stay behind Details. */
export function ApprovalBody({ a, host, cls = "" }: { a: ApprovalText; host?: string; cls?: string }) {
  const hasInput = Object.keys(a.details.input).length > 0;
  const whereLine = [a.where ? `in ${a.where}` : "", host ? `on ${host}` : ""].filter(Boolean).join(" · ");
  const metaLine = [a.details.tool || "tool", a.details.cwd ? `in ${a.details.cwd}` : "", host ? `on ${host}` : ""].filter(Boolean).join(" · ");
  return (
    <div class={`appr ${cls}`}>
      {a.outsideSandbox && <span class="ap-badge">Outside sandbox</span>}
      {a.judgeNote && <div class="ap-note">Judge: {a.judgeNote}</div>}
      {a.summary && <p class="appr-sum">{a.summary}</p>}
      {a.command && <pre class="appr-cmd">{a.command}</pre>}
      {a.fields.length > 0 && (
        <div class="appr-fields">
          {a.fields.map((f, i) => (
            <ApprovalFieldRow key={`${f.label}-${i}`} f={f} />
          ))}
        </div>
      )}
      {a.hiddenFields > 0 && <div class="appr-hidden">+{a.hiddenFields} more in Details</div>}
      {a.showWhere && whereLine && <div class="appr-where ell">{whereLine}</div>}
      <details class="eff-det">
        <summary>Details</summary>
        <div class="eff-who">{metaLine}</div>
        {hasInput && <pre>{JSON.stringify(a.details.input, null, 2)}</pre>}
      </details>
    </div>
  );
}

function DecisionCard(p: CardProps) {
  const { d } = p;
  if (d.kind === "ask") return p.card && p.card.decisionId === d.id ? <AskCard {...p} card={p.card} /> : null;
  const approval = d.kind === "approval";
  const a = approval ? describeApproval(d, p.cwd) : null;
  const x = approval ? null : describeEffect(d, p.pr);
  return (
    <div class="ask">
      <div class="ah">
        <StateIcon state="asking" size={14} />
        <b>{x ? x.label : "Approval"}</b>
        <span class="ell">{asker(d)}</span>
        {p.more > 0 && <span class="ml">+{p.more} more</span>}
      </div>
      <div class="ab">
        <div class="q">{x ? x.title : a!.title}</div>
        <div class="payload">
          {a && <ApprovalBody a={a} host={p.host} />}
          {x && <EffectBody d={d} x={x} />}
        </div>
        <div class="grid4">
          <button class="op" onClick={() => p.onAllow(d, true)}>
            <span class="a ell">{x ? x.yes : "Allow"}</span>
            <kbd>1</kbd>
            {approval && <span class="d">Run it once.</span>}
          </button>
          <button class="op" onClick={() => p.onAllow(d, false)}>
            <span class="a ell">{x ? x.no : "Deny"}</span>
            <kbd>2</kbd>
            <span class="d">{approval ? "The agent is told no and continues." : "Nothing happens; the agent is told."}</span>
          </button>
        </div>
      </div>
    </div>
  );
}

function AskCard(p: CardProps & { card: CardState }) {
  const { d, card } = p;
  const qs = questionsOf(d);
  const q = qs[card.index];
  if (!q) return null;
  const picks = card.picks[card.index] ?? [];
  const other = card.other[card.index];
  const passed = settledOf(card, d, card.index) && picks.length === 0 && !other;
  // U15: always a visible way to finish (Enter still works). V0.ask-cancel: Back (⇧Tab, ←), Skip (⇧S)
  // and Cancel (⇧Esc) sit beside it, and the button sends, moves on, or cancels what the card holds.
  const st = submitState(card, d);
  const primary = () => (st.act === "submit" ? p.onSubmitAsk?.(d) : st.act === "cancel" ? p.onCancelAsk?.(d) : st.act === "next" && p.onCard(goTo(card, d, nextOpen(card, d))));
  return (
    <div class="ask">
      <div class="ah">
        <StateIcon state="asking" size={14} />
        <b class="qn" onClick={() => qs.length > 1 && p.onCard(goTo(card, d, (card.index + 1) % qs.length))} title={qs.length > 1 ? "Next question (Tab)" : undefined}>
          Question {card.index + 1} of {qs.length}
        </b>
        <span class="ell">{asker(d)}</span>
        <span class="ml">
          {p.more > 0 && `+${p.more} more · `}
          <kbd>Tab</kbd> next question
        </span>
        <button class="ahx" title="Cancel the question without answering it (⇧Esc)" onClick={() => p.onCancelAsk?.(d)}>
          Cancel
        </button>
      </div>
      <div class="ab">
        <div class="q">{q.question}</div>
        <div class="grid4">
          {q.options.map((o, i) => (
            <button class={`op${picks.includes(o.label) ? " on" : ""}`} title={`${o.label}${o.description ? `\n${o.description}` : ""}`} onClick={() => p.onCard(pick(card, d, i))}>
              <span class="a ell">{o.label}</span>
              <kbd>{i + 1}</kbd>
              <span class="d">{o.description ?? ""}</span>
            </button>
          ))}
          <button class={`op${other ? " on" : ""}`} onClick={() => document.getElementById("composer")?.focus()} title="Answer in your own words in the field below (0)">
            <span class="a ell">Other…</span>
            <kbd>0</kbd>
            <span class="d">{other ?? ""}</span>
          </button>
        </div>
        <div class="askf">
          <span class="askp">{progressText(st)}</span>
          {card.index > 0 && (
            <button class="askb" title="Previous question (⇧Tab or ←)" onClick={() => p.onCard(back(card, d))}>
              ‹ Back
            </button>
          )}
          <button class={`askb${passed ? " on" : ""}`} title={passed ? "Take the skip back (⇧S)" : "Leave this question without an answer (⇧S)"} onClick={() => p.onCard(skip(card, d))}>
            {passed ? "Unskip" : "Skip"}
          </button>
          <button class={`btn ${st.act === "submit" || st.act === "cancel" ? "p" : ""}`} disabled={st.act === "none"} onClick={primary}>
            {st.label}
            {st.act === "submit" && <kbd>↵</kbd>}
          </button>
        </div>
      </div>
    </div>
  );
}

// ---- composer ----

/** What a send carries (SPEC V0.18): text and uploaded attachment ids; text may be empty. */
export interface SendArgs {
  text: string;
  attachments?: string[];
  /** V0.37: a press in the live view */
  from?: "view";
}

/** SPEC V0.128: the composer sends an ordinary message on every tab; sent from the View tab, the thread then shows
 *  the Conversation tab, which opens at its end, where the new message is. Desktop and phone. */
export async function sendFrom(tab: string, args: SendArgs, send: (a: SendArgs) => Promise<boolean>, setTab: (tab: string) => void): Promise<boolean> {
  const ok = await send(args);
  if (ok && tab === "view") setTab("conv");
  return ok;
}

interface ComposerProps {
  t: Thread;
  now: number;
  answer: AnswerField | null;
  onSend(args: SendArgs): Promise<boolean>;
  onInterrupt(): void;
  /** V0.143: Restart an agent that does not respond */
  onRestart(): void;
}

/**
 * Enter sends (steers a running thread). While a question is open the composer is the answer field
 * for the active question's "Other" (U15): Enter answers, ⌘Enter queues the text as a message instead.
 * Files: the paperclip, paste and drop, any type; each uploads at once and shows a chip.
 */
function Composer({ t, now, answer, onSend, onInterrupt, onRestart }: ComposerProps) {
  const [draft, setDraft] = useDraft(t.id);
  const att = useAttachments(t.hostId);
  const ta = useRef<HTMLTextAreaElement>(null);
  const sendReason = unavailableReason(t, "thread.send");
  const text = answer ? answer.text : draft;
  const setText = answer ? answer.setText : setDraft;
  const disabled = !answer && !!sendReason;
  // V0.79: `/` at the start lists the agent's commands and skills (not in the answer field).
  const slash = useSlash(answer || disabled ? null : { host: t.hostId, threadId: t.id }, text, setText, ta);

  useLayoutEffect(() => {
    const el = ta.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 200) + "px";
  }, [text]);

  const plan = sendArgs(text, att.chips);
  const send = async () => {
    if (sendReason || !plan.ok) return;
    const ok = await onSend(plan.args);
    if (ok) {
      setText("");
      att.clear();
    }
  };

  const placeholder = answer
    ? `Answer question ${answer.index + 1} in your own words…`
    : sendReason
      ? sendReason
      : t.running
        ? "Steer the running thread…"
        : "Message…";

  return (
    <div class="cmpw" onDrop={att.onDrop} onDragOver={att.onDragOver}>
      <AgentStatus t={t} now={now} onRestart={onRestart} />
      <ChipStrip chips={att.chips} onRemove={att.remove} />
      <SlashList s={slash} />
      <div class={`cmp${disabled ? " off" : ""}`}>
        <ClipPicker onFiles={att.add} disabled={!!sendReason} />
        <textarea
          ref={ta}
          id="composer"
          class="in"
          rows={1}
          value={text}
          disabled={disabled}
          placeholder={placeholder}
          title={disabled ? sendReason : undefined}
          data-other={answer ? "1" : undefined}
          onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
          onPaste={att.onPaste}
          onKeyDown={(e) => {
            if (slash.onKeyDown(e)) return;
            if (e.key !== "Enter" || e.shiftKey || e.isComposing) return;
            e.preventDefault();
            e.stopPropagation();
            if (answer && !(e.metaKey || e.ctrlKey)) answer.submit();
            else void send();
          }}
        />
        {answer && !sendReason && <span class="hint">⌘⏎ queue</span>}
        {t.running && (
          <button class="btn" disabled={!isAvailable(t, "thread.interrupt")} title={unavailableReason(t, "thread.interrupt") ?? "Interrupt (Esc)"} onClick={onInterrupt}>
            Interrupt <kbd>Esc</kbd>
          </button>
        )}
        {answer ? (
          <button class="btn p" title="Answer (Enter)" onClick={answer.submit}>
            Answer <kbd>⏎</kbd>
          </button>
        ) : (
          <button class="btn p" disabled={!!sendReason || !plan.ok} title={sendReason ?? (plan.ok ? "Send (Enter)" : plan.reason)} onClick={() => void send()}>
            Send <kbd>⏎</kbd>
          </button>
        )}
      </div>
    </div>
  );
}
