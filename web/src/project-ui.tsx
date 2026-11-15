// Project identity in the UI (SPEC 0.5 V0.23): icons, tags, lists, the project screen; the one picker and the
// one-step New project (V0.30);
// and the thread directory that started-by labels and cross-thread messages read (V0.22).

import { createContext, type ComponentChildren } from "preact";
import { useContext, useEffect, useRef, useState } from "preact/hooks";
import type { Profile, Project, QuietHours, Thread } from "./types.ts";
import { DEFAULT_QUIET, QUIET_PRESETS, WEEK, cleanWindow, hasWindow, isAllDay, quietLine, quietList, quietNote, setAllDay, toggleDay } from "./quiet.ts";
import { LINE_ICONS } from "./lucide.ts";
import { FOLDER_LINE, NO_FOLDER_LINE, PALETTE_NAMES, colorName, enterAction, exactProject, colorVar, matchProjects, monogram, newProjectDraft, newRowLabel, projectName, projectOf, projectSubtitle, resolveIcon, shortPath } from "./projects.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { deleteProjectAsk } from "./confirm.ts";
import { useAsk } from "./confirm-ui.tsx";
import { shortModel } from "./format.ts";

/** Threads and projects for labels deep in the tree, and a way to open a thread or a project. */
export interface Directory {
  threads: Record<string, Thread>;
  projects: Project[];
  openThread(id: string): void;
  openProject(id: string): void;
  /** Open the project picker for a thread (V0.30). */
  pickProject(threadId: string): void;
  /** this host's name */
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
export function ThreadProject({ t }: { t: Pick<Thread, "project" | "projectId"> }) {
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
export function ProjectList({ projects, selectedId, onPick, allLabel, autoFocus, newRow, extra }: { projects: Project[]; selectedId?: string | null; onPick(p: Project | null): void; allLabel?: string; autoFocus?: boolean; newRow?: NewRow; extra?: ComponentChildren }) {
  const [q, setQ] = useState("");
  const ref = useRef<HTMLInputElement>(null);
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (autoFocus) ref.current?.focus();
  }, []);
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

/** A project's one folder (V0.55): the short path and what it does with "Change…" through the host folder picker, or
 *  that threads are filed by hand with "Choose folder…". × leaves the folder out where onClear is given. */
export function FolderField({ folder, title, onPick, onClear }: { folder: string; title: string; onPick(path: string): void; onClear?(): void }) {
  const picker = useFolderPick();
  const pick = async () => {
    const path = await picker.pick({ title });
    if (path) onPick(path);
  };
  return (
    <div class="fld">
      <span>Folder</span>
      {folder ? (
        <div class="src pfolder">
          <span class="srcb">
            <span class="ell" title={folder}>
              {shortPath(folder)}
            </span>
            <span class="srcl">{FOLDER_LINE}</span>
          </span>
          {onClear && (
            <button class="src-x" aria-label={`Leave out ${shortPath(folder)}`} title="Leave out" onClick={onClear}>
              ×
            </button>
          )}
        </div>
      ) : (
        <p class="lede">{NO_FOLDER_LINE}.</p>
      )}
      <button class="btn sm pfchg" onClick={() => void pick()}>
        {folder ? "Change…" : "Choose folder…"}
      </button>
    </div>
  );
}

/** New project, one step (V0.30, V0.55): a name (required) and the one folder, prefilled from the thread. The rest is
 *  on the project screen. */
export function NewProjectStep({ initialName, initialFolder, fromThread, onCreate, onCancel, onDraft }: { initialName: string; initialFolder: string; fromThread: boolean; onCreate(name: string, folder: string): Promise<string | null>; onCancel(): void; /** V0.63: the name and folder as they are typed */ onDraft?(name: string, folder: string): void }) {
  const [name, setName] = useState(initialName);
  const [folder, setFolder] = useState(initialFolder);
  useEffect(() => onDraft?.(name, folder), [name, folder]);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const nameRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    nameRef.current?.focus();
    nameRef.current?.select();
  }, []);
  const submit = async () => {
    if (busy) return;
    if (!name.trim()) {
      nameRef.current?.focus();
      return setErr("A project needs a name.");
    }
    setBusy(true);
    setErr(null);
    const e = await onCreate(name.trim(), folder);
    setBusy(false);
    if (e) setErr(e);
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
          placeholder="Hemmet"
          autocapitalize="sentences"
        />
      </label>
      <FolderField folder={folder} title="Project folder" onPick={setFolder} onClear={() => setFolder("")} />
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

/** The one project picker (V0.30): file a thread, unfile it, or make a project from it in one step. */
export function ProjectPicker({ projects, threads, onPick, onCreate, onEdit }: { projects: Project[]; threads: Thread[]; onPick(p: Project | null): void; onCreate(name: string, folder: string): Promise<string | null>; onEdit?(p: Project): void }) {
  const one = threads.length === 1 ? threads[0] : undefined;
  const base = one ? newProjectDraft(one) : { name: "", folder: "" };
  const [draft, setDraft] = useState<{ name: string; folder: string } | null>(null);
  const cur = one ? projectOf(one, projects) : undefined;
  if (draft) return <NewProjectStep initialName={draft.name} initialFolder={draft.folder} fromThread={threads.length > 0} onCreate={onCreate} onCancel={() => setDraft(null)} />;
  const sub = base.folder ? `${base.name} · threads in ${shortPath(base.folder)} land there` : "Just a name; file threads here by hand";
  return (
    <>
      <ProjectList
        projects={projects}
        selectedId={one ? (cur?.id ?? null) : undefined}
        autoFocus
        allLabel="No project"
        newRow={{ fromThread: !!one, sub, onNew: (q) => setDraft({ name: q.trim() || base.name, folder: base.folder }) }}
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
  /** V0.46: quiet hours, a list of windows (decision 0014 addendum); null turns them off */
  quiet?: QuietHours[] | null;
}

const HOURS = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, "0"));
const MINUTES = Array.from({ length: 12 }, (_, i) => String(i * 5).padStart(2, "0"));

