// New thread, Adopt from T3, and the snooze menu.

import { useEffect, useRef, useState } from "preact/hooks";
import type { Profile, Project, T3Candidate } from "./types.ts";
import { ProfileQuota, quotaSuffix, useQuotas } from "./quota-ui.tsx";
import { ChipStrip, ClipPicker } from "./attach-ui.tsx";
import { NewProjectStep, ProjectIcon, ProjectList, QuietHold } from "./project-ui.tsx";
import { colorVar } from "./projects.ts";
import { relTime, shortModel } from "./format.ts";
import type { SnoozePreset } from "./store.ts";
import type { HostInfo } from "./hosts.ts";
import { folderHint, type NewThreadArgs } from "./new-thread.ts";
import { useNewThreadForm } from "./new-thread-form.ts";
import { SlashList, useSlash } from "./slash-ui.tsx";

export function Modal({ title, onClose, children, wide }: { title: string; onClose(): void; children: any; wide?: boolean }) {
  return (
    <div class="scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div class={`dlg${wide ? " wide" : ""}`} role="dialog" aria-modal="true" aria-label={title}>
        <div class="dh">
          <b>{title}</b>
          <button class="x" onClick={onClose} aria-label="Close" title="Close (Esc)">
            ×
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

/** SPEC 0.5 V0.30, V0.42: Project first; the folder follows from the project's folder, else a fresh scratch
 *  folder; "Change" opens the folder picker. #41: the state is useNewThreadForm's, shared with the phone. */
export function NewThreadDialog(p: { profiles: Profile[]; hosts?: HostInfo[]; projects: Project[]; projectFilter: string | null; clockOffset?: number; onClose(): void; onCreate(a: NewThreadArgs): Promise<string | null>; onNewProject(name: string, folder: string): Promise<{ error: string | null; id?: string }> }) {
  const n = useNewThreadForm(p);
  const { form: f, cur, chip, folder, profiles, prof, att } = n;
  const quotas = useQuotas();
  const promptRef = useRef<HTMLTextAreaElement>(null);
  // V0.79: `/` at the start lists the commands seen for this provider and folder.
  const slash = useSlash(n.commandSource, f.text, (text) => n.set({ text }), promptRef);

  useEffect(() => {
    promptRef.current?.focus();
  }, []);

  const change = async () => {
    await n.changeFolder();
    promptRef.current?.focus();
  };

  return (
    <Modal title="New thread" onClose={p.onClose} wide>
      <div class="db">
        <div class="fld">
          <span>Project</span>
          <button class="ppick" onClick={n.togglePicking} aria-expanded={f.picking} style={cur && !chip.isNew ? { "--pc": colorVar(cur.color, cur.name) } : undefined}>
            {chip.isNew ? <span class="pi all">+</span> : cur ? <ProjectIcon project={cur} name={cur.name} size={28} /> : <span class="pi all" />}
            <span class="plt">
              <span class="ell pln">{chip.name}</span>
              <span class="ell pls2">{chip.sub}</span>
            </span>
            <span class="chev">{f.picking ? "▾" : "▸"}</span>
          </button>
          {f.picking && f.creating === null && (
            <ProjectList
              projects={n.projects}
              selectedId={f.projectId ?? null}
              autoFocus
              allLabel="No project"
              newRow={{ fromThread: false, sub: "A name, and a folder if you like", onNew: n.startCreating }}
              onPick={n.choose}
              extra={<QuietHold {...n.quiet} />}
            />
          )}
          {f.creating !== null && <NewProjectStep initialName={f.creating} initialFolder="" fromThread={false} onDraft={n.setDraft} onCancel={n.cancelCreating} onCreate={n.createProject} />}
        </div>
        <div class="fld">
          <span>Folder</span>
          <div class={`fline${folder.scratch ? " scratch" : ""}`}>
            <span class="ell" title={folder.cwd}>
              {folder.label}
            </span>
            <button class="btn sm" onClick={() => void change()}>
              Change
            </button>
          </div>
          <span class="srcl">{folderHint(cur, folder)}</span>
        </div>
        <div class="row2">
          <label class="fld">
            <span>Profile</span>
            <select value={f.profile} onChange={(e) => n.pickProfile((e.target as HTMLSelectElement).value)}>
              {profiles.map((pr) => {
                const q = quotaSuffix(quotas, pr.id); // V0.44
                if (pr.unavailable) return <option value={pr.id} disabled title={pr.unavailable}>{`${pr.name} · needs setup`}</option>;
                return <option value={pr.id}>{q ? `${pr.name} · ${q}` : pr.name}</option>;
              })}
            </select>
          </label>
          <label class="fld">
            <span>Model</span>
            <select value={f.model} onChange={(e) => n.set({ model: (e.target as HTMLSelectElement).value })}>
              {(prof?.models ?? []).map((m) => (
                <option value={m}>{shortModel(m)}</option>
              ))}
            </select>
          </label>
          {n.efforts.length > 0 && (
            <label class="fld">
              <span>Effort</span>
              <select value={n.effort} onChange={(e) => n.set({ effortPick: (e.target as HTMLSelectElement).value })}>
                {!n.effort && <option value="">CLI default</option>}
                {n.efforts.map((x) => (
                  <option value={x}>{x}</option>
                ))}
              </select>
            </label>
          )}
        </div>
        <ProfileQuota profile={f.profile} profiles={profiles} />
        <label class="fld">
          <span>Title (optional)</span>
          <input value={f.title} onInput={(e) => n.set({ title: (e.target as HTMLInputElement).value })} placeholder="From the prompt when empty" />
        </label>
        <label class="fld">
          <span>Prompt</span>
          <SlashList s={slash} />
          <textarea
            ref={promptRef}
            rows={6}
            value={f.text}
            placeholder="What should the agent do? Paste or drop files here."
            onInput={(e) => n.set({ text: (e.target as HTMLTextAreaElement).value })}
            onPaste={att.onPaste}
            onDrop={att.onDrop}
            onDragOver={att.onDragOver}
            onKeyDown={(e) => {
              if (slash.onKeyDown(e)) return;
              if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
                e.preventDefault();
                void n.submit();
              }
            }}
          />
        </label>
        <div class="att-row">
          <ClipPicker onFiles={att.add} />
          <ChipStrip chips={att.chips} onRemove={att.remove} />
          <label class="lvc" title="The agent keeps a page of its state pinned above the conversation">
            <input type="checkbox" checked={f.liveView} onChange={(e) => n.set({ liveView: (e.target as HTMLInputElement).checked })} />
            <span>Live view</span>
          </label>
        </div>
        {n.err && <div class="err prose">{n.err}</div>}
      </div>
      <div class="df">
        <button class="btn" onClick={p.onClose}>
          Cancel <kbd>Esc</kbd>
        </button>
        <button class="btn p" disabled={n.busy} onClick={() => void n.submit()}>
          Start <kbd>⏎</kbd>
        </button>
      </div>
    </Modal>
  );
}

export function AdoptDialog(p: { load(): Promise<T3Candidate[]>; now: number; onClose(): void; onAdopt(ids: string[]): Promise<string | null> }) {
  const [cands, setCands] = useState<T3Candidate[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    p.load().then(setCands, () => setErr("Could not load T3 threads."));
  }, []);

  const toggle = (id: string) => {
    const n = new Set(picked);
    if (n.has(id)) n.delete(id);
    else n.add(id);
    setPicked(n);
  };

  const submit = async () => {
    if (!picked.size || busy) return;
    setBusy(true);
    const e = await p.onAdopt([...picked]);
    setBusy(false);
    if (e) setErr(e);
  };
  const submitRef = useRef(submit);
  submitRef.current = submit;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        void submitRef.current();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <Modal title="Adopt from T3" onClose={p.onClose} wide>
      <div class="db">
        <p class="lede">Open top-level T3 threads that Fyr does not track yet. Adopting keeps the Claude session, so the thread continues here.</p>
        {cands === null && !err && <div class="sys">Loading…</div>}
        {cands && cands.length === 0 && <div class="sys">No T3 threads to adopt.</div>}
        {cands && cands.length > 0 && (
          <div class="cands">
            {cands.map((c) => (
              <label class="cand">
                <input type="checkbox" checked={picked.has(c.t3Id)} onChange={() => toggle(c.t3Id)} />
                <span class="ct ell" title={c.title}>
                  {c.title}
                </span>
                <span class="age tnum">{relTime(c.updatedAt, p.now)}</span>
                <span class="cm ell" title={c.cwd}>
                  {c.cwd} · {c.model ? shortModel(c.model) : c.profile}
                  {c.hasTranscript === false ? " · no transcript" : ""}
                  {c.prUrl ? " · PR" : ""}
                </span>
              </label>
            ))}
          </div>
        )}
        {err && <div class="err prose">{err}</div>}
      </div>
      <div class="df">
        <button class="btn" onClick={p.onClose}>
          Cancel <kbd>Esc</kbd>
        </button>
        <button class="btn p" disabled={!picked.size || busy} onClick={() => void submit()}>
          Adopt {picked.size || ""} <kbd>⏎</kbd>
        </button>
      </div>
    </Modal>
  );
}

export function SnoozeMenu(p: { presets: SnoozePreset[]; count: number; onPick(p: SnoozePreset): void; onClose(): void }) {
  return (
    <Modal title={p.count > 1 ? `Snooze ${p.count} threads` : "Snooze"} onClose={p.onClose}>
      <div class="menu">
        {p.presets.map((s, i) => (
          <button class="mi" onClick={() => p.onPick(s)}>
            <kbd>{i + 1}</kbd>
            <span class="ell">{s.label}</span>
            <span class="when tnum">{s.when}</span>
          </button>
        ))}
      </div>
    </Modal>
  );
}
