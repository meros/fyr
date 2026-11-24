// New thread and the snooze menu.

import { useEffect, useRef, useState } from "preact/hooks";
import type { Project } from "./types.ts";
import { ProfileQuota } from "./quota-ui.tsx";
import { AgentPill, newThreadAgent } from "./agent-ui.tsx";
import { ChipStrip, ClipPicker } from "./attach-ui.tsx";
import { ProjectIcon, ProjectList } from "./project-ui.tsx";
import { HostField } from "./host-pick-ui.tsx";
import type { HostProfiles } from "./hosts.ts";
import type { HostId } from "./hostid.ts";
import { colorVar } from "./projects.ts";
import { customStart, customUntil, fieldTime, SNOOZE_CUSTOM_KEY, SNOOZE_CUSTOM_LABEL, type SnoozeChoice } from "./snooze.ts";
import type { HostInfo } from "./hosts.ts";
import { folderNote, LIVE_VIEW_HELP, promptPlaceholder, type NewThreadArgs } from "./new-thread.ts";
import { useNewThreadForm, type NewThreadFormOptions } from "./new-thread-form.ts";
import { SlashList, useSlash } from "./slash-ui.tsx";
import { DialogScrim } from "./motion-ui.tsx";
import { NewProjectStep } from "./project-picker.tsx";
import { QuietHold } from "./quiet-ui.tsx";
import { useGrow } from "./grow-ui.ts";
import { isTextTarget } from "./app-util.tsx";

export function Modal({ title, onClose, children, wide, cls, onKeyDown }: { title: string; onClose(): void; children: any; wide?: boolean; /** V0.119: "settings" keeps one size on every page */ cls?: string; /** the panel's own keys, before the app's */ onKeyDown?: (e: KeyboardEvent) => void }) {
  return (
    <DialogScrim onClose={onClose} onBack={onClose} label={title} cls={`${wide ? "wide" : ""}${cls ? ` ${cls}` : ""}`.trim()} onKeyDown={onKeyDown}>
      <div class="dh">
        <b>{title}</b>
        {/* data-close: the focus starts on the dialog's first control, not here (focus-trap.ts `firstFocus`) */}
        <button class="x" data-close onClick={onClose} aria-label="Close" title="Close (Esc)">
          ×
        </button>
      </div>
      {children}
    </DialogScrim>
  );
}

/** New thread on the desktop. V0.dialogs-review: the prompt first (the one field filled every time), the attach row
 *  under it, then what decides where it runs, each following from the one before: Host (with several), Project,
 *  Folder, Agent; then the rare options folded under More. The phone's screen (phone-new.tsx `MNew`) has the same
 *  fields, order and words. #41: the state is useNewThreadForm's, shared with the phone. */
