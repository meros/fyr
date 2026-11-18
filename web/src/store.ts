// Pure state store: reducer functions over the API DTOs. No I/O, no clock reads (time is passed in).
import { orderProjects } from "./projects.ts";
import type { QueueEntry } from "./slot-queue.ts";
import type { HostInfo } from "./hosts.ts";
import type { OwnerMarks } from "./host-mark.ts";
import type { DeployProgress } from "./deploy.ts";
import { decodeStall, stallsOf, upsertStall, type StallInfo } from "./stall.ts";
import { decodeProject, decodeState, decodeThread } from "./decode.ts";

import type { ActionInfo, AttentionState, FyrEvent, Profile, Project, StateDTO, Thread, ThreadDTO } from "./types.ts";

export type SectionKey = "needs" | "working" | "slots" | "unread" | "seen" | "snoozed" | "settled";

/** `g`: the section's key after g on the desktop (g1–g6). V0.137: Waiting for a slot has none, so the others keep their
 *  keys; it shows only while a host's queue is not empty (`transient`). V0.189: Unread is new and takes g6, so g3 stays
 *  Seen, g4 Snoozed and g5 Settled although it sits above them. V0.199: what is for the owner to read comes before what
 *  is still running; the g keys follow the section, not its place. */
export const SECTIONS: { key: SectionKey; label: string; short: string; state: AttentionState; g?: number; transient?: boolean }[] = [
  { key: "needs", label: "Needs you", short: "Needs", state: "asking", g: 1 },
  { key: "unread", label: "Unread", short: "Unread", state: "done", g: 6 },
  { key: "seen", label: "Seen", short: "Seen", state: "seen", g: 3 },
  { key: "working", label: "Working", short: "Working", state: "working", g: 2 },
  { key: "slots", label: "Waiting to run", short: "Waiting", state: "waiting", transient: true },
  { key: "snoozed", label: "Snoozed", short: "Snoozed", state: "snoozed", g: 4 },
  { key: "settled", label: "Settled", short: "Settled", state: "settled", g: 5 },
];

export type Conn = "connecting" | "live" | "offline";

export interface Store {
  loaded: boolean;
  conn: Conn;
  /** server clock minus client clock, ms */
  clockOffset: number;
  user: string;
  host: string;
  /** V0.125: this install's id (the state's hostId, V0.124), for Settings → Hosts when the front lists no hosts */
  hostId: string;
  /** V0.170: what this install runs on, for its mark when the front lists no hosts */
  machine: string;
  /** V0.171: the owner's host marks by install id */
  hostMarks: OwnerMarks;
  /** V0.51 */
  hosts: HostInfo[];
  threads: Record<string, Thread>;
  actions: ActionInfo[];
  profiles: Profile[];
  /** in the owner's order (V0.121: orderProjects) */
  projects: Project[];
  /** V0.121: the owner's project order; null until set (by name then) */
  projectOrder: string[] | null;
  /** V0.39 */
  deploy: DeployProgress | null;
  /** V0.147: the hosts' stalls, one per host */
  stalls: StallInfo[];
  /** timeline events per thread, oldest first; only for threads the client has loaded */
  events: Record<string, FyrEvent[]>;
  /** The last seq of each loaded timeline when the stream left live: a refetch starts there, not after the
   * events the new stream already appended (review 2026-11-15). Cleared per thread once refetched. */
  resumeFrom: Record<string, number>;
}

export function initialStore(): Store {
  return {
    loaded: false,
    conn: "connecting",
    clockOffset: 0,
    user: "",
    host: "",
    hostId: "",
    machine: "",
    hostMarks: {},
    hosts: [],
    threads: {},
    actions: [],
    profiles: [],
    projects: [],
    projectOrder: null,
    events: {},
    resumeFrom: {},
    deploy: null,
    stalls: [],
  };
}

function ts(s: string | null | undefined): number {
  if (!s) return 0;
  const n = Date.parse(s);
  return Number.isNaN(n) ? 0 : n;
}

