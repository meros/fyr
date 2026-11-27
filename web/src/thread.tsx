// Thread pane: header with chips and tabs, the tab bodies and the PR tab. The conversation, the agents strip, the
// decision card and the composer are their own modules (the rail direction (decision 0006)).

import { ArrivedBar } from "./arrived-bar.tsx";
import type { AnswerMove } from "./arrived.ts";
import { PIN_ICON } from "./row-ui.tsx";
import { useApi } from "./api-context.ts";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { SlideMark } from "./motion-ui.tsx";
import type { Decision, FyrEvent, PullRequest, Thread } from "./types.ts";
import { previewsOf } from "./conversation.ts";
import { currentDecision } from "./decode.ts";
import { StateChip } from "./state-chip.tsx";
import { AgentPill, threadAgent } from "./agent-ui.tsx";
import { UsageChip } from "./usage-ui.tsx";
import { HandoverLinkBar } from "./handover-ui.tsx";
import { type CardState } from "./question.ts";
import { isAvailable, unavailableReason } from "./store.ts";
import { deskThreadMenu, type ThreadAction } from "./actions.ts";
import { entryForKey, menuEntries } from "./menu.ts";
import { MenuRows, useKeepMenuFocus } from "./menu-ui.tsx";
import { hostChip, offlineLine, profilesOf, threadProvider, type HostProfiles } from "./hosts.ts";
import { ThreadQuotaChip } from "./quota-ui.tsx";
import { useHosts } from "./host-pick-ui.tsx";
import { HostMark } from "./host-mark-ui.tsx";
import { ProjectIcon, StartedBy, useDirectory } from "./project-ui.tsx";
import { colorVar, projectName, projectOf } from "./projects.ts";
import { Filmstrip, PreviewsChip } from "./preview-ui.tsx";
import { LiveViewPanel, useOpenedWithView } from "./view-ui.tsx";
import { rememberTab } from "./desk-history.ts";
import { tabLabel, composerAnswers, defaultTab, showsDecision, threadTabs } from "./view.ts";
import { ChangesView } from "./changes-ui.tsx";
import { CiNotesScope } from "./ci-notes-ui.tsx";
import { ChangeList, useViewAction } from "./offers-ui.tsx";
import { DockRun, showCard } from "./runcard-ui.tsx";
import { dockRun, waitingRuns } from "./runcard.ts";
import { changeChip, headerPrs, prForEffect, settleLine, threadPrs } from "./offers.ts";
import { issueChip, issuePrefix, issuesNeedRepo, threadIssues } from "./issues.ts";
import { WakeChip } from "./wake-ui.tsx";
import { FolderChip } from "./folder-ui.tsx";
import { WaitsStrip } from "./waits-ui.tsx";
import { hasMultipleRepos, pickPr, prChip, shortRepo } from "./pr.ts";
import { PrView } from "./pr-ui.tsx";
import { UndeclaredStrip } from "./undeclared-ui.tsx";
import { LeftoverStrip } from "./leftovers-ui.tsx";
import { type AnswerField, sendFrom, Composer, type SendArgs } from "./composer.tsx";
import { AgentsStrip } from "./agents-strip.tsx";
import { DecisionCard } from "./decision-card.tsx";
import { Conversation } from "./conversation-ui.tsx";


