// The only module that talks HTTP and SSE to fyrd (docs/API-v0.md). With ?mock=1 it serves fixtures instead,
// from a chunk loaded only then (#42: the fixtures stay out of the production bundle).

import type { PushHidden } from "./hidden.ts";
import type { PushPrefs } from "./push.ts";
import { decodeHostMarks } from "./store.ts";
import type { OwnerMarks } from "./host-mark.ts";
import { isTransient, sendWithRetry } from "./retry.ts";
import type { DeployProgress } from "./deploy.ts";
import { cache, hostSince, nextHostState } from "./cache.ts";
import type { Conn, ThreadPatch } from "./store.ts";
import type { CommandReply, FsDir, FsListing, FyrEvent, Profile, Project, StateDTO, T3Candidate, ThreadDTO } from "./types.ts";
import { attachmentUrl, type UploadReply } from "./attach.ts";
import { previewUrl } from "./previews.ts";
import { decodeHosts, type HostInfo } from "./hosts.ts";
import { frontPath, hostPath } from "./urls.ts";
import { addressable, bare, decodeFront, hostOfFrame, reachabilityChanged, ref, snapshotOf, stampDeploy, stampEvent, stampLoad, stampPatch, stampProject, stampSlots, stampThread, unrefDeep, type FrontState, type HostResult, type Snapshot } from "./hostfeed.ts";
import type { HostId } from "./hostid.ts";
import { initialBuildWatch, observeBuildId } from "./buildid.ts";
import type { Quota } from "./quota.ts";
import type { HostLoad } from "./load.ts";
import type { FoundDTO, ProviderDTO } from "./providers.ts";
import type { AutoDeploySetting, AutoMergeRepo } from "./automerge.ts";
import type { UpdatesView } from "./updates.ts";
import type { SetupReport } from "./setup.ts";
import type { AuthState } from "./devices.ts";
import type { PeerState } from "./peers.ts";
import { reloadIfSignedOut } from "./signout.ts";
import type { SystemAgentsView } from "./system-agents.ts";
import type { ChangesDTO, FilePatchDTO } from "./changes.ts";
import type { PrReply } from "./pr.ts";
import { reloadForNewBuild } from "./pwa.ts";
import type { SlashCommand } from "./slash.ts";
import { parsePlan, type HandoverPlan } from "./handover.ts";

export interface ApiHandlers {
  /** Slice 2b: the front's state and each host's own, by install id */
  state(snap: Snapshot): void;
  thread(t: ThreadDTO): void;
  /** V0.39: where an update of Fyr is, on the host it names (`p.host`) */
  deploy?(p: DeployProgress): void;
  /** V0.147: a host's stall */
  stall?(host: HostId, raw: unknown): void;
  /** V0.38: a merge patch against the thread copy with rev === base */
  threadPatch?(p: ThreadPatch): void;
  /** a project made or changed (V0.23); optional for older callers */
  project?(p: Project): void;
  /** V0.121: the owner's project order changed (the front's own event) */
  projectOrder?(ids: string[]): void;
  /** V0.124: the front's hosts changed (state, id); the front's own event */
  hosts?(hs: HostInfo[]): void;
  /** V0.171: the owner's host marks changed (by install id) */
  hostMarks?(m: OwnerMarks): void;
  event(e: FyrEvent): void;
  conn(c: Conn): void;
}