/** A 24-hour HH:MM picker from two selects, so no browser locale turns it into AM/PM (decision 0014 addendum). */
function TimePick({ value, label, onChange }: { value: string; label: string; onChange(v: string): void }) {
  const m = /^(\d{1,2}):(\d\d)$/.exec(value);
  const h = m ? m[1].padStart(2, "0") : "00";
  const mm = m ? m[2] : "00";
  const mins = MINUTES.includes(mm) ? MINUTES : [...MINUTES, mm].sort();
  return (
    <span class="qtime" role="group" aria-label={label}>
      <select value={h} aria-label={`${label}, hour`} onChange={(e) => onChange(`${(e.target as HTMLSelectElement).value}:${mm}`)}>
        {HOURS.map((x) => (
          <option value={x}>{x}</option>
        ))}
      </select>
      <span aria-hidden="true">:</span>
      <select value={mm} aria-label={`${label}, minutes`} onChange={(e) => onChange(`${h}:${(e.target as HTMLSelectElement).value}`)}>
        {mins.map((x) => (
          <option value={x}>{x}</option>
        ))}
      </select>
    </span>
  );
}

/** V0.46 and the decision 0014 addendum: the project screen's Quiet hours. Each row is one window: from–to in
 *  24-hour time or All day, the seven day toggles, and × to remove it. "+ Add quiet hours" and the presets add rows;
 *  no rows is off. One line says what all the rows do. */
