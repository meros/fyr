// Project identity in the UI (SPEC 0.5 V0.23): icons, tags, lists, the project screen; the one picker and the
// one-step New project (V0.30);
// and the thread directory that started-by labels and cross-thread messages read (V0.22).

import { createContext, type ComponentChildren } from "preact";
import { useContext, useEffect, useRef, useState } from "preact/hooks";
import type { Profile, Project, ProjectSource, ThreadDTO } from "./types.ts";
import { LINE_ICONS } from "./lucide.ts";
import { FOLDERS_LINE, MAIN_LINE, NO_FOLDER_LINE, PALETTE_NAMES, colorName, colorVar, createFolderLine, matchProjects, monogram, newProjectDraft, newRowLabel, projectFolders, projectName, projectOf, projectRepos, projectSubtitle, repoLine, resolveIcon, shortPath, sourceText } from "./projects.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { shortModel } from "./format.ts";

/** Threads and projects for labels deep in the tree, and a way to open a thread or a project. */
export interface Directory {
  threads: Record<string, ThreadDTO>;
  projects: Project[];
  openThread(id: string): void;
  openProject(id: string): void;
  /** Open the project picker for a thread (V0.30); startNew focuses New project from this thread. */
  pickProject(threadId: string, startNew?: boolean): void;
  /** this host's name, for source lines */
  host: string;
}
export const DirectoryContext = createContext<Directory>({ threads: {}, projects: [], openThread: () => {}, openProject: () => {}, pickProject: () => {}, host: "" });
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

/** The picker's first row (V0.30): New project, named by the search text when nothing matches. */
export interface NewRow {
  /** from a thread: "New project from this thread" */
  fromThread: boolean;
  /** what it starts with, one line */
  sub: string;
  onNew(query: string): void;
}

/** A searchable project list: the picker (Project…), the new-thread picker and the filter. Rows: New project, the
 *  null row (No project, All projects), the projects by last use, then any extra rows. */
export function ProjectList({ projects, selectedId, onPick, allLabel, autoFocus, newRow, focusNew, extra }: { projects: Project[]; selectedId?: string | null; onPick(p: Project | null): void; allLabel?: string; autoFocus?: boolean; newRow?: NewRow; focusNew?: boolean; extra?: ComponentChildren }) {
  const [q, setQ] = useState("");
  const ref = useRef<HTMLInputElement>(null);
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (focusNew) box.current?.querySelector<HTMLElement>(".pli.new")?.focus();
    else if (autoFocus) ref.current?.focus();
  }, []);
  const list = matchProjects(projects, q);
  // Keyboard: Enter in the search picks the first match, or names a new project when nothing matches; arrows walk the
  // rows and back to the search.
  const options = () => [...(box.current?.querySelectorAll<HTMLElement>("button") ?? [])];
  const onSearchKey = (e: KeyboardEvent) => {
    if (e.key === "Enter" && q.trim()) {
      e.preventDefault();
      if (list.length) onPick(list[0]);
      else newRow?.onNew(q);
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
      <input ref={ref} class="pq" type="search" placeholder={newRow ? "Search, or name a new project" : "Search projects"} value={q} onInput={(e) => setQ((e.target as HTMLInputElement).value)} onKeyDown={onSearchKey} aria-label="Search projects" autocapitalize="off" />
      <div class="pls" role="listbox" ref={box} onKeyDown={onListKey}>
        {newRow && (
          <button class="pli new" role="option" aria-selected={false} onClick={() => newRow.onNew(q)}>
            <span class="pi all">+</span>
            <span class="plt">
              <span class="ell pln">{newRowLabel(q, list.length, newRow.fromThread)}</span>
              <span class="ell pls2">{newRow.sub}</span>
            </span>
          </button>
        )}
        {allLabel && !q && (
          <button class={`pli none${!selectedId ? " on" : ""}`} role="option" aria-selected={!selectedId} onClick={() => onPick(null)}>
            <span class="pi all" />
            <span class="plt">
              <span class="ell pln">{allLabel}</span>
            </span>
            {!selectedId && <span class="plc">✓</span>}
          </button>
        )}
        {list.map((p) => (
          <button class={`pli${selectedId && p.id === selectedId ? " on" : ""}`} role="option" aria-selected={!!selectedId && p.id === selectedId} style={{ "--pc": colorVar(p.color, p.name) }} onClick={() => onPick(p)}>
            <ProjectIcon project={p} name={p.name} size={28} />
            <span class="plt">
              <span class="ell pln">{p.name}</span>
              <span class="ell pls2">{projectSubtitle(p)}</span>
            </span>
            {selectedId && p.id === selectedId ? <span class="plc">✓</span> : p.threads ? <span class="plc">{p.threads}</span> : null}
          </button>
        ))}
        {!list.length && !newRow && <div class="plempty">No project matches “{q}”.</div>}
        {extra}
      </div>
    </div>
  );
}

