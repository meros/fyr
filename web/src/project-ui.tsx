// Project identity in the UI (SPEC 0.5 V0.23): icons, tags, lists, the project screen and a new-project form;
// and the thread directory that started-by labels and cross-thread messages read (V0.22).

import { createContext } from "preact";
import { useContext, useEffect, useRef, useState } from "preact/hooks";
import type { Profile, Project, ThreadDTO } from "./types.ts";
import { LINE_ICONS } from "./lucide.ts";
import { PALETTE_NAMES, colorName, colorVar, matchProjects, monogram, projectName, projectOf, projectSubtitle, resolveIcon } from "./projects.ts";
import { shortModel } from "./format.ts";

/** Threads and projects for labels deep in the tree, and a way to open a thread or a project. */
export interface Directory {
  threads: Record<string, ThreadDTO>;
  projects: Project[];
  openThread(id: string): void;
  openProject(id: string): void;
  /** Make the thread's suggested project (V0.28). */
  createProjectFrom(threadId: string): void;
}
export const DirectoryContext = createContext<Directory>({ threads: {}, projects: [], openThread: () => {}, openProject: () => {}, createProjectFrom: () => {} });
export const useDirectory = () => useContext(DirectoryContext);

export function ProjectIcon({ project, name, size = 16 }: { project?: Project; name: string; size?: number }) {
  const nm = project?.name ?? name;
  const icon = resolveIcon(project?.icon, nm);
  const color = colorVar(project?.color, nm);
  const style = { "--pc": color, width: `${size}px`, height: `${size}px`, fontSize: `${Math.round(size * (icon.kind === "emoji" ? 0.78 : icon.value.length > 1 ? 0.46 : 0.58))}px` };
  if (icon.kind === "line")
    return (
      <span class="pi line" style={style} aria-hidden="true">
        <svg viewBox="0 0 24 24" width={Math.round(size * 0.72)} height={Math.round(size * 0.72)} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" dangerouslySetInnerHTML={{ __html: LINE_ICONS[icon.value] }} />
      </span>
    );
  return (
    <span class={`pi ${icon.kind}`} style={style} aria-hidden="true">
      {icon.value}
    </span>
  );
}

/** Icon and name in the project colour. */
export function ProjectTag({ project, name, onClick, title }: { project?: Project; name: string; onClick?: (e: MouseEvent) => void; title?: string }) {
  const nm = project?.name ?? name;
  const body = (
    <>
      <ProjectIcon project={project} name={nm} size={14} />
      <span class="ptn">{nm}</span>
    </>
  );
  const style = { "--pc": colorVar(project?.color, nm) };
  return onClick ? (
    <button class="ptag" style={style} onClick={onClick} title={title ?? nm}>
      {body}
    </button>
  ) : (
    <span class="ptag" style={style} title={title ?? nm}>
      {body}
    </span>
  );
}

/** A thread's project tag from the directory. */
export function ThreadProject({ t }: { t: Pick<ThreadDTO, "project" | "projectId"> }) {
  const dir = useDirectory();
  return <ProjectTag project={projectOf(t, dir.projects)} name={projectName(t, dir.projects)} />;
}

/** "from <parent title>" (V0.22), linked to the parent. */
export function StartedBy({ parentId, link = true }: { parentId?: string | null; link?: boolean }) {
  const dir = useDirectory();
  if (!parentId) return null;
  const title = dir.threads[parentId]?.title || "another thread";
  if (!link) return <span class="from">from {title}</span>;
  return (
    <button
      class="from link"
      title={`Started by “${title}”`}
      onClick={(e) => {
        e.stopPropagation();
        dir.openThread(parentId);
      }}
    >
      from {title}
    </button>
  );
}

/** A message another thread's agent sent (V0.22): from that thread, not the user's bubble. */
export function FromThreadMessage({ fromId, text, children }: { fromId: string; text: string; children?: preact.ComponentChildren }) {
  const dir = useDirectory();
  const t = dir.threads[fromId];
  const p = t ? projectOf(t, dir.projects) : undefined;
  return (
    <div class="fromthread">
      <button class="fth" onClick={() => dir.openThread(fromId)} title="Open the thread that sent this">
        <ProjectIcon project={p} name={t?.project ?? "?"} size={16} />
        <span class="ell">{t?.title || "Another thread"}</span>
        <span class="ftw">wrote</span>
      </button>
      <div class="ftb prose">{text}</div>
      {children}
    </div>
  );
}