export function QuietRow({ value, onChange, note }: { value: QuietHours[]; onChange(q: QuietHours[]): void; note?: string | null }) {
  const set = (i: number, w: QuietHours) => onChange(value.map((x, k) => (k === i ? w : x)));
  const presets = QUIET_PRESETS.filter((p) => !hasWindow(value, p.window));
  return (
    <div class="fld qfld">
      <span>Quiet hours</span>
      {value.map((w, i) => {
        const days = new Set(w.days?.length ? w.days : WEEK.map((d) => d.key));
        const all = isAllDay(w);
        return (
          <div class="qwin" key={i}>
            <div class="qrow">
              {all ? (
                <span class="qall">All day</span>
              ) : (
                <span class="qtimes">
                  <TimePick value={w.from} label="Quiet from" onChange={(v) => set(i, { ...w, from: v })} />
                  <span aria-hidden="true">–</span>
                  <TimePick value={w.to} label="Quiet until" onChange={(v) => set(i, { ...w, to: v })} />
                </span>
              )}
              <label class="qtog">
                <input type="checkbox" checked={all} onChange={(e) => set(i, setAllDay(w, (e.target as HTMLInputElement).checked))} />
                <span>All day</span>
              </label>
              <button class="qrm" aria-label="Remove these quiet hours" title="Remove" onClick={() => onChange(value.filter((_, k) => k !== i))}>
                ×
              </button>
            </div>
            <div class="seg qdays" role="group" aria-label="Days">
              {WEEK.map((d) => (
                <button class={days.has(d.key) ? "on" : ""} aria-pressed={days.has(d.key)} onClick={() => set(i, { ...w, days: toggleDay(w.days, d.key) })}>
                  {d.label}
                </button>
              ))}
            </div>
          </div>
        );
      })}
      <div class="qadd">
        <button class="btn sm" onClick={() => onChange([...value, { ...DEFAULT_QUIET }])}>
          + Add quiet hours
        </button>
        {presets.map((p) => (
          <button class="btn sm qpre" onClick={() => onChange([...value, { ...p.window, days: p.window.days && [...p.window.days] }])}>
            {p.label} <span>{p.hint}</span>
          </button>
        ))}
      </div>
      <p class="lede">
        {quietLine(value)}
        {note ? ` ${note}.` : ""}
      </p>
    </div>
  );
}

const quietKey = (q: QuietHours[]) => JSON.stringify(q.map(cleanWindow));

/** Edit a project (V0.55): name, its one folder (Change… through the folder picker), quiet hours, colour, icon and
 *  defaults; Delete project… at the end (V0.58). */
export function ProjectEditor({ project, profiles, now, onSave, onSetFolder, onDelete }: { project: Project; profiles: Profile[]; /** the host's clock (V0.46) */ now: number; onSave(patch: ProjectPatch): Promise<string | null>; onSetFolder(path: string): Promise<string | null>; /** V0.58: after the one question; the error, or null */ onDelete?(): Promise<string | null> }) {
  const [name, setName] = useState(project.name);
  const [color, setColor] = useState(colorName(project.color, project.name));
  const [kind, setKind] = useState<"monogram" | "emoji" | "line">(resolveIcon(project.icon, project.name).kind);
  const [emoji, setEmoji] = useState(project.icon?.kind === "emoji" ? (project.icon.value ?? "") : "");
  const [line, setLine] = useState(resolveIcon(project.icon, project.name).kind === "line" ? resolveIcon(project.icon, project.name).value : "folder");
  const [profile, setProfile] = useState(project.profile ?? "");
  const [model, setModel] = useState(project.model ?? "");
  const [quiet, setQuiet] = useState<QuietHours[]>(quietList(project.quiet));
  const [msg, setMsg] = useState<{ text: string; err: boolean } | null>(null);
  const ask = useAsk();
  useEffect(() => {
    setName(project.name);
  }, [project.id]);
  const prof = profiles.find((p) => p.id === profile);
  const icon = kind === "emoji" ? { kind, value: emoji } : kind === "line" ? { kind, value: line } : { kind, value: "" };
  const preview: Project = { ...project, name: name || project.name, color, icon };
  const save = async () => {
    const patch: ProjectPatch = { name: name.trim() || project.name, color, icon, profile, model };
    if (quietKey(quiet) !== quietKey(quietList(project.quiet))) patch.quiet = quiet.length ? quiet.map(cleanWindow) : null;
    const e = await onSave(patch);
    setMsg(e ? { text: e, err: true } : { text: "Saved", err: false });
  };
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
      <FolderField
        folder={project.folder ?? ""}
        title={`Folder for ${project.name}`}
        onPick={async (path) => {
          const e = await onSetFolder(path);
          setMsg(e ? { text: e, err: true } : null);
        }}
      />
      <QuietRow value={quiet} onChange={setQuiet} note={quietNote(project, now)} />
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
      {onDelete && (
        <div class="pdel">
          <button
            class="btn sm danger"
            onClick={async () => {
              if (!(await ask(deleteProjectAsk(project)))) return;
              const e = await onDelete();
              if (e) setMsg({ text: e, err: true });
            }}
          >
            Delete project…
          </button>
        </div>
      )}
    </div>
  );
}
