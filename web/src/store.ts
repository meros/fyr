// Pure state store: reducer functions over the API DTOs. No I/O, no clock reads (time is passed in).
import { needsGroupRank } from "./rows.ts";
import { linkProjects, orderProjects } from "./projects.ts";
import type { QueueEntry } from "./slot-queue.ts";
import type { HostInfo } from "./hosts.ts";
import type { OwnerMarks } from "./host-mark.ts";
import type { DeployProgress } from "./deploy.ts";
import { decodeStall, type StallInfo } from "./stall.ts";
import { decodeProject, decodeState, decodeThread } from "./decode.ts";
import { asHostId, type HostId } from "./hostid.ts";
import type { Snapshot } from "./hostfeed.ts";

import type { ActionInfo, AttentionState, FyrEvent, Profile, Project, StateDTO, Thread, ThreadDTO } from "./types.ts";

export type SectionKey = "needs" | "working" | "slots" | "unread" | "seen" | "snoozed" | "settled";

/** V0.your-move: the labels say whose move it is; the keys, the g keys and V0.199's order stay.
 *  `g`: the section's key after g on the desktop (g1–g6). V0.137: Waiting for a slot has none, so the others keep their
 *  keys; it shows only while a host's queue is not empty (`transient`). V0.189: Unread is new and takes g6, so g3 stays
 *  Seen, g4 Snoozed and g5 Settled although it sits above them. V0.199: what is for the owner to read comes before what
 *  is still running; the g keys follow the section, not its place. */
export const SECTIONS: { key: SectionKey; label: string; short: string; state: AttentionState; g?: number; transient?: boolean }[] = [
  { key: "needs", label: "Your move", short: "Yours", state: "asking", g: 1 },
  { key: "unread", label: "Finished, unread", short: "Unread", state: "done", g: 6 },
  { key: "seen", label: "Finished, seen", short: "Seen", state: "seen", g: 3 },
  { key: "working", label: "Moving without you", short: "Moving", state: "working", g: 2 },
  { key: "slots", label: "Waiting to run", short: "Waiting", state: "waiting", transient: true },
  { key: "snoozed", label: "Snoozed", short: "Snoozed", state: "snoozed", g: 4 },
  { key: "settled", label: "Settled", short: "Settled", state: "settled", g: 5 },
];

export type Conn = "connecting" | "live" | "offline";

/** Decision 0031 2.4, 2.6: the connection is the browser's link to Fyr, the same words through every front: the rail's
 *  foot and the phone's header (short) and their tooltips. Whether a host is reachable is its own line. */
export function connLabel(c: Conn): string {
  return c === "live" ? "Connected" : c === "offline" ? "Offline" : "Connecting…";
}
export function connWords(c: Conn): string {
  return c === "live" ? "Connected to Fyr" : c === "offline" ? "Fyr is not reachable, retrying" : "Connecting…";
}

/** Decision 0031 2.5 (slice 2b, V0.client-per-host): what the client keeps of one host, as that host sent it. Nothing here is another host's,
 *  and nothing is merged: a thread's actions and profiles are read from its own host's. */
interface HostState {
  /** the host's threads by ref */
  threads: Record<string, Thread>;
  /** the host's project records, each stamped with the host (a linked project is one record per host, decision 0034) */
  projects: Project[];
  profiles: Profile[];
  actions: ActionInfo[];
  /** V0.39: an update of Fyr on this host */
  deploy: DeployProgress | null;
  /** V0.147: the host's newest stall */
  stall: StallInfo | null;
}

export function emptyHost(): HostState {
  return { threads: {}, projects: [], profiles: [], actions: [], deploy: null, stall: null };
}

export interface Store {
  loaded: boolean;
  conn: Conn;
  /** server clock minus client clock, ms */
  clockOffset: number;
  user: string;
  /** Decision 0031 2.6: the front this browser is connected through. Read for Settings → This device and the update
   *  banner (`bannerDeploy`) only; no host is the front's, and nothing falls back to it. */
  front: { id: string; name: string };
  /** V0.171: the owner's host marks by install id */
  hostMarks: OwnerMarks;
  /** V0.51: the front's hosts, in the owner's order */
  hostList: HostInfo[];
  /** Slice 2b: each host's own state, by install id. The lists the screens draw are selectors over it (`viewOf`). */
  hosts: Record<HostId, HostState>;
  /** V0.121: the owner's project order; null until set (by name then) */
  projectOrder: string[] | null;
  /** timeline events per thread ref, oldest first; only for the threads opened last (`openTimeline`), in the order
   *  they were opened */
  events: Record<string, FyrEvent[]>;
  /** The last seq of each loaded timeline when the stream left live: a refetch starts there, not after the
   * events the new stream already appended (review 2026-11-15). Cleared per thread once refetched. */
  resumeFrom: Record<string, number>;
}