/** Keep the newer of two versions of a thread: by rev when both carry one, else by updatedAt (equal replaces). */
export function newer(cur: ThreadDTO | undefined, next: ThreadDTO): boolean {
  if (!cur) return true;
  if (typeof cur.rev === "number" && typeof next.rev === "number") return next.rev >= cur.rev;
  return ts(next.updatedAt) >= ts(cur.updatedAt);
}

/** #42: keep the newer of two versions of a project by rev (equal replaces); without a rev on both, the next one. */
function newerProject(cur: Project | undefined, next: Project): boolean {
  return !cur || typeof cur.rev !== "number" || typeof next.rev !== "number" || next.rev >= cur.rev;
}

export function applySnapshot(s: Store, raw: StateDTO, clientNow: number): Store {
  const dto = decodeState(raw);
  const threads: Record<string, Thread> = {};
  for (const t of dto.threads) threads[t.id] = t;
  // An upsert that arrived on the stream after subscribing but before the snapshot may be newer.
  // Threads missing from the snapshot are dropped (stale from an earlier connection).
  for (const id of Object.keys(threads)) {
    const cur = s.threads[id];
    if (cur && cur !== threads[id] && !newer(cur, threads[id])) threads[id] = cur;
  }
  // Projects the same way (#42): a project frame may have crossed the snapshot in flight.
  const projects = (dto.projects ?? []).map((p) => {
    const cur = p.id ? s.projects.find((x) => x.id === p.id) : undefined;
    return cur && !newerProject(cur, p) ? cur : p;
  });
  return {
    ...s,
    loaded: true,
    clockOffset: ts(dto.now) ? ts(dto.now) - clientNow : s.clockOffset,
    user: dto.user ?? "",
    host: dto.host ?? "",
    hostId: typeof dto.hostId === "string" ? dto.hostId : "",
    machine: typeof dto.machine === "string" ? dto.machine : "",
    hostMarks: decodeHostMarks(dto.hostMarks),
    hosts: dto.hosts ?? [],
    threads,
    actions: dto.actions ?? [],
    profiles: dto.profiles ?? [],
    projects: orderProjects(projects, dto.projectOrder ?? null),
    projectOrder: dto.projectOrder ?? null,
    deploy: dto.deploy ?? null,
    stalls: stallsOf(dto),
  };
}

/** The state as the device stores it (V0.38); applySnapshot reads it back on an offline start. */
export function stateForCache(s: Store, now: string): StateDTO {
  return { now, user: s.user, host: s.host, ...(s.hostId ? { hostId: s.hostId } : {}), hosts: s.hosts, deploy: s.deploy, ...(s.stalls.length ? { stalls: s.stalls } : {}), threads: Object.values(s.threads), actions: s.actions, profiles: s.profiles, projects: s.projects, ...(s.projectOrder ? { projectOrder: s.projectOrder } : {}) };
}

export function upsertThread(s: Store, raw: ThreadDTO): Store {
  const t = decodeThread(raw);
  if (!t.id) return s;
  if (!newer(s.threads[t.id], t)) return s;
  return { ...s, threads: { ...s.threads, [t.id]: t } };
}

export function setConn(s: Store, conn: Conn): Store {
  if (s.conn === conn) return s;
  if (s.conn !== "live" || conn === "live") return { ...s, conn };
  // Leaving live: remember where each loaded timeline stood. An older point (not refetched yet) stays.
  const resumeFrom = { ...s.resumeFrom };
  for (const id of Object.keys(s.events)) if (!(id in resumeFrom)) resumeFrom[id] = lastSeq(s, id);
  return { ...s, conn, resumeFrom };
}

/** Where to fetch a loaded timeline from: before a reconnect gap when there is one, else its newest seq. */
export function resumeSeq(s: Store, threadId: string): number {
  return s.resumeFrom[threadId] ?? lastSeq(s, threadId);
}

/** The timeline was refetched from `from`: forget that gap (unless a newer drop recorded another point). */
export function resumed(s: Store, threadId: string, from: number): Store {
  if (s.resumeFrom[threadId] !== from) return s;
  const { [threadId]: _, ...rest } = s.resumeFrom;
  return { ...s, resumeFrom: rest };
}

