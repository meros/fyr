// The phone's bottom sheets (split from phone.tsx). The New thread screen is phone-new.tsx.

import { type PhoneProps, ThreadQuotaRow } from "./phone.tsx";
import { I } from "./phone-icons.tsx";
import { BottomSheet } from "./motion-ui.tsx";
import { type ComponentChildren } from "preact";
import { useState } from "preact/hooks";
import type { Thread } from "./types.ts";
import { LoadSheetBody, useLoad } from "./load-ui.tsx";
import { stripHosts } from "./host-load.ts";
import { unavailableReason } from "./store.ts";
import { currentDecision } from "./decode.ts";
import { StateIcon, stateClass } from "./icons.tsx";
import { rowDetail, rowState, stateLine } from "./rows.ts";
import { SNOOZE_CUSTOM_LABEL, snoozeChoices, snoozeCommand, type SnoozeChoice } from "./snooze.ts";
import { AgentPill, threadAgent } from "./agent-ui.tsx";
import { rowPrs, threadPrs } from "./offers.ts";
import { actionGroups, actionsFor, runAction, type ActionHandlers, type ThreadAction } from "./actions.ts";
import { prChip, rowPrChip } from "./pr.ts";
import { needsCount } from "./board.ts";
import { HandoverForm } from "./handover-ui.tsx";
import { ProjectIcon, ProjectList, ProjectTag } from "./project-ui.tsx";
import { projectName, projectOf } from "./projects.ts";
import { tildePath } from "./paths.ts";
import { SnoozeCustom } from "./dialogs.tsx";
import { setToolDetails, toolDetails } from "./prefs.ts";
import type { Sheet } from "./nav.ts";
import { useSettingsEntry } from "./settings-entry.tsx";
import { PIN_ICON } from "./row-ui.tsx";
import { QuotaList } from "./quota-ui.tsx";
import { profilesOf, threadProvider, type HostInfo, type HostProfiles } from "./hosts.ts";
import { ProjectPicker } from "./project-picker.tsx";

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

/** The More sheet's Settings row. V0.settings-ia: the one count the desktop's gear shows ("2 need you"), and under the
 *  label the pages that hold it ("Sign-in 1 · Providers 1"), or what Settings holds while nothing waits. */
function MSettingsItem({ onClick }: { onClick(): void }) {
  const e = useSettingsEntry();
  return <Item label="Settings" sub={e.line} right={e.need || undefined} onClick={onClick} />;
}

/** V0.166, V0.host-tabs: the host load sheet: what the desktop rail's tooltips say, as text, and the host's provider quotas. */
function MLoadSheet({ hosts, profiles, onClose }: { hosts: HostInfo[]; profiles: HostProfiles; onClose(): void }) {
  const list = stripHosts(useLoad(), hosts);
  return (
    <MSheet onClose={onClose} label="Host load">
      <div class="m-shh">Host load</div>
      <LoadSheetBody hosts={list} renderQuotas={(host) => <QuotaList profiles={profiles} host={host} />} />
    </MSheet>
  );
}