/** New project, one step (V0.30, V0.42): a name (required), the folders it starts with (each says what it does; ×
 *  leaves it out), "Add folder…" through the folder picker, and the repo line the host will derive. The rest is on the
 *  project screen. */
export function NewProjectStep({ initialName, initialSources, host, fromThread, onCreate, onCancel }: { initialName: string; initialSources: ProjectSource[]; host: string; fromThread: boolean; onCreate(name: string, sources: ProjectSource[]): Promise<string | null>; onCancel(): void }) {
  const [name, setName] = useState(initialName);
  // V0.42: folders only; the repo follows from them.
  const [folders, setFolders] = useState(() => initialSources.filter((s) => s.kind === "folder"));
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const nameRef = useRef<HTMLInputElement>(null);
  const picker = useFolderPick();
  useEffect(() => {
    nameRef.current?.focus();
    nameRef.current?.select();
  }, []);
  const repos = folders.length ? projectRepos({ sources: initialSources }) : [];
  const submit = async () => {
    if (busy) return;
    if (!name.trim()) {
      nameRef.current?.focus();
      return setErr("A project needs a name.");
    }
    setBusy(true);
    setErr(null);
    const e = await onCreate(name.trim(), folders);
    setBusy(false);
    if (e) setErr(e);
  };
  const add = async () => {
    const path = await picker.pick({ title: "Add folder" });
    if (path && !folders.some((f) => (f as { path?: string }).path === path)) setFolders([...folders, { kind: "folder", host: "", path }]);
  };
  return (
    <div class="pform">
      <label class="fld">
        <span>Name</span>
        <input
          ref={nameRef}
          value={name}
          onInput={(e) => setName((e.target as HTMLInputElement).value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.isComposing) (e.preventDefault(), void submit());
          }}
          placeholder="Garden"
          autocapitalize="sentences"
        />
      </label>
      <div class="fld">
        <span>Folders</span>
        {folders.length ? (
          <ul class="srcs">
            {folders.map((f, i) => (
              <li class="src">
                <span class="srcb">
                  <span class="ell">{sourceText(f, host)}</span>
                  <span class="srcl">{createFolderLine(String((f as { path?: string }).path ?? ""))}</span>
                </span>
                <button class="src-x" aria-label={`Leave out ${sourceText(f, host)}`} title="Leave out" onClick={() => setFolders(folders.filter((_, k) => k !== i))}>
                  ×
                </button>
              </li>
            ))}
          </ul>
        ) : (
          <p class="lede">{NO_FOLDER_LINE}.</p>
        )}
        {repos.length > 0 && <p class="srcl">{repoLine(repos)}.</p>}
        <button class="btn sm addf" onClick={() => void add()}>
          Add folder…
        </button>
      </div>
      <p class="lede">Icon, colour and defaults are on the project screen, later.</p>
      {err && <div class="err prose">{err}</div>}
      <div class="pformb">
        <button class="btn" onClick={onCancel}>
          Back
        </button>
        <button class="btn p" disabled={busy} onClick={() => void submit()}>
          {fromThread ? "Create and file this thread" : "Create project"}
        </button>
      </div>
    </div>
  );
}

/** A project's folders (V0.42): the main one starred, a star makes another main, × removes; folders on another host
 *  show it and have no star. */
export function FolderList({ sources, host, onRemove, onSetMain }: { sources: ProjectSource[]; host: string; onRemove(i: number): void; onSetMain(i: number): void }) {
  const folders = projectFolders(sources, host);
  return (
    <ul class="srcs folders">
      {folders.map((f) => (
        <li class={`src${f.main ? " main" : ""}`}>
          {f.here ? (
            <button class={`star${f.main ? " on" : ""}`} aria-pressed={f.main} aria-label={f.main ? MAIN_LINE : `Make ${shortPath(f.path)} the main folder`} title={f.main ? MAIN_LINE : "Make this the main folder"} onClick={() => !f.main && onSetMain(f.index)}>
              {f.main ? "★" : "☆"}
            </button>
          ) : (
            <span class="star off" aria-hidden="true" />
          )}
          <span class="srcb">
            <span class="ell">{sourceText(sources[f.index], host)}</span>
            {f.main && <span class="srcl">{MAIN_LINE}</span>}
          </span>
          <button class="src-x" aria-label={`Remove ${sourceText(sources[f.index], host)}`} title="Remove" onClick={() => onRemove(f.index)}>
            ×
          </button>
        </li>
      ))}
    </ul>
  );
}