export interface Api {
  readonly mock: boolean;
  start(h: ApiHandlers): void;
  /** A host's command (decision 0031: the host is named in the path, never in `args`). `host` is its install id. */
  command(host: HostId, action: string, threadId?: string, args?: Record<string, unknown>): Promise<CommandReply>;
  /** The front's own commands (front.projectOrder {ids}, front.hostOrder {ids}, front.hostId {host}, front.hostMark). */
  frontCommand(action: string, args?: Record<string, unknown>): Promise<CommandReply>;
  events(host: HostId, threadId: string, after?: number, limit?: number): Promise<FyrEvent[]>;
  /** The newest `limit` events older than `before`, to page back. */
  eventsBefore(host: HostId, threadId: string, before: number, limit?: number): Promise<FyrEvent[]>;
  /** V0.38: one thread whole, when a patch does not fit the copy we have */
  thread(host: HostId, threadId: string): Promise<ThreadDTO | null>;
  /** V0.38: one event whole (the stream and the list cut large tool output) */
  fullEvent(host: HostId, threadId: string, seq: number): Promise<FyrEvent | null>;
  /** V0.139: that host's T3 */
  t3Candidates(host: HostId): Promise<T3Candidate[]>;
  /** Web Push (SPEC V0.15): the front's VAPID public key, base64url. */
  pushKey(): Promise<string>;
  pushSubscribe(subscription: PushSubscriptionJSON, device: string): Promise<PushReply>;
  pushUnsubscribe(endpoint: string): Promise<PushReply>;
  /** V0.207: what this device hides, so the front leaves it out of those pushes */
  pushHidden(endpoint: string, hidden: PushHidden): Promise<PushReply>;
  /** V0.notification-polish: what this device wants to be told about */
  pushPrefs(endpoint: string, prefs: PushPrefs): Promise<PushReply>;
  /** to the device behind `endpoint`; to every device without one */
  pushTest(endpoint?: string): Promise<PushReply>;
  /** Whether the front still has this subscription; null when it could not be asked. */
  pushStatus(endpoint: string): Promise<boolean | null>;
  /** Tell the front which thread of which host this device shows (V0.20); null for none. */
  presence(open: { host: HostId; threadId: string } | null, device: string): Promise<void>;
  /** Attachments (SPEC V0.18): upload one file to a host (the thread's, or the one a new thread starts on); progress
   *  as bytes sent. */
  uploadAttachment(host: HostId, file: File, onProgress: (loaded: number, total: number) => void, signal?: AbortSignal): Promise<UploadResult>;
  attachmentUrl(host: HostId | undefined, id: string): string;
  /** V0.116: the owner's image for a project's icon (PNG, JPEG or GIF, at most 2 MB), sent to the project's host. */
  uploadProjectIcon(host: HostId, projectId: string, file: Blob): Promise<{ ok: boolean; error?: string }>;
  /** An HTML preview (V0.19), for a sandboxed iframe; from the thread's host (V0.54). */
  previewUrl(host: HostId | undefined, id: string): string;
  /** The Changes tab (V0.25): the branch diff of the thread's folder. */
  changes(host: HostId, threadId: string): Promise<ChangesDTO>;
  /** One file's unified diff (V0.25). */
  filePatch(host: HostId, threadId: string, path: string, since?: string): Promise<FilePatchDTO>;
  /** The PR tab (V0.43): the thread's PR as the host reads it; fresh skips the host's 30 s cache. */
  prDetail(host: HostId, threadId: string, fresh?: boolean): Promise<PrReply>;
  /** V0.ai-search: the host's threads among `ids` (its own ids) that fit the owner's words, best first. Throws with the reason. */
  searchThreads(host: HostId, query: string, ids: string[], signal?: AbortSignal): Promise<SearchHit[]>;
  /** The host folder picker (V0.42); every path stays inside the owner's home folder. */
  fsList(host: HostId, path: string): Promise<FsListing>;
  fsRecent(host: HostId): Promise<FsDir[]>;
  fsRepos(host: HostId): Promise<FsDir[]>;
  /** Makes parent/name and returns its path. */
  fsMkdir(host: HostId, parent: string, name: string): Promise<string>;
  /** V0.44: each profile's provider quota over the hosts that are online, one read per host; [] when none can say. */
  quota(): Promise<Quota[]>;
  /** V0.80: each host's load and its threads' CPU, one read per host; [] when no host can say. */
  load(): Promise<HostLoad[]>;
  // V0.127: the per-host settings reads. An offline host answers what the front last read from it, with when (readAt).
  /** V0.50: Settings → Providers; never a key, only whether one is set. */
  providers(host: HostId): Promise<HostRead<ProviderDTO[]>>;
  /** V0.50: logins, endpoints and keys found on the host. */
  providersFound(host: HostId): Promise<HostRead<FoundDTO[]>>;
  /** V0.102: the repos Fyr knows and whether each merges agents' merge asks with no press. */
  autoMerge(host: HostId): Promise<HostRead<AutoMergeRepo[]>>;
  /** V0.111: whether Fyr approves agents' deploy asks of merged code with no press. */
  autoDeploy(host: HostId): Promise<HostRead<AutoDeploySetting>>;
  /** V0.126 (#151): the update record and whether a local build pauses auto-update. */
  updates(host: HostId): Promise<HostRead<UpdatesView>>;
  /** Decision 0037: the host's setup wizard checks derived from real host state. */
  setup?(host: HostId): Promise<HostRead<SetupReport>>;
  /** V0.157: Settings → Devices: the signed-in devices, the ones that wait, the browser way in's mode. */
  authState(): Promise<AuthState>;
  /** V0.157: approve (code or id), deny, revoke, add (a one-time link and its QR code), mode (browser way in). */
  authAct(action: "approve" | "deny" | "revoke" | "add" | "mode", body: Record<string, unknown>): Promise<{ ok: boolean; error?: string } & Record<string, unknown>>;
  /** V0.159: Settings → Hosts: the paired installs, the nodes that are not, the open invite. */
  peerState(): Promise<PeerState>;
  /** V0.159: invite (open one here: a code, a QR image, a link) or join (an invite from another install). */
  peerAct(action: "invite" | "join" | "remove", body: Record<string, unknown>): Promise<{ ok: boolean; error?: string } & Record<string, unknown>>;
  /** V0.115: the system agents setting of a host */
  systemAgents(host: HostId): Promise<HostRead<SystemAgentsView>>;
  /** V0.50: a host's current profiles (after a provider changed). */
  profiles(host: HostId): Promise<Profile[]>;
  /** V0.79: the agent's commands and skills for a thread, or for a provider and folder (New thread). */
  commands(source: CommandSource): Promise<SlashCommand[]>;
  /** V0.45, V0.trans-host-handover: which rung a handover would use and the source checkout's git summary. */
  handoverPlan?(host: HostId, threadId: string, profile?: string): Promise<HandoverPlan | null>;
  /** V0.98 (3): opening the Changes board is a visit; each host keeps the owner's boardSeenAt and returns the one
   *  before (slice 2b: the board marks a card against its own host's time; until then the first host's is returned). */
  boardVisit?(): Promise<string | null>;
}