/** What the screens read: the store with the lists over every host (decision 0031 2.5: the inbox, the board, the
 *  project list and the filters are selectors over the hosts). `threads` is keyed by ref, so two hosts' records never
 *  meet; `projects` is in the owner's order, a linked project once (decision 0034); `stalls` one per host. */
export interface View extends Store {
  threads: Record<string, Thread>;
  projects: Project[];
  stalls: StallInfo[];
}

export function initialStore(): Store {
  return {
    loaded: false,
    conn: "connecting",
    clockOffset: 0,
    user: "",
    front: { id: "", name: "" },
    hostMarks: {},
    hostList: [],
    hosts: {},
    projectOrder: null,
    events: {},
    resumeFrom: {},
  };
}

// ---- selectors over the hosts ----

/** The hosts' states in the owner's order (a host the list no longer names last, so its records still show until the
 *  next state drops them). */
function hostStates(s: Pick<Store, "hosts" | "hostList">): [HostId, HostState][] {
  const out: [HostId, HostState][] = [];
  const seen = new Set<string>();
  for (const h of s.hostList) {
    const st = h.id ? s.hosts[h.id] : undefined;
    if (h.id && st && !seen.has(h.id)) (seen.add(h.id), out.push([h.id, st]));
  }
  for (const [id, st] of Object.entries(s.hosts) as [HostId, HostState][]) if (!seen.has(id)) out.push([id, st]);
  return out;
}

/** Every host's threads by ref. */
export function allThreads(s: Pick<Store, "hosts" | "hostList">): Record<string, Thread> {
  const out: Record<string, Thread> = {};
  for (const [, st] of hostStates(s)) Object.assign(out, st.threads);
  return out;
}

/** Every host's project records, in host order. */
export function allRecords(s: Pick<Store, "hosts" | "hostList">): Project[] {
  return hostStates(s).flatMap(([, st]) => st.projects);
}

let lastView: { s: Store; v: View } | null = null;
let lastThreads: { hosts: Store["hosts"]; hostList: HostInfo[]; threads: Record<string, Thread> } | null = null;

/** The store as the screens read it. The lists are rebuilt only when the hosts' states, the host list or the order change. */
export function viewOf(s: Store): View {
  if (lastView?.s === s) return lastView.v;
  const prev = lastView?.v;
  const threads = lastThreads && lastThreads.hosts === s.hosts && lastThreads.hostList === s.hostList ? lastThreads.threads : allThreads(s);
  lastThreads = { hosts: s.hosts, hostList: s.hostList, threads };
  const sameLists = !!prev && prev.hosts === s.hosts && prev.hostList === s.hostList && prev.projectOrder === s.projectOrder;
  const projects = sameLists ? prev!.projects : linkProjects(orderProjects(allRecords(s), s.projectOrder), s.hostList);
  const stalls = sameLists ? prev!.stalls : hostStates(s).flatMap(([, st]) => (st.stall ? [st.stall] : []));
  const v: View = { ...s, threads, projects, stalls };
  lastView = { s, v };
  return v;
}

/** The profiles of one host; none for a host the store does not have. */
export function profilesOn(s: Pick<Store, "hosts">, host: HostId | undefined): Profile[] {
  return (host && s.hosts[host]?.profiles) || [];
}

const byHostMemo = new WeakMap<object, Record<string, Profile[]>>();

/** Every host's profiles by install id (hosts.ts `HostProfiles`): a thread's provider is looked up on its own host. */
export function profilesByHost(s: Pick<Store, "hosts">): Record<string, Profile[]> {
  const hit = byHostMemo.get(s.hosts);
  if (hit) return hit;
  const out: Record<string, Profile[]> = {};
  for (const [id, st] of Object.entries(s.hosts) as [HostId, HostState][]) out[id] = st.profiles;
  byHostMemo.set(s.hosts, out);
  return out;
}