export function MSheets(p: PhoneProps & { sheet: Sheet; t: Thread | undefined }) {
  const close = p.nav.back;
  const { sheet, t } = p;
  const ids = sheet.ids ?? [];
  const after = (fn: () => void) => () => {
    close();
    setTimeout(fn, 0);
  };
  if (sheet.kind === "snooze") {
    return <SnoozeSheet ids={ids} title={ids.length === 1 ? p.store.threads[ids[0]]?.title : undefined} onClose={close} threads={ids.flatMap((id) => p.store.threads[id] ?? [])} onPick={(pick) => {
      close();
      p.actions.snooze(snoozeCommand(pick, ids));
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
        <MSettingsItem onClick={() => p.nav.replace({ screen: { name: "settings" }, sheet: null })} />
        <Item label="Filter by project…" right={p.filter.id ? p.store.projects.find((x) => x.id === p.filter.id)?.name : "all"} onClick={() => p.nav.replace({ ...p.nav.at, sheet: { kind: "filter" } })} />
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
        {ids.length === 1 && p.store.threads[ids[0]]?.title && <div class="dsub ell">{p.store.threads[ids[0]].title}</div>}
        <div class="m-pform">
          <ProjectPicker
            phone
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
    // V0.dialogs-review (31): what the desktop's selection bar offers, for the phone's selection: settle, snooze,
    // file in a project, archive (last, apart, as in a thread's own sheet). Snooze and Project open their sheets
    // over this one, with the picked threads.
    const done = () => (p.list.setSelecting(false), p.list.setPicked(new Set()));
    return (
      <MSheet onClose={close} label="Selected threads">
        <div class="m-shh">{ids.length} selected</div>
        <Item label={`Settle ${ids.length}`} icon={<StateIcon state="settled" size={16} />} onClick={after(() => (p.actions.settle(ids, true), done()))} />
        <Item label="Snooze…" icon={<StateIcon state="snoozed" size={16} />} onClick={() => p.nav.replace({ ...p.nav.at, sheet: { kind: "snooze", ids } })} />
        <Item label="Project…" onClick={() => p.nav.replace({ ...p.nav.at, sheet: { kind: "project", ids } })} />
        <div class="m-mig rule" role="group">
          <Item label={`Archive ${ids.length}`} icon={I.close} onClick={after(() => (p.actions.archive(ids), done()))} />
        </div>
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
                const chip = rowPrChip(rowPrs(t));
                return [t.project, t.branch, chip?.text ?? null].filter(Boolean).join(" · ");
              })()}
            </div>
            {t.summary && (
              <div class="m-pkb prose">
                {threadProvider(t, p.profiles).name}: {t.summary}
              </div>
            )}
            {d && (
              <div class="m-pkq">
                <b class={stateClass(t.attention.state)}>{rowState(t)}</b>
                {rowDetail(t) ? ` · ${rowDetail(t)}` : ""}
              </div>
            )}
            <div class="m-pkn">Peek does not mark the thread seen.</div>
          </div>
        )}
        {!peek && <div class="m-shh ell">{t.title}</div>}
        {/* V0.dialogs-review: the one order and the groups of the action list (actions.ts), a rule between groups,
            as the desktop's two menus draw a line; Archive is last. */}
        {actionGroups(actionsFor("phone", t, peek ? "row" : "thread")).map((g, k) => (
          <div key={g[0].group} class={`m-mig${k ? " rule" : ""}`} role="group">
            {g.map((a) => {
              const l = look(a);
              // A row that is off names no swipe: the swipe does nothing then either.
              return <Item key={a.id} label={a.label} icon={l.icon} sub={a.disabled ?? a.hint} disabled={!!a.disabled} right={a.disabled ? undefined : l.right} onClick={pick(a)} />;
            })}
          </div>
        ))}
      </MSheet>
    );
  }
  if (sheet.kind === "handover" && t) {
    return (
      <MSheet onClose={close} label="Hand over">
        <div class="m-shh">Hand over</div>
        <HandoverForm
          phone
          t={t}
          profiles={profilesOf(t, p.profiles)}
          projects={p.store.projects}
          onClose={close}
          onConfirm={(pr, m, e) => p.commands.handover(t.id, pr, m, e)}
          onConfirmHost={async (args) => {
            const r = await p.commands.handoverHost(t.id, args);
            if (r.error) return r.error;
            if (r.threadId) p.nav.go({ screen: { name: "thread", id: r.threadId }, sheet: null });
            return null;
          }}
        />
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
          {row("State", stateLine(t))}
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
          {/* V0.dialogs-review (29): the host with several, and the folder short ("~/…"), as every other surface */}
          {p.store.hostList.length > 1 && t.host && row("Host", t.host)}
          {row("Folder", <span title={t.cwd}>{tildePath(t.cwd)}</span>)}
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
  return (
    <MSheet onClose={onClose} label="Snooze">
      {/* the dialog's own title, and the one thread's under it, as on the desktop (a long title is not the heading) */}
      <div class="m-shh">{ids.length > 1 ? `Snooze ${ids.length} threads` : "Snooze"}</div>
      {ids.length === 1 && title && <div class="dsub ell">{title}</div>}
      {/* no icon per row: seven moons said nothing, and the desktop's rows have none (its keys are the keyboard's) */}
      {presets.map((s) => (
        <Item label={s.label} sub={s.off} disabled={!!s.off} right={s.off ? undefined : s.when} onClick={() => onPick(s)} />
      ))}
      {/* V0.dialogs-review (15): the same last choice and the same field as the desktop's snooze dialog (dialogs.tsx).
          No Cancel row: a list sheet closes by its scrim, a drag down or Back, as the Project and Filter sheets do. */}
      {custom ? <SnoozeCustom phone onPick={onPick} onFold={() => setCustom(false)} /> : <Item label={SNOOZE_CUSTOM_LABEL} onClick={() => setCustom(true)} />}
    </MSheet>
  );
}