/** Merge events for a thread, deduplicated by seq, oldest first. */
export function mergeEvents(s: Store, threadId: string, evs: FyrEvent[], replace = false): Store {
  const cur = replace ? [] : (s.events[threadId] ?? []);
  const bySeq = new Map<number, FyrEvent>();
  for (const e of cur) bySeq.set(e.seq, e);
  for (const e of evs) bySeq.set(e.seq, e);
  const list = [...bySeq.values()].sort((a, b) => a.seq - b.seq);
  return { ...s, events: { ...s.events, [threadId]: list } };
}

/** A live event: only kept when that thread's timeline is loaded (it is fetched in full on open). */
export function liveEvent(s: Store, e: FyrEvent): Store {
  if (!s.events[e.subject]) return s;
  return mergeEvents(s, e.subject, [e]);
}

export function lastSeq(s: Store, threadId: string): number {
  const l = s.events[threadId];
  return l && l.length ? l[l.length - 1].seq : 0;
}

// ---- sections and order ----

const NEEDS_RANK: Partial<Record<AttentionState, number>> = { asking: 0, failed: 1, blocked: 2, done: 3 };

function activity(t: Thread): number {
  return ts(t.activityAt) || ts(t.updatedAt);
}

export function compareNeeds(a: Thread, b: Thread): number {
  const ra = NEEDS_RANK[a.attention.state] ?? 9;
  const rb = NEEDS_RANK[b.attention.state] ?? 9;
  if (ra !== rb) return ra - rb;
  return activity(b) - activity(a) || a.id.localeCompare(b.id);
}

/** When the thread's work started: the run, else (waiting) the earliest running agent; 0 when unknown. */
export function workStartedAt(t: Thread): number {
  const run = ts(t.runStartedAt);
  if (run) return run;
  const starts = t.tasks.filter((x) => x.status === "running").map((x) => ts(x.startedAt)).filter((n) => n > 0);
  return starts.length ? Math.min(...starts) : 0;
}

/** V0.26: a stable order, newest started first, then id; activity never moves a row. */
export function compareWorking(a: Thread, b: Thread): number {
  return workStartedAt(b) - workStartedAt(a) || a.id.localeCompare(b.id);
}

export function compareActivityDesc(a: Thread, b: Thread): number {
  return activity(b) - activity(a) || a.id.localeCompare(b.id);
}

/** Each section's threads, pinned ones included (counts and badges follow attention), and `pinned`: the pinned threads
 * in the normal order (section order, then the section's own order), which the lists draw above every section (V0.104). */
export type Sections = Record<SectionKey, Thread[]> & { pinned: Thread[] };

/** Sections that show only their header line until opened (the design draws Snoozed and Settled this way). */
export const COLLAPSED_BY_DEFAULT: SectionKey[] = ["snoozed", "settled"];

/** Whether a section is unfolded: the owner's choice, else open unless collapsed by default. */
export function isOpen(k: SectionKey, open: Partial<Record<SectionKey, boolean>>): boolean {
  return open[k] ?? !COLLAPSED_BY_DEFAULT.includes(k);
}

export function isPinned(t: Thread): boolean {
  return !!t.pinnedAt;
}

/** A section's rows under the pinned block: the section without its pinned threads. */
export function unpinned(list: Thread[], key?: SectionKey): Thread[] {
  if (key === "slots") return list; // V0.137: the queue keeps its pinned threads, in its order
  return list.filter((t) => !isPinned(t));
}

/** V0.189: the host sends a finished thread the owner has not looked at as state "done" in section "seen" (V0.101).
 *  The client splits that off as Unread, so a host of any version since V0.101 lands right. */
export function sectionOf(t: Thread): SectionKey | null {
  switch (t.attention?.section) {
    case "seen":
      return t.attention.state === "done" ? "unread" : "seen";
    case "needs":
    case "working":
    case "snoozed":
    case "settled":
      return t.attention.section;
    default:
      return null; // archived or unknown: hidden
  }
}

