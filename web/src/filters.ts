// SPEC 0.5 V0.134, V0.169, V0.192: the inbox filters on the desktop and the phone. Pure.
//
// Row 1, hosts (V0.169, V0.204): a toggle per host; all on is the default and means no host filter; a host turned off
// hides its threads, and a click on the only host still on ends the host filter. Row 2, projects: one project picked at a time (V0.134), counted only over the hosts that are on, so
// the projects of a host turned off fall out of it. A value is active when it matches at least one thread in view: in
// the pinned block or in an unfolded section (a folded section, by default Snoozed and Settled, is not considered).
// A row shows only with more than one value to choose from, or while one of its values is selected (a project
// picked, a host off); a selected value stays until the owner changes it. V0.114: a quiet project's threads count
// only while the unlock runs.
import type { HostInfo } from "./hosts.ts";
import { shortHostNames } from "./load.ts";
import { findProject, projectOf } from "./projects.ts";
import { hideQuiet, quietLocked, reachable } from "./quiet.ts";
import { isOpen, sectionOf, type SectionKey } from "./store.ts";
import type { Project, Thread } from "./types.ts";

/** V0.169: the hosts turned off, remembered per device (a JSON list of names); it replaces V0.134's one host
 *  (`fyr.hostFilter`, dropped). */
export const HOSTS_OFF_KEY = "fyr.hostsOff";
export const OLD_HOST_FILTER_KEY = "fyr.hostFilter";

export interface FilterSel {
  /** the hosts turned off; none is the default (every host on, no host filter) */
  hostsOff: string[];
  project: string | null;
}

export type Open = Partial<Record<SectionKey, boolean>>;

export interface HostChip {
  name: string;
  /** V0.152: the short name the rail uses ("meros-laptop", "meros-server" → "laptop", "server") */
  short: string;
  /** the host as the front lists it; missing for a host turned off that the front no longer lists */
  info?: HostInfo;
  /** its threads in view, whether it is on or off */
  n: number;
  on: boolean;
  /** V0.204: the only host on in the row: a click on it ends the host filter instead of hiding every thread */
  last: boolean;
}

export interface ProjectChip {
  project: Project;
  n: number;
  selected: boolean;
}

/** V0.169: what the device stored, as a list of names; anything else is none off. */
export function parseHostsOff(raw: string | null): string[] {
  if (!raw) return [];
  try {
    const v = JSON.parse(raw) as unknown;
    return Array.isArray(v) ? [...new Set(v.filter((x): x is string => typeof x === "string" && !!x))] : [];
  } catch {
    return [];
  }
}

/** V0.169: a host's toggle: off when it is on, on when it is off. */
export function toggleHost(off: readonly string[], name: string): string[] {
  return off.includes(name) ? off.filter((x) => x !== name) : [...off, name];
}

/** V0.204: what a click on a host's toggle leaves off. On the only host still on, nothing: every host comes back. */
export function clickHost(off: readonly string[], hosts: readonly Pick<HostChip, "name" | "last">[], name: string): string[] {
  return hosts.some((h) => h.name === name && h.last) ? [] : toggleHost(off, name);
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

function hostOk(t: Thread, off: readonly string[], self: string): boolean {
  return !off.length || !off.includes(threadHost(t, self));
}

function projectOk(t: Thread, project: string | null, projects: Project[]): boolean {
  return !project || projectOf(t, projects)?.id === project;
}

/** The project filter's id when that project still exists, else null (V0.23: a removed project filters nothing). */
export function liveProject(sel: FilterSel, projects: Project[]): string | null {
  return findProject(projects, sel.project)?.id ?? null; // decision 0034: any member's id names its project
}

/** The threads both rows let through: on a host that is on, and in the picked project. */
export function applyFilters(threads: Record<string, Thread>, sel: FilterSel, projects: Project[], self: string): Record<string, Thread> {
  const project = liveProject(sel, projects);
  if (!sel.hostsOff.length && !project) return threads;
  const out: Record<string, Thread> = {};
  for (const t of Object.values(threads)) if (hostOk(t, sel.hostsOff, self) && projectOk(t, project, projects)) out[t.id] = t;
  return out;
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

  // Row 1 counts every host's threads in view, whatever the project filter: the rows are separate, and row 1 comes first.
  const perHost = new Map<string, number>();
  for (const t of Object.values(shown)) {
    if (!inView(t, o.open)) continue;
    const h = threadHost(t, o.self);
    perHost.set(h, (perHost.get(h) ?? 0) + 1);
  }
  // Row 2 counts only the threads on the hosts that are on.
  const perProject = new Map<string, number>();
  for (const t of Object.values(reach)) {
    if (!inView(t, o.open) || !hostOk(t, o.sel.hostsOff, o.self)) continue;
    const id = projectOf(t, o.projects)?.id;
    if (id) perProject.set(id, (perProject.get(id) ?? 0) + 1);
  }

  // Hosts in the owner's order; a host the front does not list (yet, or any more) after them, by name. A host that is
  // off stays, as a selected value does.
  const off = new Set(o.sel.hostsOff);
  const listed = new Set(o.hosts.map((h) => h.name));
  const extra = [...new Set([...perHost.keys(), ...off])].filter((n) => !listed.has(n)).sort();
  const shownHosts = [...o.hosts.map((h) => ({ name: h.name, info: h as HostInfo | undefined })), ...extra.map((name) => ({ name, info: undefined }))]
    .map((h) => ({ ...h, n: perHost.get(h.name) ?? 0, on: !off.has(h.name) }))
    .filter((h) => h.n > 0 || !h.on);
  const short = shortHostNames(shownHosts.map((h) => h.name));
  const onCount = shownHosts.filter((h) => h.on).length;
  const hosts: HostChip[] = shownHosts.map((h) => ({ name: h.name, short: short.get(h.name) ?? h.name, ...(h.info ? { info: h.info } : {}), n: h.n, on: h.on, last: h.on && onCount === 1 }));

  // Projects in the owner's order (the store keeps it, V0.121). A locked quiet project is not offered unless picked.
  const projects: ProjectChip[] = o.projects
    .map((p) => ({ project: p, n: (p.id && perProject.get(p.id)) || 0, selected: !!p.id && p.id === project }))
    .filter((c) => c.selected || (c.n > 0 && !quietLocked(c.project, o.now, o.showUntil)));

  return { hosts: hosts.length > 1 || hosts.some((h) => !h.on) ? hosts : [], projects: projects.length > 1 || projects.some((c) => c.selected) ? projects : [] };
}

/** The empty state under the chips: null while no filter is on or a thread it lets through is in view; else the
 *  second line ("2 more in folded sections." or ""). `threads` are the filtered threads the inbox would list. */
export function filterEmpty(threads: Record<string, Thread>, sel: FilterSel, open: Open): string | null {
  if (!sel.hostsOff.length && !sel.project) return null;
  let folded = 0;
  for (const t of Object.values(threads)) {
    if (inView(t, open)) return null;
    if (sectionOf(t)) folded++;
  }
  return folded ? `${folded.toLocaleString()} more in folded section${folded === 1 ? "" : "s"}.` : "";
}
