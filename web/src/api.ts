// The only module that talks HTTP and SSE to fyrd (docs/API-v0.md). With ?mock=1 it serves fixtures instead,
// from a chunk loaded only then (#42: the fixtures stay out of the production bundle).

import { isTransient, sendWithRetry } from "./retry.ts";
import type { DeployProgress } from "./deploy.ts";
import { cache, mergeStateDelta, sinceRev } from "./cache.ts";
import type { Conn, ThreadPatch } from "./store.ts";
import type { CommandReply, FsDir, FsListing, FyrEvent, Profile, Project, StateDTO, T3Candidate, ThreadDTO } from "./types.ts";
import { attachmentUrl, hostQuery, type UploadReply } from "./attach.ts";
import { previewUrl } from "./previews.ts";
import { decodeHosts, type HostInfo } from "./hosts.ts";
import { settingsPath } from "./host-settings.ts";
import { fsQuery } from "./folders.ts";
import { initialBuildWatch, observeBuildId } from "./buildid.ts";
import type { Quota } from "./quota.ts";
import type { HostLoad } from "./load.ts";
import type { FoundDTO, ProviderDTO } from "./providers.ts";
import type { AutoDeploySetting, AutoMergeRepo } from "./automerge.ts";
import type { UpdatesView } from "./updates.ts";
import type { AuthState } from "./devices.ts";
import type { PeerState } from "./peers.ts";
import { reloadIfSignedOut } from "./signout.ts";
import type { SystemAgentsView } from "./system-agents.ts";
import type { ChangesDTO, FilePatchDTO } from "./changes.ts";
import type { PrReply } from "./pr.ts";
import { reloadForNewBuild } from "./pwa.ts";
import type { SlashCommand } from "./slash.ts";

export interface ApiHandlers {
  state(dto: StateDTO): void;
  thread(t: ThreadDTO): void;
  /** V0.39: where an update of Fyr is */
  deploy?(p: DeployProgress): void;
  /** V0.147 */
  stall?(raw: unknown): void;
  /** V0.38: a merge patch against the thread copy with rev === base */
  threadPatch?(p: ThreadPatch): void;
  /** a project made or changed (V0.23); optional for older callers */
  project?(p: Project): void;
  /** V0.121: the owner's project order changed (the front's own event) */
  projectOrder?(ids: string[]): void;
  /** V0.124: the front's hosts changed (state, id); the front's own event */
  hosts?(hs: HostInfo[]): void;
  event(e: FyrEvent): void;
  conn(c: Conn): void;
}