/** Whose commands: a thread's, or the newest of a provider and folder on a host (V0.79). */
export type CommandSource = { host: HostId; threadId: string } | { host: HostId; cwd: string; profile: string };

/** V0.127: a per-host settings read; readAt is set when the host is offline and this is what the front last read. */
export interface HostRead<T> {
  data: T;
  readAt?: string;
}

async function hostGet<T>(host: HostId, path: string): Promise<HostRead<T>> {
  const r = await fetch(hostPath(host, path), { headers: { Accept: "application/json" } });
  const j = (await r.json().catch(() => ({}))) as T & { error?: string };
  if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
  const at = r.headers.get("X-Fyr-Read-At");
  return at ? { data: j, readAt: at } : { data: j };
}

async function getJson<T>(path: string): Promise<T> {
  const r = await fetch(path, { headers: { Accept: "application/json" } });
  const j = (await r.json().catch(() => ({}))) as T & { error?: string };
  if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
  return j;
}

/** V0.ai-search: a thread the model picked (the host's own id) and what in it fits. */
export interface SearchHit {
  id: string;
  why?: string;
}

export type UploadResult = { ok: true; reply: UploadReply } | { ok: false; status: number | null; error?: string };

export interface PushReply {
  ok: boolean;
  /** the test: how many devices the push service took it for */
  delivered?: number;
  error?: string;
}