/** V0.137: `queue` (slotQueue of the load) moves every queued thread to Waiting for a slot, in the queue's order (host by
 *  host, then its place), except a thread in Needs you, which stays there: the owner acts on it first. A pinned queued
 *  thread is in the queue's section and not in the pinned block, so the queue reads in its real order. */
export function sections(threads: Record<string, Thread>, queue: readonly QueueEntry[] = []): Sections {
  const out: Sections = { needs: [], working: [], slots: [], unread: [], seen: [], snoozed: [], settled: [], pinned: [] };
  const queued = new Set(queue.map((e) => e.owner));
  for (const t of Object.values(threads)) {
    const k = sectionOf(t);
    if (k && k !== "needs" && queued.has(t.id)) continue;
    if (k) out[k].push(t);
  }
  for (const e of queue) {
    const t = threads[e.owner];
    if (t && !e.priority && sectionOf(t) && sectionOf(t) !== "needs" && !out.slots.includes(t)) out.slots.push(t);
  }
  out.needs.sort(compareNeeds);
  out.working.sort(compareWorking);
  out.unread.sort(compareActivityDesc);
  out.seen.sort(compareActivityDesc);
  out.snoozed.sort(compareActivityDesc);
  out.settled.sort(compareActivityDesc);
  out.pinned = SECTIONS.filter((s) => s.key !== "slots").flatMap((s) => out[s.key].filter(isPinned));
  return out;
}

/** Flat order used by j/k: the pinned threads, then the fixed section order. A limit counts a section's unpinned rows. */
export function flatOrder(sec: Sections, limits?: Partial<Record<SectionKey, number>>): string[] {
  const ids: string[] = sec.pinned.map((t) => t.id);
  for (const { key } of SECTIONS) {
    const list = unpinned(sec[key], key);
    const n = limits?.[key] ?? list.length;
    for (let i = 0; i < Math.min(n, list.length); i++) ids.push(list[i].id);
  }
  return ids;
}

/** Search: the query (any case, padded or not) in the title, the project or `#<PR number>`. */
export function matchesQuery(t: Thread, q: string): boolean {
  return `${t.title} ${t.project} ${t.pr ? "#" + t.pr.number : ""}`.toLowerCase().includes(q.trim().toLowerCase());
}

export function isAvailable(t: Thread | undefined, action: string): boolean {
  return !!t && !(action in t.unavailable);
}

export function unavailableReason(t: Thread | undefined, action: string): string | undefined {
  if (!t) return "No thread";
  return t.unavailable[action];
}

// ---- undo of deferred actions (settle, archive, snooze) ----

export const UNDO_MS = 6000;

export type UndoKind = "settle" | "archive" | "snooze";

export interface UndoEntry {
  kind: UndoKind;
  threadIds: string[];
  at: number;
  label: string;
}

const INVERSE: Record<UndoKind, string> = {
  settle: "thread.unsettle",
  archive: "thread.unarchive",
  snooze: "thread.unsnooze",
};

export function undoKindOf(action: string): UndoKind | null {
  if (action === "thread.settle") return "settle";
  if (action === "thread.archive") return "archive";
  if (action === "thread.snooze" || action === "thread.snoozeWhenDone") return "snooze";
  return null;
}

/** The toast's verb for an undoable action. */
export function undoVerb(kind: UndoKind): string {
  return kind === "settle" ? "Settled" : kind === "archive" ? "Archived" : "Snoozed";
}

export function recordUndo(kind: UndoKind, threadIds: string[], now: number, label?: string): UndoEntry {
  const n = threadIds.length;
  const verb = undoVerb(kind);
  return { kind, threadIds, at: now, label: label ?? (n === 1 ? verb : `${verb} ${n} threads`) };
}

/** V0.162: a bulk settle (⇧E, Settle merged, the phone's Settle N) takes the threads that can settle and leaves the
 * pinned ones out: no settle, no unpin. `kept` counts the pinned threads it left. V0.189: a `sweep` (⇧E on a section) also
 * leaves Unread out, what the owner has not read is not swept away (`unread` counts them); threads the owner picked one
 * by one are their explicit choice and settle even when unread. */
