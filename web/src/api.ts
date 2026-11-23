// The only module that talks HTTP and SSE to fyrd (docs/API-v0.md). With ?mock=1 it serves fixtures instead,
// from a chunk loaded only then (#42: the fixtures stay out of the production bundle).
//
// One contract (V0.api-contract): no method of the Api rejects, whatever the host or the network does.
// - A read whose caller shows why it failed answers `Result<T>`: `{ ok: true, data }` (with `readAt` when an offline
//   host's last copy answers) or `{ ok: false, error }`, the reason in words a screen can show.
// - A read whose caller has nothing to show for a failure answers its empty value, as documented on it: `null` for one
//   record (thread, fullEvent, pushStatus, boardVisit), `[]` for a list over the hosts (profiles, quota, load).
// - A command, a write and an upload answer `{ ok, error }` (CommandReply, PushReply, UploadResult, the mutations).
// A caller therefore needs no catch for an Api call; the mock Api (mock-api.ts) keeps the same contract.

import type { PushHidden } from "./hidden.ts";
import type { PushPrefs } from "./push.ts";
import { decodeHostMarks } from "./store.ts";
import type { OwnerMarks } from "./host-mark.ts";
import { isTransient, sendWithRetry } from "./retry.ts";
import type { DeployProgress } from "./deploy.ts";
import { cache, hostSince, nextHostState } from "./cache.ts";
import type { Conn, ThreadPatch } from "./store.ts";
import type { CommandReply, FsDir, FsListing, FyrEvent, Profile, Project, StateDTO, ThreadDTO } from "./types.ts";
import { attachmentUrl, type UploadReply } from "./attach.ts";
import { previewUrl } from "./previews.ts";
import { decodeHosts, type HostInfo } from "./hosts.ts";
import { frontPath, hostPath } from "./urls.ts";
import { addressable, bare, decodeFront, hostOfFrame, reachabilityChanged, ref, snapshotOf, stampDeploy, stampEvent, stampLoad, stampPatch, stampProject, stampSlots, stampThread, unrefDeep, type FrontState, type HostResult, type Snapshot } from "./hostfeed.ts";
import type { HostId } from "./hostid.ts";
import { initialBuildWatch, observeBuildId } from "./buildid.ts";
import type { Quota } from "./quota.ts";
import type { HostLoad } from "./host-load.ts";
import type { FoundDTO, ProviderDTO } from "./providers.ts";
import type { AutoDeploySetting, AutoMergeRepo } from "./automerge.ts";
import type { UpdatesView } from "./updates.ts";
import type { SetupReport } from "./setup.ts";
import type { AuthState } from "./devices.ts";
import type { PeerState } from "./peers.ts";
import { reloadIfSignedOut } from "./signout.ts";
import { request, replyError, upload, type Reply, type RequestOptions } from "./http.ts";
import { arr, obj, decodeThread } from "./decode.ts";
import type { SystemAgentsView } from "./system-agents.ts";
import { decodeChanges, decodeFilePatch, type ChangesDTO, type FilePatchDTO } from "./changes.ts";
import type { PrReply } from "./pr.ts";
import { reloadForNewBuild } from "./pwa.ts";
import type { SlashCommand } from "./slash.ts";
import { parsePlan, type HandoverPlan } from "./handover.ts";

