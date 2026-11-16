// The Changes board (SPEC 0.5 V0.64): every change of every thread on one screen, grouped by stage. Pure.

import type { Change, Project, Thread } from "./types.ts";
import { changeList, ended, isDone, needsOwner } from "./offers.ts";
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
  /** V0.78 (#54): show the changes of settled and snoozed threads; off, only those that need the owner stay */
  settled?: boolean;
}

/** V0.78 (#54): a thread whose work is put away. Its changes are history unless one needs the owner. */
export function putAway(t: Thread): boolean {
  return t.attention.section === "settled" || t.attention.section === "snoozed";
}

/** A change that fails or waits for the owner goes to Needs you, whatever its phase; one the owner parked with Not
 *  now (V0.84) stays in its phase. */
export function columnOf(c: Change): Column {
  if (ended(c.phase)) return "ended";
  if (needsOwner(c)) return "needs";
  return c.phase as Column;
}

/** Every change of every thread that is not archived, filtered. V0.98 (2): a done change is not on the board, unless
 *  it still needs the owner, or "Show ended" asks for the ended ones. */
export function boardItems(threads: Record<string, Thread>, projects: Project[], f: BoardFilter = {}): BoardItem[] {
  return collect(threads, projects, f, (c) => (f.ended && ended(c.phase)) || (!isDone(c) && !ended(c.phase)) || columnOf(c) === "needs");
}

function collect(threads: Record<string, Thread>, projects: Project[], f: BoardFilter, keep: (c: Change) => boolean): BoardItem[] {
  const q = (f.q ?? "").trim().toLowerCase();
  const out: BoardItem[] = [];
  for (const t of Object.values(threads)) {
    if (t.archivedAt) continue;
    const project = projectOf(t, projects);
    if (f.projectId && project?.id !== f.projectId) continue;
    const away = !f.settled && putAway(t);
    for (const c of changeList(t)) {
      if (!keep(c)) continue;
      if (away && columnOf(c) !== "needs") continue; // before once(): a host record shows under an open thread
      if (q && ![c.title, c.key, c.group ?? "", ...(c.targets ?? []), t.title].join(" ").toLowerCase().includes(q)) continue;
      out.push({ change: c, thread: t, project });
    }
  }
  return once(out);
}

/** #35: a host record (an observed branch) is in every thread that shows it, and once on the board. V0.87 (3), #69:
 *  a record that a shown change follows is that change's card; a thread's change is one card per id. */
function once<T extends { change: Change; thread: Thread }>(items: T[]): T[] {
  const followed = new Set(items.map((i) => i.change.observed).filter((id): id is string => !!id));
  const seen = new Set<string>();
  return items.filter(({ change: c, thread: t }) => {
    if (c.threads && followed.has(c.id)) return false;
    const key = c.threads ? c.id : `${t.id}/${c.id}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

/** A column's entries: changes of one group together under the group's name (a cross-repo set), a stack in the
 *  order it builds (base first), the rest newest first. */
export type Entry = { kind: "one"; item: BoardItem } | { kind: "group"; group: string; items: BoardItem[] };

export function columns(items: BoardItem[], showEnded = false, now?: number): { key: Column; label: string; entries: Entry[]; count: number }[] {
  // V0.98 (4): with the time, an untouched change is in its own group, not in a column.
  const shown = now === undefined ? items : items.filter((i) => !untouched(i.change, now));
  return COLUMNS.filter((c) => c.key !== "ended" || showEnded).map((col) => {
    const mine = shown.filter((i) => columnOf(i.change) === col.key).sort((a, b) => b.change.updatedAt.localeCompare(a.change.updatedAt));
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

/** V0.98 (4): an open change goes to "Untouched for 3 days" after 3 days without a push, PR event, report or owner
 *  action. One that needs the owner stays in Needs you. */
export const UNTOUCHED_MS = 3 * 24 * 3600 * 1000;

export function untouched(c: Change, now: number): boolean {
  if (isDone(c) || ended(c.phase) || columnOf(c) === "needs") return false;
  const at = Date.parse(c.touchedAt ?? c.updatedAt);
  return !Number.isNaN(at) && now - at >= UNTOUCHED_MS;
}

/** V0.98 (4): the board's Untouched group, the longest untouched first. */
export function untouchedItems(items: BoardItem[], now: number): BoardItem[] {
  const at = (i: BoardItem) => i.change.touchedAt ?? i.change.updatedAt;
  return items.filter((i) => untouched(i.change, now)).sort((a, b) => Date.parse(at(a)) - Date.parse(at(b)));
}

/** V0.98 (3): Finished since your last visit: the changes done after seenAt (every done one before a first visit),
 *  newest first, with the board's project and text filter. */
export function finishedSince(threads: Record<string, Thread>, projects: Project[], seenAt: string | null, f: BoardFilter = {}): BoardItem[] {
  const items = collect(threads, projects, { ...f, settled: true }, (c) => isDone(c) && (!seenAt || Date.parse(c.done!.at) > Date.parse(seenAt)));
  return items.sort((a, b) => Date.parse(b.change.done!.at) - Date.parse(a.change.done!.at));
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
  const items: { change: Change; thread: Thread }[] = [];
  for (const t of Object.values(threads)) if (!t.archivedAt) for (const c of changeList(t)) if (columnOf(c) === "needs") items.push({ change: c, thread: t });
  return once(items).length;
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

/** V0.97: one merge of Merge all green: the press of a change's Merge, bound to the head the board showed. */
export interface GreenMerge {
  thread: string;
  change: string;
  pr: string;
  args: { on: string; action: string; digest: string; head: string };
}

/** The PR of a URL without its tail (/files, #x) and case, so one PR is one merge. */
const prKey = (url: string) => (/^(.*?\/pull\/\d+)/i.exec(url)?.[1] ?? url).toLowerCase();

/** V0.97: the changes whose PR is ready to merge (the host's Ready is "Merge"), one per PR, in board order. */
export function greenMerges(items: BoardItem[]): GreenMerge[] {
  const seen = new Set<string>();
  const out: GreenMerge[] = [];
  for (const { change: c, thread: t } of items) {
    const head = c.pr?.head;
    const offer = c.offers?.find((o) => o.run === "fyr" && o.effect === "pr.merge");
    if (c.ready !== "Merge" || !c.pr?.url || !head || !offer) continue;
    const k = prKey(c.pr.url);
    if (seen.has(k)) continue;
    seen.add(k);
    out.push({ thread: t.id, change: c.id, pr: c.pr.url, args: { on: c.id, action: offer.id, digest: offer.digest, head } });
  }
  return out;
}

/** V0.97: sends the merges one after another; a refusal is kept for its row (by thread/change) and the rest go on. */
export async function mergeAllGreen(list: GreenMerge[], send: (m: GreenMerge) => Promise<{ ok: boolean; error?: string }>): Promise<Record<string, string>> {
  const errs: Record<string, string> = {};
  for (const m of list) {
    let r: { ok: boolean; error?: string };
    try {
      r = await send(m);
    } catch (e) {
      r = { ok: false, error: e instanceof Error ? e.message : String(e) };
    }
    if (!r.ok) errs[`${m.thread}/${m.change}`] = r.error ?? "Not merged";
  }
  return errs;
}
