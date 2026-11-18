// Decision 0031 slice 2a: the front's state (/api/front/state: the hosts) and each host's own state
// (/api/h/<id>/state) become the one state the store reads today. A thread's host is the host whose state or frame it
// came in: it is stamped here, by name for what is shown (`host`) and by install id for what is sent (`hostId`). The
// store keeps one threads map until slice 2b, so what is stamped is all that tells a record's host. Pure.
import type { HostInfo } from "./hosts.ts";
import { decodeHosts } from "./hosts.ts";
import type { DeployProgress } from "./deploy.ts";
import type { HostLoad, Slots } from "./load.ts";
import type { FyrEvent, Profile, Project, StateDTO, ThreadDTO } from "./types.ts";

/** /api/front/state: what belongs to no host. */
export interface FrontState {
  now: string;
  user: string;
  buildId?: string;
  front: { id: string; name: string };
  hosts: HostInfo[];
  projectOrder?: string[];
  hostMarks?: StateDTO["hostMarks"];
}

export function decodeFront(raw: unknown): FrontState {
  const r = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>;
  const f = (r.front && typeof r.front === "object" ? r.front : {}) as Record<string, unknown>;
  return {
    now: typeof r.now === "string" ? r.now : "",
    user: typeof r.user === "string" ? r.user : "",
    ...(typeof r.buildId === "string" ? { buildId: r.buildId } : {}),
    front: { id: typeof f.id === "string" ? f.id : "", name: typeof f.name === "string" ? f.name : "" },
    hosts: decodeHosts(r.hosts),
    ...(Array.isArray(r.projectOrder) ? { projectOrder: r.projectOrder.filter((x): x is string => typeof x === "string") } : {}),
    ...(r.hostMarks && typeof r.hostMarks === "object" ? { hostMarks: r.hostMarks as StateDTO["hostMarks"] } : {}),
  };
}

/** The hosts that can be addressed: a host without an install id (a Fyr older than V0.124) is listed and has nothing. */
export function addressable(hosts: HostInfo[]): HostInfo[] {
  return hosts.filter((h) => !!h.id);
}

// ---- refs (decision 0031 2.5) ----
// A record is named by the pair (host id, record id), written `<hostId>/<recordId>`. The client's own id of a thread or a
// project is its ref: nothing the client keys (the store, the cache, a draft, the selection, an open thread) can then
// mix two hosts' records, whatever ids the hosts mint. The host's own id stays in `rid`, and every call to a host sends
// that: `bare` takes the ref's host off again, and `unrefDeep` does it in a command's arguments. An id inside a record
// that points at another record (a thread's project, its parent, a change's threads) is the same host's: it becomes a ref too.

/** The ref of a record: `<hostId>/<id>`; a host with no install id has none, and its id stays as it is. */
export function ref(host: string, id: string): string {
  return host && id ? `${host}/${id}` : id;
}

/** The host's own id of a ref of that host; any other string is returned as it is. */
export function bare(host: string, id: string): string {
  return host && id.startsWith(`${host}/`) ? id.slice(host.length + 1) : id;
}

/** `value` with every string that is a ref of `host` made bare: a command's arguments name records by the host's own ids. */
export function unrefDeep<T>(host: string, value: T): T {
  if (typeof value === "string") return bare(host, value) as T;
  if (Array.isArray(value)) return value.map((x) => unrefDeep(host, x)) as T;
  if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, unrefDeep(host, v)])) as T;
  return value;
}

/** A thread as the client keeps it: its ref as its id (the host's in `rid`), its host's name and id, and the time the
 *  host went away while it is unreachable. */
export function stampThread(t: ThreadDTO, h: HostInfo): ThreadDTO {
  const { hostOffline: _, ...rest } = t;
  const host = h.id ?? "";
  return {
    ...rest,
    id: ref(host, t.id),
    rid: t.id,
    host: h.name,
    hostId: host,
    ...(t.projectId ? { projectId: ref(host, t.projectId) } : {}),
    ...(t.parent ? { parent: ref(host, t.parent) } : {}),
    ...(t.changes ? { changes: t.changes.map((c) => (c.threads ? { ...c, threads: c.threads.map((x) => ref(host, x)) } : c)) } : {}),
    ...(!h.online ? { hostOffline: h.since ?? new Date(0).toISOString() } : {}),
  };
}

export function stampProject(p: Project, h: HostInfo): Project {
  const host = h.id ?? "";
  return { ...p, ...(p.id ? { id: ref(host, p.id), rid: p.id } : {}), host: h.name, hostId: host };
}

/** A timeline event of a host's thread: its subject is the thread's ref. */
export function stampEvent(e: FyrEvent, host: string): FyrEvent {
  return { ...e, subject: ref(host, e.subject) };
}