/** Decision 0031 2.6 (2): the update banner is the update of the host the front runs in: that host's own deploy. */
export function bannerDeploy(s: Pick<Store, "hosts" | "front">): DeployProgress | null {
  return s.hosts[s.front.id as HostId]?.deploy ?? null;
}

/** The host whose state has the thread, or undefined: a thread the store does not have has no host to ask. */
export function hostOfThread(s: Pick<Store, "hosts">, id: string): HostId | undefined {
  for (const [h, st] of Object.entries(s.hosts) as [HostId, HostState][]) if (st.threads[id]) return h;
  return undefined;
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

/** One host's state from its snapshot, over what the store had of it: an upsert that arrived on the stream after
 *  subscribing but before the snapshot may be newer, and is kept. Threads missing from the snapshot are dropped (stale
 *  from an earlier connection); a project frame may have crossed the snapshot in flight (#42). */
function hostFromSnapshot(prev: HostState | undefined, raw: StateDTO, info: HostInfo, host: HostId): HostState {
  const dto = decodeState(raw, host);
  const threads: Record<string, Thread> = {};
  for (const t of dto.threads) threads[t.id] = t;
  for (const id of Object.keys(threads)) {
    const cur = prev?.threads[id];
    if (cur && cur !== threads[id] && !newer(cur, threads[id])) threads[id] = cur;
  }
  const projects = (dto.projects ?? []).map((p) => {
    const q: Project = { ...p, hostId: host };
    const cur = q.id ? prev?.projects.find((x) => x.id === q.id) : undefined;
    return cur && !newerProject(cur, q) ? cur : q;
  });
  const profiles = dto.profiles?.length ? dto.profiles : (info.profiles ?? []);
  return { threads, projects, profiles, actions: dto.actions ?? [], deploy: dto.deploy ?? null, stall: decodeStall(raw.stall, info.name) };
}

export function applySnapshot(s: Store, snap: Snapshot, clientNow: number): Store {
  const hosts: Record<HostId, HostState> = {};
  for (const h of snap.hosts) {
    if (!h.id) continue; // a host older than V0.124 cannot be addressed: it is listed and has nothing
    const raw = snap.states[h.id];
    hosts[h.id] = raw ? hostFromSnapshot(s.hosts[h.id], raw, h, h.id) : (s.hosts[h.id] ?? emptyHost());
  }
  return {
    ...s,
    loaded: true,
    clockOffset: ts(snap.now) ? ts(snap.now) - clientNow : s.clockOffset,
    user: snap.user ?? "",
    front: { id: snap.front?.id ?? "", name: snap.front?.name ?? "" },
    hostMarks: decodeHostMarks(snap.hostMarks),
    hostList: snap.hosts ?? [],
    hosts,
    projectOrder: snap.projectOrder ?? null,
  };
}

/** The state as the device stores it (V0.38): the snapshot that applySnapshot reads back on an offline start. */
export function stateForCache(s: Store, now: string): Snapshot {
  const states: Record<string, StateDTO> = {};
  for (const [id, st] of Object.entries(s.hosts) as [HostId, HostState][]) {
    states[id] = { now, user: s.user, threads: Object.values(st.threads), projects: st.projects, profiles: st.profiles, actions: st.actions, deploy: st.deploy, ...(st.stall ? { stall: st.stall } : {}) };
  }
  return { now, user: s.user, front: s.front, hosts: s.hostList, ...(Object.keys(s.hostMarks).length ? { hostMarks: s.hostMarks } : {}), ...(s.projectOrder ? { projectOrder: s.projectOrder } : {}), states };
}

/** One host's state changed. */
function withHost(s: Store, host: HostId, f: (h: HostState) => HostState): Store {
  const cur = s.hosts[host] ?? emptyHost();
  const next = f(cur);
  return next === cur ? s : { ...s, hosts: { ...s.hosts, [host]: next } };
}

/** A thread from the stream, into the state of the host it came in (its `hostId`, which api.ts stamps). A thread that
 *  names no host has no state to go in, and is dropped: it is not taken for some host's. */
export function upsertThread(s: Store, raw: ThreadDTO): Store {
  const host = asHostId(raw.hostId);
  if (!host) return s;
  const t = decodeThread(raw, host);
  if (!t.id) return s;
  return withHost(s, host, (h) => (newer(h.threads[t.id], t) ? { ...h, threads: { ...h.threads, [t.id]: t } } : h));
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

/** Events into a timeline, deduplicated by seq, oldest first (an event with a seq already there replaces it). The
 *  usual case, events newer than the last in order (a live event, the next page), appends without a sort; only events
 *  out of order or already there pay for the merge (V0.event-store). An array with nothing new is returned as it is. */
export function mergeSeq(cur: FyrEvent[], evs: FyrEvent[]): FyrEvent[] {
  if (!evs.length) return cur;
  let last = cur.length ? cur[cur.length - 1].seq : -Infinity;
  let inOrder = true;
  for (const e of evs) {
    if (!(e.seq > last)) {
      inOrder = false;
      break;
    }
    last = e.seq;
  }
  if (inOrder) return cur.concat(evs);
  const bySeq = new Map<number, FyrEvent>();
  for (const e of cur) bySeq.set(e.seq, e);
  for (const e of evs) bySeq.set(e.seq, e);
  return [...bySeq.values()].sort((a, b) => a.seq - b.seq);
}

/** Merge events for a thread (`mergeSeq`); `replace` starts the timeline over from `evs`. */
export function mergeEvents(s: Store, threadId: string, evs: FyrEvent[], replace = false): Store {
  const cur = s.events[threadId];
  const list = replace ? mergeSeq([], evs) : mergeSeq(cur ?? [], evs);
  if (list === cur) return s;
  return { ...s, events: { ...s.events, [threadId]: list } };
}

/** V0.event-store: how many timelines stay in memory: the open thread's and the ones opened just before it. Another
 *  thread opened again draws from the device's copy (cache.ts) and fetches what is newer. */
export const EVENTS_KEPT = 4;

/** V0.event-store: `id` is opened. Its timeline becomes the newest opened, and the timelines beyond the `keep` newest
 *  opened are dropped, with their reconnect points: a dropped timeline takes no more live events. The order is the
 *  insertion order of `events` (its keys are refs, never integer-like, so the order is kept as inserted). */
export function openTimeline(s: Store, id: string, keep = EVENTS_KEPT): Store {
  const others = Object.keys(s.events).filter((k) => k !== id);
  const has = id in s.events;
  const drop = others.slice(0, Math.max(0, others.length - (keep - 1)));
  const ids = Object.keys(s.events);
  if (!drop.length && (!has || ids[ids.length - 1] === id)) return s;
  const gone = new Set(drop);
  const events: Record<string, FyrEvent[]> = {};
  for (const k of others) if (!gone.has(k)) events[k] = s.events[k];
  if (has) events[id] = s.events[id];
  let resumeFrom = s.resumeFrom;
  if (drop.some((k) => k in resumeFrom)) {
    resumeFrom = { ...resumeFrom };
    for (const k of drop) delete resumeFrom[k];
  }
  return { ...s, events, resumeFrom };
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

function activity(t: Thread): number {
  return ts(t.activityAt) || ts(t.updatedAt);
}

/** When the thread's work started: the run, else (waiting) the earliest running agent; 0 when unknown. */
function workStartedAt(t: Thread): number {
  const run = ts(t.runStartedAt);
  if (run) return run;
  const starts = t.tasks.filter((x) => x.status === "running").map((x) => ts(x.startedAt)).filter((n) => n > 0);
  return starts.length ? Math.min(...starts) : 0;
}

/** V0.stable-groups: where a thread stands in its group's order (still-list.ts `trackArrivals`). `key` is the group
 *  (Your move's group, Working or Unread); `at` is when the thread came there. */
export interface Arrival {
  key: string;
  at: number;
}

/** The thread's own time in its group: the run's start in Working, else the last activity. Used on a first sight (a
 *  reload) and where no arrival is kept (search). */
export function naturalAt(t: Thread): number {
  return (sectionOf(t) === "working" && workStartedAt(t)) || activity(t);
}

/** V0.stable-groups: oldest arrival first, so a new row goes to the bottom and nothing a thread does moves it within its
 *  group. Your move keeps its groups (V0.your-move) and orders by arrival inside each. Amends V0.26 (Working newest
 *  started first), V0.207 (Unread newest first) and the state order inside a Your move group. */
function compareArrival(arrivals: Readonly<Record<string, Arrival>>): (a: Thread, b: Thread) => number {
  const at = (t: Thread) => arrivals[t.id]?.at ?? naturalAt(t);
  return (a, b) => at(a) - at(b) || a.id.localeCompare(b.id);
}

function compareNeeds(arrivals: Readonly<Record<string, Arrival>> = {}): (a: Thread, b: Thread) => number {
  const byArrival = compareArrival(arrivals);
  return (a, b) => needsGroupRank(a) - needsGroupRank(b) || byArrival(a, b);
}

function compareActivityDesc(a: Thread, b: Thread): number {
  return activity(b) - activity(a) || a.id.localeCompare(b.id);
}

/** V0.207: Seen, the thread read last first. Reading is the owner's act, so nothing else moves a row here. */
function compareSeen(a: Thread, b: Thread): number {
  return (ts(b.view?.seenAt) || activity(b)) - (ts(a.view?.seenAt) || activity(a)) || a.id.localeCompare(b.id);
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

/** V0.162: the Seen threads whose PR has merged and that can settle (a pinned one is listed so the bulk settle counts
 *  it, and left out of the settle). `any` says whether the "Settle merged" button shows. One rule for both fronts. */
export function mergedToSettle(seen: readonly Thread[]): { ids: string[]; any: boolean } {
  const merged = seen.filter((t) => t.pr?.state === "MERGED" && (isPinned(t) || isAvailable(t, "thread.settle")));
  return { ids: merged.map((t) => t.id), any: merged.some((t) => !isPinned(t)) };
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
 *  thread is in the queue's section and not in the pinned block, so the queue reads in its real order.
 *  V0.stable-groups: `arrivals` (still-list.ts) is when each thread came into its group: Your move (inside each group),
 *  Working and Unread are oldest first by that time, so a new row goes last and later activity does not move a row. */
export function sections(threads: Record<string, Thread>, queue: readonly QueueEntry[] = [], arrivals: Readonly<Record<string, Arrival>> = {}): Sections {
  const out: Sections = { needs: [], working: [], slots: [], unread: [], seen: [], snoozed: [], settled: [], pinned: [] };
  queue = queue.filter((e) => !e.grow); // V0.admission-tidy: a thread waiting to grow its slot runs; it stays in its group
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
  const byArrival = compareArrival(arrivals);
  out.needs.sort(compareNeeds(arrivals));
  out.working.sort(byArrival);
  out.unread.sort(byArrival);
  out.seen.sort(compareSeen);
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

/** Search: every word of the query (any case, any order; V0.ai-search) in the title, the project or `#<PR number>`. */
export function matchesQuery(t: Thread, q: string): boolean {
  const text = `${t.title} ${t.project} ${t.pr ? "#" + t.pr.number : ""}`.toLowerCase();
  return q.toLowerCase().split(/\s+/).every((w) => text.includes(w));
}

export function isAvailable(t: Thread | undefined, action: string): boolean {
  return !!t && !(action in t.unavailable);
}

export function unavailableReason(t: Thread | undefined, action: string): string | undefined {
  if (!t) return "No thread";
  return t.unavailable[action];
}

// ---- undo of deferred actions (settle, archive, snooze) ----

/** The desktop's Undo toast. Longer than the phone's PHONE_UNDO_MS (mobile.ts): a pointer has farther to travel. */
export const UNDO_MS = 6000;

type UndoKind = "settle" | "archive" | "snooze";

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
  if (action === "thread.settle" || action === "thread.settleWhenDone") return "settle";
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
export function bulkSettle(threads: Thread[], sweep = false): { ids: string[]; kept: number; unread: number; running: number } {
  const ids: string[] = [];
  let kept = 0;
  let unread = 0;
  let running = 0; // V0.203: only a thread's own Settle stops its run
  for (const t of threads) {
    if (isPinned(t)) kept++;
    else if (t.running) running++;
    else if (sweep && sectionOf(t) === "unread") unread++;
    else if (isAvailable(t, "thread.settle")) ids.push(t.id);
  }
  return { ids, kept, unread, running };
}

/** V0.189: the phone's Select all is a sweep: it picks every row but the unread ones (a pinned one stays picked, the
 *  settle counts it as pinned), so "Settle N" counts what it will settle and a row tapped afterwards is the owner's pick.
 *  V0.203: a running row is left out too, a bulk settle does not take it. */
export function selectAllIds(threads: Thread[]): string[] {
  return threads.filter((t) => isPinned(t) || (!t.running && sectionOf(t) !== "unread")).map((t) => t.id);
}

/** V0.189: the unread threads a Select all left out and the owner has not picked since, for the toast. */
export function sweptUnread(threads: Thread[], picked: ReadonlySet<string>): number {
  return threads.filter((t) => !isPinned(t) && sectionOf(t) === "unread" && !picked.has(t.id)).length;
}

/** The toast after a bulk settle: "Settled 7 threads; 1 pinned thread kept". */
export function bulkSettleText(settled: number, kept: number, unread = 0, running = 0): string {
  const head = settled === 0 ? "Nothing settled" : settled === 1 ? "Settled" : `Settled ${settled} threads`;
  const left = [kept && `${kept} pinned`, unread && `${unread} unread`].filter(Boolean).join(" and ");
  const parts = [head, left && `${left} ${kept + unread === 1 ? "thread" : "threads"} kept`, running && `${running} still running`];
  return parts.filter(Boolean).join("; ");
}

export function undoLive(u: UndoEntry | null, now: number): boolean {
  return !!u && now - u.at < UNDO_MS;
}

interface PlannedCommand {
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

/** A project from the stream (`event: project`, V0.23), into its host's records: replace by id unless older (#42), or
 *  add; `{id, deleted: true}` removes it (V0.58). A project that names no host is dropped. */
export function upsertProject(s: Store, raw: Project): Store {
  const host = asHostId(raw.hostId);
  if (!host) return s;
  return withHost(s, host, (h) => {
    const records = h.projects;
    if (raw.deleted) return raw.id && records.some((x) => x.id === raw.id) ? { ...h, projects: records.filter((x) => x.id !== raw.id) } : h;
    const p = { ...decodeProject(raw), hostId: host };
    const i = records.findIndex((x) => (p.id ? x.id === p.id : x.name === p.name));
    if (i >= 0 && !newerProject(records[i], p)) return h;
    return { ...h, projects: i >= 0 ? records.map((x, j) => (j === i ? p : x)) : [...records, p] };
  });
}

/** V0.121: the owner's project order changed (the front's `projectOrder` event, or this device's own move). */
export function setProjectOrder(s: Store, ids: string[]): Store {
  return { ...s, projectOrder: ids };
}

/** The front's list of hosts changed: a linked project shows its first online member (decision 0034, `viewOf`). */
export function setHosts(s: Store, hosts: HostInfo[]): Store {
  return { ...s, hostList: hosts };
}

/** V0.50: a host's profiles read again (after a provider changed). */
export function setProfiles(s: Store, host: HostId, profiles: Profile[]): Store {
  return withHost(s, host, (h) => ({ ...h, profiles }));
}

/** RFC 7396 JSON Merge Patch: objects merge key by key, null deletes, anything else replaces. */
function mergePatch(target: unknown, patch: unknown): unknown {
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
  /** Slice 2b: the host whose stream sent it (api.ts stamps it) */
  host: HostId;
  rev: number;
  base: number;
  patch: Record<string, unknown>;
}

/** SPEC 0.5 V0.38: apply a thread patch only on the copy it was made against; else ask for the whole thread. */
export function applyThreadPatch(s: Store, p: ThreadPatch): { store: Store; needFull: boolean } {
  const cur = s.hosts[p.host]?.threads[p.id];
  if (!cur || cur.rev !== p.base) return { store: s, needFull: true };
  const next = decodeThread(mergePatch(cur, p.patch), p.host);
  return { store: withHost(s, p.host, (h) => ({ ...h, threads: { ...h.threads, [p.id]: next } })), needFull: false };
}

/** V0.147: a stall from the stream, its host's newest. */
export function setStall(s: Store, host: HostId, raw: unknown): Store {
  const name = s.hostList.find((h) => h.id === host)?.name;
  const st = decodeStall(raw, name);
  return st ? withHost(s, host, (h) => ({ ...h, stall: st })) : s;
}

/** V0.39: a deploy progress from the stream, of the host it names (stampDeploy). */
export function setDeploy(s: Store, p: DeployProgress | null, host?: HostId): Store {
  const id = host ?? asHostId(p?.host);
  return id ? withHost(s, id, (h) => ({ ...h, deploy: p })) : s;
}

/** V0.171: the owner's host marks as the state or the "hostMarks" event sends them; anything else is left out. */
export function decodeHostMarks(raw: unknown): OwnerMarks {
  const out: OwnerMarks = {};
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return out;
  for (const [id, v] of Object.entries(raw as Record<string, unknown>)) {
    if (!v || typeof v !== "object") continue;
    const { icon, color, by, setup } = v as { icon?: unknown; color?: unknown; by?: { icon?: unknown; color?: unknown } | null; setup?: { thread?: unknown; at?: unknown; error?: unknown } | null };
    const m: OwnerMarks[string] = {};
    if (typeof icon === "string" && icon) m.icon = icon;
    if (typeof color === "string" && color) m.color = color;
    // V0.208: who set each field, and the host's last setup run.
    const who = (x: unknown) => (x === "owner" || x === "agent" ? x : undefined);
    const byIcon = who(by?.icon);
    const byColor = who(by?.color);
    if (byIcon || byColor) m.by = { ...(byIcon ? { icon: byIcon } : {}), ...(byColor ? { color: byColor } : {}) };
    if (setup && typeof setup.at === "string") m.setup = { at: setup.at, ...(typeof setup.thread === "string" ? { thread: setup.thread } : {}), ...(typeof setup.error === "string" && setup.error ? { error: setup.error } : {}) };
    if (m.icon || m.color || m.by || m.setup) out[id] = m;
  }
  return out;
}

/** V0.client-refs (decision 0031 2.5): a host's install id rotated from `was` to `now`: its state, the records in it
 *  (keyed by refs of the old id) and the timelines loaded for them move to the new id. The next state names them by it. */
export function moveHostInStore(s: Store, was: HostId, now: HostId): Store {
  const pre = `${was}/`;
  if (!was || !now || was === now) return s;
  const mv = (id: string) => (id.startsWith(pre) ? `${now}/${id.slice(pre.length)}` : id);
  const keys = <T,>(m: Record<string, T>, f: (v: T, k: string) => T = (v) => v): Record<string, T> => {
    let hit = false;
    const out: Record<string, T> = {};
    for (const [k, v] of Object.entries(m)) {
      const k2 = mv(k);
      if (k2 !== k) hit = true;
      out[k2] = k2 !== k ? f(v, k2) : v;
    }
    return hit ? out : m;
  };
  const old = s.hosts[was];
  let hosts = s.hosts;
  if (old) {
    const { [was]: _, ...rest } = s.hosts;
    const threads: Record<string, Thread> = {};
    for (const [k, t] of Object.entries(old.threads)) threads[mv(k)] = { ...t, id: mv(k), hostId: now };
    const projects = old.projects.map((p) => ({ ...p, ...(p.id ? { id: mv(p.id) } : {}), hostId: now }));
    hosts = { ...rest, [now]: { ...(s.hosts[now] ?? emptyHost()), ...old, threads: { ...s.hosts[now]?.threads, ...threads }, projects } } as Record<HostId, HostState>;
  }
  const events = keys(s.events, (evs, k) => evs.map((e) => ({ ...e, subject: k })));
  const resumeFrom = keys(s.resumeFrom);
  if (hosts === s.hosts && events === s.events && resumeFrom === s.resumeFrom) return s;
  return { ...s, hosts, events, resumeFrom };
}

/** A link names a record by the host's own id (`?open=t_12`, an old push tap, a stored selection): the ref of the one
 *  record that has it. An id that is already a ref, or that two hosts have, or that none has, comes back as it is
 *  (decision 0031 C12: a link without a host opens the one thread that has the id, and is refused when two have it). */
export function resolveRef<T extends { id?: string; rid?: string }>(records: readonly T[], id: string): string {
  if (!id || records.some((r) => r.id === id)) return id;
  const hit = records.filter((r) => r.rid === id);
  return hit.length === 1 ? (hit[0].id ?? id) : id;
}