/** A searchable project list: the picker, Move to project and the filter. */
export function ProjectList({ projects, selectedId, onPick, onNew, allLabel, autoFocus }: { projects: Project[]; selectedId?: string | null; onPick(p: Project | null): void; onNew?: () => void; allLabel?: string; autoFocus?: boolean }) {
  const [q, setQ] = useState("");
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (autoFocus) ref.current?.focus();
  }, []);
  const box = useRef<HTMLDivElement>(null);
  const list = matchProjects(projects, q);
  // Keyboard: Enter in the search picks the first match; arrows walk the list and back to the search.
  const options = () => [...(box.current?.querySelectorAll<HTMLElement>("button") ?? [])];
  const onSearchKey = (e: KeyboardEvent) => {
    if (e.key === "Enter" && q.trim() && list.length) {
      e.preventDefault();
      onPick(list[0]);
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      options()[0]?.focus();
    }
  };
  const onListKey = (e: KeyboardEvent) => {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    e.preventDefault();
    const items = options();
    const i = items.indexOf(document.activeElement as HTMLElement) + (e.key === "ArrowDown" ? 1 : -1);
    if (i < 0) ref.current?.focus();
    else items[Math.min(i, items.length - 1)]?.focus();
  };
  return (
    <div class="plist">
      <input ref={ref} class="pq" type="search" placeholder="Search projects" value={q} onInput={(e) => setQ((e.target as HTMLInputElement).value)} onKeyDown={onSearchKey} aria-label="Search projects" />
      <div class="pls" role="listbox" ref={box} onKeyDown={onListKey}>
        {allLabel && !q && (
          <button class={`pli${!selectedId ? " on" : ""}`} role="option" aria-selected={!selectedId} onClick={() => onPick(null)}>
            <span class="pi all" />
            <span class="plt">
              <span class="ell">{allLabel}</span>
            </span>
          </button>
        )}
        {list.map((p) => (
          <button class={`pli${selectedId && p.id === selectedId ? " on" : ""}`} role="option" aria-selected={!!selectedId && p.id === selectedId} style={{ "--pc": colorVar(p.color, p.name) }} onClick={() => onPick(p)}>
            <ProjectIcon project={p} name={p.name} size={28} />
            <span class="plt">
              <span class="ell pln">{p.name}</span>
              <span class="ell pls2">{projectSubtitle(p)}</span>
            </span>
            {p.threads ? <span class="plc">{p.threads}</span> : null}
          </button>
        ))}
        {!list.length && <div class="plempty">No project matches “{q}”.</div>}
        {onNew && (
          <button class="pli new" onClick={onNew}>
            <span class="pi all">+</span>
            <span class="plt">
              <span class="ell">New project…</span>
            </span>
          </button>
        )}
      </div>
    </div>
  );
}

/** A new project: a name, and optionally a folder on this host or a repo URL. No source makes a label. */
export function NewProjectForm({ onCreate, onCancel, initialName }: { onCreate(name: string, folder: string, repo: string): Promise<string | null>; onCancel(): void; initialName?: string }) {
  const [name, setName] = useState(initialName ?? "");
  const [folder, setFolder] = useState("");
  const [repo, setRepo] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!name.trim()) return setErr("A project needs a name.");
    setBusy(true);
    const e = await onCreate(name.trim(), folder.trim(), repo.trim());
    setBusy(false);
    if (e) setErr(e);
  };
  return (
    <div class="pform">
      <label class="fld">
        <span>Name</span>
        <input value={name} onInput={(e) => setName((e.target as HTMLInputElement).value)} placeholder="Hemmet" autoFocus />
      </label>
      <label class="fld">
        <span>Folder on this host (optional)</span>
        <input value={folder} onInput={(e) => setFolder((e.target as HTMLInputElement).value)} placeholder="/home/me/src/app" spellcheck={false} />
      </label>
      <label class="fld">
        <span>Repo URL (optional)</span>
        <input value={repo} onInput={(e) => setRepo((e.target as HTMLInputElement).value)} placeholder="github.com/me/app" spellcheck={false} />
      </label>
      <p class="lede">Without a folder or a repo the project is a label: file threads into it by hand.</p>
      {err && <div class="err prose">{err}</div>}
      <div class="pformb">
        <button class="btn" onClick={onCancel}>
          Cancel
        </button>
        <button class="btn p" disabled={busy} onClick={() => void submit()}>
          Create project
        </button>
      </div>
    </div>
  );
}

const ICON_GRID = Object.keys(LINE_ICONS);

export interface ProjectPatch {
  name?: string;
  color?: string;
  icon?: { kind: string; value: string };
  profile?: string;
  model?: string;
}

