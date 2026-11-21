// Project identity in the UI (SPEC 0.5 V0.23): icons, tags, lists, the project screen; the one picker and the
// one-step New project (V0.30);
// and the thread directory that started-by labels and cross-thread messages read (V0.22).

import { createContext, type ComponentChildren } from "preact";
import { useContext, useEffect, useRef, useState } from "preact/hooks";
import type { Profile, Project, QuietHours, Thread } from "./types.ts";
import { DEFAULT_QUIET, QUIET_HOLD_MS, QUIET_PRESETS, WEEK, hasWindow, isAllDay, loadQuietShow, quietLine, quietList, quietNote, quietSaver, setAllDay, setQuietShow, subscribeQuietShow, toggleDay } from "./quiet.ts";
import { LINE_ICONS } from "./lucide.ts";
import { FOLDER_LINE, NO_FOLDER_LINE, addHosts, linkCandidates, sameRepo, PALETTE_NAMES, iconFileError, nameError, patchFor, priorityLine, priorityOf, type ProjectPriority, agentSettleLine, agentSettleOf, type AgentSettle, toolApprovalLine, toolApprovalOf, type ToolApproval, projectIconUrl, setByAgent, setupLine, colorName, enterAction, exactProject, colorVar, matchProjects, monogram, newProjectDraft, newRowLabel, projectDefaults, projectName, projectOf, projectSubtitle, resolveIcon, shortPath } from "./projects.ts";
import { Group, Row, SaveField, Seg, SetContext, Switch, useSet } from "./settings-rows.tsx";
import { rowProjectName } from "./rows.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { HostChip, HostField, useHosts } from "./host-pick-ui.tsx";
import { orderedMembers, type HostTabItem } from "./host-tabs.ts";
import { HostTabs } from "./host-tabs-ui.tsx";
import { hostChip, hostOfflineNote, multiHost, pickerStart } from "./hosts.ts";
import type { HostId } from "./hostid.ts";
import { deleteProjectAsk } from "./confirm.ts";
import { useAsk } from "./confirm-ui.tsx";
import { effortsFor, offLabel, projectModelOff } from "./providers.ts";
import { modelName } from "./model-choice.ts";
import { useApi } from "./api-context.ts";

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

/** V0.74: the row under New thread's project list while a project is quiet. Holding it (finger or mouse button) for
 *  30 s shows quiet projects for 30 minutes; lifting, moving off or closing the list before then starts over. No key
 *  and no tap, on purpose. */
