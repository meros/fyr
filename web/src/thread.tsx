// Thread pane: header with chips and tabs, conversation, docked decision card and composer
// (design/directions/04-rail.html).

import { copySource, copyToClipboard } from "./copy.ts";
import { useDraft } from "./drafts.ts";
import { liveLine } from "./tasks.ts";
import { describeEffect, type EffectText } from "./effects.ts";
import { Boundary, CrashProbe, crashProbe } from "./boundary.tsx";
import { useApi } from "./api-context.ts";
import { useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import { createContext } from "preact";
import type { Decision, FyrEvent, Profile, PullRequest, TaskDTO, Thread } from "./types.ts";
import { buildConversation, pace, type Entry, deliveryLabel, capCalls, cutOutput, friendlyName, fullInput, groupStats, groupSummary, previewsOf, segments, type Item, type ToolCall, runLabel, fullOutputOf } from "./conversation.ts";
import { renderMarkdown } from "./markdown.ts";
import { currentDecision } from "./decode.ts";
import { StateIcon, stateClass } from "./icons.tsx";
import { stateLabel } from "./rows.ts";
import { clockTime, duration, relTime, shortModel } from "./format.ts";
import { effortsFor, startEffort } from "./providers.ts";
import { dividerLine, rungDone } from "./handover.ts";
import { pick, questionsOf, goTo, type CardState, nextOpen, submitState } from "./question.ts";
import { isAvailable, unavailableReason } from "./store.ts";
import { offlineLine } from "./hosts.ts";
import { onToolDetails, toolDetails } from "./prefs.ts";
import { FromThreadMessage, ProjectIcon, StartedBy, useDirectory } from "./project-ui.tsx";
import { colorVar, projectName, projectOf } from "./projects.ts";
import { ChipStrip, ClipPicker, SentAttachments, useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { Filmstrip, PreviewCardView } from "./preview-ui.tsx";
import { LiveViewPanel, ViewChat } from "./view-ui.tsx";
import { TAB_LABEL, defaultTab, showsDecision, threadTabs } from "./view.ts";
import type { PreviewCard } from "./previews.ts";
import { agentsSummary, groupTasks, taskLine, taskStats } from "./tasks.ts";
import { ChangesView } from "./changes-ui.tsx";
import { CiNotesScope } from "./ci-notes-ui.tsx";
import { ChangeList, ThreadOffers, useViewAction } from "./offers-ui.tsx";
import { changeChip, settleLine } from "./offers.ts";
import { prChip } from "./pr.ts";
import { PrView } from "./pr-ui.tsx";

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
  thread: Thread;
  mode: "open" | "peek";
  events: FyrEvent[] | undefined;
  profiles: Profile[];
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
  /** Opens the project picker for this thread (V0.26, V0.30); absent: no ⋯ menu. */
  onMoveProject?: () => void;
  /** Opens the handover dialog (V0.45). */
  onHandover?: () => void;
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
  const tab = defaultTab(t, pick?.id === t.id ? pick.tab : urlTab(t.id));
  const setTab = (tab: string) => setPick({ id: t.id, tab });
  useEffect(() => setPick(null), [t.id]); // another thread opens on its own default
  const profile = p.profiles.find((x) => x.id === t.profile);
  const decision = currentDecision(t);
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
            <button class="btn" disabled={!!snoozeReason} title={snoozeReason ?? "Snooze (s)"} onClick={p.onSnooze}>
              Snooze <kbd>s</kbd>
            </button>
            <button class="btn" disabled={!!settleReason} title={settleReason ?? (t.running ? "Settle when done (e)" : "Settle (e)")} onClick={() => p.onRun("thread.settle")}>
              Settle <kbd>e</kbd>
            </button>
            {p.onMoveProject && <ThreadMenu onMove={p.onMoveProject} liveView={!!t.liveView} onLiveView={() => p.onRun("thread.setView", { on: !t.liveView })} onHandover={p.onHandover} handoverReason={unavailableReason(t, "thread.handover")} />}
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
          {t.hostOffline && (
            <span class="chip m" title="The front shows this thread from its last copy; commands wait until the host is back">
              <span>{offlineLine(t)}</span>
            </span>
          )}
          {t.branch && (
            <span class="chip m brn" title={t.branch}>
              <span>{t.branch}</span>
            </span>
          )}
          <ModelChip t={t} profile={profile} onRun={p.onRun} />
          <EffortChip t={t} profile={profile} onRun={p.onRun} />
          {t.pr && <PrChip pr={t.pr} onDismiss={isAvailable(t, "thread.dismissPR") ? () => p.onRun("thread.dismissPR") : undefined} />}
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
        </div>
        <ThreadOffers t={t} onRun={p.onRun} />
        <div class="tabs" role="tablist">
          {threadTabs(t).map((k) => (
            <span class={tab === k ? "on" : ""} role="tab" onClick={() => setTab(k)}>
              {TAB_LABEL[k]}
            </span>
          ))}
        </div>
      </div>
      {t.tasks.length > 0 && <AgentsStrip key={t.id} tasks={t.tasks} now={p.now} />}
      <ThreadTabBody t={t} tab={tab} setTab={setTab} events={p.events} now={p.now} onAtEnd={p.onAtEnd} onEarlier={p.onEarlier} run={p.onRun} send={p.onSend} />
      {showsDecision(tab) && decision && <DecisionCard d={decision} more={t.decisions.length - 1} card={p.card} onCard={p.onCard} onAllow={p.onAllow} onSubmitAsk={p.onSubmitAsk} pr={t.pr} />}
      <Composer key={t.id} t={t} now={p.now} onSend={tab === "view" ? (a) => p.onSend({ ...a, from: "view-typed" }) : p.onSend} onInterrupt={() => p.onRun("thread.interrupt")} answer={tab === "conv" ? p.answerField : null} />
    </section>
  );
}