export function newCommandId(): string {
  const c = globalThis.crypto;
  if (c && typeof c.randomUUID === "function") return c.randomUUID();
  return `c-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

/** The mock scenario of `?mock=…`, or null for the real host. */
export function mockScenario(search: string): string | null {
  const m = /[?&]mock=([^&]*)/.exec(search);
  if (!m || m[1] === "0" || m[1] === "") return null;
  return decodeURIComponent(m[1]);
}

export async function createApi(search: string = location.search): Promise<Api> {
  const scenario = mockScenario(search);
  return scenario ? (await import("./mock-api.ts")).createMockApi(scenario) : createHttpApi();
}

function createHttpApi(): Api {
  let watch = initialBuildWatch();
  let fromNetwork = false;
  let handlers: ApiHandlers | null = null;
  let es: EventSource | null = null;
  let backoff = 500;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let generation = 0;
  /** The front's hosts as last read or sent (a `hosts` frame): who a frame's host id is, and who is reachable. */
  let front: FrontState | null = null;
  const hostsNow = () => front?.hosts ?? [];

  const checkBuild = (buildId: unknown) => {
    const r = observeBuildId(watch, buildId);
    watch = r.watch;
    if (r.reload) reloadForNewBuild();
    return r.reload;
  };

  /** The front's state, then every host's own, in parallel: the snapshot the store reads, one state per host
   *  (hostfeed.snapshotOf). A host whose state cannot be read is listed and has no records. */
  const loadAll = async (): Promise<Snapshot> => {
    const r = await fetch(frontPath("/state"), { headers: { Accept: "application/json" } });
    if (!r.ok) {
      if (r.status === 401) void reloadIfSignedOut(); // V0.157: a revoked device lands on the signed-out page
      throw new Error(`state ${r.status}`);
    }
    const f = decodeFront(await r.json());
    front = f;
    // V0.38: with a stored copy, ask each host only for the threads changed since it. A rev is its host's own clock, so
    // "since" is each host's own newest, from that host's own stored state (V0.host-ids, decision 0031 2.5); its partial
    // answer merges into that state, which the store keeps as that host's (slice 2b).
    const results = await Promise.all(
      addressable(f.hosts).map(async (host): Promise<HostResult> => {
        const id = host.id;
        const stored = await cache.hostState(id);
        const since = hostSince(stored);
        try {
          const state = nextHostState(stored, await getJson<StateDTO & { partial?: boolean }>(hostPath(id, since ? `/state?since=${since}` : "/state")));
          void cache.saveHostState(id, state);
          return { host, state };
        } catch {
          return { host, state: { now: f.now, user: f.user, threads: [], actions: [], profiles: [], projects: [] } };
        }
      }),
    );
    return snapshotOf(f, results);
  };

  const reload = async (gen: number) => {
    try {
      const dto = await loadAll();
      if (gen === generation) handlers?.state(dto);
    } catch {
      /* the next hello or frame tries again */
    }
  };

  const connect = () => {
    if (!handlers) return;
    const h = handlers;
    const gen = ++generation;
    h.conn("connecting");
    es = new EventSource(frontPath("/stream"));
    const frame = <T,>(m: Event): T | null => {
      try {
        return JSON.parse((m as MessageEvent).data) as T;
      } catch {
        return null; // ignore malformed
      }
    };
    // Subscribe first, then fetch the snapshots (so nothing is lost between them).
    es.addEventListener("hello", async (m) => {
      const hello = frame<{ now?: string; buildId?: string }>(m) ?? {};
      if (checkBuild(hello.buildId)) return;
      try {
        const dto = await loadAll();
        fromNetwork = true;
        if (gen !== generation) return;
        if (checkBuild(dto.buildId)) return;
        backoff = 500;
        h.state(dto);
        h.conn("live");
      } catch {
        if (gen === generation) retry();
      }
    });
    es.addEventListener("project", (m) => {
      const p = frame<Project & { host?: string; deleted?: boolean }>(m);
      const host = p && hostOfFrame(hostsNow(), p);
      if (p && host) h.project?.({ ...stampProject(p, host), ...(p.deleted ? { deleted: true } : {}) } as Project);
    });
    es.addEventListener("projectOrder", (m) => {
      const ids = frame<{ ids?: unknown }>(m)?.ids;
      if (Array.isArray(ids)) h.projectOrder?.(ids.filter((x): x is string => typeof x === "string"));
    });
    es.addEventListener("hosts", (m) => {
      const hs = decodeHosts(frame<{ hosts?: unknown }>(m)?.hosts);
      const changed = reachabilityChanged(hostsNow(), hs);
      if (front) front = { ...front, hosts: hs };
      h.hosts?.(hs);
      if (changed) void reload(gen); // a host went away or came back: its threads' stamps and records
    });
    es.addEventListener("hostMarks", (m) => {
      h.hostMarks?.(decodeHostMarks(frame<{ marks?: unknown }>(m)?.marks));
    });
    es.addEventListener("stall", (m) => {
      const s = frame<{ host?: string }>(m);
      const host = s && hostOfFrame(hostsNow(), s);
      if (s && host) h.stall?.(host.id, { ...s, host: host.name });
    });
    es.addEventListener("deploy", (m) => {
      // Each host's update goes to that host's state; the banner reads the one of the host the front runs in (store.ts).
      const d = frame<DeployProgress & { host?: string }>(m);
      const host = d && hostOfFrame(hostsNow(), d);
      if (d && host) h.deploy?.(stampDeploy(d, host.id));
    });
    es.addEventListener("thread.patch", (m) => {
      const p = frame<ThreadPatch & { host?: string }>(m);
      const host = p && hostOfFrame(hostsNow(), p);
      if (p && host) h.threadPatch?.(stampPatch(p, host.id));
    });
    es.addEventListener("thread", (m) => {
      const t = frame<ThreadDTO>(m);
      const host = t && hostOfFrame(hostsNow(), t);
      if (t && host) h.thread(stampThread(t, host));
    });
    es.addEventListener("event", (m) => {
      const e = frame<FyrEvent & { host?: string }>(m);
      const host = e && hostOfFrame(hostsNow(), e);
      if (e && host) h.event(stampEvent(e, host.id));
    });
    es.onerror = () => {
      if (gen === generation) {
        void reloadIfSignedOut(); // V0.157: a revoked device lands on the signed-out page
        retry();
      }
    };
  };

  const retry = () => {
    es?.close();
    es = null;
    handlers?.conn("offline");
    if (timer) clearTimeout(timer);
    const wait = backoff;
    backoff = Math.min(backoff * 2, 15000);
    timer = setTimeout(() => {
      timer = null;
      connect();
    }, wait);
  };

  const postJson = async (path: string, body: unknown): Promise<PushReply> => {
    try {
      const r = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json", "X-Fyr": "1" }, body: JSON.stringify(body) });
      let j: Partial<PushReply> = {};
      try {
        j = await r.json();
      } catch {
        /* empty body */
      }
      return r.ok ? { ok: j.ok !== false, error: j.error, delivered: j.delivered } : { ok: false, error: j.error ?? `HTTP ${r.status}` };
    } catch {
      return { ok: false, error: "Fyr is not reachable" };
    }
  };

  const post = async (path: string, action: string, threadId: string | undefined, args: Record<string, unknown> | undefined): Promise<CommandReply> => {
    // One commandId for every try: the host applies it once (I7), so a restart during a deploy is ridden out
    // (SPEC 0.5 V0.13).
    const body = { commandId: newCommandId(), action, threadId, args: args ?? {} };
    let reply: CommandReply = { ok: false, error: "Fyr is not reachable" };
    const { gaveUp } = await sendWithRetry(
      async () => {
        try {
          const r = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json", "X-Fyr": "1" }, body: JSON.stringify(body) });
          if (isTransient(r.status)) return r.status;
          try {
            reply = (await r.json()) as CommandReply;
          } catch {
            reply = { ok: false, error: `HTTP ${r.status}` };
          }
          if (!r.ok && reply.ok !== false) reply = { ok: false, error: `HTTP ${r.status}` };
          return r.status;
        } catch {
          return null;
        }
      },
      undefined,
      () => handlers?.conn("connecting"),
    );
    return gaveUp ? { ok: false, error: "Fyr is restarting or not reachable; try again in a moment" } : reply;
  };

  const eachOne = async <T,>(host: HostId, path: string): Promise<T[]> => {
    try {
      const r = await fetch(hostPath(host, path));
      const j = (await r.json().catch(() => [])) as unknown;
      return r.ok && Array.isArray(j) ? (j as T[]) : [];
    } catch {
      return [];
    }
  };

  const mutate = async (path: string, body: unknown): Promise<{ ok: boolean; error?: string } & Record<string, unknown>> => {
    try {
      const r = await fetch(path, { method: "POST", headers: { "X-Fyr": "1", "Content-Type": "application/json" }, body: JSON.stringify(body) });
      const j = (await r.json().catch(() => ({}))) as { ok?: boolean; error?: string } & Record<string, unknown>;
      return r.ok && j.ok !== false ? { ...j, ok: true } : { ...j, ok: false, error: j.error ?? `HTTP ${r.status}` };
    } catch {
      return { ok: false, error: "Fyr is not reachable" };
    }
  };

  return {
    mock: false,
    async fsList(host, path) {
      const l = await getJson<FsListing>(hostPath(host, `/fs/list?path=${encodeURIComponent(path)}`));
      return { path: l.path, parent: l.parent ?? "", dirs: l.dirs ?? [] };
    },
    async fsRecent(host) {
      return (await getJson<{ dirs?: FsDir[] }>(hostPath(host, "/fs/recent"))).dirs ?? [];
    },
    async fsRepos(host) {
      return (await getJson<{ dirs?: FsDir[] }>(hostPath(host, "/fs/repos"))).dirs ?? [];
    },
    async fsMkdir(host, parent, name) {
      const r = await fetch(hostPath(host, "/fs/mkdir"), { method: "POST", headers: { "Content-Type": "application/json", "X-Fyr": "1" }, body: JSON.stringify({ parent, name }) });
      const j = (await r.json().catch(() => ({}))) as { path?: string; error?: string };
      if (!r.ok || !j.path) throw new Error(j.error ?? `HTTP ${r.status}`);
      return j.path;
    },
    start(h) {
      handlers = h;
      // V0.38: draw the device's copy at once (offline too); the network state replaces it.
      void cache.state().then((stored) => {
        if (stored && !fromNetwork) h.state({ ...stored, now: new Date().toISOString() });
      });
      connect();
      // A page the browser keeps in its back/forward cache would keep its stream open, and a browser has six connections
      // per origin over HTTP/1.1 (decision 0031 2.4): a few pages left that way and the next one cannot connect. The
      // stream closes when the page is hidden for good and opens again if the page comes back.
      if (typeof window !== "undefined") {
        window.addEventListener("pagehide", () => {
          es?.close();
          es = null;
          if (timer) clearTimeout(timer);
          timer = null;
        });
        window.addEventListener("pageshow", (e) => {
          if ((e as PageTransitionEvent).persisted && !es) connect();
        });
      }
    },
    async command(host, action, threadId, args) {
      // The host knows its own ids: the refs of its records go out bare, and the ids in the reply come back as refs.
      const r = await post(hostPath(host, "/command"), action, threadId ? bare(host, threadId) : threadId, unrefDeep(host, args));
      if (!r.ok) return r;
      return {
        ...r,
        ...(r.threadId ? { threadId: ref(host, r.threadId) } : {}),
        ...(r.result && typeof r.result.id === "string" && action.startsWith("project.") ? { result: { ...r.result, id: ref(host, r.result.id) } } : {}),
        ...(r.result && action === "host.slotOrder" && Array.isArray((r.result as { waiting?: unknown }).waiting) ? { result: stampSlots(r.result as never, host) as never } : {}),
      };
    },
    frontCommand: (action, args) => post(frontPath("/command"), action, undefined, args),
    async events(host, threadId, after = 0, limit = 400) {
      const r = await fetch(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/events?after=${after}&limit=${limit}`));
      if (!r.ok) throw new Error(`events ${r.status}`);
      const j = (await r.json()) as { events?: FyrEvent[] };
      return (j.events ?? []).map((e) => stampEvent(e, host));
    },
    async thread(host, threadId) {
      const r = await fetch(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}`));
      if (!r.ok) return null;
      const t = (await r.json()) as ThreadDTO;
      const info = addressable(hostsNow()).find((x) => x.id === host);
      return info ? stampThread(t, info) : t;
    },
    async fullEvent(host, threadId, seq) {
      const r = await fetch(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/events/${seq}`));
      return r.ok ? stampEvent((await r.json()) as FyrEvent, host) : null;
    },
    async eventsBefore(host, threadId, before, limit = 400) {
      const r = await fetch(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/events?before=${before}&limit=${limit}`));
      if (!r.ok) throw new Error(`events ${r.status}`);
      const j = (await r.json()) as { events?: FyrEvent[] };
      return (j.events ?? []).map((e) => stampEvent(e, host));
    },
    async t3Candidates(host) {
      const r = await fetch(hostPath(host, "/t3/candidates")); // V0.139: that host's T3
      if (!r.ok) throw new Error(`candidates ${r.status}`);
      const j = (await r.json()) as { threads?: T3Candidate[] };
      return j.threads ?? [];
    },
    async pushKey() {
      const r = await fetch(frontPath("/push/key"));
      if (!r.ok) throw new Error(`The host has no push key (HTTP ${r.status})`);
      const j = (await r.json()) as { publicKey?: string };
      if (!j.publicKey) throw new Error("The host sent no push key");
      return j.publicKey;
    },
    pushSubscribe(subscription, device) {
      return postJson(frontPath("/push/subscribe"), { subscription, device });
    },
    pushUnsubscribe(endpoint) {
      return postJson(frontPath("/push/unsubscribe"), { endpoint });
    },
    pushHidden(endpoint, hidden) {
      return postJson(frontPath("/push/hidden"), { endpoint, ...hidden });
    },
    pushPrefs(endpoint, prefs) {
      return postJson(frontPath("/push/prefs"), { endpoint, ...prefs });
    },
    pushTest(endpoint) {
      return postJson(frontPath("/push/test"), endpoint ? { endpoint } : {});
    },
    uploadAttachment(host, file, onProgress, signal) {
      return new Promise<UploadResult>((resolve) => {
        const x = new XMLHttpRequest();
        x.open("POST", hostPath(host, "/attachments"));
        x.setRequestHeader("X-Fyr", "1");
        x.setRequestHeader("X-Fyr-Filename", encodeURIComponent(file.name || "file"));
        x.setRequestHeader("Content-Type", file.type || "application/octet-stream");
        x.upload.onprogress = (e) => onProgress(e.loaded, e.lengthComputable ? e.total : file.size);
        x.onload = () => {
          let j: Partial<UploadReply> & { error?: string } = {};
          try {
            j = JSON.parse(x.responseText);
          } catch {
            /* not JSON */
          }
          if (x.status >= 200 && x.status < 300 && j.id) resolve({ ok: true, reply: j as UploadReply });
          else resolve({ ok: false, status: x.status, error: j.error });
        };
        x.onerror = () => resolve({ ok: false, status: null });
        x.onabort = () => resolve({ ok: false, status: 0, error: "cancelled" });
        signal?.addEventListener("abort", () => x.abort());
        x.send(file);
      });
    },
    attachmentUrl,
    async uploadProjectIcon(host, projectId, file) {
      try {
        const r = await fetch(hostPath(host, `/projects/${encodeURIComponent(bare(host, projectId))}/icon`), { method: "POST", headers: { "X-Fyr": "1", "Content-Type": file.type || "application/octet-stream" }, body: file });
        let j: { ok?: boolean; error?: string } = {};
        try {
          j = await r.json();
        } catch {
          /* not JSON */
        }
        return r.ok && j.ok !== false ? { ok: true } : { ok: false, error: j.error ?? `HTTP ${r.status}` };
      } catch {
        return { ok: false, error: "Fyr is not reachable" };
      }
    },
    previewUrl,
    async commands(source) {
      const path =
        "threadId" in source
          ? `/threads/${encodeURIComponent(bare(source.host, source.threadId))}/commands`
          : `/commands?cwd=${encodeURIComponent(source.cwd)}&profile=${encodeURIComponent(source.profile)}`;
      const r = await fetch(hostPath(source.host, path), { headers: { Accept: "application/json" } });
      const j = (await r.json().catch(() => ({}))) as { commands?: SlashCommand[]; error?: string };
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      return Array.isArray(j.commands) ? j.commands : [];
    },
    async changes(host, threadId) {
      const r = await fetch(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/changes`));
      const j = (await r.json().catch(() => ({}))) as ChangesDTO & { error?: string };
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      return j;
    },
    async filePatch(host, threadId, path, since) {
      const r = await fetch(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/changes?path=${encodeURIComponent(path)}${since ? `&since=${encodeURIComponent(since)}` : ""}`));
      const j = (await r.json().catch(() => ({}))) as FilePatchDTO & { error?: string };
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      return j;
    },
    providers: (host) => hostGet<ProviderDTO[]>(host, "/providers"),
    providersFound: (host) => hostGet<FoundDTO[]>(host, "/providers/found"),
    autoMerge: (host) => hostGet<AutoMergeRepo[]>(host, "/autoMerge"),
    autoDeploy: (host) => hostGet<AutoDeploySetting>(host, "/autoDeploy"),
    updates: (host) => hostGet<UpdatesView>(host, "/updates"),
    setup: (host) => hostGet<SetupReport>(host, "/setup"),
    systemAgents: (host) => hostGet<SystemAgentsView>(host, "/systemAgents"),
    authState: () => getJson<AuthState>(frontPath("/auth/state")),
    // A front may send null for an empty list (a nil Go slice): the page reads lists.
    peerState: () => getJson<PeerState>(frontPath("/peer/state")).then((s) => ({ ...s, members: s.members ?? [], candidates: s.candidates ?? [] })),
    peerAct: (action, body) => mutate(frontPath(`/peer/${action}`), body),
    authAct: (action, body) => mutate(frontPath(`/auth/${action}`), body),
    profiles: (host) => eachOne<Profile>(host, "/profiles"),
    // Slice 2b: each quota names the host it was read from, so its provider's name is that host's.
    async quota() {
      const lists = await Promise.all(addressable(hostsNow()).filter((h) => h.online).map(async (h) => (await eachOne<Quota>(h.id, "/quota")).map((q) => ({ ...q, host: q.host || h.name, hostId: h.id }))));
      return lists.flat();
    },
    async load() {
      const lists = await Promise.all(
        addressable(hostsNow())
          .filter((h) => h.online)
          .map(async (h) => (await eachOne<HostLoad>(h.id, "/load")).map((l) => stampLoad(l, h.id))),
      );
      return lists.flat();
    },
    async handoverPlan(host, threadId, profile) {
      const q = profile ? `?profile=${encodeURIComponent(profile)}` : "";
      const r = await fetch(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/handover${q}`), { headers: { Accept: "application/json" } });
      if (!r.ok) return null;
      return parsePlan(await r.json());
    },
    async boardVisit() {
      // Each host keeps the owner's boardSeenAt (F13); until the store is per host the first host's is the one returned.
      const seen = await Promise.all(
        addressable(hostsNow())
          .filter((h) => h.online)
          .map(async (h) => {
            try {
              const r = await fetch(hostPath(h.id, "/board/visit"), { method: "POST", headers: { "Content-Type": "application/json", "X-Fyr": "1" }, body: "{}" });
              const j = (await r.json().catch(() => ({}))) as { seenAt?: string | null };
              return r.ok && typeof j.seenAt === "string" ? j.seenAt : null;
            } catch {
              return null;
            }
          }),
      );
      return seen.find((x) => x !== null) ?? null;
    },
    async searchThreads(host, query, ids, signal) {
      const r = await fetch(hostPath(host, "/search"), { method: "POST", headers: { "Content-Type": "application/json", "X-Fyr": "1" }, body: JSON.stringify({ query, ids }), signal });
      const j = (await r.json().catch(() => ({}))) as { hits?: SearchHit[]; error?: string };
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      return j.hits ?? [];
    },
    async prDetail(host, threadId, fresh) {
      try {
        const r = await fetch(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/pr${fresh ? "?fresh=1" : ""}`));
        const j = (await r.json().catch(() => ({}))) as PrReply;
        if (!r.ok && !("error" in j)) return { error: `HTTP ${r.status}` };
        return j;
      } catch {
        return { error: "Fyr is not reachable." };
      }
    },
    async presence(open, device) {
      try {
        const body = open ? { host: open.host, threadId: bare(open.host, open.threadId), device } : { host: "", threadId: "", device };
        await fetch(frontPath("/presence"), { method: "POST", headers: { "Content-Type": "application/json", "X-Fyr": "1" }, body: JSON.stringify(body), keepalive: true });
      } catch {
        /* best effort */
      }
    },
    async pushStatus(endpoint) {
      try {
        const r = await fetch(frontPath("/push/status"), { method: "POST", headers: { "Content-Type": "application/json", "X-Fyr": "1" }, body: JSON.stringify({ endpoint }) });
        if (!r.ok) return null;
        const j = (await r.json()) as { known?: boolean };
        return typeof j.known === "boolean" ? j.known : null;
      } catch {
        return null;
      }
    },
  };
}
