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
import { arrowRow, customProblem, customStart, customUntil, fieldTime, SNOOZE_CUSTOM_FIELD, SNOOZE_CUSTOM_KEY, SNOOZE_CUSTOM_LABEL, type SnoozeChoice } from "./snooze.ts";
import { useBackLayer } from "./back-layer.ts";
import type { HostInfo } from "./hosts.ts";
import { folderNote, folderValue, LIVE_VIEW_HELP, moreSummary, promptPlaceholder, type NewThreadArgs } from "./new-thread.ts";
import { DraftLine, StartNote } from "./new-thread-ui.tsx";
import { useAsk } from "./confirm-ui.tsx";
import { I } from "./phone-icons.tsx";
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
  const ask = useAsk();
  // V0.79: `/` at the start lists the commands seen for this provider and folder.
  const slash = useSlash(n.commandSource, f.text, (text) => n.set({ text }), promptRef);
  useGrow(promptRef, f.text);
  // More is open when it holds something (a restored draft's title, Live view), and stays as the owner left it; its
  // summary says what is set in it when it is closed (new-thread.ts `moreSummary`).
  const [more, setMore] = useState(() => !!f.title || f.liveView);

  // Desktop only: the prompt has the focus on open and again after every pick that took the focused control away
  // (V0.dialogs-review (9)), so typing goes on. The phone does neither: a focused field raises its keyboard.
  const toPrompt = () => promptRef.current?.focus();
  useEffect(toPrompt, []);

  const change = async () => {
    await n.changeFolder();
    toPrompt();
  };
  const start = async (by?: "enter") => {
    const at = await n.submit(by);
    if (at === "prompt") toPrompt();
    else if (at === "name") body.current?.querySelector<HTMLInputElement>(".pform input")?.focus();
  };
  /** Close the form. What was typed stays as the draft; only what a close would lose for good asks first. */
  const close = async () => {
    const q = n.closeQuestion;
    if (!q || (await ask({ title: "Close New thread?", text: q, confirm: "Close", danger: true }))) p.onClose();
  };
  // V0.dialogs-review (3): Esc goes one level up: the `/` list (its own handler, before this one), the New project
  // step, the project list, and only then the dialog (app-keyboard.tsx), which keeps the draft.
  const onKey = (e: KeyboardEvent) => {
    // ⌘⏎ or Ctrl+⏎ presses the primary button from any field (the Agent pill, Title, the step's Name).
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.isComposing && !e.defaultPrevented) {
      e.preventDefault();
      e.stopPropagation();
      return void start();
    }
    if (e.key !== "Escape" || e.defaultPrevented) return;
    const list = f.creating === null;
    if (!n.levelUp()) {
      if (!n.closeQuestion) return; // the app closes the dialog
      e.preventDefault();
      e.stopPropagation();
      return void close();
    }
    e.preventDefault();
    e.stopPropagation();
    if (list) pickRef.current?.focus(); // the list closed: the focus is on the button that opens it
  };
  const enter = (e: KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey && !e.metaKey && !e.ctrlKey && !e.isComposing) {
      e.preventDefault();
      void start("enter");
    }
  };
  const fv = folderValue(cur, folder);

  return (
    <Modal title="New thread" onClose={() => void close()} wide onKeyDown={onKey}>
      <div class="db nt" ref={body}>
        <div class="fld ntp">
          <DraftLine n={n} />
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
            {/* the phone's chevron: one sign for "opens a list" on both fronts (▸ is the More fold's) */}
            <span class="chev">{f.picking || f.creating !== null ? I.up : I.down}</span>
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
              // V0.dialogs-review (6, 13): the project being made has the folder field; one line, above the step's
              // buttons, says where the thread starts.
              note={n.making ? folderNote(cur, folder, n.making) : undefined}
              // Its errors go to the foot, which is always in view (the step's own line can be under the fold).
              onError={n.setErr}
              // Enter in Name does what the form's one primary button says (create the project and start); with
              // nothing to send yet that button is off, and Enter makes the project, as the step's own button.
              onEnter={n.block ? undefined : () => void start()}
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
        {!n.making && (
          <div class="fld">
            <span>Folder</span>
            <div class={`fline${fv.plain ? " scratch" : ""}`}>
              <span class="ell" title={folder.cwd}>
                {fv.label}
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
          <summary>{moreSummary(f)}</summary>
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
        {/* "Close", not "Cancel": what was typed stays as the draft; Discard (beside the note) throws it away */}
        <button class="btn" onClick={() => void close()}>
          Close <kbd>Esc</kbd>
        </button>
        <button class="btn p" disabled={n.busy || !!n.block} onClick={() => void start()}>
          {n.startLabel} <kbd>⏎</kbd>
        </button>
      </div>
    </Modal>
  );
}

/** V0.dialogs-review (15): a date and time of the owner's own, the last choice of the snooze list on both fronts.
 *  It starts on tomorrow morning and takes no time before now; why its button is off is text under the field. Esc
 *  (desktop) and Back (phone) fold the field back to its row before they close the dialog (`onFold`). The phone
 *  differs in the classes (a 52 px field and button for a finger), and its field is not focused on open: that would
 *  raise the system's picker at once. */
export function SnoozeCustom({ phone, onPick, onFold }: { phone?: boolean; onPick(c: Pick<SnoozeChoice, "until">): void; onFold(): void }) {
  const [val, setVal] = useState(() => fieldTime(customStart(new Date())));
  const field = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!phone) field.current?.focus();
  }, [phone]);
  // The phone's Back folds the field: it is a layer over the Snooze sheet (back-layer.ts). The desktop's Esc is the
  // dialog's own key handler (SnoozeMenu).
  useBackLayer(!!phone, onFold);
  const until = customUntil(val, new Date());
  const why = customProblem(val, new Date());
  const go = () => until && onPick({ until });
  return (
    <div class={phone ? "m-customw" : "snzw"}>
      <span class="snzl">{SNOOZE_CUSTOM_FIELD}</span>
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
        <button class={phone ? "m-bt p" : "btn p"} disabled={!until} onClick={go}>
          Snooze
        </button>
      </div>
      {why && (
        <p class="snzy" role="alert">
          {why}
        </p>
      )}
    </div>
  );
}