export interface ThreadTabBodyProps {
  t: Thread;
  /** from defaultTab */
  tab: string;
  setTab(tab: string): void;
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
 *  (V0.19), Changes (V0.25, V0.59) and PR (V0.43). */
export function ThreadTabBody({ t, tab, setTab, events, now, onAtEnd, onEarlier, run, send, phone }: ThreadTabBodyProps) {
  const api = useApi();
  const previews = useMemo(() => previewsOf(events ?? [], t.host, api), [events, t.host]);
  const viewAction = useViewAction(run, t.cwd);
  return (
    <>
      {tab === "view" && <LiveViewPanel full t={t} now={now} onPress={(text) => send({ text, from: "view" })} onAction={viewAction} />}
      {tab === "view" && <ViewChat t={t} events={events} now={now} onOpen={() => setTab("conv")} />}
      {tab === "conv" && <Filmstrip cards={previews} collapsible={phone} />}
      {tab === "conv" && (
        <Conversation
          outbox={t.outbox.map((o) => o.id)}
          events={events}
          onAtEnd={onAtEnd}
          threadId={t.id}
          onEarlier={onEarlier}
          cwd={t.cwd}
          host={t.host}
          delivery={{ cancel: async (messageId) => run("thread.cancelMessage", { messageId }), resend: send }}
        />
      )}
      {tab === "changes" && (
        <div class="chtab">
          <ChangeList t={t} onRun={run} />
          <CiNotesScope changes={t.changes}>
            <ChangesView threadId={t.id} changes={t.changes} />
          </CiNotesScope>
        </div>
      )}
      {tab === "pr" && <PrTab t={t} now={now} phone={phone} onSend={(text) => send({ text })} onDismiss={() => run("thread.dismissPR")} onChanges={() => setTab("changes")} />}
    </>
  );
}

/** The thread ⋯ menu (V0.26): opens on click or Enter, arrows move, Esc closes and returns focus. */
function ThreadMenu({ onMove, liveView, onLiveView, onHandover, handoverReason }: { onMove(): void; liveView: boolean; onLiveView(): void; onHandover?: () => void; handoverReason?: string }) {
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
  const run = (fn: () => void) => () => {
    setOpen(false);
    fn();
  };
  const onKey = (e: KeyboardEvent) => {
    // The menu owns its keys: the inbox shortcuts must not act while it has focus.
    e.stopPropagation();
    if (!open) return;
    const items = [...(ref.current?.querySelectorAll<HTMLElement>("[role=menuitem]") ?? [])];
    const i = items.indexOf(document.activeElement as HTMLElement);
    if (e.key === "Escape") {
      e.preventDefault();
      setOpen(false);
      btn.current?.focus();
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const n = items.length;
      items[(i + (e.key === "ArrowDown" ? 1 : n - 1) + n) % n]?.focus();
    } else if (e.key === "Tab") setOpen(false);
  };
  return (
    <div class="tmenu" ref={ref} onKeyDown={onKey}>
      <button ref={btn} class="btn tmenu-b" aria-haspopup="menu" aria-expanded={open} aria-label="More actions" title="More actions" onClick={() => setOpen(!open)}>
        ⋯
      </button>
      {open && (
        <div class="tmenu-l" role="menu" aria-label="Thread actions">
          <button class="mi" role="menuitem" onClick={run(onMove)}>
            <kbd>p</kbd>
            <span class="ell">Project…</span>
            <span />
          </button>
          <button class="mi" role="menuitem" onClick={run(onLiveView)}>
            <span />
            <span class="ell">{liveView ? "Turn off live view" : "Turn on live view"}</span>
            <span />
          </button>
          {onHandover && (
            <button class="mi" role="menuitem" disabled={!!handoverReason} title={handoverReason} onClick={run(onHandover)}>
              <span />
              <span class="ell">Hand over…</span>
              <span />
            </button>
          )}
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

/** The PR tab (V0.43). */
function PrTab({ t, now, onSend, onDismiss, onChanges, phone }: { t: Thread; now: number; onSend(text: string): Promise<boolean>; onDismiss(): void; onChanges(): void; phone?: boolean }) {
  if (!t.pr) return <div class="pane-note">No pull request is linked to this thread.</div>;
  return <PrView key={t.id} t={t} now={now} onSend={onSend} onDismiss={onDismiss} onChanges={onChanges} phone={phone} />;
}

function ModelChip({ t, profile, onRun }: { t: Thread; profile: Profile | undefined; onRun(a: string, args?: Record<string, unknown>): void }) {
  const models = profile?.models ?? [];
  const reason = unavailableReason(t, "thread.setModel");
  const can = models.length > 1 && !reason;
  return (
    <span class={`chip m${can ? " msel" : ""}`} title={can ? `${t.model} · change model` : reason ? `${t.model} · ${reason}` : t.model}>
      <span>{shortModel(t.model)}</span>
      {can && (
        <select value={t.model} aria-label="Model" onChange={(e) => onRun("thread.setModel", { model: (e.target as HTMLSelectElement).value })}>
          {!models.includes(t.model) && <option value={t.model}>{shortModel(t.model)}</option>}
          {models.map((m) => (
            <option value={m}>{shortModel(m)}</option>
          ))}
        </select>
      )}
    </span>
  );
}

/** V0.50: the thread's effort, when its model takes one. */
function EffortChip({ t, profile, onRun }: { t: Thread; profile: Profile | undefined; onRun(a: string, args?: Record<string, unknown>): void }) {
  const efforts = effortsFor(profile, t.model);
  if (!efforts.length) return null;
  const cur = startEffort(profile, t.model, t.effort);
  const reason = unavailableReason(t, "thread.setModel");
  return (
    <span class={`chip m${reason ? "" : " msel"}`} title={reason ? `Effort ${cur || "default"} · ${reason}` : `Effort ${cur || "default"} · change effort`}>
      <span>{cur || "effort"}</span>
      {!reason && (
        <select value={cur} aria-label="Effort" onChange={(e) => onRun("thread.setModel", { effort: (e.target as HTMLSelectElement).value })}>
          {!cur && <option value="">CLI default</option>}
          {efforts.map((x) => (
            <option value={x}>{x}</option>
          ))}
        </select>
      )}
    </span>
  );
}

function PrChip({ pr, onDismiss }: { pr: PullRequest; onDismiss?: () => void }) {
  const l = prChip(pr);
  const done = pr.state === "MERGED" || pr.state === "CLOSED";
  return (
    <span class="prw">
      <a class={`chip pr ${l.cls}`} href={pr.url} target="_blank" rel="noopener noreferrer" title={l.title}>
        {l.text}
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

function TaskRow({ t, now }: { t: TaskDTO; now: number }) {
  const line = taskLine(t);
  return (
    <div class={`tk ${t.status}`} title={`${t.description}\n${line}`}>
      <StateIcon state={TASK_ICON[t.status] ?? "seen"} size={14} />
      <span class="tkt">
        <span class="tkd ell">{t.description || "Agent"}</span>
        <span class="tkl ell">{line}</span>
      </span>
      <span class="tks">{taskStats(t)}</span>
      <span class="tka">{relTime(t.startedAt, now)}</span>
    </div>
  );
}

/** Running tasks always show; ended ones fold behind "N finished". On a phone the whole strip is one line until opened. */
export function AgentsStrip({ tasks, now }: { tasks: TaskDTO[]; now: number }) {
  const [showEnded, setShowEnded] = useState(false);
  const [open, setOpen] = useState(false);
  const g = groupTasks(tasks);
  return (
    <div class={`agents${open ? " open" : ""}`}>
      <button class="agsum ell" onClick={() => setOpen(!open)} aria-expanded={open}>
        <StateIcon state={g.running.length ? "waiting" : "settled"} size={14} />
        <span class="ell">{agentsSummary(tasks)}</span>
        <span class="chev">{open ? "▾" : "▸"}</span>
      </button>
      <div class="agrows">
        {g.running.map((t) => (
          <TaskRow t={t} now={now} key={t.id} />
        ))}
        {g.ended.length > 0 && (
          <button class="tkmore" onClick={() => setShowEnded(!showEnded)} aria-expanded={showEnded}>
            {g.ended.length} finished {showEnded ? "▾" : "▸"}
          </button>
        )}
        {showEnded && g.ended.map((t) => <TaskRow t={t} now={now} key={t.id} />)}
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
const HostContext = createContext<string | undefined>(undefined);

/** The host's profiles, for the handover divider's names (V0.45). */
export const ProfilesContext = createContext<Profile[]>([]);

export function Conversation({ events, onAtEnd, threadId, onEarlier, cwd, delivery, outbox, host }: { events: FyrEvent[] | undefined; outbox?: string[]; onAtEnd(b: boolean): void; threadId: string; onEarlier?: () => void; cwd?: string; delivery?: DeliveryActions; host?: string }) {
  const details = useToolDetails();
  const api = useApi();
  const outKey = outbox ? outbox.join(",") : null;
  const all = useMemo(() => buildConversation(events ?? [], { details, cwd, host, urls: api, outbox: outKey === null ? undefined : outKey ? outKey.split(",") : [] }), [events, details, cwd, host, outKey]);
  // V0.33: messages the agent has not read stand after "Agent is here".
  const [items, pending] = useMemo(() => [all.filter((i) => !(i.kind === "user" && i.pending)), all.filter((i) => i.kind === "user" && i.pending)], [all]);
  const previews = useMemo(() => items.flatMap((it) => (it.kind === "preview" ? [it.card] : [])), [items]);
  // V0.41: outcome at full weight, the work quiet and folded.
  const entries = useMemo(() => pace(items, { details }), [items, details]);
  const ref = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  const check = () => {
    const el = ref.current;
    if (!el) return;
    const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    stick.current = atEnd;
    onAtEnd(atEnd);
  };

  useLayoutEffect(() => {
    stick.current = true;
  }, [threadId]);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    // V0.38: at the bottom, follow new content without reading the layout first (a read forces a
    // layout of the whole conversation on every update); away from the bottom, nothing moves.
    if (stick.current) {
      el.scrollTop = 1e9;
      onAtEnd(true);
    }
  }, [all, threadId]);

  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => {
      if (stick.current) el.scrollTop = el.scrollHeight;
      check();
    });
    ro.observe(el);
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
    <div class="convo" ref={ref} onScroll={check} onClick={onCodeCopy}>
      <div class="convo-in">
        {events === undefined && <div class="sys">Loading…</div>}
        {events !== undefined && items.length === 0 && <div class="sys">No messages yet.</div>}
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
          {pending.length > 0 && (
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
    </div>
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
function QuietLine({ icon, line, full, fail }: { icon: "settled" | "failed" | "seen" | "working"; line: string; full?: string; fail?: boolean }) {
  const [open, setOpen] = useState(false);
  const more = !!full && full.trim() !== line.trim();
  return (
    <div class={`ql${fail ? " fail" : ""}`}>
      <div class={`qlh${more ? " click" : ""}`} role={more ? "button" : undefined} tabIndex={more ? 0 : undefined} aria-expanded={more ? open : undefined} onClick={more ? () => setOpen(!open) : undefined} onKeyDown={more ? keyActivate(() => setOpen(!open)) : undefined}>
        <StateIcon state={icon} size={12} />
        <span class="ell">{line}</span>
      </div>
      {open && full && <CopyPre cls="qlx" text={full} />}
    </div>
  );
}

/** The end of a turn: a small mark; a tap shows time, cost and the failure. A failure keeps its text in one line. */
function RunMark({ it }: { it: Extract<Item, { kind: "run" }> }) {
  const [open, setOpen] = useState(false);
  const facts = [it.durationMs !== undefined ? duration(it.durationMs) : "", it.costUsd !== undefined ? `$${it.costUsd.toFixed(2)}` : ""].filter(Boolean).join(" · ");
  const icon = it.failure ? "failed" : it.interrupted ? "seen" : "settled";
  const word = it.failure ? `Failed (${it.failure.class}): ${it.failure.message}` : it.interrupted ? "Interrupted" : "Finished";
  if (!it.compact) {
    return (
      <div class={`sys prose${it.failure ? " fail" : ""}`}>
        <StateIcon state={icon} size={14} />
        <span>{word}</span>
        {facts && <span class="tnum">{facts}</span>}
      </div>
    );
  }
  // V0.21: words, never a bare symbol; the cost is one tap away.
  const lbl = runLabel(it);
  return (
    <div class={`runmark${it.failure ? " fail" : ""}`}>
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

/** V0.41: the agent's steps between two outcomes as one quiet line; a tap shows every step in order. */
function Fold({ en, previews }: { en: Extract<Entry, { kind: "fold" }>; previews: PreviewCard[] }) {
  const [open, setOpen] = useState(false);
  const toggle = () => setOpen(!open);
  const time = en.durationMs !== null ? duration(en.durationMs) : "";
  return (
    <div class={`foldw${open ? " open" : ""}`}>
      <div class="tl fold" role="button" tabIndex={0} aria-expanded={open} data-keys="self" title={en.text} onClick={toggle} onKeyDown={keyActivate(toggle)}>
        <StateIcon state={en.failed ? "failed" : en.running ? "working" : "settled"} size={12} />
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
    case "user": {
      // V0.33: unread messages are dimmed; a cancelled or lost one is struck through.
      const cls = it.pending ? " pend" : it.delivery === "cancelled" || it.delivery === "lost" ? " gone" : "";
      if (it.from === "view" || it.from === "view-typed")
        // V0.37 a press, V0.49 a message typed on the View tab: one bubble with the same View tag.
        return (
          <div class={`youw${cls}`}>
            <div class={`you prose vmsg${it.from === "view" ? " vchip" : ""}`} title={it.from === "view" ? "Pressed in the live view" : "Typed on the View tab"}>
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
          <div class="cl prose md" dangerouslySetInnerHTML={{ __html: renderMarkdown(it.text, host, api.attachmentUrl) }} />
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
    case "preview":
      return <PreviewCardView card={it.card} all={previews} />;
    case "task":
      return <QuietLine icon={it.started ? "working" : TASK_ICON[it.status] ?? "settled"} line={it.text} fail={it.status === "failed"} />;
    case "answer":
      return <QuietLine icon="settled" line={`Answered: ${it.text}`} />;
    case "handover":
      return <HandoverDivider it={it} />;
  }
}

/** V0.45: "Handed over from Claude to Claude (norban) · 17:20", and how the context went over. */
function HandoverDivider({ it }: { it: Extract<Item, { kind: "handover" }> }) {
  const profiles = useContext(ProfilesContext);
  return (
    <div class="hodiv" role="separator">
      <span class="hot">
        <span>{dividerLine(it, profiles, clockTime(it.atIso))}</span>
        <small title={it.note}>
          {rungDone(it.rung)}
          {it.note ? ` · ${it.note}` : ""}
        </small>
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
      <div class="tl grp" role="button" tabIndex={0} aria-expanded={isOpen} data-keys="self" title={text} onClick={toggle} onKeyDown={keyActivate(toggle)}>
        <StateIcon state={st.failed ? "failed" : st.running ? "working" : "settled"} size={14} />
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
  const [whole, setWhole] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const loadWhole = async () => {
    if (!c.cutSeq) return;
    setLoading(true);
    const ev = await api.fullEvent(threadId, c.cutSeq).catch(() => null);
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
              {c.subCalls.length > 0 && <ToolCard calls={c.subCalls} nested />}
              {c.subTexts.length > 0 && <div class="subtext prose">{c.subTexts[c.subTexts.length - 1]}</div>}
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
  /** the thread's PR, to name it in a merge card */
  pr?: PullRequest | null;
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

function DecisionCard(p: CardProps) {
  const { d } = p;
  if (d.kind === "ask") return p.card && p.card.decisionId === d.id ? <AskCard {...p} card={p.card} /> : null;
  const approval = d.kind === "approval";
  const x = approval ? null : describeEffect(d, p.pr);
  const cmd = approval && typeof d.input?.command === "string" ? (d.input.command as string) : null;
  const rest = approval && d.input ? Object.fromEntries(Object.entries(d.input).filter(([k]) => k !== "command")) : {};
  return (
    <div class="ask">
      <div class="ah">
        <StateIcon state="asking" size={14} />
        <b>{x ? x.label : "Approval"}</b>
        <span class="ell">{asker(d)}</span>
        {p.more > 0 && <span class="ml">+{p.more} more</span>}
      </div>
      <div class="ab">
        <div class="q">{x ? x.title : <>Allow {d.tool}?</>}</div>
        <div class="payload">
          {cmd && <pre class="cmd">{cmd}</pre>}
          {approval && Object.keys(rest).length > 0 && <pre>{JSON.stringify(rest, null, 2)}</pre>}
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
          <kbd>Tab</kbd> multi-select next
        </span>
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
        {(() => {
          // U15: always a visible way to finish (Enter still works).
          const st = submitState(card, d);
          return (
            <div class="askf">
              <span class="askp">
                {st.total > 1 ? `${st.answered} of ${st.total} answered` : st.answered ? "Answered" : ""}
              </span>
              <button
                class={`btn ${st.act === "submit" ? "p" : ""}`}
                disabled={st.act === "none"}
                onClick={() => (st.act === "submit" ? p.onSubmitAsk?.(d) : st.act === "next" && p.onCard(goTo(card, d, nextOpen(card, d))))}
              >
                {st.label}
                {st.act === "submit" && <kbd>↵</kbd>}
              </button>
            </div>
          );
        })()}
      </div>
    </div>
  );
}

// ---- composer ----

/** What a send carries (SPEC V0.18): text and uploaded attachment ids; text may be empty. */
export interface SendArgs {
  text: string;
  attachments?: string[];
  /** V0.37: a press in the live view; V0.49: typed on the View tab */
  from?: "view" | "view-typed";
}

interface ComposerProps {
  t: Thread;
  now: number;
  answer: AnswerField | null;
  onSend(args: SendArgs): Promise<boolean>;
  onInterrupt(): void;
}

/**
 * Enter sends (steers a running thread). While a question is open the composer is the answer field
 * for the active question's "Other" (U15): Enter answers, ⌘Enter queues the text as a message instead.
 * Files: the paperclip, paste and drop, any type; each uploads at once and shows a chip.
 */
function Composer({ t, now, answer, onSend, onInterrupt }: ComposerProps) {
  const [draft, setDraft] = useDraft(t.id);
  const att = useAttachments(t.host);
  const ta = useRef<HTMLTextAreaElement>(null);
  const sendReason = unavailableReason(t, "thread.send");
  const text = answer ? answer.text : draft;
  const setText = answer ? answer.setText : setDraft;
  const disabled = !answer && !!sendReason;

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
      {t.running && (
        <div class="sys live">
          <span class="spin icw">
            <StateIcon state="working" size={14} />
          </span>
          <span class="ell">{liveLine(t, now)}</span>
        </div>
      )}
      <ChipStrip chips={att.chips} onRemove={att.remove} />
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