export interface Api {
  readonly mock: boolean;
  start(h: ApiHandlers): void;
  command(action: string, threadId?: string, args?: Record<string, unknown>): Promise<CommandReply>;
  events(threadId: string, after?: number, limit?: number): Promise<FyrEvent[]>;
  /** The newest `limit` events older than `before`, to page back. */
  eventsBefore(threadId: string, before: number, limit?: number): Promise<FyrEvent[]>;
  /** V0.38: one thread whole, when a patch does not fit the copy we have */
  thread(threadId: string): Promise<ThreadDTO | null>;
  /** V0.38: one event whole (the stream and the list cut large tool output) */
  fullEvent(threadId: string, seq: number): Promise<FyrEvent | null>;
  /** V0.139: `host` names whose T3 ("" or missing: the first host) */
  t3Candidates(host?: string): Promise<T3Candidate[]>;
  /** Web Push (SPEC V0.15): the host's VAPID public key, base64url. */
  pushKey(): Promise<string>;
  pushSubscribe(subscription: PushSubscriptionJSON, device: string): Promise<PushReply>;
  pushUnsubscribe(endpoint: string): Promise<PushReply>;
  pushTest(): Promise<PushReply>;
  /** Whether the host still has this subscription; null when it could not be asked. */
  pushStatus(endpoint: string): Promise<boolean | null>;
  /** Tell the host which thread this device shows (V0.20); "" for none. */
  presence(threadId: string, device: string): Promise<void>;
  /** Attachments (SPEC V0.18): upload one file; progress as bytes sent. */
  /** V0.54: `host` is the thread's host (or the one a new thread starts on); the front stores the file there. */
  uploadAttachment(file: File, onProgress: (loaded: number, total: number) => void, signal?: AbortSignal, host?: string): Promise<UploadResult>;
  attachmentUrl(id: string, host?: string): string;
  /** V0.116: the owner's image for a project's icon (PNG, JPEG or GIF, at most 2 MB); the front sends it to the
   *  project's host. */
  uploadProjectIcon(projectId: string, file: Blob): Promise<{ ok: boolean; error?: string }>;
  /** An HTML preview (V0.19), for a sandboxed iframe; from the thread's host (V0.54). */
  previewUrl(id: string, host?: string): string;
  /** The Changes tab (V0.25): the branch diff of the thread's folder. */
  changes(threadId: string): Promise<ChangesDTO>;
  /** One file's unified diff (V0.25). */
  filePatch(threadId: string, path: string, since?: string): Promise<FilePatchDTO>;
  /** The PR tab (V0.43): the thread's PR as the host reads it; fresh skips the host's 30 s cache. */
  prDetail(threadId: string, fresh?: boolean): Promise<PrReply>;
  /** The host folder picker (V0.42); every path stays inside the owner's home folder. V0.132: `host` names whose
   *  folders ("" or missing: the front's first host). */
  fsList(path: string, host?: string): Promise<FsListing>;
  fsRecent(host?: string): Promise<FsDir[]>;
  fsRepos(host?: string): Promise<FsDir[]>;
  /** Makes parent/name and returns its path. */
  fsMkdir(parent: string, name: string, host?: string): Promise<string>;
  /** V0.44: each profile's provider quota; [] when the host cannot say. */
  quota(): Promise<Quota[]>;
  /** V0.80: each host's load and its threads' CPU; [] when no host can say. */
  load(): Promise<HostLoad[]>;
  // V0.127: the per-host settings reads. `host` names the host ("" or missing: the front's first host); an offline
  // host answers what the front last read from it, with when (readAt).
  /** V0.50: Settings → Providers; never a key, only whether one is set. */
  providers(host?: string): Promise<HostRead<ProviderDTO[]>>;
  /** V0.50: logins, endpoints and keys found on the host. */
  providersFound(host?: string): Promise<HostRead<FoundDTO[]>>;
  /** V0.102: the repos Fyr knows and whether each merges agents' merge asks with no press. */
  autoMerge(host?: string): Promise<HostRead<AutoMergeRepo[]>>;
  /** V0.111: whether Fyr approves agents' deploy asks of merged code with no press. */
  autoDeploy(host?: string): Promise<HostRead<AutoDeploySetting>>;
  /** V0.126 (#151): the update record and whether a local build pauses auto-update. */
  updates(host?: string): Promise<HostRead<UpdatesView>>;
  /** V0.157: Settings → Devices: the signed-in devices, the ones that wait, the browser way in's mode. */
  authState(): Promise<AuthState>;
  /** V0.157: approve (code or id), deny, revoke, add (a one-time link and its QR code), mode (browser way in). */
  authAct(action: "approve" | "deny" | "revoke" | "add" | "mode", body: Record<string, unknown>): Promise<{ ok: boolean; error?: string } & Record<string, unknown>>;
  /** V0.159: Settings → Hosts: the paired installs, the nodes that are not, the open invite. */
  peerState(): Promise<PeerState>;
  /** V0.159: invite (open one here: a code, a QR image, a link) or join (an invite from another install). */
  peerAct(action: "invite" | "join" | "remove", body: Record<string, unknown>): Promise<{ ok: boolean; error?: string } & Record<string, unknown>>;
  /** V0.115: the system agents setting of a host */
  systemAgents(host?: string): Promise<HostRead<SystemAgentsView>>;
  /** V0.50: the current profiles (after a provider changed). */
  profiles(): Promise<Profile[]>;
  /** V0.79: the agent's commands and skills for a thread, or for a provider and folder (New thread). */
  commands(source: CommandSource): Promise<SlashCommand[]>;
  /** V0.98 (3): opening the Changes board is a visit; the host keeps the owner's boardSeenAt and returns the one before. */
  boardVisit?(): Promise<string | null>;
}

/** Whose commands: a thread's, or the newest of a provider and folder on a host (V0.79). */
export type CommandSource = { threadId: string } | { cwd: string; profile: string; host?: string };

/** V0.127: a per-host settings read; readAt is set when the host is offline and this is what the front last read. */
export interface HostRead<T> {
  data: T;
  readAt?: string;
}

async function hostGet<T>(path: string, host: string | undefined): Promise<HostRead<T>> {
  const r = await fetch(settingsPath(path, host ?? ""), { headers: { Accept: "application/json" } });
  const j = (await r.json().catch(() => ({}))) as T & { error?: string };
  if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
  const at = r.headers.get("X-Fyr-Read-At");
  return at ? { data: j, readAt: at } : { data: j };
}

