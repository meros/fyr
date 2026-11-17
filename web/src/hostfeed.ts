// Decision 0031 slice 2a: the front's state (/api/front/state: the hosts) and each host's own state
// (/api/h/<id>/state) become the one state the store reads today. A thread's host is the host whose state or frame it
// came in: it is stamped here, by name for what is shown (`host`) and by install id for what is sent (`hostId`). The
// store keeps one threads map until slice 2b, so what is stamped is all that tells a record's host. Pure.
import type { HostInfo } from "./hosts.ts";
import { decodeHosts } from "./hosts.ts";
import type { Profile, Project, StateDTO, ThreadDTO } from "./types.ts";

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

/** A thread as the client keeps it: its host's name and id, and the time the host went away while it is unreachable. */
export function stampThread(t: ThreadDTO, h: HostInfo): ThreadDTO {
  const { hostOffline: _, ...rest } = t;
  return { ...rest, host: h.name, hostId: h.id ?? "", ...(!h.online ? { hostOffline: h.since ?? new Date(0).toISOString() } : {}) };
}

export function stampProject(p: Project, h: HostInfo): Project {
  return { ...p, host: h.name, hostId: h.id ?? "" };
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
    if (host.id === front.front.id && state.deploy) deploy = state.deploy;
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