interface ApiHandlers {
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
  /** the device may keep this host's events and state (a mock scenario's fixtures are not kept) */
  readonly cache: boolean;
  /** the host takes this device's push settings (hidden list, preferences) */
  readonly pushes: boolean;
  start(h: ApiHandlers): void;
  /** Close the stream and drop the page listeners `start` added (the App's effect cleanup). */
  stop(): void;
  /** A host's command (decision 0031: the host is named in the path, never in `args`). `host` is its install id. */
  command(host: HostId, action: string, threadId?: string, args?: Record<string, unknown>): Promise<CommandReply>;
  /** The front's own commands (front.projectOrder {ids}, front.hostOrder {ids}, front.hostId {host}, front.hostMark). */
  frontCommand(action: string, args?: Record<string, unknown>): Promise<CommandReply>;
  events(host: HostId, threadId: string, after?: number, limit?: number): Promise<Result<FyrEvent[]>>;
  /** The newest `limit` events older than `before`, to page back. */
  eventsBefore(host: HostId, threadId: string, before: number, limit?: number): Promise<Result<FyrEvent[]>>;
  /** V0.38: one thread whole, when a patch does not fit the copy we have. Never rejects: null when it cannot be read. */
  thread(host: HostId, threadId: string): Promise<ThreadDTO | null>;
  /** V0.38: one event whole (the stream and the list cut large tool output). Never rejects: null when it cannot be read. */
  fullEvent(host: HostId, threadId: string, seq: number): Promise<FyrEvent | null>;
  /** Web Push (SPEC V0.15): the front's VAPID public key, base64url. */
  pushKey(): Promise<Result<string>>;
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
  changes(host: HostId, threadId: string): Promise<Result<ChangesDTO>>;
  /** One file's unified diff (V0.25). */
  filePatch(host: HostId, threadId: string, path: string, since?: string): Promise<Result<FilePatchDTO>>;
  /** The PR tab (V0.43): the thread's PR as the host reads it; fresh skips the host's 30 s cache. */
  prDetail(host: HostId, threadId: string, fresh?: boolean, prUrl?: string): Promise<PrReply>;
  /** V0.ai-search: the host's threads among `ids` (its own ids) that fit the owner's words, best first. */
  searchThreads(host: HostId, query: string, ids: string[], signal?: AbortSignal): Promise<Result<SearchHit[]>>;
  /** The host folder picker (V0.42); every path stays inside the owner's home folder. */
  fsList(host: HostId, path: string): Promise<Result<FsListing>>;
  fsRecent(host: HostId): Promise<Result<FsDir[]>>;
  fsRepos(host: HostId): Promise<Result<FsDir[]>>;
  /** Makes parent/name and answers its path. */
  fsMkdir(host: HostId, parent: string, name: string): Promise<Result<string>>;
  /** V0.44: each profile's provider quota over the hosts that are online, one read per host; [] when none can say. */
  quota(): Promise<Quota[]>;
  /** V0.80: each host's load and its threads' CPU, one read per host; [] when no host can say. */
  load(): Promise<HostLoad[]>;
  // V0.127: the per-host settings reads. An offline host answers what the front last read from it, with when (readAt).
  /** V0.50: Settings → Providers; never a key, only whether one is set. */
  providers(host: HostId): Promise<Result<ProviderDTO[]>>;
  /** V0.50: logins, endpoints and keys found on the host. */
  providersFound(host: HostId): Promise<Result<FoundDTO[]>>;
  /** V0.102: the repos Fyr knows and whether each merges agents' merge asks with no press. */
  autoMerge(host: HostId): Promise<Result<AutoMergeRepo[]>>;
  /** V0.111: whether Fyr approves agents' deploy asks of merged code with no press. */
  autoDeploy(host: HostId): Promise<Result<AutoDeploySetting>>;
  /** V0.126 (#151): the update record and whether a local build pauses auto-update. */
  updates(host: HostId): Promise<Result<UpdatesView>>;
  /** Decision 0037: the host's setup wizard checks derived from real host state. */
  setup?(host: HostId): Promise<Result<SetupReport>>;
  /** V0.157: Settings → Devices: the signed-in devices, the ones that wait, the browser way in's mode. */
  authState(): Promise<Result<AuthState>>;
  /** V0.157: approve (code or id), deny, revoke, add (a one-time link and its QR code), mode (browser way in). */
  authAct(action: "approve" | "deny" | "revoke" | "add" | "mode", body: Record<string, unknown>): Promise<{ ok: boolean; error?: string } & Record<string, unknown>>;
  /** V0.159: Settings → Hosts: the paired installs, the nodes that are not, the open invite. */
  peerState(): Promise<Result<PeerState>>;
  /** V0.159: invite (open one here: a code, a QR image, a link) or join (an invite from another install). */
  peerAct(action: "invite" | "join" | "remove", body: Record<string, unknown>): Promise<{ ok: boolean; error?: string } & Record<string, unknown>>;
  /** V0.115: the system agents setting of a host */
  systemAgents(host: HostId): Promise<Result<SystemAgentsView>>;
  /** V0.50: a host's current profiles (after a provider changed). Never rejects: none when they cannot be read. */
  profiles(host: HostId): Promise<Profile[]>;
  /** V0.79: the agent's commands and skills for a thread, or for a provider and folder (New thread). */
  commands(source: CommandSource): Promise<Result<SlashCommand[]>>;
  /** V0.45, V0.trans-host-handover: which rung a handover would use and the source checkout's git summary. */
  handoverPlan?(host: HostId, threadId: string, profile?: string): Promise<Result<HandoverPlan>>;
  /** V0.98 (3): opening the Changes board is a visit; each host keeps the owner's boardSeenAt and returns the one
   *  before (slice 2b: the board marks a card against its own host's time; until then the first host's is returned). */
  boardVisit?(): Promise<string | null>;
}

