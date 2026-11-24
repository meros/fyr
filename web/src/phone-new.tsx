// The phone's New thread screen (split from phone-sheets.tsx).

import { type PhoneProps, MHeader } from "./phone.tsx";
import { I } from "./phone-icons.tsx";
import { useRef, useState } from "preact/hooks";
import { AgentPill, newThreadAgent } from "./agent-ui.tsx";
import { ChipStrip, PhonePickers } from "./attach-ui.tsx";
import { SlashList, useSlash } from "./slash-ui.tsx";
import { ProjectIcon, ProjectList } from "./project-ui.tsx";
import { colorVar } from "./projects.ts";
import { folderNote, folderValue, LIVE_VIEW_HELP, moreSummary, promptPlaceholder } from "./new-thread.ts";
import { useNewThreadForm } from "./new-thread-form.ts";
import { HostField } from "./host-pick-ui.tsx";
import { ProfileQuota, QuotaList } from "./quota-ui.tsx";
import { NewProjectStep } from "./project-picker.tsx";
import { QuietHold } from "./quiet-ui.tsx";
import { DraftLine, StartNote } from "./new-thread-ui.tsx";
import { useAsk } from "./confirm-ui.tsx";
import { useGrow } from "./grow-ui.ts";
import { useBackLayer } from "./back-layer.ts";

/** New thread on the phone. V0.dialogs-review: the same fields, order and words as the desktop dialog (dialogs.tsx
 *  `NewThreadDialog`): the prompt, the attach row, Host, Project, Folder, Agent, More, and one Start button with the
 *  error beside it. Each difference from the desktop has its reason at the code that makes it. #41: the state is
 *  useNewThreadForm's, shared with the desktop dialog. */