interface ThreadPaneProps {
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
  onCard(fn: (cs: CardState) => CardState): void;
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
  const openedWithView = useOpenedWithView(t);
  const tab = defaultTab(t, pick?.id === t.id ? pick.tab : urlTab(t.id), openedWithView);
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
          <StateChip t={t} />
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
          {/* V0.folder-chip: the folder and its branch in one chip (it replaced the branch chip); a click shows the full
              path, the branch, the host and Copy path */}
          <FolderChip t={t} />
          {/* V0.filmstrip-fold: a hidden filmstrip comes back from here, beside the folder chip */}
          <PreviewsChip threadRef={t.id} host={t.hostId} events={p.events} onShow={() => setTab("conv")} />
          {/* V0.agent-pill: provider, model and effort in one pill; the model and the effort pick in place */}
          <AgentPill size="chip" segs={threadAgent(t, profile, provider, unavailableReason(t, "thread.setModel"), (a) => p.onRun("thread.setModel", a))} />
          {/* V0.usage-ledger: the thread's tokens so far; the phone header shows the same chip */}
          <UsageChip u={t.usage} />
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
          {/* V0.thread-issues: the linked issues beside the PRs; a click opens the issue on GitHub. The phone header has the same chips. */}
          {(() => {
            const issues = threadIssues(t);
            const need = issuesNeedRepo(issues, openPrs);
            return issues.map((i) => {
              const l = issueChip(i, issuePrefix(i, need));
              return (
                <a key={i.url} class={`chip pr issue ${l.cls}`} href={i.url} target="_blank" rel="noopener noreferrer" title={l.title}>
                  <span>{l.text}</span>
                </a>
              );
            });
          })()}
          {(() => {
            const c = changeChip(t); // V0.59 (8)
            return c ? (
              <button class={`chip prc ${c.cls}`} title={c.title} onClick={() => setTab("changes")}>
                <span>{c.text}</span>
              </button>
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
            <button key={k} class={tab === k ? "on" : ""} role="tab" aria-selected={tab === k} onClick={() => setTab(k)}>
              {tabLabel(t, k)}
            </button>
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
      <WaitsStrip t={t} onCancel={(rule) => p.onRun("thread.cancelWait", { rule })} onSnooze={(rule) => p.onRun("thread.snoozeUntilWait", { rule })} />
      <Composer key={t.id} t={t} now={p.now} onSend={(a) => sendFrom(tab, a, p.onSend, setTab)} onInterrupt={() => p.onRun("thread.interrupt")} onRestart={() => p.onRun("thread.restart")} answer={composerAnswers(tab) ? p.answerField : null} />
    </section>
  );
}

interface ThreadTabBodyProps {
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
  const previews = useMemo(() => previewsOf(events ?? [], t.hostId, api), [api, events, t.hostId]);
  const viewAction = useViewAction(run, t.cwd);
  const prList = prs ?? threadPrs(t);
  const currentPr = activePr ?? pickPr(prList, undefined, t.pr?.url);
  return (
    <>
      {tab === "view" && <LiveViewPanel full t={t} now={now} onPress={(text) => send({ text, from: "view" })} onAction={viewAction} />}
      {tab === "conv" && (
        <Conversation
          strip={<Filmstrip cards={previews} threadRef={t.id} phone={phone} />}
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
            <ChangesView host={t.hostId} threadId={t.id} changes={t.changes} phone={phone} onSend={(text) => send({ text })} sendOff={unavailableReason(t, "thread.send")} />
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

/** The thread ⋯ menu (V0.26): opens on click or Enter, arrows move, Esc closes and returns focus. V0.dialogs-review:
 *  its rows are the row menu's (menu-ui.tsx `MenuRows`, menu.ts `menuEntries`): one order, one look, a reason under
 *  an entry that is off, and a key shown beside an entry runs it while the menu is open. */
function ThreadMenu({ items, onPick }: { items: ThreadAction[]; onPick(a: ThreadAction): void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const entries = menuEntries(items);
  useKeepMenuFocus(ref);
  useEffect(() => {
    if (!open) return;
    ref.current?.querySelector<HTMLElement>("[role=menuitem]:not([aria-disabled=true])")?.focus();
    const away = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", away);
    return () => document.removeEventListener("mousedown", away);
  }, [open]);
  const run = (id: string) => {
    const a = items.find((x) => x.id === id);
    setOpen(false);
    if (a) onPick(a);
  };
  const onKey = (e: KeyboardEvent) => {
    // The menu owns its keys: the inbox shortcuts must not act while it has focus.
    e.stopPropagation();
    if (!open) return;
    const els = [...(ref.current?.querySelectorAll<HTMLElement>("[role=menuitem]:not([aria-disabled=true])") ?? [])];
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
    else if (!e.metaKey && !e.ctrlKey && !e.altKey) {
      const it = entryForKey(entries, e.key);
      if (it) (e.preventDefault(), run(it.id));
    }
  };
  return (
    <div class="tmenu" ref={ref} onKeyDown={onKey}>
      <button ref={btn} class="btn tmenu-b" aria-haspopup="menu" aria-expanded={open} aria-label="More actions" title="More actions" onClick={() => setOpen(!open)}>
        ⋯
      </button>
      {open && (
        <div class="tmenu-l" role="menu" aria-label="Thread actions">
          <MenuRows items={entries} onPick={(it) => run(it.id)} />
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






