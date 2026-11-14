// New thread, Adopt from T3, and the snooze menu.

import { useEffect, useRef, useState } from "preact/hooks";
import type { ImageArg, Profile, Project, T3Candidate } from "./types.ts";
import { ImageStrip, useImagePaste } from "./thread.tsx";
import { relTime, shortModel } from "./format.ts";
import type { SnoozePreset } from "./store.ts";

function Modal({ title, onClose, children, wide }: { title: string; onClose(): void; children: any; wide?: boolean }) {
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
  images: ImageArg[];
  profile: string;
  model: string;
  title?: string;
}

export function NewThreadDialog(p: { profiles: Profile[]; projects: Project[]; onClose(): void; onCreate(a: NewThreadArgs): Promise<string | null> }) {
  const sortedProjects = [...p.projects].sort((a, b) => (b.lastUsed ?? "").localeCompare(a.lastUsed ?? ""));
  const [cwd, setCwd] = useState(sortedProjects[0]?.cwd ?? "");
  const [profile, setProfile] = useState(p.profiles[0]?.id ?? "");
  const prof = p.profiles.find((x) => x.id === profile);
  const [model, setModel] = useState(prof?.defaultModel ?? prof?.models[0] ?? "");
  const [text, setText] = useState("");
  const [title, setTitle] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const img = useImagePaste();
  const promptRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    promptRef.current?.focus();
  }, []);

  const pickProfile = (id: string) => {
    setProfile(id);
    const np = p.profiles.find((x) => x.id === id);
    setModel(np?.defaultModel ?? np?.models[0] ?? "");
  };

  const submit = async () => {
    if (busy) return;
    if (!cwd.trim()) return setErr("Pick a folder.");
    if (!text.trim() && !img.images.length) return setErr("Write a prompt.");
    setBusy(true);
    setErr(null);
    const e = await p.onCreate({ cwd: cwd.trim(), text, images: img.images, profile, model, title: title.trim() || undefined });
    setBusy(false);
    if (e) setErr(e);
  };

  return (
    <Modal title="New thread" onClose={p.onClose} wide>
      <div class="db">
        <label class="fld">
          <span>Folder</span>
          <input list="fyr-projects" value={cwd} onInput={(e) => setCwd((e.target as HTMLInputElement).value)} placeholder="/home/me/src/app" spellcheck={false} />
          <datalist id="fyr-projects">
            {sortedProjects.map((pr) => (
              <option value={pr.cwd}>{pr.name}</option>
            ))}
          </datalist>
        </label>
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
            placeholder="What should the agent do? Paste or drop screenshots here."
            onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
            onPaste={img.onPaste}
            onDrop={img.onDrop}
            onDragOver={img.onDragOver}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
                e.preventDefault();
                void submit();
              }
            }}
          />
        </label>
        <ImageStrip images={img.images} onRemove={img.remove} />
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
