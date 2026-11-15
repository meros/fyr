// New thread, Adopt from T3, and the snooze menu.

import { useEffect, useRef, useState } from "preact/hooks";
import type { Profile, Project, T3Candidate } from "./types.ts";
import { ProfileQuota, quotaSuffix, useQuotas } from "./quota-ui.tsx";
import { ChipStrip, ClipPicker, useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { NewProjectStep, ProjectIcon, ProjectList } from "./project-ui.tsx";
import { LAST_PROJECT_KEY, colorVar, localProject, newThreadFolder, projectChip, projectDefaults, startProject } from "./projects.ts";
import { useFolderPick, type PickOptions } from "./folder-picker.tsx";
import { relTime, shortModel } from "./format.ts";
import { effortsFor, startEffort } from "./providers.ts";
import type { SnoozePreset } from "./store.ts";
import { hostForProject, profilesOn, type HostInfo } from "./hosts.ts";

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
  /** "" or missing: the project's main folder, else a fresh scratch folder (V0.42) */
  cwd?: string;
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
  /** V0.50: effort; "" is the provider's default */
  effort?: string;
}



/** The last new thread's project on this device (V0.30). */
export function lastProject(): string | null {
  try {
    return localStorage.getItem(LAST_PROJECT_KEY);
  } catch {
    return null;
  }
}

/** "Change" in a new thread offers the project's folder first when another one is picked (V0.42, V0.55). */
export function changeOptions(pr: Project | undefined, current: string): PickOptions {
  const f = pr?.folder;
  const dirs = f && f !== current ? [{ name: f.split("/").pop() || f, path: f }] : [];
  return { title: "Folder for this thread", first: dirs.length ? { title: `${pr!.name}'s folder`, dirs } : undefined };
}

/** SPEC 0.5 V0.30, V0.42: Project first; the folder follows from the project's folder, else a fresh scratch
 *  folder; "Change" opens the folder picker. */