/** The one project picker (V0.30): file a thread, unfile it, or make a project from it in one step. */
export function ProjectPicker({ projects, threads, host, startNew, onPick, onCreate, onEdit }: { projects: Project[]; threads: ThreadDTO[]; host: string; startNew?: boolean; onPick(p: Project | null): void; onCreate(name: string, sources: ProjectSource[]): Promise<string | null>; onEdit?(p: Project): void }) {
  const one = threads.length === 1 ? threads[0] : undefined;
  const base = one ? newProjectDraft(one) : { name: "", sources: [] };
  const [draft, setDraft] = useState<{ name: string; sources: ProjectSource[] } | null>(null);
  const cur = one ? projectOf(one, projects) : undefined;
  if (draft) return <NewProjectStep initialName={draft.name} initialSources={draft.sources} host={host} fromThread={threads.length > 0} onCreate={onCreate} onCancel={() => setDraft(null)} />;
  const baseFolders = base.sources.filter((s) => s.kind === "folder");
  const sub = baseFolders.length ? `${base.name} · threads in ${baseFolders.map((s) => sourceText(s, host)).join(" and ")} land there` : "Just a name; file threads here by hand";
  return (
    <>
      <ProjectList
        projects={projects}
        selectedId={one ? (cur?.id ?? null) : undefined}
        autoFocus
        focusNew={startNew}
        allLabel="No project"
        newRow={{ fromThread: !!one, sub, onNew: (q) => setDraft({ name: q.trim() || base.name, sources: base.sources }) }}
        onPick={onPick}
      />
      {cur && onEdit && (
        <button class="plfoot" onClick={() => onEdit(cur)}>
          Edit {cur.name}…
        </button>
      )}
    </>
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

/** Edit a project's identity and defaults, and its folders (V0.42: the main star, ×, Add folder… through the picker;
 *  the repo line is read-only). */
export function ProjectEditor({ project, profiles, host, onSave, onAddFolder, onRemoveSource, onSetMain }: { project: Project; profiles: Profile[]; host: string; onSave(patch: ProjectPatch): Promise<string | null>; onAddFolder(path: string): Promise<string | null>; onRemoveSource(index: number): Promise<string | null>; onSetMain(index: number): Promise<string | null> }) {
  const picker = useFolderPick();
  const [name, setName] = useState(project.name);
  const [color, setColor] = useState(colorName(project.color, project.name));
  const [kind, setKind] = useState<"monogram" | "emoji" | "line">(resolveIcon(project.icon, project.name).kind);
  const [emoji, setEmoji] = useState(project.icon?.kind === "emoji" ? (project.icon.value ?? "") : "");
  const [line, setLine] = useState(resolveIcon(project.icon, project.name).kind === "line" ? resolveIcon(project.icon, project.name).value : "folder");
  const [profile, setProfile] = useState(project.profile ?? "");
  const [model, setModel] = useState(project.model ?? "");
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
  const folders = projectFolders(sources, host);
  const repos = projectRepos(project);
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
        <span>Folders</span>
        <p class="lede">{folders.length ? FOLDERS_LINE : `${NO_FOLDER_LINE}.`}</p>
        <FolderList
          sources={sources}
          host={host}
          onRemove={async (i) => {
            const e = await onRemoveSource(i);
            setMsg(e ? { text: e, err: true } : null);
          }}
          onSetMain={async (i) => {
            const e = await onSetMain(i);
            setMsg(e ? { text: e, err: true } : null);
          }}
        />
        {repos.length > 0 && <p class="srcl">{repoLine(repos)}.</p>}
        <button
          class="btn sm addf"
          onClick={async () => {
            const path = await picker.pick({ title: `Add folder to ${project.name}` });
            if (!path) return;
            const e = await onAddFolder(path);
            setMsg(e ? { text: e, err: true } : null);
          }}
        >
          Add folder…
        </button>
      </div>
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
      {msg && <div class={msg.err ? "err prose" : "lede"}>{msg.text}</div>}
      <div class="pformb">
        <button class="btn p" onClick={() => void save()}>
          Save
        </button>
      </div>
    </div>
  );
}