export function MNew(p: PhoneProps) {
  const n = useNewThreadForm({ onAddProject: p.projectActions.add, projects: p.store.projects, profiles: p.profiles, hosts: p.store.hostList, projectFilter: p.filter.id, clockOffset: p.store.clockOffset, onCreate: p.actions.create, onNewProject: (name, folder, host) => p.projectActions.create(name, folder, [], host) });
  const { form: f, cur, chip, folder, profiles, att } = n;
  const ta = useRef<HTMLTextAreaElement>(null);
  // V0.79: `/` at the start lists the commands seen for this provider and folder.
  const slash = useSlash(n.commandSource, f.text, (text) => n.set({ text }), ta);
  useGrow(ta, f.text);
  const [more, setMore] = useState(() => !!f.title || f.liveView);
  // V0.dialogs-review (4): the project list and the New project step are layers over the screen: Back closes the
  // step, then the list, and only then leaves New thread (the desktop's Esc does the same, without history). The
  // header's × leaves the screen in one step; either way the draft stays.
  const chooser = f.picking || f.creating !== null;
  useBackLayer(chooser, n.levelUp);
  useBackLayer(f.creating !== null, n.levelUp);

  const ask = useAsk();
  /** The header's ×: the same question as the desktop's close when closing would lose a file for good. The system's
   *  Back cannot ask (the browser has left the entry by then): the draft's line says what was not kept. */
  const close = async () => {
    const q = n.closeQuestion;
    if (!q || (await ask({ title: "Close New thread?", text: q, confirm: "Close", danger: true }))) p.nav.back();
  };
  const fv = folderValue(cur, folder);
  // Enter in Title starts the thread, as on the desktop (a one-line field has no new line to make).
  const enter = (e: KeyboardEvent) => {
    if (e.key === "Enter" && !e.isComposing) {
      e.preventDefault();
      void n.submit("enter");
    }
  };

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
      <MHeader onBack={() => void close()} title="New thread" close />
      <div class="m-nbody" onDrop={att.onDrop} onDragOver={att.onDragOver}>
        <DraftLine n={n} />
        <SlashList s={slash} phone />
        {/* Not focused on open, and no return of the focus after a pick (the desktop does both): a focused field
            raises the keyboard over the form. Enter is a new line: the phone has no modifier key for one, so Start
            is the button. */}
        <textarea
          ref={ta}
          class="m-ta"
          rows={3}
          value={f.text}
          aria-label="Prompt"
          placeholder={promptPlaceholder(att.chips.length > 0)}
          onInput={(e) => n.set({ text: (e.target as HTMLTextAreaElement).value })}
          onKeyDown={(e) => void slash.onKeyDown(e)}
          onPaste={att.onPaste}
        />
        {/* Four buttons where the desktop has a paperclip and "Paste or drop files": touch has no drop, the clipboard
            needs a press, and camera and photo library are separate system pickers. */}
        <div class="m-capr">
          <button class="m-capb" onClick={() => void paste()}>
            {I.paste}
            Paste
          </button>
          <PhonePickers onFiles={att.add} big />
        </div>
        <ChipStrip chips={att.chips} onRemove={att.remove} />
        <div class="m-defs">
          {/* V0.139: the host decides the projects, folders and providers below it */}
          <HostField value={n.host} onPick={n.chooseHost} quiet what="The thread runs there. The projects, folders and providers below are that host's." />
          {f.reset && (
            <div class="m-mnote" role="status">
              {f.reset}
            </div>
          )}
          <div class="m-dl">Project</div>
          <button class="m-dchip m-ppick" onClick={n.togglePicking} aria-expanded={chooser} style={cur && !chip.isNew ? { "--pc": colorVar(cur.color, cur.name) } : undefined}>
            {chip.isNew ? <span class="pi all">+</span> : cur ? <ProjectIcon project={cur} name={cur.name} size={28} /> : <span class="pi all" />}
            <span class="plt">
              <span class="ell pln">{chip.name}</span>
              <span class="ell pls2">{chip.sub}</span>
            </span>
            {chooser ? I.up : I.down}
          </button>
          {f.picking && f.creating === null && (
            <div class="m-pickpanel">
              <ProjectList
                projects={n.projects}
                selectedId={f.projectId ?? null}
                allLabel="No project"
                initialQuery={n.listQuery}
                newRow={{ fromThread: false, sub: "A name, and a folder if you like", onNew: n.startCreating }}
                onPick={n.choose}
                extra={<QuietHold {...n.quiet} />}
              />
            </div>
          )}
          {n.missing && (
            // .btn, not the desktop's .btn.sm: a finger needs the 44 px of the phone's .btn.
            <button class="btn" onClick={() => void n.addHere()}>
              Add {n.missing.name} on {n.hostName}…
            </button>
          )}
          {f.creating !== null && (
            <div class="m-pickpanel">
              {/* as on the desktop: the line about where the thread starts above the step's buttons, its errors in
                  the bar that is always in view, Enter (the keyboard's Go) as the one primary button */}
              <NewProjectStep
                initialName={f.creating}
                initialFolder={f.draft?.folder ?? ""}
                fromThread={false}
                host={n.host}
                plain
                note={n.making ? folderNote(cur, folder, n.making) : undefined}
                onError={n.setErr}
                onEnter={n.block ? undefined : () => void n.submit()}
                onDraft={n.setDraft}
                onCancel={n.cancelCreating}
                onCreate={n.createProject}
              />
            </div>
          )}
          {!n.making && (
            <>
              <div class="m-dl">Folder</div>
              <div class={`m-dchip m-fline${fv.plain ? " scratch" : ""}`}>
                <span class="ell">{fv.label}</span>
                <button class="m-fchg" onClick={() => void n.changeFolder()}>
                  Change…
                </button>
              </div>
              {folderNote(cur, folder) && <div class="m-fhint">{folderNote(cur, folder)}</div>}
            </>
          )}
          <div class="m-dl">Agent</div>
          {/* V0.agent-pill on V0.model-choice: the desktop dialog's pill, at a finger's size */}
          <AgentPill size="field" phone segs={newThreadAgent({ ...f, effort: n.effort, efforts: n.efforts }, profiles, n.pickModel, n.pickEffort)} />
          {n.note && (
            <div class="m-mnote" role="status">
              {n.note}
            </div>
          )}
          {/* the picked provider's quota here, as on the desktop: what decides which provider to pick is not in a fold */}
          <ProfileQuota profile={f.profile} profiles={profiles} host={p.store.hostList.find((h) => h.id === n.host)?.name} />
          {/* The fold also holds every provider's quota, where the desktop's holds none: the phone has no rail that
              shows the others. Its summary names that too. */}
          <details class="ntmore" open={more} onToggle={(e) => setMore((e.currentTarget as HTMLDetailsElement).open)}>
            <summary>{moreSummary(f, ["every provider's quota"])}</summary>
            <div class="m-dl">Title</div>
            <input class="m-dchip m-title" value={f.title} onInput={(e) => n.set({ title: (e.target as HTMLInputElement).value })} onKeyDown={enter} placeholder="From the prompt when empty" aria-label="Title" enterkeyhint="go" />
            <label class="m-dchip m-lvc">
              <input type="checkbox" checked={f.liveView} onChange={(e) => n.set({ liveView: (e.target as HTMLInputElement).checked })} />
              <span>
                Live view
                <span class="srcl">{LIVE_VIEW_HELP}</span>
              </span>
            </label>
            <QuotaList profiles={p.profiles} host={p.store.hostList.length > 1 ? p.store.hostList.find((h) => h.id === n.host)?.name : undefined} />
          </details>
        </div>
      </div>
      <div class="m-abar">
        <StartNote n={n} />
        <button class="m-bt p" disabled={n.busy || !!n.block} onClick={() => void n.submit()}>
          {n.startLabel}
        </button>
      </div>
    </div>
  );
}