/** Whose commands: a thread's, or the newest of a provider and folder on a host (V0.79). */
export type CommandSource = { host: HostId; threadId: string } | { host: HostId; cwd: string; profile: string };

/** What a read answers (the contract at the top). `readAt` (V0.127) is set when the host is offline and this is what
 *  the front last read from it. */
export type Result<T> = { ok: true; data: T; readAt?: string } | { ok: false; error: string };

/** What a call that could not reach the host answers. */
export const UNREACHABLE = "Fyr is not reachable";

/** A failed Result, for any T. */
export const failed = (error: string): { ok: false; error: string } => ({ ok: false, error });

/** A request whose reply `decode` turns into the type the screen reads: it gets whatever the host sent (an empty or
 *  unparsable body, a literal null). A refusal answers the host's error; a network failure, a timeout or the caller's
 *  cancel answers UNREACHABLE; a body the decoder refuses (it throws) answers the decoder's message. */
async function read<T>(url: string, decode: (raw: unknown) => T, o?: RequestOptions): Promise<Result<T>> {
  let reply: Reply;
  try {
    reply = await request(url, o);
  } catch {
    return failed(UNREACHABLE);
  }
  if (!reply.ok) return failed(replyError(reply));
  let data: T;
  try {
    data = decode(reply.json);
  } catch (e) {
    return failed(e instanceof Error ? e.message : String(e));
  }
  const at = reply.headers.get("X-Fyr-Read-At");
  return at ? { ok: true, data, readAt: at } : { ok: true, data };
}

/** A settings object as sent: an empty or null body is an empty object, which the page reads with its own defaults. */
const object = <T,>(raw: unknown): T => obj(raw) as T;

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
function mockScenario(search: string): string | null {
  const m = /[?&]mock=([^&]*)/.exec(search);
  if (!m || m[1] === "0" || m[1] === "") return null;
  return decodeURIComponent(m[1]);
}

export async function createApi(search: string = location.search): Promise<Api> {
  const scenario = mockScenario(search);
  return scenario ? (await import("./mock-api.ts")).createMockApi(scenario) : createHttpApi();
}

/** A command may run git or start a worktree before it answers; its tries are bounded by this, each. */
const COMMAND_TIMEOUT_MS = 60_000;
/** The AI search reads threads with a model before it answers. */
const SEARCH_TIMEOUT_MS = 120_000;