/** A thread patch: the thread's ref, and the ids inside the patch as refs. */
export function stampPatch(p: { id: string; rev: number; base: number; patch: Record<string, unknown> }, host: string) {
  const patch = { ...p.patch };
  for (const k of ["projectId", "parent"]) if (typeof patch[k] === "string" && patch[k]) patch[k] = ref(host, patch[k] as string);
  return { ...p, id: ref(host, p.id), patch };
}

/** The ids a load or deploy names (a thread of that host; "self-deploy" is not one) as refs. */
export function stampSlots(x: Slots, host: string): Slots {
  const r = (id: string) => (id === "self-deploy" ? id : ref(host, id));
  return {
    ...x,
    held: x.held.map((h) => ({ ...h, owner: r(h.owner) })),
    waiting: x.waiting.map((h) => ({ ...h, owner: r(h.owner) })),
    ...(x.undeclared ? { undeclared: x.undeclared.map((u) => ({ ...u, thread: r(u.thread) })) } : {}),
  };
}

export function stampLoad(l: HostLoad, host: string): HostLoad {
  const r = (id: string) => (id === "self-deploy" ? id : ref(host, id));
  const slots = (x: Slots) => stampSlots(x, host);
  const heavy = (l as { heavy?: Slots }).heavy;
  return {
    ...l,
    threads: (l.threads ?? []).map((t) => ({ ...t, id: r(t.id) })),
    ...(l.slots ? { slots: slots(l.slots) } : {}),
    ...(heavy ? ({ heavy: slots(heavy) } as object) : {}),
    ...(l.leftovers ? { leftovers: l.leftovers.map((x) => ({ ...x, thread: r(x.thread) })) } : {}),
    ...(l.temps ? { temps: l.temps.map((x) => (x.thread ? { ...x, thread: r(x.thread) } : x)) } : {}),
  };
}

export function stampDeploy(d: DeployProgress, host: string): DeployProgress {
  return d.blocking ? { ...d, blocking: d.blocking.map((x) => ref(host, x)) } : d;
}

/** One host's state with the host it came from. */
export interface HostResult {
  host: HostInfo;
  state: StateDTO;
}

/** The front's state and every host's, in the shape the store reads: records stamped, actions the first host's,
 *  profiles merged by id with the first host's winning, the deploy of the host the front runs in (the banner, until
 *  slice 4), each host's stall named by its host. */
export function flatten(front: FrontState, results: HostResult[]): StateDTO {
  const threads: ThreadDTO[] = [];
  const projects: Project[] = [];
  const profiles: Profile[] = [];
  const seen = new Set<string>();
  const stalls: unknown[] = [];
  let actions: StateDTO["actions"] = [];
  let deploy: StateDTO["deploy"] = null;
  for (const { host, state } of results) {
    for (const t of state.threads ?? []) threads.push(stampThread(t, host));
    for (const p of state.projects ?? []) projects.push(stampProject(p, host));
    for (const p of state.profiles ?? []) if (!seen.has(p.id)) (seen.add(p.id), profiles.push(p));
    if (!actions.length && state.actions?.length) actions = state.actions;
    if (host.id === front.front.id && state.deploy) deploy = stampDeploy(state.deploy, host.id ?? "");
    if (state.stall && typeof state.stall === "object") stalls.push({ ...(state.stall as object), host: host.name });
  }
  const self = front.hosts.find((h) => h.id === front.front.id);
  return {
    now: front.now,
    user: front.user,
    ...(front.buildId ? { buildId: front.buildId } : {}),
    host: front.front.name,
    hostId: front.front.id,
    ...(self?.machine ? { machine: self.machine } : {}),
    ...(front.hostMarks ? { hostMarks: front.hostMarks } : {}),
    hosts: front.hosts,
    threads,
    projects,
    actions,
    profiles,
    deploy,
    ...(stalls.length ? { stalls } : {}),
    ...(front.projectOrder ? { projectOrder: front.projectOrder } : {}),
  };
}

/** A frame of a host's stream names the host by install id: the host's row, or undefined for one not listed. */
export function hostOfFrame(hosts: HostInfo[], frame: { host?: unknown }): HostInfo | undefined {
  return typeof frame.host === "string" ? hosts.find((h) => h.id === frame.host) : undefined;
}

/** Whether the hosts that are reachable changed: then every host's state is read again (a thread's offline stamp and
 *  the records of a host that came back change with it). */
export function reachabilityChanged(before: HostInfo[], after: HostInfo[]): boolean {
  const key = (hs: HostInfo[]) => hs.map((h) => `${h.id ?? ""}:${h.online ? 1 : 0}`).sort().join(",");
  return key(before) !== key(after);
}
