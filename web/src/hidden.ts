// SPEC V0.207: hosts and projects this device never shows. Kept in this browser; other devices are not affected and
// nothing stops on the host. Unlike the inbox filters (V0.169), which narrow the list for a moment, a hidden record
// leaves the whole front: inbox, filters, Changes, pickers and this device's notifications. Settings keeps listing it,
// because that is where it comes back. Pure.
import { threadHost } from "./filters.ts";
import type { HostInfo } from "./hosts.ts";
import { linkProjects, projectOf, projectRecords } from "./projects.ts";
import type { Store } from "./store.ts";
import type { Project, Thread } from "./types.ts";

export const HIDDEN_KEY = "fyr.hidden";

export interface Hidden {
  /** hosts by install id (decision 0031), by name for a host that reports none */
  hosts: string[];
  /** projects by ref */
  projects: string[];
}

export const NOTHING_HIDDEN: Hidden = { hosts: [], projects: [] };

type HostRef = Pick<HostInfo, "name" | "id">;

const names = (v: unknown): string[] => (Array.isArray(v) ? [...new Set(v.filter((x): x is string => typeof x === "string" && !!x))] : []);

/** What the device stored; anything else hides nothing. */
export function parseHidden(raw: string | null): Hidden {
  if (!raw) return NOTHING_HIDDEN;
  try {
    const v = JSON.parse(raw) as Partial<Hidden> | null;
    return { hosts: names(v?.hosts), projects: names(v?.projects) };
  } catch {
    return NOTHING_HIDDEN;
  }
}

export function hiddenCount(h: Hidden): number {
  return h.hosts.length + h.projects.length;
}

export function hostIsHidden(h: HostRef, hidden: Hidden): boolean {
  return hidden.hosts.includes(h.name) || (!!h.id && hidden.hosts.includes(h.id));
}

/** The one host still shown cannot be hidden: a front with no host at all has nothing to start a thread on. */
export function lastShownHost(h: HostRef, hosts: readonly HostRef[], hidden: Hidden): boolean {
  return !hostIsHidden(h, hidden) && hosts.filter((x) => !hostIsHidden(x, hidden)).length === 1;
}

export function setHostShown(hidden: Hidden, h: HostRef, shown: boolean): Hidden {
  const rest = hidden.hosts.filter((x) => x !== h.name && x !== h.id);
  return { ...hidden, hosts: shown ? rest : [...rest, h.id || h.name] };
}

export function setProjectShown(hidden: Hidden, project: string | Pick<Project, "id" | "members">, shown: boolean): Hidden {
  const p = typeof project === "string" ? { id: project } : project;
  const ids = (("members" in p && p.members) || [p]).map((m) => m.id);
  const rest = hidden.projects.filter((x) => !ids.includes(x));
  return { ...hidden, projects: shown || !p.id ? rest : [...rest, p.id] };
}

/** The ids and names that hide a host. With every listed host hidden (the list changed since) none is: the front stays usable. */
function hiddenHostKeys(hosts: readonly HostRef[], hidden: Hidden): Set<string> {
  if (hosts.length && hosts.every((h) => hostIsHidden(h, hidden))) return new Set();
  const keys = new Set(hidden.hosts);
  for (const h of hosts) {
    if (!hostIsHidden(h, hidden)) continue;
    keys.add(h.name);
    if (h.id) keys.add(h.id);
  }
  return keys;
}

/** Decision 0034: hiding any member's id hides the linked project; the id the lists show can change with the hosts. */
export function projectIsHidden(p: Project, hidden: Hidden): boolean {
  return (p.members ?? [p]).some((m) => !!m.id && hidden.projects.includes(m.id));
}

/** The store as this device shows it. Nothing hidden gives the same object back. */
export function applyHidden(s: Store, hidden: Hidden): Store {
  if (!hiddenCount(hidden)) return s;
  const keys = hiddenHostKeys(s.hosts, hidden);
  const threadGone = (t: Thread) => keys.has(threadHost(t, s.host ?? "")) || (!!t.hostId && keys.has(t.hostId)) || (() => {
    const p = projectOf(t, s.projects);
    return !!p && projectIsHidden(p, hidden);
  })();
  const threads: Record<string, Thread> = {};
  for (const t of Object.values(s.threads)) if (!threadGone(t)) threads[t.id] = t;
  // Decision 0034: a hidden host takes only its own record of a linked project; the others still show it.
  const hosts = s.hosts.filter((h) => !keys.has(h.name));
  const shown = projectRecords(s.projects.filter((p) => !projectIsHidden(p, hidden))).filter((m) => !(m.host && keys.has(m.host)) && !(m.hostId && keys.has(m.hostId)));
  return {
    ...s,
    threads,
    projects: linkProjects(shown, hosts),
    hosts,
    stalls: s.stalls.filter((x) => !x.host || !keys.has(x.host)),
  };
}

export interface PushHidden {
  hosts: string[];
  projects: { host: string; id: string }[];
}

/** What the front needs to leave this device out of a push. It sees host names and each host's own project ids. */
export function pushHiddenOf(hosts: readonly HostRef[], projects: readonly Project[], hidden: Hidden): PushHidden {
  const keys = hiddenHostKeys(hosts, hidden);
  return {
    hosts: hosts.filter((h) => keys.has(h.name)).map((h) => h.name),
    projects: projects.filter((p) => projectIsHidden(p, hidden)).flatMap((p) => p.members ?? [p]).map((p) => ({ host: p.host ?? "", id: p.rid ?? p.id! })),
  };
}
