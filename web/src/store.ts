// Pure state store: reducer functions over the API DTOs. No I/O, no clock reads (time is passed in).

import type { ActionInfo, AttentionState, FyrEvent, Profile, Project, StateDTO, ThreadDTO } from "./types.ts";

export type SectionKey = "needs" | "working" | "seen" | "snoozed" | "settled";

export const SECTIONS: { key: SectionKey; label: string; short: string; state: AttentionState }[] = [
  { key: "needs", label: "Needs you", short: "Needs", state: "asking" },
  { key: "working", label: "Working", short: "Working", state: "working" },
  { key: "seen", label: "Seen", short: "Seen", state: "seen" },
  { key: "snoozed", label: "Snoozed", short: "Snoozed", state: "snoozed" },
  { key: "settled", label: "Settled", short: "Settled", state: "settled" },
];

export type Conn = "connecting" | "live" | "offline";

export interface Store {
  loaded: boolean;
  conn: Conn;
  /** server clock minus client clock, ms */
  clockOffset: number;
  user: string;
  threads: Record<string, ThreadDTO>;
  actions: ActionInfo[];
  profiles: Profile[];
  projects: Project[];
  /** timeline events per thread, oldest first; only for threads the client has loaded */
  events: Record<string, FyrEvent[]>;
}

export function initialStore(): Store {
  return {
    loaded: false,
    conn: "connecting",
    clockOffset: 0,
    user: "",
    threads: {},
    actions: [],
    profiles: [],
    projects: [],
    events: {},
  };
}

function ts(s: string | null | undefined): number {
  if (!s) return 0;
  const n = Date.parse(s);
  return Number.isNaN(n) ? 0 : n;
}

/** Keep the newer of two versions of a thread. Equal updatedAt replaces: attention can change with time alone. */
function newer(cur: ThreadDTO | undefined, next: ThreadDTO): boolean {
  if (!cur) return true;
  return ts(next.updatedAt) >= ts(cur.updatedAt);
}

export function applySnapshot(s: Store, dto: StateDTO, clientNow: number): Store {
  const threads: Record<string, ThreadDTO> = {};
  for (const t of dto.threads ?? []) threads[t.id] = t;
  // An upsert that arrived on the stream after subscribing but before the snapshot may be newer.
  // Threads missing from the snapshot are dropped (stale from an earlier connection).
  for (const id of Object.keys(threads)) {
    const cur = s.threads[id];
    if (cur && ts(cur.updatedAt) > ts(threads[id].updatedAt)) threads[id] = cur;
  }
  return {
    ...s,
    loaded: true,
    clockOffset: ts(dto.now) ? ts(dto.now) - clientNow : s.clockOffset,
    user: dto.user ?? "",
    threads,
    actions: dto.actions ?? [],
    profiles: dto.profiles ?? [],
    projects: dto.projects ?? [],
  };
}

export function upsertThread(s: Store, t: ThreadDTO): Store {
  if (!newer(s.threads[t.id], t)) return s;
  return { ...s, threads: { ...s.threads, [t.id]: t } };
}

export function setConn(s: Store, conn: Conn): Store {
  return s.conn === conn ? s : { ...s, conn };
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

function activity(t: ThreadDTO): number {
  return ts(t.activityAt) || ts(t.updatedAt);
}

export function compareNeeds(a: ThreadDTO, b: ThreadDTO): number {
  const ra = NEEDS_RANK[a.attention.state] ?? 9;
  const rb = NEEDS_RANK[b.attention.state] ?? 9;
  if (ra !== rb) return ra - rb;
  return activity(b) - activity(a) || a.id.localeCompare(b.id);
}

export function compareWorking(a: ThreadDTO, b: ThreadDTO): number {
  const sa = ts(a.runStartedAt) || Number.MAX_SAFE_INTEGER;
  const sb = ts(b.runStartedAt) || Number.MAX_SAFE_INTEGER;
  return sa - sb || a.id.localeCompare(b.id);
}

export function compareActivityDesc(a: ThreadDTO, b: ThreadDTO): number {
  return activity(b) - activity(a) || a.id.localeCompare(b.id);
}

export type Sections = Record<SectionKey, ThreadDTO[]>;

export function sectionOf(t: ThreadDTO): SectionKey | null {
  switch (t.attention?.section) {
    case "needs":
    case "working":
    case "seen":
    case "snoozed":
    case "settled":
      return t.attention.section;
    default:
      return null; // archived or unknown: hidden
  }
}

export function sections(threads: Record<string, ThreadDTO>): Sections {
  const out: Sections = { needs: [], working: [], seen: [], snoozed: [], settled: [] };
  for (const t of Object.values(threads)) {
    const k = sectionOf(t);
    if (k) out[k].push(t);
  }
  out.needs.sort(compareNeeds);
  out.working.sort(compareWorking);
  out.seen.sort(compareActivityDesc);
  out.snoozed.sort(compareActivityDesc);
  out.settled.sort(compareActivityDesc);
  return out;
}

/** Flat order used by j/k, in the fixed section order. */
export function flatOrder(sec: Sections, limits?: Partial<Record<SectionKey, number>>): string[] {
  const ids: string[] = [];
  for (const { key } of SECTIONS) {
    const list = sec[key];
    const n = limits?.[key] ?? list.length;
    for (let i = 0; i < Math.min(n, list.length); i++) ids.push(list[i].id);
  }
  return ids;
}

export function isAvailable(t: ThreadDTO | undefined, action: string): boolean {
  return !!t && !(t.unavailable && action in t.unavailable);
}

export function unavailableReason(t: ThreadDTO | undefined, action: string): string | undefined {
  if (!t) return "No thread";
  return t.unavailable?.[action];
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
  if (action === "thread.snooze") return "snooze";
  return null;
}

export function recordUndo(kind: UndoKind, threadIds: string[], now: number, label?: string): UndoEntry {
  const n = threadIds.length;
  const verb = kind === "settle" ? "Settled" : kind === "archive" ? "Archived" : "Snoozed";
  return { kind, threadIds, at: now, label: label ?? (n === 1 ? verb : `${verb} ${n} threads`) };
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

// ---- snooze presets (local time) ----

export interface SnoozePreset {
  label: string;
  until: Date;
}

export function snoozePresets(now: Date): SnoozePreset[] {
  const in3 = new Date(now.getTime() + 3 * 3600_000);
  const tomorrow = new Date(now);
  tomorrow.setDate(now.getDate() + 1);
  tomorrow.setHours(6, 0, 0, 0);
  const monday = new Date(now);
  const add = ((8 - now.getDay()) % 7) || 7; // next Monday, a week ahead when today is Monday
  monday.setDate(now.getDate() + add);
  monday.setHours(6, 0, 0, 0);
  return [
    { label: "3 hours", until: in3 },
    { label: "Tomorrow 06:00", until: tomorrow },
    { label: "Monday 06:00", until: monday },
  ];
}