export function bulkSettle(threads: Thread[], sweep = false): { ids: string[]; kept: number; unread: number } {
  const ids: string[] = [];
  let kept = 0;
  let unread = 0;
  for (const t of threads) {
    if (isPinned(t)) kept++;
    else if (sweep && sectionOf(t) === "unread") unread++;
    else if (isAvailable(t, "thread.settle")) ids.push(t.id);
  }
  return { ids, kept, unread };
}

/** V0.189: the phone's Select all is a sweep: it picks every row but the unread ones (a pinned one stays picked, the
 *  settle counts it as pinned), so "Settle N" counts what it will settle and a row tapped afterwards is the owner's pick. */
export function selectAllIds(threads: Thread[]): string[] {
  return threads.filter((t) => isPinned(t) || sectionOf(t) !== "unread").map((t) => t.id);
}

/** V0.189: the unread threads a Select all left out and the owner has not picked since, for the toast. */
export function sweptUnread(threads: Thread[], picked: ReadonlySet<string>): number {
  return threads.filter((t) => !isPinned(t) && sectionOf(t) === "unread" && !picked.has(t.id)).length;
}

/** The toast after a bulk settle: "Settled 7 threads; 1 pinned thread kept". */
export function bulkSettleText(settled: number, kept: number, unread = 0): string {
  const head = settled === 0 ? "Nothing settled" : settled === 1 ? "Settled" : `Settled ${settled} threads`;
  const left = [kept && `${kept} pinned`, unread && `${unread} unread`].filter(Boolean).join(" and ");
  if (!left) return head;
  return `${head}; ${left} ${kept + unread === 1 ? "thread" : "threads"} kept`;
}

export function undoLive(u: UndoEntry | null, now: number): boolean {
  return !!u && now - u.at < UNDO_MS;
}

export interface PlannedCommand {
  action: string;
  threadId?: string;
  args?: Record<string, unknown>;
}

/** The commands that undo the entry, or null when there is nothing to undo or the window has passed. */
export function undoCommands(u: UndoEntry | null, now: number): PlannedCommand[] | null {
  if (!u || !undoLive(u, now)) return null;
  return u.threadIds.map((id) => ({ action: INVERSE[u.kind], threadId: id }));
}

// ---- snooze presets (local time), one list for desktop and phone (SPEC 0.5 V0.67) ----

export interface SnoozePreset {
  label: string;
  until: Date;
  /** "15:41", "today 18:00", "Sun 06:00" */
  when: string;
}