export function NewThreadDialog(p: { onAddProject?: NewThreadFormOptions["onAddProject"]; profiles: HostProfiles; hosts?: HostInfo[]; projects: Project[]; projectFilter: string | null; clockOffset?: number; onClose(): void; onCreate(a: NewThreadArgs): Promise<string | null>; onNewProject(name: string, folder: string, host: HostId): Promise<{ error: string | null; id?: string }> }) {
  const n = useNewThreadForm(p);
  const { form: f, cur, chip, folder, profiles, att } = n;
  const promptRef = useRef<HTMLTextAreaElement>(null);
  const pickRef = useRef<HTMLButtonElement>(null);
  const body = useRef<HTMLDivElement>(null);
  // V0.79: `/` at the start lists the commands seen for this provider and folder.
  const slash = useSlash(n.commandSource, f.text, (text) => n.set({ text }), promptRef);
  useGrow(promptRef, f.text);
  // More is open when it holds something (a restored draft's title, Live view), and stays as the owner left it.
  const [more, setMore] = useState(() => !!f.title || f.liveView);

  // Desktop only: the prompt has the focus on open and again after every pick that took the focused control away
  // (V0.dialogs-review (9)), so typing goes on. The phone does neither: a focused field raises its keyboard.
  const toPrompt = () => promptRef.current?.focus();
  useEffect(toPrompt, []);

  const change = async () => {
    await n.changeFolder();
    toPrompt();
  };
  const start = async () => {
    const at = await n.submit();
    if (at === "prompt") toPrompt();
    else if (at === "name") body.current?.querySelector<HTMLInputElement>(".pform input")?.focus();
  };
  // V0.dialogs-review (3): Esc goes one level up: the `/` list (its own handler, before this one), the New project
  // step, the project list, and only then the dialog (app-keyboard.tsx), which keeps the draft.
  const onKey = (e: KeyboardEvent) => {
    if (e.key !== "Escape" || e.defaultPrevented) return;
    const list = f.creating === null;
    if (!n.levelUp()) return;
    e.preventDefault();
    e.stopPropagation();
    if (list) pickRef.current?.focus(); // the list closed: the focus is on the button that opens it
  };
  const enter = (e: KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      void start();
    }
  };

  return (
    <Modal title="New thread" onClose={p.onClose} wide onKeyDown={onKey}>
      <div class="db nt" ref={body}>
        <div class="fld ntp">
          <SlashList s={slash} />
          <textarea
            ref={promptRef}
            class="ntta"
            rows={5}
            value={f.text}
            aria-label="Prompt"
            placeholder={promptPlaceholder(att.chips.length > 0)}
            onInput={(e) => n.set({ text: (e.target as HTMLTextAreaElement).value })}
            onPaste={att.onPaste}
            onDrop={att.onDrop}
            onDragOver={att.onDragOver}
            onKeyDown={(e) => {
              if (slash.onKeyDown(e)) return;
              enter(e);
            }}
          />
        </div>
        <div class="att-row">
          <ClipPicker onFiles={att.add} />
          <ChipStrip chips={att.chips} onRemove={att.remove} />
          {!att.chips.length && <span class="srcl atth">Paste or drop files</span>}
        </div>
        {/* V0.139: the host decides the projects, folders and providers below it */}
        <HostField value={n.host} onPick={n.chooseHost} quiet what="The thread runs there. The projects, folders and providers below are that host's." />
        {f.reset && (
          <span class="srcl mnote" role="status">
            {f.reset}
          </span>
        )}
        <div class="fld">
          <span>Project</span>
          <button ref={pickRef} class="ppick" onClick={n.togglePicking} aria-expanded={f.picking || f.creating !== null} style={cur && !chip.isNew ? { "--pc": colorVar(cur.color, cur.name) } : undefined}>
            {chip.isNew ? <span class="pi all">+</span> : cur ? <ProjectIcon project={cur} name={cur.name} size={28} /> : <span class="pi all" />}
            <span class="plt">
              <span class="ell pln">{chip.name}</span>
              <span class="ell pls2">{chip.sub}</span>
            </span>
            <span class="chev">{f.picking || f.creating !== null ? "▾" : "▸"}</span>
          </button>
          {f.picking && f.creating === null && (
            <ProjectList
              projects={n.projects}
              selectedId={f.projectId ?? null}
              autoFocus
              allLabel="No project"
              initialQuery={n.listQuery}
              newRow={{ fromThread: false, sub: "A name, and a folder if you like", onNew: n.startCreating }}
              onPick={(pr) => (n.choose(pr), toPrompt())}
              extra={<QuietHold {...n.quiet} />}
            />
          )}
          {n.missing && (
            <button class="btn sm" onClick={() => void n.addHere().then(toPrompt)}>
              Add {n.missing.name} on {n.hostName}…
            </button>
          )}
          {f.creating !== null && (
            <NewProjectStep
              initialName={f.creating}
              initialFolder={f.draft?.folder ?? ""}
              fromThread={false}
              host={n.host}
              plain
              onDraft={n.setDraft}
              onCancel={n.cancelCreating}
              onCreate={async (name, dir) => {
                const e = await n.createProject(name, dir);
                if (!e) toPrompt();
                return e;
              }}
            />
          )}
        </div>
        {n.making ? (
          // V0.dialogs-review (6, 13): while a project is being made its own folder field is the folder; one line says
          // where the thread starts, about the project being made.
          <span class="srcl">{folderNote(cur, folder, n.making)}</span>
        ) : (
          <div class="fld">
            <span>Folder</span>
            <div class={`fline${folder.scratch ? " scratch" : ""}`}>
              <span class="ell" title={folder.cwd}>
                {folder.label}
              </span>
              <button class="btn sm" onClick={() => void change()}>
                Change…
              </button>
            </div>
            {folderNote(cur, folder) && <span class="srcl">{folderNote(cur, folder)}</span>}
          </div>
        )}
        {/* V0.agent-pill on V0.model-choice: provider · model (one choice) and effort, in one pill */}
        <div class="fld">
          <span>Agent</span>
          <AgentPill size="field" segs={newThreadAgent({ ...f, effort: n.effort, efforts: n.efforts }, profiles, n.pickModel, n.pickEffort)} />
        </div>
        {n.note && (
          <span class="srcl mnote" role="status">
            {n.note}
          </span>
        )}
        <ProfileQuota profile={f.profile} profiles={profiles} host={p.hosts?.find((h) => h.id === n.host)?.name} />
        <details class="ntmore" open={more} onToggle={(e) => setMore((e.currentTarget as HTMLDetailsElement).open)}>
          <summary>More</summary>
          <label class="fld">
            <span>Title</span>
            <input value={f.title} onInput={(e) => n.set({ title: (e.target as HTMLInputElement).value })} onKeyDown={enter} placeholder="From the prompt when empty" />
          </label>
          <label class="lvc">
            <input type="checkbox" checked={f.liveView} onChange={(e) => n.set({ liveView: (e.target as HTMLInputElement).checked })} />
            <span>
              Live view
              <span class="srcl">{LIVE_VIEW_HELP}</span>
            </span>
          </label>
        </details>
      </div>
      <div class="df">
        <StartNote n={n} />
        <button class="btn" onClick={p.onClose}>
          Cancel <kbd>Esc</kbd>
        </button>
        <button class="btn p" disabled={n.busy || !!n.block} onClick={() => void start()}>
          {n.startLabel} <kbd>⏎</kbd>
        </button>
      </div>
    </Modal>
  );
}