export function SnoozeMenu(p: { presets: SnoozeChoice[]; count: number; /** the one thread's title, under the dialog's own */ title?: string; onPick(p: Pick<SnoozeChoice, "until" | "done" | "ids">): void; onClose(): void }) {
  const [custom, setCustom] = useState(false);
  const menu = useRef<HTMLDivElement>(null);
  const rows = () => [...(menu.current?.querySelectorAll<HTMLElement>("button.mi:not(:disabled)") ?? [])];
  const fold = () => {
    setCustom(false);
    // the focus goes back to the row that opened the field (it is the last row again after this render)
    setTimeout(() => rows().at(-1)?.focus(), 0);
  };
  // The digits are the app's (app-keyboard.tsx); here the arrows walk the choices (the focus starts on the first,
  // V0.dialogs-review (10)), `t` opens the date and time, and Esc folds it back before it closes the dialog.
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape" && custom && !e.defaultPrevented) {
      e.preventDefault();
      e.stopPropagation();
      return fold();
    }
    if (isTextTarget(e.target)) return;
    if (e.key === SNOOZE_CUSTOM_KEY && !custom) {
      e.preventDefault();
      return setCustom(true);
    }
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    e.preventDefault();
    const list = rows();
    list[arrowRow(list.indexOf(document.activeElement as HTMLElement), list.length, e.key === "ArrowDown")]?.focus();
  };
  return (
    <Modal title={p.count > 1 ? `Snooze ${p.count} threads` : "Snooze"} onClose={p.onClose} onKeyDown={onKey}>
      {p.title && <div class="dsub ell">{p.title}</div>}
      <div class="menu" ref={menu}>
        {p.presets.map((s, i) => (
          <button class="mi" disabled={!!s.off} onClick={() => p.onPick(s)}>
            <kbd>{i + 1}</kbd>
            <span class="ell">{s.label}</span>
            <span class="when tnum">{s.off ?? s.when}</span>
          </button>
        ))}
        {custom ? (
          <SnoozeCustom onPick={p.onPick} onFold={fold} />
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