async function fsGet<T>(path: string): Promise<T> {
  const r = await fetch(path, { headers: { Accept: "application/json" } });
  const j = (await r.json().catch(() => ({}))) as T & { error?: string };
  if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
  return j;
}

export type UploadResult = { ok: true; reply: UploadReply } | { ok: false; status: number | null; error?: string };

export interface PushReply {
  ok: boolean;
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

  const checkBuild = (buildId: unknown) => {
    const r = observeBuildId(watch, buildId);
    watch = r.watch;
    if (r.reload) reloadForNewBuild();
    return r.reload;
  };

  const connect = () => {
    if (!handlers) return;
    const h = handlers;
    const gen = ++generation;
    h.conn("connecting");
    es = new EventSource("/api/stream");
    // Subscribe first, then fetch the snapshot (so nothing is lost between them).
    es.addEventListener("hello", async (m) => {
      let hello: { now?: string; buildId?: string } = {};
      try {
        hello = JSON.parse((m as MessageEvent).data);
      } catch {
        /* keep empty */
      }
      if (checkBuild(hello.buildId)) return;
      try {
        // V0.38: with a stored copy, ask only for threads changed since it.
        const stored = await cache.state();
        const since = stored ? sinceRev(stored) : 0;
        const r = await fetch(since ? `/api/state?since=${since}` : "/api/state", { headers: { Accept: "application/json" } });
        if (!r.ok) {
          if (r.status === 401) void reloadIfSignedOut(); // V0.157: a revoked device lands on the signed-out page
          throw new Error(`state ${r.status}`);
        }
        let dto = (await r.json()) as StateDTO & { buildId?: string; partial?: boolean };
        if (dto.partial && stored) dto = mergeStateDelta(stored, dto) as StateDTO & { buildId?: string };
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
      try {
        h.project?.(JSON.parse((m as MessageEvent).data));
      } catch {
        /* ignore malformed */
      }
    });
    es.addEventListener("projectOrder", (m) => {
      try {
        const ids = (JSON.parse((m as MessageEvent).data) as { ids?: unknown }).ids;
        if (Array.isArray(ids)) h.projectOrder?.(ids.filter((x): x is string => typeof x === "string"));
      } catch {
        /* ignore malformed */
      }
    });
    es.addEventListener("hosts", (m) => {
      try {
        h.hosts?.(decodeHosts((JSON.parse((m as MessageEvent).data) as { hosts?: unknown }).hosts));
      } catch {
        /* ignore malformed */
      }
    });
    es.addEventListener("stall", (m) => {
      try {
        h.stall?.(JSON.parse((m as MessageEvent).data));
      } catch {
        /* ignore malformed */
      }
    });
    es.addEventListener("deploy", (m) => {
      try {
        h.deploy?.(JSON.parse((m as MessageEvent).data));
      } catch {
        /* ignore malformed */
      }
    });
    es.addEventListener("thread.patch", (m) => {
      try {
        h.threadPatch?.(JSON.parse((m as MessageEvent).data));
      } catch {
        /* ignore malformed */
      }
    });
    es.addEventListener("thread", (m) => {
      try {
        h.thread(JSON.parse((m as MessageEvent).data));
      } catch {
        /* ignore malformed */
      }
    });
    es.addEventListener("event", (m) => {
      try {
        h.event(JSON.parse((m as MessageEvent).data));
      } catch {
        /* ignore malformed */
      }
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
      return r.ok ? { ok: j.ok !== false, error: j.error } : { ok: false, error: j.error ?? `HTTP ${r.status}` };
    } catch {
      return { ok: false, error: "Fyr is not reachable" };
    }
  };

  return {
    mock: false,
    async fsList(path, host) {
      const l = await fsGet<FsListing>(fsQuery(`/api/fs/list?path=${encodeURIComponent(path)}`, host));
      return { path: l.path, parent: l.parent ?? "", dirs: l.dirs ?? [] };
    },
    async fsRecent(host) {
      return (await fsGet<{ dirs?: FsDir[] }>(fsQuery("/api/fs/recent", host))).dirs ?? [];
    },
    async fsRepos(host) {
      return (await fsGet<{ dirs?: FsDir[] }>(fsQuery("/api/fs/repos", host))).dirs ?? [];
    },
    async fsMkdir(parent, name, host) {
      const r = await fetch(fsQuery("/api/fs/mkdir", host), { method: "POST", headers: { "Content-Type": "application/json", "X-Fyr": "1" }, body: JSON.stringify({ parent, name }) });
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
    },
    async command(action, threadId, args) {
      // One commandId for every try: the host applies it once (I7), so a
      // restart during a deploy is ridden out (SPEC 0.5 V0.13).
      const body = { commandId: newCommandId(), action, threadId, args: args ?? {} };
      let reply: CommandReply = { ok: false, error: "Fyr is not reachable" };
      const { gaveUp } = await sendWithRetry(
        async () => {
          try {
            const r = await fetch("/api/command", { method: "POST", headers: { "Content-Type": "application/json", "X-Fyr": "1" }, body: JSON.stringify(body) });
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
    },
    async events(threadId, after = 0, limit = 400) {
      const r = await fetch(`/api/threads/${encodeURIComponent(threadId)}/events?after=${after}&limit=${limit}`);
      if (!r.ok) throw new Error(`events ${r.status}`);
      const j = (await r.json()) as { events?: FyrEvent[] };
      return j.events ?? [];
    },
    async thread(threadId) {
      const r = await fetch(`/api/threads/${encodeURIComponent(threadId)}`);
      return r.ok ? ((await r.json()) as ThreadDTO) : null;
    },
    async fullEvent(threadId, seq) {
      const r = await fetch(`/api/threads/${encodeURIComponent(threadId)}/events/${seq}`);
      return r.ok ? ((await r.json()) as FyrEvent) : null;
    },
    async eventsBefore(threadId, before, limit = 400) {
      const r = await fetch(`/api/threads/${encodeURIComponent(threadId)}/events?before=${before}&limit=${limit}`);
      if (!r.ok) throw new Error(`events ${r.status}`);
      const j = (await r.json()) as { events?: FyrEvent[] };
      return j.events ?? [];
    },
    async t3Candidates(host) {
      const r = await fetch(fsQuery("/api/t3/candidates", host)); // V0.139: that host's T3
      if (!r.ok) throw new Error(`candidates ${r.status}`);
      const j = (await r.json()) as { threads?: T3Candidate[] };
      return j.threads ?? [];
    },
    async pushKey() {
      const r = await fetch("/api/push/key");
      if (!r.ok) throw new Error(`The host has no push key (HTTP ${r.status})`);
      const j = (await r.json()) as { publicKey?: string };
      if (!j.publicKey) throw new Error("The host sent no push key");
      return j.publicKey;
    },
    pushSubscribe(subscription, device) {
      return postJson("/api/push/subscribe", { subscription, device });
    },
    pushUnsubscribe(endpoint) {
      return postJson("/api/push/unsubscribe", { endpoint });
    },
    pushTest() {
      return postJson("/api/push/test", {});
    },
    uploadAttachment(file, onProgress, signal, host) {
      return new Promise<UploadResult>((resolve) => {
        const x = new XMLHttpRequest();
        x.open("POST", `/api/attachments${hostQuery(host)}`);
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
    async uploadProjectIcon(projectId, file) {
      try {
        const r = await fetch(`/api/projects/${encodeURIComponent(projectId)}/icon`, { method: "POST", headers: { "X-Fyr": "1", "Content-Type": file.type || "application/octet-stream" }, body: file });
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
          ? `/api/threads/${encodeURIComponent(source.threadId)}/commands`
          : `/api/commands?cwd=${encodeURIComponent(source.cwd)}&profile=${encodeURIComponent(source.profile)}${source.host ? `&host=${encodeURIComponent(source.host)}` : ""}`;
      const r = await fetch(path, { headers: { Accept: "application/json" } });
      const j = (await r.json().catch(() => ({}))) as { commands?: SlashCommand[]; error?: string };
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      return Array.isArray(j.commands) ? j.commands : [];
    },
    async changes(threadId) {
      const r = await fetch(`/api/threads/${encodeURIComponent(threadId)}/changes`);
      const j = (await r.json().catch(() => ({}))) as ChangesDTO & { error?: string };
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      return j;
    },
    async filePatch(threadId, path, since) {
      const r = await fetch(`/api/threads/${encodeURIComponent(threadId)}/changes?path=${encodeURIComponent(path)}${since ? `&since=${encodeURIComponent(since)}` : ""}`);
      const j = (await r.json().catch(() => ({}))) as FilePatchDTO & { error?: string };
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      return j;
    },
    async providers(host) {
      return hostGet<ProviderDTO[]>("/api/providers", host);
    },
    async providersFound(host) {
      return hostGet<FoundDTO[]>("/api/providers/found", host);
    },
    async autoMerge(host) {
      return hostGet<AutoMergeRepo[]>("/api/autoMerge", host);
    },
    async autoDeploy(host) {
      return hostGet<AutoDeploySetting>("/api/autoDeploy", host);
    },
    async authState() {
      const r = await fetch("/api/auth/state", { headers: { Accept: "application/json" } });
      const j = (await r.json().catch(() => ({}))) as AuthState & { error?: string };
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      return j;
    },
    async peerState() {
      const r = await fetch("/api/peer/state", { headers: { Accept: "application/json" } });
      const j = (await r.json().catch(() => ({}))) as PeerState & { error?: string };
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      return j;
    },
    async peerAct(action, body) {
      try {
        const r = await fetch(`/api/peer/${action}`, { method: "POST", headers: { "X-Fyr": "1", "Content-Type": "application/json" }, body: JSON.stringify(body) });
        const j = (await r.json().catch(() => ({}))) as { ok?: boolean; error?: string } & Record<string, unknown>;
        return r.ok && j.ok !== false ? { ...j, ok: true } : { ...j, ok: false, error: j.error ?? `HTTP ${r.status}` };
      } catch {
        return { ok: false, error: "Fyr is not reachable" };
      }
    },
    async authAct(action, body) {
      try {
        const r = await fetch(`/api/auth/${action}`, { method: "POST", headers: { "X-Fyr": "1", "Content-Type": "application/json" }, body: JSON.stringify(body) });
        const j = (await r.json().catch(() => ({}))) as { ok?: boolean; error?: string } & Record<string, unknown>;
        return r.ok && j.ok !== false ? { ...j, ok: true } : { ...j, ok: false, error: j.error ?? `HTTP ${r.status}` };
      } catch {
        return { ok: false, error: "Fyr is not reachable" };
      }
    },
    async updates(host) {
      return hostGet<UpdatesView>("/api/updates", host);
    },
    async systemAgents(host) {
      return hostGet<SystemAgentsView>("/api/systemAgents", host);
    },
    async profiles() {
      return fsGet<Profile[]>("/api/profiles");
    },
    async quota() {
      try {
        const r = await fetch("/api/quota");
        const j = (await r.json().catch(() => [])) as unknown;
        return r.ok && Array.isArray(j) ? (j as Quota[]) : [];
      } catch {
        return [];
      }
    },
    async boardVisit() {
      try {
        const r = await fetch("/api/board/visit", { method: "POST", headers: { "Content-Type": "application/json", "X-Fyr": "1" }, body: "{}" });
        const j = (await r.json().catch(() => ({}))) as { seenAt?: string | null };
        return r.ok && typeof j.seenAt === "string" ? j.seenAt : null;
      } catch {
        return null;
      }
    },
    async load() {
      try {
        const r = await fetch("/api/load");
        const j = (await r.json().catch(() => [])) as unknown;
        return r.ok && Array.isArray(j) ? (j as HostLoad[]) : [];
      } catch {
        return [];
      }
    },
    async prDetail(threadId, fresh) {
      try {
        const r = await fetch(`/api/threads/${encodeURIComponent(threadId)}/pr${fresh ? "?fresh=1" : ""}`);
        const j = (await r.json().catch(() => ({}))) as PrReply;
        if (!r.ok && !("error" in j)) return { error: `HTTP ${r.status}` };
        return j;
      } catch {
        return { error: "Fyr is not reachable." };
      }
    },
    async presence(threadId, device) {
      try {
        await fetch("/api/presence", { method: "POST", headers: { "Content-Type": "application/json", "X-Fyr": "1" }, body: JSON.stringify({ threadId, device }), keepalive: true });
      } catch {
        /* best effort */
      }
    },
    async pushStatus(endpoint) {
      try {
        const r = await fetch("/api/push/status", { method: "POST", headers: { "Content-Type": "application/json", "X-Fyr": "1" }, body: JSON.stringify({ endpoint }) });
        if (!r.ok) return null;
        const j = (await r.json()) as { known?: boolean };
        return typeof j.known === "boolean" ? j.known : null;
      } catch {
        return null;
      }
    },
  };
}