/** What stands beside New thread's Start on both fronts (the desktop's foot, the phone's bottom bar), always in
 *  view: the error of the last try (an alert), else why Start is off, and that a draft came back. */
export function StartNote({ n }: { n: Pick<ReturnType<typeof useNewThreadForm>, "err" | "block" | "restored" | "clear"> }) {
  return (
    <div class="dfn">
      {n.err ? (
        <span class="err" role="alert">
          {n.err}
        </span>
      ) : (
        n.block && <span>{n.block}</span>
      )}
      {n.restored && (
        <span>
          Draft restored ·{" "}
          <button class="dfl" onClick={n.clear}>
            Clear
          </button>
        </span>
      )}
    </div>
  );
}

/** V0.dialogs-review (15): a date and time of the owner's own, the last choice of the snooze list on both fronts.
 *  It starts on tomorrow 09:00 and takes no time before now. The phone differs in the classes only (a 52 px field
 *  and button for a finger), and its field is not focused on open: that would raise the system's picker at once. */
export function SnoozeCustom({ phone, onPick }: { phone?: boolean; onPick(c: Pick<SnoozeChoice, "until">): void }) {
  const [val, setVal] = useState(() => fieldTime(customStart(new Date())));
  const field = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!phone) field.current?.focus();
  }, [phone]);
  const until = customUntil(val, new Date());
  const go = () => until && onPick({ until });
  return (
    <div class={phone ? "m-custom" : "snzc"}>
      <input
        ref={field}
        type="datetime-local"
        value={val}
        min={fieldTime(new Date())}
        onInput={(e) => setVal((e.target as HTMLInputElement).value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") (e.preventDefault(), go());
        }}
        aria-label="Snooze until"
        aria-invalid={!until}
      />
      <button class={phone ? "m-bt p" : "btn p"} disabled={!until} title={until ? undefined : "Pick a time after now"} onClick={go}>
        Snooze
      </button>
    </div>
  );
}

export function SnoozeMenu(p: { presets: SnoozeChoice[]; count: number; onPick(p: Pick<SnoozeChoice, "until" | "done" | "ids">): void; onClose(): void }) {
  const [custom, setCustom] = useState(false);
  const menu = useRef<HTMLDivElement>(null);
  // The digits are the app's (app-keyboard.tsx); here the arrows walk the choices (the focus starts on the first,
  // V0.dialogs-review (10)) and `t` opens the date and time.
  const onKey = (e: KeyboardEvent) => {
    if (isTextTarget(e.target)) return;
    if (e.key === SNOOZE_CUSTOM_KEY && !custom) {
      e.preventDefault();
      return setCustom(true);
    }
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    e.preventDefault();
    const rows = [...(menu.current?.querySelectorAll<HTMLElement>("button.mi:not(:disabled)") ?? [])];
    const i = rows.indexOf(document.activeElement as HTMLElement) + (e.key === "ArrowDown" ? 1 : -1);
    rows[(i + rows.length) % rows.length]?.focus();
  };
  return (
    <Modal title={p.count > 1 ? `Snooze ${p.count} threads` : "Snooze"} onClose={p.onClose} onKeyDown={onKey}>
      <div class="menu" ref={menu}>
        {p.presets.map((s, i) => (
          <button class="mi" disabled={!!s.off} onClick={() => p.onPick(s)}>
            <kbd>{i + 1}</kbd>
            <span class="ell">{s.label}</span>
            <span class="when tnum">{s.off ?? s.when}</span>
          </button>
        ))}
        {custom ? (
          <SnoozeCustom onPick={p.onPick} />
        ) : (
          <button class="mi" onClick={() => setCustom(true)}>
            <kbd>{SNOOZE_CUSTOM_KEY}</kbd>
            <span class="ell">{SNOOZE_CUSTOM_LABEL}</span>
          </button>
        )}
      </div>
    </Modal>
  );
}
