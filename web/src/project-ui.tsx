// Project identity in the UI (SPEC 0.5 V0.23): icons, tags and lists, and the thread directory that started-by
// labels and cross-thread messages read (V0.22). The picker, the editor, linking and quiet hours are their own modules.

import { createContext, type ComponentChildren } from "preact";
import { useContext, useEffect, useRef, useState } from "preact/hooks";
import type { Project, Thread } from "./types.ts";
import { LINE_ICONS } from "./lucide.ts";
import { projectIconUrl, enterAction, exactProject, colorVar, matchProjects, newRowLabel, projectName, projectOf, projectSubtitle, resolveIcon } from "./projects.ts";
import { rowProjectName } from "./rows.ts";

/** Threads and projects for labels deep in the tree, and a way to open a thread or a project. */
export interface Directory {
  threads: Record<string, Thread>;
  projects: Project[];
  openThread(id: string): void;
  openProject(id: string): void;
  /** Open the project picker for a thread (V0.30). */
  pickProject(threadId: string): void;
  /** the project filter on the inbox (V0.135: then a row does not name its project) */
  projectFilter?: string | null;
}
export const DirectoryContext = createContext<Directory>({ threads: {}, projects: [], openThread: () => {}, openProject: () => {}, pickProject: () => {} });
export const useDirectory = () => useContext(DirectoryContext);

export function ProjectIcon({ project, name, size = 16 }: { project?: Project; name: string; size?: number }) {
  const nm = project?.name ?? name;
  // V0.116: the image icon where there is one; the glyph when there is none or it does not load.
  const url = projectIconUrl(project, size);
  const [broken, setBroken] = useState<string | null>(null);
  if (url && broken !== url)
    return (
      <span class="pi img" style={{ width: `${size}px`, height: `${size}px` }} aria-hidden="true">
        <img src={url} alt="" width={size} height={size} decoding="async" draggable={false} onError={() => setBroken(url)} />
      </span>
    );
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

/** V0.116: the mark beside a field the setup agent filled; the owner's change makes it theirs. V0.208: a host's too. */
export function SetBy({ of = "project" }: { of?: "project" | "host" }) {
  return (
    <em class="setby" title={`Set by the ${of} setup agent. Change it and it is yours.`}>
      Set by agent
    </em>
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

/** V0.135: a row's project, the icon alone before the title; the name is its tooltip. */
export function RowProject({ t, size }: { t: Pick<Thread, "project" | "projectId">; size: number }) {
  const dir = useDirectory();
  const p = projectOf(t, dir.projects);
  const nm = p?.name ?? projectName(t, dir.projects);
  return (
    <span class="rpi" title={nm} role="img" aria-label={nm}>
      <ProjectIcon project={p} name={nm} size={size} />
    </span>
  );
}

/** V0.135: the project's name on a row's second line, only when its icon is not an image and no filter is on. */
export function RowProjectName({ t, sep }: { t: Pick<Thread, "project" | "projectId">; sep: boolean }) {
  const dir = useDirectory();
  const nm = rowProjectName(projectOf(t, dir.projects), projectName(t, dir.projects), dir.projectFilter ?? null);
  if (!nm) return null;
  return (
    <>
      <span class="rpn" title={nm.endsWith("…") ? projectName(t, dir.projects) : undefined}>
        {nm}
      </span>
      {sep ? " · " : ""}
    </>
  );
}

/** "from <parent title>" (V0.22), linked to the parent. V0.135: in an inbox row (mark) only a quiet "↳"; many rows
 *  share one parent, so the title is the mark's tooltip and stays in full in the open thread's header. */
export function StartedBy({ parentId, link = true, mark = false }: { parentId?: string | null; link?: boolean; mark?: boolean }) {
  const dir = useDirectory();
  if (!parentId) return null;
  const title = dir.threads[parentId]?.title || "another thread";
  const text = mark ? "↳" : `from ${title}`;
  const cls = `from${mark ? " mark" : ""}`;
  if (!link) return <span class={cls} title={mark ? `from ${title}` : undefined} aria-label={mark ? `from ${title}` : undefined}>{text}</span>;
  return (
    <button
      class={`${cls} link`}
      title={mark ? `from ${title}` : `Started by “${title}”`}
      aria-label={mark ? `from ${title}` : undefined}
      onClick={(e) => {
        e.stopPropagation();
        dir.openThread(parentId);
      }}
    >
      {text}
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
interface NewRow {
  /** from a thread: "New project from this thread" */
  fromThread: boolean;
  /** what it starts with, one line */
  sub: string;
  onNew(query: string): void;
}

/** A searchable project list: the picker (Project…), the new-thread picker and the filter. Rows: New project, the
 *  null row (No project, All projects), the projects by last use, then any extra rows. */
export function ProjectList({ projects, selectedId, onPick, allLabel, autoFocus, newRow, extra }: { projects: Project[]; selectedId?: string | null; onPick(p: Project | null): void; allLabel?: string; autoFocus?: boolean; newRow?: NewRow; extra?: ComponentChildren }) {
  const [q, setQ] = useState("");
  const ref = useRef<HTMLInputElement>(null);
  const box = useRef<HTMLDivElement>(null);
  /* eslint-disable react-hooks/exhaustive-deps -- focus on mount only */
  useEffect(() => {
    if (autoFocus) ref.current?.focus();
  }, []);
  /* eslint-enable react-hooks/exhaustive-deps */
  const list = matchProjects(projects, q);
  // Keyboard: Enter in the search picks the first match, or names a new project when nothing matches; arrows walk the
  // rows and back to the search.
  const options = () => [...(box.current?.querySelectorAll<HTMLElement>("button") ?? [])];
  const onSearchKey = (e: KeyboardEvent) => {
    const act = e.key === "Enter" ? enterAction(projects, list, q, !!newRow) : null;
    if (act) {
      e.preventDefault();
      if ("pick" in act) onPick(act.pick);
      else newRow?.onNew(act.create);
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
              <span class="ell pln">{newRowLabel(q, !!exactProject(projects, q), newRow.fromThread)}</span>
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







