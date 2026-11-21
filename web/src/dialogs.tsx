// New thread, Adopt from T3, and the snooze menu.

import { useEffect, useRef, useState } from "preact/hooks";
import type { Project, T3Candidate } from "./types.ts";
import { ProfileQuota } from "./quota-ui.tsx";
import { AgentPill, newThreadAgent } from "./agent-ui.tsx";
import { ChipStrip, ClipPicker } from "./attach-ui.tsx";
import { NewProjectStep, ProjectIcon, ProjectList, QuietHold } from "./project-ui.tsx";
import { HostField, useHosts } from "./host-pick-ui.tsx";
import { hostOfflineNote, pickerStart, type HostProfiles } from "./hosts.ts";
import type { HostId } from "./hostid.ts";
import { colorVar } from "./projects.ts";
import { relTime, shortModel } from "./format.ts";
import type { SnoozeChoice } from "./snooze.ts";
import type { HostInfo } from "./hosts.ts";
import { folderHint, type NewThreadArgs } from "./new-thread.ts";
import { useNewThreadForm, type NewThreadFormOptions } from "./new-thread-form.ts";
import { SlashList, useSlash } from "./slash-ui.tsx";
import { DialogScrim } from "./motion-ui.tsx";

export function Modal({ title, onClose, children, wide, cls }: { title: string; onClose(): void; children: any; wide?: boolean; /** V0.119: "settings" keeps one size on every page */ cls?: string }) {
  return (
    <DialogScrim onClose={onClose} onBack={onClose} label={title} cls={`${wide ? "wide" : ""}${cls ? ` ${cls}` : ""}`.trim()}>
      <div class="dh">
        <b>{title}</b>
        <button class="x" onClick={onClose} aria-label="Close" title="Close (Esc)">
          ×
        </button>
      </div>
      {children}
    </DialogScrim>
  );
}

/** SPEC 0.5 V0.30, V0.42: Project first; the folder follows from the project's folder, else a fresh scratch
 *  folder; "Change" opens the folder picker. #41: the state is useNewThreadForm's, shared with the phone. */
export function NewThreadDialog(p: { onAddProject?: NewThreadFormOptions["onAddProject"]; profiles: HostProfiles; hosts?: HostInfo[]; projects: Project[]; projectFilter: string | null; clockOffset?: number; onClose(): void; onCreate(a: NewThreadArgs): Promise<string | null>; onNewProject(name: string, folder: string, host: HostId): Promise<{ error: string | null; id?: string }> }) {
  const n = useNewThreadForm(p);
  const { form: f, cur, chip, folder, profiles, att } = n;
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
        {/* V0.139: host first; it decides the projects, folders and providers below */}
        <HostField value={n.host} onPick={n.chooseHost} what="The thread runs there. The projects, folders and providers below are that host's." />
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
          {n.missing && (
            <button class="btn sm" onClick={() => void n.addHere()}>
              Add {n.missing.name} on {n.host}…
            </button>
          )}
          {f.creating !== null && <NewProjectStep initialName={f.creating} initialFolder="" fromThread={false} host={n.host} onDraft={n.setDraft} onCancel={n.cancelCreating} onCreate={n.createProject} />}
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
        {/* V0.agent-pill on V0.model-choice: provider · model (one choice) and effort, in one pill */}
        <div class="fld">
          <span>Agent</span>
          <AgentPill size="field" segs={newThreadAgent({ ...f, effort: n.effort, efforts: n.efforts }, profiles, n.pickModel, (x) => n.set({ effortPick: x }))} />
        </div>
        {n.note && <span class="srcl mnote" role="status">{n.note}</span>}
        <ProfileQuota profile={f.profile} profiles={profiles} host={p.hosts?.find((h) => h.id === n.host)?.name} />
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

export function AdoptDialog(p: { load(host: HostId): Promise<T3Candidate[]>; now: number; onClose(): void; onAdopt(ids: string[], host: HostId): Promise<string | null> }) {
  const [cands, setCands] = useState<T3Candidate[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  // V0.139: T3's threads are a host's; the owner picks which (the picker starts on the first in the host order), and
  // they run there.
  const hs = useHosts();
  const [host, setHost] = useState<HostId | undefined>(() => pickerStart(hs.hosts));

  useEffect(() => {
    setCands(null);
    setErr(null);
    setPicked(new Set());
    if (!host || hostOfflineNote(host, hs.hosts, hs.now)) return;
    p.load(host).then(setCands, () => setErr("Could not load T3 threads."));
  }, [host]);

  const toggle = (id: string) => {
    const n = new Set(picked);
    if (n.has(id)) n.delete(id);
    else n.add(id);
    setPicked(n);
  };

  const submit = async () => {
    if (!picked.size || busy || !host) return;
    setBusy(true);
    const e = await p.onAdopt([...picked], host);
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
        <HostField value={host} onPick={setHost} what="T3's threads on that host; an adopted thread runs there." />
        <p class="lede">Open top-level T3 threads that Fyr does not track yet. Adopting keeps the Claude session, so the thread continues here.</p>
        {cands === null && !err && !hostOfflineNote(host, hs.hosts, hs.now) && <div class="sys">Loading…</div>}
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

export function SnoozeMenu(p: { presets: SnoozeChoice[]; count: number; onPick(p: SnoozeChoice): void; onClose(): void }) {
  return (
    <Modal title={p.count > 1 ? `Snooze ${p.count} threads` : "Snooze"} onClose={p.onClose}>
      <div class="menu">
        {p.presets.map((s, i) => (
          <button class="mi" disabled={!!s.off} onClick={() => p.onPick(s)}>
            <kbd>{i + 1}</kbd>
            <span class="ell">{s.label}</span>
            <span class="when tnum">{s.off ?? s.when}</span>
          </button>
        ))}
      </div>
    </Modal>
  );
}
