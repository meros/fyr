// New thread, Adopt from T3, and the snooze menu.

import { useEffect, useRef, useState } from "preact/hooks";
import type { Profile, Project, ProjectSource, T3Candidate } from "./types.ts";
import { ChipStrip, ClipPicker, useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { NewProjectStep, ProjectIcon, ProjectList } from "./project-ui.tsx";
import { LAST_PROJECT_KEY, colorVar, localProject, projectDefaults, projectSubtitle, startProject } from "./projects.ts";
import { relTime, shortModel } from "./format.ts";
import type { SnoozePreset } from "./store.ts";

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

export interface NewThreadArgs {
  cwd: string;
  text: string;
  /** uploaded attachment ids (SPEC V0.18) */
  attachments?: string[];
  profile: string;
  model: string;
  title?: string;
  /** the picked project (V0.23): filed with thread.setProject after the start */
  projectId?: string;
  /** V0.37: start with a live view */
  view?: boolean;
}



/** The last new thread's project on this device (V0.30). */
export function lastProject(): string | null {
  try {
    return localStorage.getItem(LAST_PROJECT_KEY);
  } catch {
    return null;
  }
}

/** SPEC 0.5 V0.30: Project first; the folder follows from the project's folder on this host, else it is asked. */
export function NewThreadDialog(p: { profiles: Profile[]; projects: Project[]; host: string; projectFilter: string | null; onClose(): void; onCreate(a: NewThreadArgs): Promise<string | null>; onNewProject(name: string, sources: ProjectSource[]): Promise<{ error: string | null; id?: string }> }) {
  const [made, setMade] = useState<Project | null>(null);
  const projects = made && !p.projects.some((x) => x.id === made.id) ? [...p.projects, made] : p.projects;
  const [projectId, setProjectId] = useState<string | undefined>(() => startProject(p.projects, p.projectFilter, lastProject()));
  const cur = projects.find((x) => x.id === projectId);
  const [picking, setPicking] = useState(false);
  const [creating, setCreating] = useState<string | null>(null);
  const [folderOpen, setFolderOpen] = useState(false);
  const [cwd, setCwd] = useState(cur?.cwd ?? "");
  const startDefs = cur ? projectDefaults(cur, p.profiles) : {};
  const [profile, setProfile] = useState(startDefs.profile ?? p.profiles[0]?.id ?? "");
  const prof = p.profiles.find((x) => x.id === profile);
  const [model, setModel] = useState(startDefs.model ?? prof?.defaultModel ?? prof?.models[0] ?? "");
  const [text, setText] = useState("");
  const [title, setTitle] = useState("");
  const [liveView, setLiveView] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const att = useAttachments();
  const promptRef = useRef<HTMLTextAreaElement>(null);
  const folderRef = useRef<HTMLInputElement>(null);
  const askFolder = !cur?.cwd || folderOpen;

  useEffect(() => {
    promptRef.current?.focus();
  }, []);

  const pickProfile = (id: string) => {
    setProfile(id);
    const np = p.profiles.find((x) => x.id === id);
    setModel(np?.defaultModel ?? np?.models[0] ?? "");
  };
  const choose = (pr: Project | null) => {
    setPicking(false);
    setCreating(null);
    setProjectId(pr?.id);
    setFolderOpen(false);
    setCwd(pr?.cwd ?? "");
    if (pr) {
      const d = projectDefaults(pr, p.profiles);
      if (d.profile) {
        setProfile(d.profile);
        if (d.model) setModel(d.model);
      }
    }
    if (pr && !pr.cwd) setTimeout(() => folderRef.current?.focus(), 0);
  };

  const submit = async () => {
    if (busy) return;
    if (!cwd.trim()) {
      folderRef.current?.focus();
      return setErr(cur ? `${cur.name} has no folder on this computer: pick one for this thread.` : "Pick a folder for this thread.");
    }
    const plan = sendArgs(text, att.chips);
    if (!plan.ok) return setErr(plan.reason === "Nothing to send" ? "Write a prompt or attach a file." : plan.reason);
    setBusy(true);
    setErr(null);
    const e = await p.onCreate({ cwd: cwd.trim(), text, attachments: plan.args.attachments, profile, model, title: title.trim() || undefined, projectId, view: liveView || undefined });
    setBusy(false);
    if (e) setErr(e);
  };

  return (
    <Modal title="New thread" onClose={p.onClose} wide>
      <div class="db">
        <div class="fld">
          <span>Project</span>
          <button class="ppick" onClick={() => (setPicking(!picking), setCreating(null))} aria-expanded={picking} style={cur ? { "--pc": colorVar(cur.color, cur.name) } : undefined}>
            {cur ? <ProjectIcon project={cur} name={cur.name} size={28} /> : <span class="pi all" />}
            <span class="plt">
              <span class="ell pln">{cur?.name ?? "No project"}</span>
              <span class="ell pls2">{cur ? projectSubtitle(cur) : "Not filed; pick a folder below"}</span>
            </span>
            <span class="chev">{picking ? "▾" : "▸"}</span>
          </button>
          {picking && creating === null && (
            <ProjectList
              projects={projects}
              selectedId={projectId ?? null}
              autoFocus
              allLabel="No project"
              newRow={{ fromThread: false, sub: "Just a name; add a folder or repo if you like", onNew: (q) => setCreating(q.trim()) }}
              onPick={choose}
              extra={
                <button class="pli more" onClick={() => (choose(null), setFolderOpen(true), setTimeout(() => folderRef.current?.focus(), 0))}>
                  <span class="pi all">…</span>
                  <span class="plt">
                    <span class="ell pln">Pick a folder…</span>
                    <span class="ell pls2">No project; type the folder</span>
                  </span>
                </button>
              }
            />
          )}
          {creating !== null && (
            <NewProjectStep
              initialName={creating}
              initialSources={[]}
              host={p.host}
              fromThread={false}
              onCancel={() => setCreating(null)}
              onCreate={async (name, sources) => {
                const r = await p.onNewProject(name, sources);
                if (r.error) return r.error;
                if (r.id) {
                  const np = localProject(r.id, name, sources, p.host);
                  setMade(np);
                  choose(np);
                }
                return null;
              }}
            />
          )}
        </div>
        {askFolder ? (
          <label class="fld">
            <span>Folder</span>
            <input ref={folderRef} value={cwd} onInput={(e) => setCwd((e.target as HTMLInputElement).value)} placeholder="~/src/app" spellcheck={false} />
            {cur && !cur.cwd && <span class="srcl">{cur.name} has no folder on this computer. This thread runs here; add the folder to the project on its screen to make it the default.</span>}
          </label>
        ) : (
          <div class="fld">
            <span>Folder</span>
            <div class="fline">
              <span class="ell">{cwd.replace(/^\/home\/[^/]+(?=\/|$)/, "~")}</span>
              <button class="btn sm" onClick={() => (setFolderOpen(true), setTimeout(() => folderRef.current?.focus(), 0))}>
                Change
              </button>
            </div>
          </div>
        )}
        <div class="row2">
          <label class="fld">
            <span>Profile</span>
            <select value={profile} onChange={(e) => pickProfile((e.target as HTMLSelectElement).value)}>
              {p.profiles.map((pr) => (
                <option value={pr.id}>{pr.name}</option>
              ))}
            </select>
          </label>
          <label class="fld">
            <span>Model</span>
            <select value={model} onChange={(e) => setModel((e.target as HTMLSelectElement).value)}>
              {(prof?.models ?? []).map((m) => (
                <option value={m}>{shortModel(m)}</option>
              ))}
            </select>
          </label>
        </div>
        <label class="fld">
          <span>Title (optional)</span>
          <input value={title} onInput={(e) => setTitle((e.target as HTMLInputElement).value)} placeholder="From the prompt when empty" />
        </label>
        <label class="fld">
          <span>Prompt</span>
          <textarea
            ref={promptRef}
            rows={6}
            value={text}
            placeholder="What should the agent do? Paste or drop files here."
            onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
            onPaste={att.onPaste}
            onDrop={att.onDrop}
            onDragOver={att.onDragOver}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
                e.preventDefault();
                void submit();
              }
            }}
          />
        </label>
        <div class="att-row">
          <ClipPicker onFiles={att.add} />
          <ChipStrip chips={att.chips} onRemove={att.remove} />
          <label class="lvc" title="The agent keeps a page of its state pinned above the conversation">
            <input type="checkbox" checked={liveView} onChange={(e) => setLiveView((e.target as HTMLInputElement).checked)} />
            <span>Live view</span>
          </label>
        </div>
        {err && <div class="err prose">{err}</div>}
      </div>
      <div class="df">
        <button class="btn" onClick={p.onClose}>
          Cancel <kbd>Esc</kbd>
        </button>
        <button class="btn p" disabled={busy} onClick={() => void submit()}>
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
            <span class="when tnum">
              {s.until.toLocaleString(undefined, { weekday: "short", hour: "2-digit", minute: "2-digit", hourCycle: "h23" })}
            </span>
          </button>
        ))}
      </div>
    </Modal>
  );
}
