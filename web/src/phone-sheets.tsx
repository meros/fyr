// The phone's bottom sheets and the New thread screen (split from phone.tsx).

import { type PhoneProps, ThreadQuotaRow, MHeader } from "./phone.tsx";
import { I } from "./phone-icons.tsx";
import { BottomSheet } from "./motion-ui.tsx";
import { useApi } from "./api-context.ts";
import { type ComponentChildren } from "preact";
import { useRef, useState } from "preact/hooks";
import type { Thread } from "./types.ts";
import { LoadSheetBody, useLoad } from "./load-ui.tsx";
import { stripHosts } from "./host-load.ts";
import { unavailableReason } from "./store.ts";
import { currentDecision } from "./decode.ts";
import { StateIcon } from "./icons.tsx";
import { rowDetail, rowState, stateLabel } from "./rows.ts";
import { snoozeChoices, snoozeCommand, type SnoozeChoice } from "./snooze.ts";
import { AgentPill, newThreadAgent, threadAgent } from "./agent-ui.tsx";
import { ChipStrip, PhonePickers } from "./attach-ui.tsx";
import { SlashList, useSlash } from "./slash-ui.tsx";
import { headerPrs, threadPrs } from "./offers.ts";
import { actionsFor, runAction, type ActionHandlers, type ThreadAction } from "./actions.ts";
import { prChip, rowPrChip } from "./pr.ts";
import { needsCount } from "./board.ts";
import { HandoverForm } from "./handover-ui.tsx";
import { ProjectIcon, ProjectList, ProjectTag } from "./project-ui.tsx";
import { colorVar, projectName, projectOf } from "./projects.ts";
import { folderHint } from "./new-thread.ts";
import { useNewThreadForm } from "./new-thread-form.ts";
import { setToolDetails, toolDetails } from "./prefs.ts";
import type { Sheet } from "./nav.ts";
import { hostsNeed, settingsNeed } from "./settings-nav.ts";
import { HostField } from "./host-pick-ui.tsx";
import { PIN_ICON } from "./row-ui.tsx";
import { QuotaList } from "./quota-ui.tsx";
import { profilesOf, threadProvider, type HostInfo, type HostProfiles } from "./hosts.ts";
import { ProjectPicker, NewProjectStep } from "./project-picker.tsx";
import { QuietHold } from "./quiet-ui.tsx";
import { useProvidersNeed } from "./settings-load.ts";

// ---- new thread ----

/** SPEC 0.5 V0.30: Project first; the folder follows from the project's folder on this host, else it is asked.
 *  #41: the state is useNewThreadForm's, shared with the desktop dialog. */
export function MNew(p: PhoneProps) {
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
          <QuotaList profiles={p.profiles} host={p.store.hostList.length > 1 ? p.store.hostList.find((h) => h.id === n.host)?.name : undefined} />
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
        <MSettingsItem hosts={p.store.hostList} onClick={() => p.nav.replace({ screen: { name: "settings" }, sheet: null })} />
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
                <b class="s-ask">{rowState(t)}</b>
                {rowDetail(t) ? ` · ${rowDetail(t)}` : ""}
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

