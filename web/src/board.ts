// The Changes board (SPEC 0.5 V0.64): every change of every thread on one screen, grouped by stage. Pure.

import type { Change, Project, Thread } from "./types.ts";
import { ended } from "./offers.ts";
import { projectOf } from "./projects.ts";

export type Column = "needs" | "working" | "review" | "landed" | "live" | "ended";

export const COLUMNS: { key: Column; label: string }[] = [
  { key: "needs", label: "Needs you" },
  { key: "working", label: "Working" },
  { key: "review", label: "Review" },
  { key: "landed", label: "Landed" },
  { key: "live", label: "Live" },
  { key: "ended", label: "Ended" },
];

export interface BoardItem {
  change: Change;
  thread: Thread;
  project?: Project;
}

export interface BoardFilter {
  /** null: every project */
  projectId?: string | null;
  /** matches the change's title, key, targets and group, and the thread's title */
  q?: string;
  /** show abandoned and reverted changes */
  ended?: boolean;
}

/** A change that fails or waits for the owner goes to Needs you, whatever its phase. */
export function columnOf(c: Change): Column {
  if (ended(c.phase)) return "ended";
  if (c.needs === "owner" || c.conditions?.some((k) => k.status === "fail")) return "needs";
  return c.phase as Column;
}

/** Every change of every thread that is not archived, filtered. */
export function boardItems(threads: Record<string, Thread>, projects: Project[], f: BoardFilter = {}): BoardItem[] {
  const q = (f.q ?? "").trim().toLowerCase();
  const out: BoardItem[] = [];
  for (const t of Object.values(threads)) {
    if (t.archivedAt) continue;
    const project = projectOf(t, projects);
    if (f.projectId && project?.id !== f.projectId) continue;
    for (const c of t.changes) {
      if (!f.ended && ended(c.phase)) continue;
      if (q && ![c.title, c.key, c.group ?? "", ...(c.targets ?? []), t.title].join(" ").toLowerCase().includes(q)) continue;
      out.push({ change: c, thread: t, project });
    }
  }
  return out;
}

/** A column's entries: changes of one group together under the group's name (a cross-repo set), a stack in the
 *  order it builds (base first), the rest newest first. */
export type Entry = { kind: "one"; item: BoardItem } | { kind: "group"; group: string; items: BoardItem[] };

export function columns(items: BoardItem[], showEnded = false): { key: Column; label: string; entries: Entry[]; count: number }[] {
  return COLUMNS.filter((c) => c.key !== "ended" || showEnded).map((col) => {
    const mine = items.filter((i) => columnOf(i.change) === col.key).sort((a, b) => b.change.updatedAt.localeCompare(a.change.updatedAt));
    const groups = new Map<string, BoardItem[]>();
    const entries: Entry[] = [];
    for (const i of mine) {
      const g = i.change.group;
      if (!g) {
        entries.push({ kind: "one", item: i });
        continue;
      }
      if (!groups.has(g)) {
        groups.set(g, []);
        entries.push({ kind: "group", group: g, items: groups.get(g)! });
      }
      groups.get(g)!.push(i);
    }
    for (const e of entries) if (e.kind === "group") e.items.splice(0, e.items.length, ...stackOrder(e.items));
    return { key: col.key, label: col.label, entries, count: mine.length };
  });
}

/** Base first: a change comes after the changes it builds on (`after`, by id or key). */
export function stackOrder(items: BoardItem[]): BoardItem[] {
  const ref = (i: BoardItem) => [i.change.id, i.change.key];
  const done: BoardItem[] = [];
  const left = [...items];
  while (left.length) {
    const k = left.findIndex((i) => !(i.change.after ?? []).some((a) => left.some((o) => o !== i && ref(o).includes(a))));
    done.push(...left.splice(k < 0 ? 0 : k, 1));
  }
  return done;
}

/** The rail and menu badge: how many changes need the owner. */
export function needsCount(threads: Record<string, Thread>): number {
  let n = 0;
  for (const t of Object.values(threads)) if (!t.archivedAt) for (const c of t.changes) if (columnOf(c) === "needs") n++;
  return n;
}

/** The projects that have changes, for the filter chips. */
export function boardProjects(threads: Record<string, Thread>, projects: Project[]): Project[] {
  const ids = new Set<string>();
  for (const t of Object.values(threads)) if (!t.archivedAt && t.changes.length) {
    const p = projectOf(t, projects);
    if (p?.id) ids.add(p.id);
  }
  return projects.filter((p) => p.id && ids.has(p.id)).sort((a, b) => a.name.localeCompare(b.name));
}