export function QuietHold({ count, showUntil, show, hide, text }: { count: number; showUntil: number; show(): void; hide(): void; text?: string }) {
  const [start, setStart] = useState<number | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const done = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const stop = () => {
    clearTimeout(done.current);
    setStart(null);
  };
  useEffect(() => () => clearTimeout(done.current), []);
  useEffect(() => {
    if (start === null) return;
    const id = setInterval(() => setNow(Date.now()), 200);
    return () => clearInterval(id);
  }, [start]);
  if (!count) return null;
  const n = count === 1 ? "1 quiet project" : `${count} quiet projects`;
  if (showUntil) {
    const d = new Date(showUntil);
    const hm = `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
    return (
      <div class="qhold on">
        <span class="qht">Quiet projects show until {hm}</span>
        <button type="button" class="btn sm" onClick={hide}>
          Hide
        </button>
      </div>
    );
  }
  const begin = (e: PointerEvent) => {
    if (e.button !== 0) return;
    e.preventDefault();
    clearTimeout(done.current);
    const t = Date.now();
    setStart(t);
    setNow(t);
    done.current = setTimeout(() => {
      setStart(null);
      show();
    }, QUIET_HOLD_MS);
  };
  const part = start === null ? 0 : Math.min(1, (now - start) / QUIET_HOLD_MS);
  const left = Math.max(1, Math.ceil((QUIET_HOLD_MS - (now - (start ?? now))) / 1000));
  return (
    <div
      class={`qhold${start !== null ? " held" : ""}`}
      onPointerDown={begin}
      onPointerUp={stop}
      onPointerLeave={stop}
      onPointerCancel={stop}
      onContextMenu={(e) => {
        e.preventDefault();
        e.stopPropagation();
      }}
    >
      <span class="qhf" style={{ width: `${part * 100}%` }} />
      <span class="qht">{start === null ? (text ?? `${n} hidden. Hold here for 30 s to show them for 30 minutes.`) : `Keep holding · ${left} s`}</span>
    </div>
  );
}

function quietStorage(): Storage | null {
  try {
    return typeof localStorage !== "undefined" ? localStorage : null;
  } catch {
    return null;
  }
}

export interface QuietUnlock {
  /** the end of the unlock on this device on the host's clock, or 0 when none runs */
  showUntil: number;
  show(): void;
  hide(): void;
}

/** V0.114: the one unlock of quiet projects on this device. New thread, the project filter, search and the gate on a
 *  quiet thread all read it, so one hold opens every path and Hide shuts every path; another tab follows through
 *  the storage event. It re-renders when the 30 minutes end. clockOffset: the host's clock minus this browser's. */
export function useQuietUnlock(clockOffset = 0): QuietUnlock {
  const [end, setEnd] = useState(() => loadQuietShow(quietStorage()));
  useEffect(() => {
    const off = subscribeQuietShow(setEnd);
    const onStorage = (e: StorageEvent) => e.key === "fyr.quietShowUntil" && setEnd(loadQuietShow(quietStorage()));
    window.addEventListener("storage", onStorage);
    return () => (off(), window.removeEventListener("storage", onStorage));
  }, []);
  const [, setTick] = useState(0);
  useEffect(() => {
    const left = end - (Date.now() + clockOffset);
    if (left <= 0) return;
    const t = setTimeout(() => setTick((n) => n + 1), Math.min(left + 250, 2 ** 31 - 1));
    return () => clearTimeout(t);
  }, [end, clockOffset]);
  return {
    showUntil: end > Date.now() + clockOffset ? end : 0,
    show: () => void setQuietShow(quietStorage(), Date.now() + clockOffset),
    hide: () => void setQuietShow(quietStorage(), null),
  };
}

/** V0.114: what shows instead of a quiet project's threads (the project filter, search) or a quiet thread (a link, back
 *  and forward, a notice, a board card, ?open=, a push): the project is quiet until HH:MM, and the 30 s hold. */
export function QuietGate({ project, now, unlock, what }: { project?: Project; now: number; unlock: QuietUnlock; what: "thread" | "threads" | "search" }) {
  const name = project?.name ?? "This project";
  const note = quietNote(project, now);
  const head =
    what === "search" ? "Threads of quiet projects are left out of search." : `${name} is ${note ? note.replace(/^Quiet/, "quiet") : "quiet"}.`;
  const body = what === "thread" ? "This thread is in its quiet hours." : what === "threads" ? "Its threads are hidden in its quiet hours." : null;
  return (
    <div class="qgate" role="region" aria-label="Quiet hours">
      <div class="qgh">{head}</div>
      {body && <div class="qgb">{body}</div>}
      <QuietHold count={1} showUntil={0} show={unlock.show} hide={unlock.hide} text="Hold here for 30 s to open quiet projects for 30 minutes." />
    </div>
  );
}

/** A project's one folder (V0.55): the short path and what it does with "Change…" through the host folder picker, or
 *  that threads are filed by hand with "Choose folder…". × leaves the folder out where onClear is given. */
export function FolderField({ folder, title, host, onPick, onClear }: { folder: string; title: string; /** V0.132: whose folders */ host?: HostId; onPick(path: string): void; onClear?(): void }) {
  const picker = useFolderPick();
  const pick = async () => {
    const path = await picker.pick({ title, ...(host ? { host } : {}) });
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
 *  on the project screen. V0.132: with several hosts, the host it lives on comes first: picked here (Settings), or set
 *  by the caller (`host`: the New thread form's host, or the threads' host when it is made from them); the folder is
 *  that host's, and a new host drops a folder picked on another. */
export function NewProjectStep({ initialName, initialFolder, fromThread, host: fixed, onCreate, onCancel, onDraft }: { initialName: string; initialFolder: string; fromThread: boolean; host?: HostId; onCreate(name: string, folder: string, host: HostId): Promise<string | null>; onCancel(): void; /** V0.63: the name and folder as they are typed */ onDraft?(name: string, folder: string): void }) {
  const hs = useHosts();
  // Slice 2b: the picker starts on the first host in the owner's order (shown in it), the only one with one host.
  const [own, setOwn] = useState<HostId | undefined>(() => pickerStart(hs.hosts));
  const host = fixed ?? own;
  const down = hostOfflineNote(host, hs.hosts, hs.now);
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
    if (down || !host) return setErr(down); // V0.132: never on another host instead
    setBusy(true);
    setErr(null);
    const e = await onCreate(name.trim(), folder, host);
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
          placeholder="Garden"
          autocapitalize="sentences"
        />
      </label>
      {fixed === undefined ? (
        <HostField
          value={own}
          what="The project lives there: its folder, its threads and its providers are that host's."
          onPick={(h) => {
            if (h !== own) setFolder(""); // a folder of another host does not come along
            setOwn(h);
          }}
        />
      ) : (
        multiHost(hs.hosts) && fixed && <p class="lede">On {hs.hosts.find((h) => h.id === fixed)?.name ?? "a host this Fyr does not follow"}.</p>
      )}
      <FolderField folder={folder} title="Project folder" host={host} onPick={setFolder} onClear={() => setFolder("")} />
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
  // V0.132: a project made from threads lives on their host.
  if (draft) return <NewProjectStep initialName={draft.name} initialFolder={draft.folder} fromThread={threads.length > 0} host={threads[0]?.hostId} onCreate={onCreate} onCancel={() => setDraft(null)} />;
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
  /** V0.project-priority: null is normal */
  priority?: "high" | "low" | null;
  /** V0.settle-request: null is ask */
  agentSettle?: "auto" | null;
}

const HOURS = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, "0"));
const MINUTES = Array.from({ length: 12 }, (_, i) => String(i * 5).padStart(2, "0"));

/** A 24-hour HH:MM picker from two selects, so no browser locale turns it into AM/PM (decision 0014 addendum). */
function TimePick({ value, label, onChange }: { value: string; label: string; onChange(v: string): void }) {
  const env = useSet();
  const m = /^(\d{1,2}):(\d\d)$/.exec(value);
  const h = m ? m[1].padStart(2, "0") : "00";
  const mm = m ? m[2] : "00";
  const mins = MINUTES.includes(mm) ? MINUTES : [...MINUTES, mm].sort();
  return (
    <span class="qtime" role="group" aria-label={label}>
      <select value={h} aria-label={`${label}, hour`} disabled={env.readOnly} onChange={(e) => onChange(`${(e.target as HTMLSelectElement).value}:${mm}`)}>
        {HOURS.map((x) => (
          <option value={x}>{x}</option>
        ))}
      </select>
      <span aria-hidden="true">:</span>
      <select value={mm} aria-label={`${label}, minutes`} disabled={env.readOnly} onChange={(e) => onChange(`${h}:${(e.target as HTMLSelectElement).value}`)}>
        {mins.map((x) => (
          <option value={x}>{x}</option>
        ))}
      </select>
    </span>
  );
}

/** V0.46, the decision 0014 addendum, V0.119: the project page's Quiet hours group. One row per window: from–to in
 *  24-hour time, an All day switch, ×, and the seven day chips under them; the last row adds a window or a preset
 *  that is not there yet; the foot says what all the rows do. No rows is off. */
export function QuietGroup({ value, onChange, note }: { value: QuietHours[]; onChange(q: QuietHours[]): void; note?: string | null }) {
  const env = useSet();
  const set = (i: number, w: QuietHours) => onChange(value.map((x, k) => (k === i ? w : x)));
  const presets = QUIET_PRESETS.filter((p) => !hasWindow(value, p.window));
  return (
    <Group title="Quiet hours" end={note ?? undefined} class="qfld" label="Quiet hours" foot={quietLine(value)}>
      {value.map((w, i) => {
        const days = new Set(w.days?.length ? w.days : WEEK.map((d) => d.key));
        const all = isAllDay(w);
        return (
          <div class="sr col qwin" key={i}>
            <div class="qw-top">
              {!all && (
                <span class="qtimes">
                  <TimePick value={w.from} label="Quiet from" onChange={(v) => set(i, { ...w, from: v })} />
                  <span aria-hidden="true">–</span>
                  <TimePick value={w.to} label="Quiet until" onChange={(v) => set(i, { ...w, to: v })} />
                </span>
              )}
              <label class={`allday qtog${all ? " first" : ""}`}>
                All day
                <Switch on={all} label="All day" onChange={(on) => set(i, setAllDay(w, on))} />
              </label>
              <button type="button" class="xb qrm" aria-label="Remove these quiet hours" title="Remove" disabled={env.readOnly} onClick={() => onChange(value.filter((_, k) => k !== i))}>
                ×
              </button>
            </div>
            <div class="qd qdays" role="group" aria-label="Days">
              {WEEK.map((d) => (
                <button type="button" class={days.has(d.key) ? "on" : ""} aria-pressed={days.has(d.key)} disabled={env.readOnly} onClick={() => set(i, { ...w, days: toggleDay(w.days, d.key) })}>
                  {d.label}
                </button>
              ))}
            </div>
          </div>
        );
      })}
      <div class="sr qadd">
        <div class="chips">
          <button type="button" class="btn" disabled={env.readOnly} onClick={() => onChange([...value, { ...DEFAULT_QUIET }])}>
            Add quiet hours
          </button>
          {presets.map((p) => (
            <button type="button" class="btn qpre" disabled={env.readOnly} onClick={() => onChange([...value, { ...p.window, days: p.window.days && [...p.window.days] }])}>
              {p.label} <span>{p.hint}</span>
            </button>
          ))}
        </div>
      </div>
    </Group>
  );
}

type IconTab = "image" | "monogram" | "emoji" | "line";

/** A project's page (V0.55, V0.58, V0.116; V0.119 amends the Save button away): Identity, Threads, Quiet hours, Delete.
 *  Each change saves at once with only its own field (V0.116 (2)); quiet hours 600 ms after the last change and at
 *  once when the page closes. The same page in the desktop Settings and on the phone's project screen. */
type EditorProps = { project: Project; profiles: Profile[]; /** the host's clock (V0.46) */ now: number; onSave(patch: ProjectPatch): Promise<string | null>; onSetFolder(path: string): Promise<string | null>; /** V0.58: after the one question; the error, or null */ onDelete?(): Promise<string | null> };

/** V0.139, V0.host-tabs: a project lives on one host, or on several when linked (decision 0034). Linked projects show
 *  HostTabs directly under the header to switch between host records in the owner's host order; unlinked projects on a
 *  multi-host front keep their read-only Host chip. While the shown host is offline the screen is read-only and says so.
 *  The same on the desktop (Settings → Projects → a project) and the phone (the project screen). */
export function ProjectEditor(p: EditorProps & { linking?: ProjectLinking }) {
  const hs = useHosts();
  const env = useSet();
  const chip = hostChip(p.project.hostId, hs.hosts, hs.now);
  const profiles = p.profiles; // V0.139: the caller hands its host's (store.ts profilesOn)
  const rawMembers = p.linking?.group.members ?? [];
  const linked = rawMembers.length > 1;
  const members = linked ? orderedMembers(rawMembers, hs.hosts ?? []) : [];
  // A member's tab is its host, with its folder on hover. A member without an id cannot be opened or named: it is left
  // out of the strip (V0.host-tabs: every other tab names a host a call can reach).
  const memberTabs: HostTabItem[] = members
    .filter((m) => m.id)
    .map((m) => {
      const mc = hostChip(m.hostId, hs.hosts, hs.now);
      const name = mc?.name ?? m.host ?? "";
      return { id: m.id!, name, label: name, offline: !!mc?.offline, title: m.folder ? `${name} · ${shortPath(m.folder)}` : name };
    });
  return (
    <SetContext.Provider value={{ ...env, readOnly: env.readOnly || !!chip?.offline }}>
      <ProjectEditorBody
        {...p}
        profiles={profiles}
        linked={linked}
        shown={linked ? p.linking!.group : undefined}
        imageTargets={(linked ? rawMembers : [p.project]).filter((m) => hs.hosts?.find((h) => h.id === m.hostId)?.online !== false)}
        host={
          linked && p.linking ? (
            <>
              <HostTabs items={memberTabs} active={p.project.id!} onPick={(id) => id !== p.project.id && p.linking!.open(id)} label="Project hosts" />
              {chip?.offline && (
                <div class="note hnote">
                  {chip.name} is {chip.line.replace(" · read-only", "")}. This project shows as last read and cannot be changed until it is back.
                </div>
              )}
            </>
          ) : (
            chip && (
              <Group title="Host" label="Host">
                <Row
                  class="hostrow"
                  title={<HostChip chip={chip} />}
                  desc={chip.offline ? `${chip.name} is ${chip.line.replace(" · read-only", "")}. This project shows as last read and cannot be changed until it is back.` : "The project lives here: its folder, its threads and the providers below are this host's. A project does not move to another host."}
                />
                {p.linking && <LinkRows project={p.project} linking={p.linking} />}
              </Group>
            )
          )
        }
        hostManage={
          linked && p.linking ? (
            <Group title="Hosts" label="Hosts" fold={{ id: `project:${p.linking.group.id}:hosts`, open: false, summary: `${members.length} linked hosts` }}>
              <LinkRows project={p.project} linking={p.linking} tabbed />
            </Group>
          ) : undefined
        }
      />
    </SetContext.Provider>
  );
}

/** Decision 0034: what the project screen needs to show and change a project's links. */
export interface ProjectLinking {
  /** the project as the lists show it */
  group: Project;
  candidates: Project[];
  link(other: string): Promise<string | null>;
  unlink(id: string): Promise<string | null>;
  add(host: HostId, folder: string): Promise<string | null>;
  /** open a member's page: its folder and defaults are its own */
  open(id: string): void;
}

export function linkingFor(group: Project, projects: Project[], actions: { link(id: string, other: string): Promise<string | null>; unlink(id: string): Promise<string | null>; add(id: string, host: HostId, folder: string): Promise<string | null> }, open: (id: string) => void): ProjectLinking {
  return { group, candidates: linkCandidates(group, projects), link: (other) => actions.link(group.id!, other), unlink: actions.unlink, add: (host, folder) => actions.add(group.id!, host, folder), open };
}

/** The Host group's rows for a linked project: each host's record (omitted when HostTabs renders them above), and what
 *  can join. The same on the desktop and the phone. */
function LinkRows({ project, linking, tabbed }: { project: Project; linking: ProjectLinking; tabbed?: boolean }) {
  const hs = useHosts();
  const env = useSet();
  const [pick, setPick] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const run = async (f: () => Promise<string | null>) => {
    setBusy(true);
    const e = await f();
    setBusy(false);
    setErr(e);
    if (!e) env.saved();
    return e;
  };
  const picker = useFolderPick();
  const [hostPick, setHostPick] = useState("");
  const addable = addHosts(linking.group, hs.hosts ?? []);
  const addTo = addable.find((h) => h.id === hostPick) ?? addable[0];
  const members = linking.group.members ?? [];
  const chosen = linking.candidates.some((c) => c.id === pick) ? pick : (linking.candidates[0]?.id ?? "");
  return (
    <>
      {!tabbed &&
        members
          .filter((m) => m.id !== project.id)
          .map((m) => {
            const chip = hostChip(m.hostId, hs.hosts, hs.now);
            return (
              <Row
                key={m.id}
                class="hostrow"
                title={chip ? <HostChip chip={chip} /> : m.host}
                desc={`Also here: ${m.folder ? shortPath(m.folder) : "no folder"}`}
                end={
                  <button type="button" class="btn" onClick={() => linking.open(m.id!)}>
                    Open
                  </button>
                }
              />
            );
          })}
      {members.length > 1 && (
        <Row
          title="Unlink this host"
          desc="This host's project stands alone again, with its folder and threads."
          end={
            <button type="button" class="btn" disabled={busy || env.readOnly} onClick={() => void run(() => linking.unlink(project.id!))}>
              Unlink
            </button>
          }
        />
      )}
      {linking.candidates.length > 0 && (
        <Row
          title="Same project on another host"
          desc={sameRepo(linking.group, linking.candidates[0]) ? `${linking.candidates[0].name} on ${linking.candidates[0].host} looks like the same repository. Link it to show them as one project.` : "Show them as one project. Each host keeps its own folder and threads."}
          stack
          class="two"
          end={
            <>
              <select aria-label="Project on another host" value={chosen} disabled={busy || env.readOnly} onChange={(e) => setPick((e.target as HTMLSelectElement).value)}>
                {linking.candidates.map((c) => (
                  <option value={c.id}>
                    {c.name} · {c.host}
                    {sameRepo(linking.group, c) ? " · same repository" : ""}
                  </option>
                ))}
              </select>
              <button type="button" class="btn" disabled={busy || env.readOnly || !chosen} onClick={() => void run(() => linking.link(chosen))}>
                Link
              </button>
            </>
          }
        />
      )}
      {addTo && (
        <Row
          title="Add on another host"
          desc="Choose a folder there. The project keeps its name, colour and quiet hours."
          stack
          class="two"
          end={
            <>
              <select aria-label="Host to add the project on" value={addTo.id} disabled={busy || env.readOnly} onChange={(e) => setHostPick((e.target as HTMLSelectElement).value)}>
                {addable.map((h) => (
                  <option value={h.id}>{h.name}</option>
                ))}
              </select>
              <button
                type="button"
                class="btn"
                disabled={busy || env.readOnly}
                onClick={async () => {
                  const path = await picker.pick({ title: `Folder for ${linking.group.name} on ${addTo.name}`, host: addTo.id });
                  if (path) await run(() => linking.add(addTo.id!, path));
                }}
              >
                Choose folder…
              </button>
            </>
          }
        />
      )}
      {err && (
        <span class="sr-d err" role="alert">
          {err}
        </span>
      )}
    </>
  );
}

function ProjectEditorBody({ project, profiles, now, onSave, onSetFolder, onDelete, host, hostManage, linked, shown, imageTargets }: EditorProps & { /** V0.139, V0.host-tabs: HostTabs (linked) or the Host group (single-host), under the header */ host?: ComponentChildren; /** V0.host-tabs: compact folded linking actions for a linked project */ hostManage?: ComponentChildren; /** decision 0034: other hosts have this project too */ linked?: boolean; /** decision 0034: the linked project, whose shared values this page shows */ shown?: Project; /** decision 0034: the records an image goes to: every online member */ imageTargets: Project[] }) {
  // Decision 0034: name, colour, icon, quiet hours, priority, agentSettle and toolApproval are the link's; the rest of the page is this host's record.
  if (shown) project = { ...project, name: shown.name, color: shown.color, icon: shown.icon, quiet: shown.quiet, priority: shown.priority, agentSettle: shown.agentSettle, toolApproval: shown.toolApproval, image: shown.image, imageFrom: shown.imageFrom };
  const env = useSet();
  const api = useApi();
  const ask = useAsk();
  const picker = useFolderPick();
  const [errs, setErrs] = useState<Record<string, string | null>>({});
  const err = (k: string, e: string | null) => setErrs((x) => ({ ...x, [k]: e }));
  const save = async (k: string, patch: ProjectPatch): Promise<string | null> => {
    const e = await onSave(patch);
    err(k, e);
    if (!e) env.saved();
    return e;
  };
  // What the owner just picked shows at once; the host's answer replaces it (a refusal puts it back).
  const [color, setColor] = useState<string | null>(null);
  useEffect(() => setColor(null), [project.color]);
  const shownColor = color ?? colorName(project.color, project.name);
  const icon0 = resolveIcon(project.icon, project.name);
  const [tab, setTab] = useState<IconTab>(project.image ? "image" : icon0.kind);
  const [quiet, setQuiet] = useState<QuietHours[]>(quietList(project.quiet));
  // V0.project-priority: the pick shows at once; the host's answer replaces it.
  const [prio, setPrio] = useState<ProjectPriority | null>(null);
  useEffect(() => setPrio(null), [project.priority]);
  const shownPrio = prio ?? priorityOf(project);
  // V0.settle-request: the same, for what an agent's settle_request does.
  const [ags, setAgs] = useState<AgentSettle | null>(null);
  useEffect(() => setAgs(null), [project.agentSettle]);
  const shownAgs = ags ?? agentSettleOf(project);
  // V0.tool-approval-judge: the same, for agent tool approvals.
  const [tapp, setTapp] = useState<ToolApproval | null>(null);
  useEffect(() => setTapp(null), [project.toolApproval]);
  const shownTapp = tapp ?? toolApprovalOf(project);
  const saver = useRef(quietSaver<QuietHours[]>((q) => void save("quiet", patchFor("quiet", q))));
  useEffect(() => () => saver.current.flush(), []);
  const [busy, setBusy] = useState(false);
  const file = useRef<HTMLInputElement>(null);
  const prof = profiles.find((p) => p.id === (project.profile ?? ""));
  const defEfforts = prof ? effortsFor(prof, projectDefaults(project, profiles).model ?? "") : [];
  const preview: Project = { ...project, color: shownColor };

  const upload = async (f: File | undefined) => {
    if (!f || !project.id) return;
    const bad = iconFileError(f);
    if (bad) return err("icon", bad);
    setBusy(true);
    const rs = await Promise.all(imageTargets.map((m) => (m.hostId ? api.uploadProjectIcon(m.hostId, m.id!, f) : Promise.resolve({ ok: false, error: "This host reports no install id" }))));
    setBusy(false);
    const bad2 = rs.find((r) => !r.ok);
    err("icon", bad2 ? (bad2.error ?? "The upload did not work") : null);
    if (!bad2) env.saved();
  };
  const removeImage = async () => {
    const rs = await Promise.all(imageTargets.map((m) => (m.hostId ? api.command(m.hostId, "project.update", undefined, { id: m.id, image: "" }) : Promise.resolve({ ok: false, error: "This host reports no install id" }))));
    const bad2 = rs.find((r) => !r.ok);
    err("icon", bad2 ? (bad2.error ?? "That did not work") : null);
    if (!bad2) env.saved();
  };
  const setup = async () => {
    if (!project.hostId) return err("setup", "This host reports no install id");
    setBusy(true);
    const r = await api.command(project.hostId, "project.setupRun", undefined, { id: project.id });
    setBusy(false);
    err("setup", r.ok ? null : (r.error ?? "That did not work"));
  };
  const setup0 = setupLine(project);
  const n = project.threads ?? 0;
  const iconBy = setByAgent(project, "image") && !!project.image;
  return (
    <div class="pedit" style={{ "--pc": colorVar(shownColor, project.name) }}>
      <div class="phead">
        <ProjectIcon project={preview} name={project.name} size={44} />
        <div class="plt">
          <span class="pln big">{project.name}</span>
          <span class="pls2">
            {project.folder ? shortPath(project.folder) : "No folder"} · {n === 1 ? "1 thread" : `${n} threads`}
          </span>
        </div>
      </div>
      {host}
      <Group title="Identity" label="Identity">
        <Row
          title={<>Name{setByAgent(project, "name") && <SetBy />}</>}
          error={errs.name}
          stack
          wide
          class="pname"
          end={<SaveField value={project.name} label="Name" check={nameError} onError={(e) => err("name", e)} onSave={(v) => save("name", patchFor("name", v))} />}
        />
        <Row
          title={<>Colour{setByAgent(project, "color") && <SetBy />}</>}
          error={errs.color}
          stack
          end={
            <div class="swatches" role="radiogroup" aria-label="Colour">
              {PALETTE_NAMES.map((c) => (
                <button
                  type="button"
                  class={`sw${c === shownColor ? " on" : ""}`}
                  role="radio"
                  aria-checked={c === shownColor}
                  aria-label={c}
                  title={c}
                  style={{ "--pc": `var(--p-${c})` }}
                  disabled={env.readOnly}
                  onClick={async () => {
                    if (c === shownColor) return;
                    setColor(c);
                    if (await save("color", patchFor("color", c))) setColor(null);
                  }}
                />
              ))}
            </div>
          }
        />
        <div class={`sr col picon${errs.icon ? " bad" : ""}`}>
          <div class="sbar">
            <span class="sr-t">Icon{iconBy && <SetBy />}</span>
            <span class="end">
              <Seg
                label="Icon"
                value={tab}
                options={[
                  { value: "image", label: "Image" },
                  { value: "monogram", label: "Letters" },
                  { value: "emoji", label: "Emoji" },
                  { value: "line", label: "Symbol" },
                ]}
                onChange={(t) => {
                  setTab(t);
                  // Letters has nothing more to pick: the tab is the pick.
                  if (t === "monogram" && (project.icon?.kind ?? "monogram") !== "monogram") void save("icon", patchFor("icon", { kind: "monogram", value: "" }));
                }}
              />
            </span>
          </div>
          {tab === "image" ? (
            <div class="pimg">
              {project.image && <ProjectIcon project={project} name={project.name} size={44} />}
              <button type="button" class="btn" disabled={busy || !project.id || env.readOnly} onClick={() => file.current?.click()}>
                {project.image ? "Replace…" : "Upload…"}
              </button>
              {project.image && (
                <button type="button" class="btn" disabled={busy || env.readOnly} onClick={() => void removeImage()}>
                  Remove
                </button>
              )}
              <span class="sr-d">PNG, JPEG or GIF, at most 2 MB.</span>
              <input ref={file} type="file" accept="image/png,image/jpeg,image/gif" hidden onChange={(e) => (void upload((e.target as HTMLInputElement).files?.[0]), ((e.target as HTMLInputElement).value = ""))} />
            </div>
          ) : (
            project.image && <span class="sr-d">Used when the image is removed or cannot load.</span>
          )}
          {tab === "monogram" && <span class="sr-d">{monogram(project.name)} · the first letters of the name.</span>}
          {tab === "emoji" && (
            <span class="pemoji">
              <SaveField value={project.icon?.kind === "emoji" ? (project.icon.value ?? "") : ""} label="Emoji" placeholder="🏡" onError={(e) => err("icon", e)} onSave={(v) => save("icon", patchFor("icon", { kind: "emoji", value: v.trim() }))} />
            </span>
          )}
          {tab === "line" && (
            <div class="igrid" role="radiogroup" aria-label="Symbol">
              {ICON_GRID.map((nm) => {
                const on = icon0.kind === "line" && icon0.value === nm;
                return (
                  <button type="button" class={`ig${on ? " on" : ""}`} role="radio" aria-checked={on} title={nm} aria-label={nm} disabled={env.readOnly} onClick={() => !on && void save("icon", patchFor("icon", { kind: "line", value: nm }))}>
                    <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" dangerouslySetInnerHTML={{ __html: LINE_ICONS[nm] }} />
                  </button>
                );
              })}
            </div>
          )}
          {errs.icon && (
            <span class="sr-d err" role="alert">
              {errs.icon}
            </span>
          )}
        </div>
        <Row
          title="Set up automatically"
          class="psetup"
          desc={
            !errs.setup && !setup0?.error ? (
              <>
                An agent finds the logo and the real name and fills in what you have not set.
                {!project.folder ? " Needs a folder." : setup0 ? ` ${setup0.text}` : ""}
              </>
            ) : undefined
          }
          error={errs.setup ?? (setup0?.error ? setup0.text : null)}
          end={
            <button type="button" class="btn" disabled={busy || !project.folder || env.readOnly} onClick={() => void setup()}>
              {project.setup ? "Run again" : "Run setup"}
            </button>
          }
        />
      </Group>
      <Group title="Threads" label="Threads">
        <Row
          title="Folder"
          error={errs.folder}
          desc={
            project.folder ? (
              <span class="pfolder">
                <span class="ell mono" title={project.folder}>
                  {shortPath(project.folder)}
                </span>{" "}
                · <span class="srcl">{FOLDER_LINE}</span>
              </span>
            ) : (
              `${NO_FOLDER_LINE}.`
            )
          }
          end={
            <button
              type="button"
              class="btn pfchg"
              disabled={env.readOnly}
              onClick={async () => {
                const path = await picker.pick({ title: `Folder for ${project.name}`, ...(project.hostId ? { host: project.hostId } : {}) }); // V0.132: on its host
                if (!path) return;
                const e = await onSetFolder(path);
                err("folder", e);
                if (!e) env.saved();
              }}
            >
              {project.folder ? "Change…" : "Choose…"}
            </button>
          }
        />
        <Row
          title="Default provider, model and effort"
          desc="What a new thread here starts with."
          error={errs.defaults ?? projectModelOff(project, profiles)}
          stack
          class="two"
          end={
            <>
              <select aria-label="Default provider" value={project.profile ?? ""} disabled={env.readOnly} onChange={(e) => void save("defaults", patchFor("profile", (e.target as HTMLSelectElement).value))}>
                <option value="">None</option>
                {profiles.map((p) => (
                  <option value={p.id}>{p.name}</option>
                ))}
              </select>
              <select aria-label="Default model" value={project.model ?? ""} disabled={!prof || env.readOnly} onChange={(e) => void save("defaults", patchFor("model", (e.target as HTMLSelectElement).value))}>
                <option value="">{prof ? "The provider's default" : "None"}</option>
                {prof && project.model && !prof.models.includes(project.model) && <option value={project.model}>{offLabel(project.model)}</option>}
                {(prof?.models ?? []).map((m) => (
                  <option value={m}>{modelName(prof, m)}</option>
                ))}
              </select>
              {/* V0.model-choice (#23): the default effort, only for a model that takes one */}
              {defEfforts.length > 0 && (
                <select aria-label="Default effort" value={project.effort ?? ""} disabled={env.readOnly} onChange={(e) => void save("defaults", patchFor("effort", (e.target as HTMLSelectElement).value))}>
                  <option value="">The provider's default</option>
                  {defEfforts.map((x) => (
                    <option value={x}>{x}</option>
                  ))}
                </select>
              )}
            </>
          }
        />
        <Row
          title="Priority"
          desc={priorityLine(shownPrio)}
          error={errs.priority}
          stack
          end={
            <Seg
              label="Priority"
              value={shownPrio}
              disabled={env.readOnly}
              options={[
                { value: "low", label: "Low" },
                { value: "normal", label: "Normal" },
                { value: "high", label: "High" },
              ]}
              onChange={async (v) => {
                setPrio(v);
                if (await save("priority", patchFor("priority", v))) setPrio(null);
              }}
            />
          }
        />
        <Row
          title="Agent says done"
          desc={agentSettleLine(shownAgs)}
          error={errs.agentSettle}
          stack
          end={
            <Seg
              label="Agent says done"
              value={shownAgs}
              disabled={env.readOnly}
              options={[
                { value: "ask", label: "Ask me" },
                { value: "auto", label: "Settle" },
              ]}
              onChange={async (v) => {
                setAgs(v);
                if (await save("agentSettle", patchFor("agentSettle", v))) setAgs(null);
              }}
            />
          }
        />
        <Row
          title="Tool approvals"
          desc={toolApprovalLine(shownTapp)}
          error={errs.toolApproval}
          stack
          end={
            <Seg
              label="Tool approvals"
              value={shownTapp}
              disabled={env.readOnly}
              options={[
                { value: "ask", label: "Ask me" },
                { value: "auto", label: "Auto-judge" },
              ]}
              onChange={async (v) => {
                setTapp(v);
                if (await save("toolApproval", patchFor("toolApproval", v))) setTapp(null);
              }}
            />
          }
        />
      </Group>
      <QuietGroup
        value={quiet}
        note={quietNote(project, now)}
        onChange={(q) => {
          setQuiet(q);
          saver.current.push(q);
        }}
      />
      {errs.quiet && (
        <div class="seterr" role="alert">
          {errs.quiet}
        </div>
      )}
      {hostManage}
      {onDelete && (
        <Group danger class="pdel">
          <Row
            title={linked ? `Delete from ${project.host}` : "Delete project"}
            desc={`${n ? `Its ${n === 1 ? "thread stays" : `${n} threads stay`}, without a project.` : "It has no threads."}${linked ? " The other hosts keep theirs." : ""}`}
            error={errs.delete}
            end={
              <button
                type="button"
                class="btn danger"
                disabled={env.readOnly}
                onClick={async () => {
                  if (!(await ask(deleteProjectAsk(project)))) return;
                  err("delete", await onDelete());
                }}
              >
                Delete…
              </button>
            }
          />
        </Group>
      )}
    </div>
  );
}