const isRecord = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);

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

  /** V0.157: the front's answer to /state, for signout.ts: its status and body. */
  const probeState = async (): Promise<{ status: number; body: unknown }> => {
    const r = await request(frontPath("/state"));
    return { status: r.status, body: r.json };
  };

  /** The front's state, then every host's own, in parallel: the snapshot the store reads, one state per host
   *  (hostfeed.snapshotOf). A host whose state cannot be read is listed and has no records. */
  const loadAll = async (): Promise<Snapshot> => {
    const r = await request(frontPath("/state"));
    if (!r.ok) {
      if (r.status === 401) void reloadIfSignedOut(probeState); // V0.157: a revoked device lands on the signed-out page
      throw new Error(`state ${r.status}`);
    }
    const f = decodeFront(r.json);
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
          const r = await read(hostPath(id, since ? `/state?since=${since}` : "/state"), object<StateDTO & { partial?: boolean }>);
          if (!r.ok) throw new Error(r.error);
          const state = nextHostState(stored, r.data);
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
        void reloadIfSignedOut(probeState); // V0.157: a revoked device lands on the signed-out page
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
      const r = await request(path, { method: "POST", body });
      const j = obj(r.json) as Partial<PushReply>;
      return r.ok ? { ok: j.ok !== false, error: j.error, delivered: j.delivered } : { ok: false, error: replyError(r) };
    } catch {
      return failed(UNREACHABLE);
    }
  };

  const closeStream = () => {
    es?.close();
    es = null;
    if (timer) clearTimeout(timer);
    timer = null;
  };
  const onPageHide = () => closeStream();
  const onPageShow = (e: Event) => {
    if (!(e as PageTransitionEvent).persisted || es || !handlers) return;
    backoff = 500; // the page was away, not failing: the wait of the failures before it does not apply
    connect();
  };

  const post = async (path: string, action: string, threadId: string | undefined, args: Record<string, unknown> | undefined): Promise<CommandReply> => {
    // One commandId for every try: the host applies it once (I7), so a restart during a deploy is ridden out
    // (SPEC 0.5 V0.13).
    const body = { commandId: newCommandId(), action, threadId, args: args ?? {} };
    let reply: CommandReply = failed(UNREACHABLE);
    const { gaveUp } = await sendWithRetry(
      async () => {
        try {
          const r = await request(path, { method: "POST", body, timeoutMs: COMMAND_TIMEOUT_MS });
          if (isTransient(r.status)) return r.status;
          reply = r.json && typeof r.json === "object" ? (r.json as CommandReply) : { ok: false, error: `HTTP ${r.status}` };
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
      const r = await request(hostPath(host, path));
      return r.ok ? arr<T>(r.json) : [];
    } catch {
      return [];
    }
  };

  /** A page of a thread's events; an answer without events is an empty page. */
  const readEvents = (host: HostId, threadId: string, query: string): Promise<Result<FyrEvent[]>> =>
    read(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/events?${query}`), (j) => arr<FyrEvent>(obj(j).events).filter(isRecord).map((e) => stampEvent(e, host)));

  const mutate = async (path: string, body: unknown): Promise<{ ok: boolean; error?: string } & Record<string, unknown>> => {
    try {
      const r = await request(path, { method: "POST", body });
      const j = obj(r.json) as { ok?: boolean; error?: string } & Record<string, unknown>;
      return r.ok && j.ok !== false ? { ...j, ok: true } : { ...j, ok: false, error: replyError(r) };
    } catch {
      return failed(UNREACHABLE);
    }
  };

  return {
    mock: false,
    cache: true,
    pushes: true,
    fsList: (host, path) =>
      read(hostPath(host, `/fs/list?path=${encodeURIComponent(path)}`), (j) => {
        const l = object<Partial<FsListing>>(j);
        return { path: l.path ?? path, parent: l.parent ?? "", dirs: arr<FsDir>(l.dirs) };
      }),
    fsRecent: (host) => read(hostPath(host, "/fs/recent"), (j) => arr<FsDir>(obj(j).dirs)),
    fsRepos: (host) => read(hostPath(host, "/fs/repos"), (j) => arr<FsDir>(obj(j).dirs)),
    fsMkdir: (host, parent, name) =>
      read(
        hostPath(host, "/fs/mkdir"),
        (j) => {
          const path = obj(j).path;
          if (typeof path !== "string" || !path) throw new Error("The host did not say which folder it made");
          return path;
        },
        { method: "POST", body: { parent, name } },
      ),
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
        window.addEventListener("pagehide", onPageHide);
        window.addEventListener("pageshow", onPageShow);
      }
    },
    stop() {
      handlers = null;
      generation++; // what a stream or a snapshot still in flight brings is stale
      closeStream();
      if (typeof window !== "undefined") {
        window.removeEventListener("pagehide", onPageHide);
        window.removeEventListener("pageshow", onPageShow);
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
    events: (host, threadId, after = 0, limit = 400) => readEvents(host, threadId, `after=${after}&limit=${limit}`),
    async thread(host, threadId) {
      try {
        const r = await request(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}`));
        if (!r.ok) return null;
        const t = decodeThread(r.json, host);
        if (!t.id) return null; // not a thread
        const info = addressable(hostsNow()).find((x) => x.id === host);
        return info ? stampThread(t, info) : t;
      } catch {
        return null;
      }
    },
    async fullEvent(host, threadId, seq) {
      try {
        const r = await request(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/events/${seq}`));
        return r.ok && isRecord(r.json) ? stampEvent(r.json as unknown as FyrEvent, host) : null;
      } catch {
        return null;
      }
    },
    eventsBefore: (host, threadId, before, limit = 400) => readEvents(host, threadId, `before=${before}&limit=${limit}`),
    pushKey: () =>
      read(frontPath("/push/key"), (j) => {
        const key = obj(j).publicKey;
        if (typeof key !== "string" || !key) throw new Error("The host sent no push key");
        return key;
      }),
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
    async uploadAttachment(host, file, onProgress, signal) {
      const headers = { "X-Fyr-Filename": encodeURIComponent(file.name || "file"), "Content-Type": file.type || "application/octet-stream" };
      const a = await upload(hostPath(host, "/attachments"), file, headers, onProgress, signal);
      const j = obj(a.json) as Partial<UploadReply> & { error?: string };
      if (a.status !== null && a.status >= 200 && a.status < 300 && j.id) return { ok: true, reply: j as UploadReply };
      if (a.status === 0) return { ok: false, status: 0, error: "cancelled" };
      return { ok: false, status: a.status, error: a.stalled ? "the host stopped answering" : typeof j.error === "string" ? j.error : undefined };
    },
    attachmentUrl,
    async uploadProjectIcon(host, projectId, file) {
      try {
        const r = await request(hostPath(host, `/projects/${encodeURIComponent(bare(host, projectId))}/icon`), { method: "POST", raw: file, headers: { "Content-Type": file.type || "application/octet-stream" } });
        return r.ok && (obj(r.json) as { ok?: boolean }).ok !== false ? { ok: true } : { ok: false, error: replyError(r) };
      } catch {
        return failed(UNREACHABLE);
      }
    },
    previewUrl,
    async commands(source) {
      const path =
        "threadId" in source
          ? `/threads/${encodeURIComponent(bare(source.host, source.threadId))}/commands`
          : `/commands?cwd=${encodeURIComponent(source.cwd)}&profile=${encodeURIComponent(source.profile)}`;
      return read(hostPath(source.host, path), (j) => arr<SlashCommand>(obj(j).commands));
    },
    changes: (host, threadId) => read(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/changes`), decodeChanges),
    filePatch: (host, threadId, path, since) => read(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/changes?path=${encodeURIComponent(path)}${since ? `&since=${encodeURIComponent(since)}` : ""}`), (j) => decodeFilePatch(j, path)),
    providers: (host) => read(hostPath(host, "/providers"), arr<ProviderDTO>),
    providersFound: (host) => read(hostPath(host, "/providers/found"), arr<FoundDTO>),
    autoMerge: (host) => read(hostPath(host, "/autoMerge"), arr<AutoMergeRepo>),
    autoDeploy: (host) => read(hostPath(host, "/autoDeploy"), object<AutoDeploySetting>),
    updates: (host) => read(hostPath(host, "/updates"), object<UpdatesView>),
    setup: (host) => read(hostPath(host, "/setup"), object<SetupReport>),
    systemAgents: (host) => read(hostPath(host, "/systemAgents"), object<SystemAgentsView>),
    authState: () => read(frontPath("/auth/state"), object<AuthState>),
    // A front may send null for an empty list (a nil Go slice): the page reads lists.
    peerState: () =>
      read(frontPath("/peer/state"), (j) => {
        const s = object<PeerState>(j);
        return { ...s, members: s.members ?? [], candidates: s.candidates ?? [] };
      }),
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
      return read(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/handover${q}`), (j) => {
        const plan = parsePlan(j);
        if (!plan) throw new Error("The host sent no handover plan");
        return plan;
      });
    },
    async boardVisit() {
      // Each host keeps the owner's boardSeenAt (F13); until the store is per host the first host's is the one returned.
      const seen = await Promise.all(
        addressable(hostsNow())
          .filter((h) => h.online)
          .map(async (h) => {
            try {
              const r = await request(hostPath(h.id, "/board/visit"), { method: "POST", body: {} });
              const seenAt = obj(r.json).seenAt;
              return r.ok && typeof seenAt === "string" ? seenAt : null;
            } catch {
              return null;
            }
          }),
      );
      return seen.find((x) => x !== null) ?? null;
    },
    searchThreads: (host, query, ids, signal) => read(hostPath(host, "/search"), (j) => arr<SearchHit>(obj(j).hits), { method: "POST", body: { query, ids }, signal, timeoutMs: SEARCH_TIMEOUT_MS }),
    async prDetail(host, threadId, fresh, prUrl) {
      try {
        const q = new URLSearchParams();
        if (fresh) q.set("fresh", "1");
        if (prUrl) q.set("pr", prUrl);
        const qs = q.toString();
        const r = await request(hostPath(host, `/threads/${encodeURIComponent(bare(host, threadId))}/pr${qs ? `?${qs}` : ""}`));
        const j = obj(r.json) as PrReply;
        if (!r.ok && !("error" in j)) return { error: `HTTP ${r.status}` };
        return j;
      } catch {
        return { error: "Fyr is not reachable." };
      }
    },
    async presence(open, device) {
      try {
        const body = open ? { host: open.host, threadId: bare(open.host, open.threadId), device } : { host: "", threadId: "", device };
        await request(frontPath("/presence"), { method: "POST", body, keepalive: true });
      } catch {
        /* best effort */
      }
    },
    async pushStatus(endpoint) {
      try {
        const r = await request(frontPath("/push/status"), { method: "POST", body: { endpoint } });
        const known = obj(r.json).known;
        return r.ok && typeof known === "boolean" ? known : null;
      } catch {
        return null;
      }
    },
  };
}