function hm(d: Date): string {
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

/** In 1 hour, in 3 hours, this evening (only before 17:00), tomorrow morning, Monday morning (a week ahead on a Monday). */
export function snoozePresets(now: Date): SnoozePreset[] {
  const out: SnoozePreset[] = [];
  const in1 = new Date(now.getTime() + 3600_000);
  const in3 = new Date(now.getTime() + 3 * 3600_000);
  out.push({ label: "In 1 hour", until: in1, when: hm(in1) });
  out.push({ label: "In 3 hours", until: in3, when: hm(in3) });
  if (now.getHours() < 17) {
    const ev = new Date(now);
    ev.setHours(18, 0, 0, 0);
    out.push({ label: "This evening", until: ev, when: "today 18:00" });
  }
  const tm = new Date(now);
  tm.setDate(now.getDate() + 1);
  tm.setHours(6, 0, 0, 0);
  out.push({ label: "Tomorrow morning", until: tm, when: `${DAYS[tm.getDay()]} 06:00` });
  const mon = new Date(now);
  mon.setDate(now.getDate() + (((8 - now.getDay()) % 7) || 7));
  mon.setHours(6, 0, 0, 0);
  out.push({ label: "Monday morning", until: mon, when: `${DAYS[mon.getDay()]} 06:00` });
  return out;
}

/** The desktop keys 1–5: the preset at that place in the list shown, or null. */
export function presetForKey<T extends SnoozePreset>(presets: T[], key: string): T | null {
  return /^[1-9]$/.test(key) ? (presets[Number(key) - 1] ?? null) : null;
}

/** A project from the stream (`event: project`, V0.23): replace by id unless older (#42), or add; `{id, deleted: true}`
 *  removes it (V0.58). */
export function upsertProject(s: Store, raw: Project): Store {
  if (raw.deleted) return raw.id && s.projects.some((x) => x.id === raw.id) ? { ...s, projects: s.projects.filter((x) => x.id !== raw.id) } : s;
  const p = decodeProject(raw);
  const i = s.projects.findIndex((x) => (p.id ? x.id === p.id : x.name === p.name));
  if (i >= 0 && !newerProject(s.projects[i], p)) return s;
  const projects = i >= 0 ? s.projects.map((x, j) => (j === i ? p : x)) : [...s.projects, p];
  return { ...s, projects: orderProjects(projects, s.projectOrder) };
}

/** V0.121: the owner's project order changed (the front's `projectOrder` event, or this device's own move). */
export function setProjectOrder(s: Store, ids: string[]): Store {
  return { ...s, projectOrder: ids, projects: orderProjects(s.projects, ids) };
}

/** RFC 7396 JSON Merge Patch: objects merge key by key, null deletes, anything else replaces. */
export function mergePatch(target: unknown, patch: unknown): unknown {
  if (!patch || typeof patch !== "object" || Array.isArray(patch)) return patch;
  const out: Record<string, unknown> = target && typeof target === "object" && !Array.isArray(target) ? { ...(target as Record<string, unknown>) } : {};
  for (const [k, v] of Object.entries(patch as Record<string, unknown>)) {
    if (v === null) delete out[k];
    else out[k] = mergePatch(out[k], v);
  }
  return out;
}

export interface ThreadPatch {
  id: string;
  rev: number;
  base: number;
  patch: Record<string, unknown>;
}

/** SPEC 0.5 V0.38: apply a thread patch only on the copy it was made against; else ask for the whole thread. */
export function applyThreadPatch(s: Store, p: ThreadPatch): { store: Store; needFull: boolean } {
  const cur = s.threads[p.id];
  if (!cur || cur.rev !== p.base) return { store: s, needFull: true };
  const next = decodeThread(mergePatch(cur, p.patch));
  return { store: { ...s, threads: { ...s.threads, [p.id]: next } }, needFull: false };
}

/** V0.147: a stall from the stream. */
export function setStall(s: Store, raw: unknown): Store {
  const st = decodeStall(raw, s.host || undefined);
  return st ? { ...s, stalls: upsertStall(s.stalls, st) } : s;
}

/** V0.39: a deploy progress from the stream. */
export function setDeploy(s: Store, p: DeployProgress | null): Store {
  return { ...s, deploy: p };
}

/** V0.171: the owner's host marks as the state or the "hostMarks" event sends them; anything else is left out. */
export function decodeHostMarks(raw: unknown): OwnerMarks {
  const out: OwnerMarks = {};
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return out;
  for (const [id, v] of Object.entries(raw as Record<string, unknown>)) {
    if (!v || typeof v !== "object") continue;
    const { icon, color } = v as { icon?: unknown; color?: unknown };
    const m: { icon?: string; color?: string } = {};
    if (typeof icon === "string" && icon) m.icon = icon;
    if (typeof color === "string" && color) m.color = color;
    if (m.icon || m.color) out[id] = m;
  }
  return out;
}

/** A link names a record by the host's own id (`?open=t_12`, an old push tap, a stored selection): the ref of the one
 *  record that has it. An id that is already a ref, or that two hosts have, or that none has, comes back as it is
 *  (decision 0031 C12: a link without a host opens the one thread that has the id, and is refused when two have it). */
export function resolveRef<T extends { id?: string; rid?: string }>(records: readonly T[], id: string): string {
  if (!id || records.some((r) => r.id === id)) return id;
  const hit = records.filter((r) => r.rid === id);
  return hit.length === 1 ? (hit[0].id ?? id) : id;
}