export function NewThreadDialog(p: { profiles: Profile[]; hosts?: HostInfo[]; projects: Project[]; host: string; projectFilter: string | null; onClose(): void; onCreate(a: NewThreadArgs): Promise<string | null>; onNewProject(name: string, folder: string): Promise<{ error: string | null; id?: string }> }) {
  const [made, setMade] = useState<Project | null>(null);
  const projects = made && !p.projects.some((x) => x.id === made.id) ? [...p.projects, made] : p.projects;
  const [projectId, setProjectId] = useState<string | undefined>(() => startProject(p.projects, p.projectFilter, lastProject()));
  const cur = projects.find((x) => x.id === projectId);
  const [picking, setPicking] = useState(false);
  const [creating, setCreating] = useState<string | null>(null);
  // V0.63: the project being made in the New project step, as it is typed.
  const [draft, setDraft] = useState<{ name: string; folder: string } | null>(null);
  const making = creating !== null ? (draft ?? { name: creating, folder: "" }) : null;
  // V0.42: a folder picked for this thread; "" follows the project (V0.63: the one being made).
  const [picked, setPicked] = useState("");
  const folder = newThreadFolder(picked, making ? localProject("", making.name, making.folder) : cur);
  const chip = projectChip(cur, making);
  const picker = useFolderPick();
  // V0.54: a thread starts on its project's host, with that host's profiles; its files upload there.
  const target = hostForProject(cur?.host, p.hosts);
  const profiles = profilesOn(target, p.hosts, p.profiles);
  const startDefs = cur ? projectDefaults(cur, profiles) : {};
  // V0.50: a provider that cannot run (no token yet) is shown but not picked.
  const firstOk = profiles.find((x) => !x.unavailable)?.id ?? "";
  const [profile, setProfile] = useState(startDefs.profile && !profiles.find((x) => x.id === startDefs.profile)?.unavailable ? startDefs.profile : firstOk);
  const prof = profiles.find((x) => x.id === profile);
  const quotas = useQuotas();
  const [model, setModel] = useState(startDefs.model ?? prof?.defaultModel ?? prof?.models[0] ?? "");
  const [effortPick, setEffort] = useState("");
  const efforts = effortsFor(prof, model);
  const effort = startEffort(prof, model, effortPick);
  const [text, setText] = useState("");
  const [title, setTitle] = useState("");
  const [liveView, setLiveView] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const att = useAttachments(target);
  const promptRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    promptRef.current?.focus();
  }, []);

  const pickProfile = (id: string) => {
    setProfile(id);
    const np = profiles.find((x) => x.id === id);
    setModel(np?.defaultModel ?? np?.models[0] ?? "");
  };
  const choose = (pr: Project | null) => {
    setPicking(false);
    setCreating(null);
    setDraft(null);
    setProjectId(pr?.id);
    setPicked("");
    const there = profilesOn(hostForProject(pr?.host, p.hosts), p.hosts, p.profiles);
    const d = pr ? projectDefaults(pr, there) : {};
    if (d.profile) {
      setProfile(d.profile);
      if (d.model) setModel(d.model);
    } else if (!there.some((x) => x.id === profile)) {
      // V0.54: the project is on a host without this profile
      const np = there.find((x) => !x.unavailable);
      setProfile(np?.id ?? "");
      setModel(np?.defaultModel ?? np?.models[0] ?? "");
    }
  };
  const change = async () => {
    const path = await picker.pick(changeOptions(cur, folder.cwd ?? ""));
    if (path) setPicked(path);
    promptRef.current?.focus();
  };

  const submit = async () => {
    if (busy) return;
    const plan = sendArgs(text, att.chips);
    if (!plan.ok) return setErr(plan.reason === "Nothing to send" ? "Write a prompt or attach a file." : plan.reason);
    setBusy(true);
    setErr(null);
    // V0.63: Start with the New project step open makes the project first, then files the thread in it.
    let pid = projectId;
    if (making) {
      if (!making.name.trim()) return (setBusy(false), setErr("A project needs a name."));
      const r = await p.onNewProject(making.name.trim(), making.folder);
      if (r.error || !r.id) return (setBusy(false), setErr(r.error ?? "The project was not made."));
      const np = localProject(r.id, making.name.trim(), making.folder);
      setMade(np);
      choose(np);
      pid = r.id;
    }
    const e = await p.onCreate({ cwd: making && !picked ? making.folder || undefined : folder.cwd, text, attachments: plan.args.attachments, profile, model, effort: effortPick && effort ? effort : undefined, title: title.trim() || undefined, projectId: pid, view: liveView || undefined });
    setBusy(false);
    if (e) setErr(e);
  };

  return (
    <Modal title="New thread" onClose={p.onClose} wide>
      <div class="db">
        <div class="fld">
          <span>Project</span>
          <button class="ppick" onClick={() => (setPicking(!picking), setCreating(null), setDraft(null))} aria-expanded={picking} style={cur && !chip.isNew ? { "--pc": colorVar(cur.color, cur.name) } : undefined}>
            {chip.isNew ? <span class="pi all">+</span> : cur ? <ProjectIcon project={cur} name={cur.name} size={28} /> : <span class="pi all" />}
            <span class="plt">
              <span class="ell pln">{chip.name}</span>
              <span class="ell pls2">{chip.sub}</span>
            </span>
            <span class="chev">{picking ? "▾" : "▸"}</span>
          </button>
          {picking && creating === null && (
            <ProjectList
              projects={projects}
              selectedId={projectId ?? null}
              autoFocus
              allLabel="No project"
              newRow={{ fromThread: false, sub: "A name, and a folder if you like", onNew: (q) => setCreating(q.trim()) }}
              onPick={choose}
            />
          )}
          {creating !== null && (
            <NewProjectStep
              initialName={creating}
              initialFolder=""
              fromThread={false}
              onDraft={(name, folder) => setDraft({ name, folder })}
              onCancel={() => (setCreating(null), setDraft(null))}
              onCreate={async (name, folder) => {
                const r = await p.onNewProject(name, folder);
                if (r.error) return r.error;
                if (r.id) {
                  const np = localProject(r.id, name, folder);
                  setMade(np);
                  choose(np);
                }
                return null;
              }}
            />
          )}
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
            <select value={profile} onChange={(e) => pickProfile((e.target as HTMLSelectElement).value)}>
              {profiles.map((pr) => {
                const q = quotaSuffix(quotas, pr.id); // V0.44
                if (pr.unavailable) return <option value={pr.id} disabled title={pr.unavailable}>{`${pr.name} · needs setup`}</option>;
                return <option value={pr.id}>{q ? `${pr.name} · ${q}` : pr.name}</option>;
              })}
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
          {efforts.length > 0 && (
            <label class="fld">
              <span>Effort</span>
              <select value={effort} onChange={(e) => setEffort((e.target as HTMLSelectElement).value)}>
                {!effort && <option value="">CLI default</option>}
                {efforts.map((x) => (
                  <option value={x}>{x}</option>
                ))}
              </select>
            </label>
          )}
        </div>
        <ProfileQuota profile={profile} profiles={profiles} />
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

/** One line under the folder: how the folder and the project relate (V0.42, V0.55). */
export function folderHint(cur: Project | undefined, folder: { cwd?: string; scratch: boolean }): string {
  if (folder.scratch) return cur ? `${cur.name} has no folder; the thread is filed in it anyway.` : "No project: a new folder just for this thread.";
  if (!cur) return "No project.";
  return cur.folder && cur.folder === folder.cwd ? `${cur.name}'s folder.` : `Filed in ${cur.name}.`;
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
