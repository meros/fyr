// SPEC 0.5 V0.134, V0.169, V0.192: the inbox filters on the desktop and the phone. Pure.
//
// Row 1, hosts (V0.169, V0.204): a toggle per host; all on is the default and means no host filter; a host turned off
// hides its threads, and a click on the only host still on ends the host filter. Row 2, projects: one project picked at a time (V0.134), counted only over the hosts that are on, so
// the projects of a host turned off fall out of it. A value is active when it matches at least one thread in view: in
// the pinned block or in an unfolded section (a folded section, by default Snoozed and Settled, is not considered).
// A row shows only with more than one value to choose from, or while one of its values is selected (a project
// picked, a host off); a selected value stays until the owner changes it. V0.114: a quiet project's threads count
// only while the unlock runs.
import { shortID, type HostInfo } from "./hosts.ts";
import { shortHostNames } from "./load.ts";
import { findProject, projectOf } from "./projects.ts";
import { hideQuiet, quietLocked, reachable } from "./quiet.ts";
import { isOpen, sectionOf, type SectionKey } from "./store.ts";
import type { Project, Thread } from "./types.ts";

/** V0.169: the hosts turned off, remembered per device; it replaces V0.134's one host (`fyr.hostFilter`, dropped).
 *  V0.client-refs: a JSON list of install ids (a name only for a host that reports no id); names migrate once. */
export const HOSTS_OFF_KEY = "fyr.hostsOff";
export const OLD_HOST_FILTER_KEY = "fyr.hostFilter";

export interface FilterSel {
  /** the hosts turned off; none is the default (every host on, no host filter) */
  hostsOff: string[];
  project: string | null;
}

export type Open = Partial<Record<SectionKey, boolean>>;

export interface HostChip {
  /** V0.client-refs: what the filter keeps: the install id, or the name of a host that reports none */
  key: string;
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

/** V0.169: what the device stored, as a list of host keys; anything else is none off. */
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
export function toggleHost(off: readonly string[], key: string): string[] {
  return off.includes(key) ? off.filter((x) => x !== key) : [...off, key];
}

/** V0.204: what a click on a host's toggle leaves off. On the only host still on, nothing: every host comes back. */
export function clickHost(off: readonly string[], hosts: readonly Pick<HostChip, "key" | "last">[], key: string): string[] {
  return hosts.some((h) => h.key === key && h.last) ? [] : toggleHost(off, key);
}

/** The host a thread runs on: the host whose state it came in (decision 0031 2.4); no fallback to the front's own. */
export function threadHost(t: Pick<Thread, "host">): string {
  return t.host ?? "";
}

/** Whether the list shows the thread: pinned (always open), or in a section that is not folded. */
export function inView(t: Thread, open: Open): boolean {
  const k = sectionOf(t);
  if (!k) return false;
  return !!t.pinnedAt || isOpen(k, open);
}

/** V0.client-refs: the key the host filter knows a host by: its install id, else its name (a host older than V0.124). */
export function hostKey(h: Pick<HostInfo, "id" | "name">): string {
  return h.id || h.name;
}

/** The key of the host a thread runs on (hostKey). */
export function threadHostKey(t: Pick<Thread, "host" | "hostId">): string {
  return t.hostId || threadHost(t);
}

function hostOk(t: Thread, off: readonly string[]): boolean {
  return !off.length || !off.includes(threadHostKey(t));
}

function projectOk(t: Thread, project: string | null, projects: Project[]): boolean {
  return !project || projectOf(t, projects)?.id === project;
}

/** The project filter's id when that project still exists, else null (V0.23: a removed project filters nothing). */
export function liveProject(sel: FilterSel, projects: Project[]): string | null {
  return findProject(projects, sel.project)?.id ?? null; // decision 0034: any member's id names its project
}

/** The threads both rows let through: on a host that is on, and in the picked project. */
export function applyFilters(threads: Record<string, Thread>, sel: FilterSel, projects: Project[]): Record<string, Thread> {
  const project = liveProject(sel, projects);
  if (!sel.hostsOff.length && !project) return threads;
  const out: Record<string, Thread> = {};
  for (const t of Object.values(threads)) if (hostOk(t, sel.hostsOff) && projectOk(t, project, projects)) out[t.id] = t;
  return out;
}

export interface FilterInput {
  threads: Record<string, Thread>;
  projects: Project[];
  /** the front's hosts, in the owner's order (V0.132) */
  hosts: HostInfo[];
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
  const nameOf = new Map<string, string>();
  for (const t of Object.values(shown)) {
    if (!inView(t, o.open)) continue;
    const k = threadHostKey(t);
    perHost.set(k, (perHost.get(k) ?? 0) + 1);
    nameOf.set(k, threadHost(t));
  }
  // Row 2 counts only the threads on the hosts that are on.
  const perProject = new Map<string, number>();
  for (const t of Object.values(reach)) {
    if (!inView(t, o.open) || !hostOk(t, o.sel.hostsOff)) continue;
    const id = projectOf(t, o.projects)?.id;
    if (id) perProject.set(id, (perProject.get(id) ?? 0) + 1);
  }

  // Hosts in the owner's order; a host the front does not list (yet, or any more) after them, by name. A host that is
  // off stays, as a selected value does.
  const off = new Set(o.sel.hostsOff);
  const listed = new Set(o.hosts.map(hostKey));
  const extra = [...new Set([...perHost.keys(), ...off])]
    .filter((k) => !listed.has(k))
    // A host turned off that the front no longer lists and that has no thread in view: only its id is left; it says so.
    .map((key) => ({ key, name: nameOf.get(key) ?? (/^[A-Z0-9]{4}(-[A-Z0-9]{4}){3}$/.test(key) ? `id ${shortID(key)}` : key), info: undefined as HostInfo | undefined }))
    .sort((a, b) => a.name.localeCompare(b.name));
  const shownHosts = [...o.hosts.map((h) => ({ key: hostKey(h), name: h.name, info: h as HostInfo | undefined })), ...extra]
    .map((h) => ({ ...h, n: perHost.get(h.key) ?? 0, on: !off.has(h.key) }))
    .filter((h) => h.n > 0 || !h.on);
  const short = shortHostNames(shownHosts.map((h) => h.name));
  const onCount = shownHosts.filter((h) => h.on).length;
  const hosts: HostChip[] = shownHosts.map((h) => ({ key: h.key, name: h.name, short: short.get(h.name) ?? h.name, ...(h.info ? { info: h.info } : {}), n: h.n, on: h.on, last: h.on && onCount === 1 }));

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