/** Edit a project's identity and defaults, and add or remove its sources (a folder path or a repo URL). */
export function ProjectEditor({ project, profiles, onSave, onAddSource, onRemoveSource, onMoveThread, onNewProject }: { project: Project; profiles: Profile[]; onSave(patch: ProjectPatch): Promise<string | null>; onAddSource(value: string): Promise<string | null>; onRemoveSource(index: number): Promise<string | null>; onMoveThread?: () => void; onNewProject?: () => void }) {
  const [name, setName] = useState(project.name);
  const [color, setColor] = useState(colorName(project.color, project.name));
  const [kind, setKind] = useState<"monogram" | "emoji" | "line">(resolveIcon(project.icon, project.name).kind);
  const [emoji, setEmoji] = useState(project.icon?.kind === "emoji" ? (project.icon.value ?? "") : "");
  const [line, setLine] = useState(resolveIcon(project.icon, project.name).kind === "line" ? resolveIcon(project.icon, project.name).value : "folder");
  const [profile, setProfile] = useState(project.profile ?? "");
  const [model, setModel] = useState(project.model ?? "");
  const [repo, setRepo] = useState("");
  const [msg, setMsg] = useState<{ text: string; err: boolean } | null>(null);
  useEffect(() => {
    setName(project.name);
  }, [project.id]);
  const prof = profiles.find((p) => p.id === profile);
  const icon = kind === "emoji" ? { kind, value: emoji } : kind === "line" ? { kind, value: line } : { kind, value: "" };
  const preview: Project = { ...project, name: name || project.name, color, icon };
  const save = async () => {
    const e = await onSave({ name: name.trim() || project.name, color, icon, profile, model });
    setMsg(e ? { text: e, err: true } : { text: "Saved", err: false });
  };
  const sources = project.sources ?? [];
  return (
    <div class="pedit" style={{ "--pc": colorVar(color, name) }}>
      <div class="phead">
        <ProjectIcon project={preview} name={preview.name} size={44} />
        <div class="plt">
          <span class="pln big">{preview.name}</span>
          <span class="pls2">{projectSubtitle(project)}</span>
        </div>
      </div>
      <label class="fld">
        <span>Name</span>
        <input value={name} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
      </label>
      <div class="fld">
        <span>Colour</span>
        <div class="swatches" role="radiogroup" aria-label="Colour">
          {PALETTE_NAMES.map((c) => (
            <button class={`sw${c === color ? " on" : ""}`} role="radio" aria-checked={c === color} aria-label={c} title={c} style={{ "--pc": `var(--p-${c})` }} onClick={() => setColor(c)} />
          ))}
        </div>
      </div>
      <div class="fld">
        <span>Icon</span>
        <div class="seg" role="tablist">
          {(["monogram", "emoji", "line"] as const).map((k) => (
            <button class={kind === k ? "on" : ""} role="tab" aria-selected={kind === k} onClick={() => setKind(k)}>
              {k === "monogram" ? `Letters (${monogram(name || project.name)})` : k === "emoji" ? "Emoji" : "Icon"}
            </button>
          ))}
        </div>
        {kind === "emoji" && <input class="emoji" value={emoji} placeholder="🏡" onInput={(e) => setEmoji((e.target as HTMLInputElement).value)} aria-label="Emoji" />}
        {kind === "line" && (
          <div class="igrid">
            {ICON_GRID.map((n) => (
              <button class={`ig${n === line ? " on" : ""}`} title={n} aria-label={n} onClick={() => setLine(n)}>
                <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" dangerouslySetInnerHTML={{ __html: LINE_ICONS[n] }} />
              </button>
            ))}
          </div>
        )}
      </div>
      <div class="row2">
        <label class="fld">
          <span>Default profile</span>
          <select value={profile} onChange={(e) => (setProfile((e.target as HTMLSelectElement).value), setModel(""))}>
            <option value="">None</option>
            {profiles.map((p) => (
              <option value={p.id}>{p.name}</option>
            ))}
          </select>
        </label>
        <label class="fld">
          <span>Default model</span>
          <select value={model} onChange={(e) => setModel((e.target as HTMLSelectElement).value)} disabled={!prof}>
            <option value="">{prof ? "The profile's default" : "None"}</option>
            {(prof?.models ?? []).map((m) => (
              <option value={m}>{shortModel(m)}</option>
            ))}
          </select>
        </label>
      </div>
      <div class="fld">
        <span>Sources</span>
        <ul class="srcs">
          {sources.length === 0 && <li class="src none">No source: a label</li>}
          {sources.map((s, i) => (
            <li class="src">
              <b>{s.kind === "folder" ? "Folder" : s.kind === "repo" ? "Repo" : s.kind}</b>
              <span class="ell">{s.kind === "folder" ? `${String((s as { host?: string }).host ?? "")}: ${String((s as { path?: string }).path ?? "")}` : String((s as { url?: string }).url ?? JSON.stringify(s))}</span>
              <button
                class="src-x"
                aria-label="Remove source"
                title="Remove source"
                onClick={async () => {
                  const e = await onRemoveSource(i);
                  setMsg(e ? { text: e, err: true } : null);
                }}
              >
                ×
              </button>
            </li>
          ))}
        </ul>
        <div class="addsrc">
          <input value={repo} placeholder="/home/me/src/app or github.com/me/app" spellcheck={false} autocapitalize="off" onInput={(e) => setRepo((e.target as HTMLInputElement).value)} aria-label="Folder path or repo URL" />
          <button
            class="btn"
            disabled={!repo.trim()}
            onClick={async () => {
              const e = await onAddSource(repo.trim());
              setMsg(e ? { text: e, err: true } : null);
              if (!e) setRepo("");
            }}
          >
            Add source
          </button>
        </div>
      </div>
      {msg && <div class={msg.err ? "err prose" : "lede"}>{msg.text}</div>}
      <div class="pformb">
        {onNewProject && (
          <button class="btn" onClick={onNewProject}>
            New project…
          </button>
        )}
        {onMoveThread && (
          <button class="btn" onClick={onMoveThread}>
            Move this thread…
          </button>
        )}
        <button class="btn p" onClick={() => void save()}>
          Save
        </button>
      </div>
    </div>
  );
}
