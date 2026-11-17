// SPEC 0.5 V0.134: the inbox filters, host and project, on the desktop and the phone. Pure.
//
// A value (a host, a project) is active when it matches at least one thread in view: in the pinned block or in an
// unfolded section (a folded section, by default Snoozed and Settled, is not considered). A group shows only with more
// than one active value, or while one of its values is selected; a selected value stays, selected, until the owner
// clears it. Each group counts the threads the other group lets through (host AND project). V0.114: a quiet project's
// threads count only while the unlock runs.
import type { HostInfo } from "./hosts.ts";
import { projectOf } from "./projects.ts";
import { hideQuiet, quietLocked, reachable } from "./quiet.ts";
import { isOpen, sectionOf, type SectionKey } from "./store.ts";
import type { Project, Thread } from "./types.ts";

/** The remembered host filter (per device), beside the project filter's FILTER_KEY. */
export const HOST_FILTER_KEY = "fyr.hostFilter";

export interface FilterSel {
  host: string | null;
  project: string | null;
}

export type Open = Partial<Record<SectionKey, boolean>>;

export interface HostChip {
  name: string;
  /** the host as the front lists it; missing for a selected host the front no longer lists */
  info?: HostInfo;
  /** its threads in view */
  n: number;
  selected: boolean;
}

export interface ProjectChip {
  project: Project;
  n: number;
  selected: boolean;
}

/** The host a thread runs on: its own, else the front's own host (a host older than V0.51 names none). */
export function threadHost(t: Pick<Thread, "host">, self: string): string {
  return t.host || self;
}

/** Whether the list shows the thread: pinned (always open), or in a section that is not folded. */
export function inView(t: Thread, open: Open): boolean {
  const k = sectionOf(t);
  if (!k) return false;
  return !!t.pinnedAt || isOpen(k, open);
}

function hostOk(t: Thread, host: string | null, self: string): boolean {
  return !host || threadHost(t, self) === host;
}

function projectOk(t: Thread, project: string | null, projects: Project[]): boolean {
  return !project || projectOf(t, projects)?.id === project;
}

/** The project filter's id when that project still exists, else null (V0.23: a removed project filters nothing). */
export function liveProject(sel: FilterSel, projects: Project[]): string | null {
  return sel.project && projects.some((p) => p.id === sel.project) ? sel.project : null;
}

/** The threads both filters let through. */
export function applyFilters(threads: Record<string, Thread>, sel: FilterSel, projects: Project[], self: string): Record<string, Thread> {
  const project = liveProject(sel, projects);
  if (!sel.host && !project) return threads;
  const out: Record<string, Thread> = {};
  for (const t of Object.values(threads)) if (hostOk(t, sel.host, self) && projectOk(t, project, projects)) out[t.id] = t;
  return out;
}

/** V0.138: the filters after the open thread's are cleared: the ones it fails become null, the others stay; null
 *  when it passes every filter (nothing to change). A project filter whose project no longer exists filters nothing,
 *  so it is not cleared. */
export function clearFor(t: Thread, sel: FilterSel, projects: Project[], self: string): FilterSel | null {
  const host = !hostOk(t, sel.host, self);
  const project = !projectOk(t, liveProject(sel, projects), projects);
  if (!host && !project) return null;
  return { host: host ? null : sel.host, project: project ? null : sel.project };
}

export interface FilterInput {
  threads: Record<string, Thread>;
  projects: Project[];
  /** the front's hosts, in the owner's order (V0.132) */
  hosts: HostInfo[];
  /** the front's own host name */
  self: string;
  sel: FilterSel;
  open: Open;
  now: number;
  /** V0.114: the unlock of quiet projects runs until then */
  showUntil: number;
}

/** The chips to show, group by group; a group with nothing to show is empty. */
export function activeFilters(o: FilterInput): { hosts: HostChip[]; projects: ProjectChip[] } {
  const project = liveProject(o.sel, o.projects);
  // V0.114: quiet threads only while the unlock runs. For the project chips that is enough (a quiet project is picked
  // through its chip); for the others a quiet thread counts only when its project is picked, as the inbox shows it.
  const reach = reachable(o.threads, o.now, o.showUntil);
  const shown = hideQuiet(reach, !!project, o.now);

  const perHost = new Map<string, number>();
  for (const t of Object.values(shown)) {
    if (!inView(t, o.open) || !projectOk(t, project, o.projects)) continue;
    const h = threadHost(t, o.self);
    perHost.set(h, (perHost.get(h) ?? 0) + 1);
  }
  const perProject = new Map<string, number>();
  for (const t of Object.values(reach)) {
    if (!inView(t, o.open) || !hostOk(t, o.sel.host, o.self)) continue;
    const id = projectOf(t, o.projects)?.id;
    if (id) perProject.set(id, (perProject.get(id) ?? 0) + 1);
  }

  // Hosts in the owner's order; a host the front does not list (yet, or any more) after them, by name.
  const listed = new Set(o.hosts.map((h) => h.name));
  const extra = [...new Set([...perHost.keys(), ...(o.sel.host ? [o.sel.host] : [])])].filter((n) => !listed.has(n)).sort();
  const hosts: HostChip[] = [...o.hosts.map((h) => ({ name: h.name, info: h as HostInfo | undefined })), ...extra.map((name) => ({ name, info: undefined }))]
    .map((h) => ({ ...h, n: perHost.get(h.name) ?? 0, selected: h.name === o.sel.host }))
    .filter((h) => h.n > 0 || h.selected)
    .map((h) => (h.info ? h : { name: h.name, n: h.n, selected: h.selected }));

  // Projects in the owner's order (the store keeps it, V0.121). A locked quiet project is not offered unless picked.
  const projects: ProjectChip[] = o.projects
    .map((p) => ({ project: p, n: (p.id && perProject.get(p.id)) || 0, selected: !!p.id && p.id === project }))
    .filter((c) => c.selected || (c.n > 0 && !quietLocked(c.project, o.now, o.showUntil)));

  const group = <T extends { selected: boolean }>(xs: T[]) => (xs.length > 1 || xs.some((x) => x.selected) ? xs : []);
  return { hosts: group(hosts), projects: group(projects) };
}

/** The empty state under the chips: null while no filter is on or a thread it lets through is in view; else the
 *  second line ("2 more in folded sections." or ""). `threads` are the filtered threads the inbox would list. */
export function filterEmpty(threads: Record<string, Thread>, sel: FilterSel, open: Open): string | null {
  if (!sel.host && !sel.project) return null;
  let folded = 0;
  for (const t of Object.values(threads)) {
    if (inView(t, open)) return null;
    if (sectionOf(t)) folded++;
  }
  return folded ? `${folded.toLocaleString()} more in folded section${folded === 1 ? "" : "s"}.` : "";
}
