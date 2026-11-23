// SPEC V0.project-archive (decision 0017): a finished project the owner archived. The host stamps the project
// (`archivedAt`) and shelves each of its threads (`shelvedAt`) in the same write; a new message or question in a
// shelved thread brings that thread back (the host clears `shelvedAt`) and leaves the project archived. On both fronts
// an archived project leaves `store.projects`, so the project filter and every picker (New thread, Hand over, Project…)
// lose it at once, and a shelved thread leaves the inbox. Settings → Projects lists the archived ones under
// "Archived projects", where their page has Unarchive. Pure.
import { projectOf, projectRecords } from "./projects.ts";
import type { View } from "./store.ts";
import type { Project, Thread } from "./types.ts";

/** Decision 0034: a linked project is archived when every member is; Archive sends it to each member's host. */
export function isArchived(p: Project): boolean {
  return (p.members ?? [p]).every((m) => !!m.archivedAt);
}

/** The records Archive (on) or Unarchive (off) still has to change: a member already there gets no command. */
export function archiveTargets(p: Project, on: boolean): Project[] {
  return (p.members ?? [p]).filter((m) => !!m.archivedAt !== on);
}

/** The thread is put away with its archived project: its own host's record of the project is still archived. */
export function isShelved(t: Pick<Thread, "shelvedAt" | "projectId" | "project">, projects: readonly Project[]): boolean {
  if (!t.shelvedAt) return false;
  const p = projectOf(t, projects as Project[]);
  if (!p) return false;
  const own = (p.members ?? [p]).find((m) => m.id === t.projectId) ?? p;
  return !!own.archivedAt;
}

/** The store as the lists show it: archived projects gone. The threads stay, so a shelved thread still opens (from its
 *  project's page) and the inbox leaves it out (`inboxThreads`). No archived project gives the same object back. */
export function applyArchived(s: View): View {
  if (!s.projects.some(isArchived)) return s;
  return { ...s, projects: s.projects.filter((p) => !isArchived(p)) };
}

/** The threads the inbox sections, its counts and search read: without the shelved ones. `projects` are all of them,
 *  the archived ones too. Nothing shelved gives the same object back. */
export function inboxThreads(threads: Record<string, Thread>, projects: readonly Project[]): Record<string, Thread> {
  const ts = Object.values(threads);
  if (!ts.some((t) => isShelved(t, projects))) return threads;
  const out: Record<string, Thread> = {};
  for (const t of ts) if (!isShelved(t, projects)) out[t.id] = t;
  return out;
}

/** The archived projects, for Settings → Projects, in the list's order. */
export function archivedProjects(projects: readonly Project[]): Project[] {
  return projects.filter((p) => p.id && isArchived(p));
}

/** The threads of the project (any member), newest first: what its archived page lists to open. */
export function projectThreads(p: Project, threads: Record<string, Thread>): Thread[] {
  const ids = new Set(projectRecords([p]).map((m) => m.id));
  return Object.values(threads)
    .filter((t) => (t.projectId ? ids.has(t.projectId) : t.project === p.name))
    .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
}

/** Why the project cannot be archived now, as the host says it (internal/core/projectarchive.go ShelveBlock), or null:
 *  a thread of it that works (a run, a message on its way) or asks. An archived or shelved thread is not looked at. */
export function archiveBlock(p: Project, threads: Record<string, Thread>): string | null {
  for (const t of projectThreads(p, threads)) {
    if (t.archivedAt || t.shelvedAt) continue;
    const name = t.title || t.rid || t.id;
    if (t.running || t.outbox.length) return `“${name}” is working: stop its run or wait for it first`;
    if (t.decisions.length) return `“${name}” has an open question: answer it first`;
  }
  return null;
}

/** What a project's Archive group shows. */
export interface ArchiveInfo {
  archived: boolean;
  block: string | null;
  threads: Thread[];
}

/** The toast after Archive or Unarchive. */
export function archiveToast(name: string, on: boolean): string {
  return on ? `Archived ${name}` : `Unarchived ${name}`;
}
