import { isTransient, sendWithRetry } from "./retry.ts";
// The only module that talks HTTP and SSE to fyrd (docs/API-v0.md). With ?mock=1 it serves fixtures instead.

import type { DeployProgress } from "./deploy.ts";
import { cache, mergeStateDelta, sinceRev } from "./cache.ts";
import type { ThreadPatch } from "./store.ts";
import type { CommandReply, FyrEvent, Project, StateDTO, T3Candidate, ThreadDTO } from "./types.ts";
import type { UploadReply } from "./attach.ts";
import { initialBuildWatch, observeBuildId } from "./buildid.ts";
import { MOCK_PREVIEWS, MOCK_SHOTS, createMockServer, mockChanges, mockFilePatch, mockScenario } from "./mock.ts";
import type { ChangesDTO, FilePatchDTO } from "./changes.ts";
import type { PrReply } from "./pr.ts";
import { reloadForNewBuild } from "./pwa.ts";

export type ConnState = "connecting" | "live" | "offline";

export interface ApiHandlers {
  state(dto: StateDTO): void;
  thread(t: ThreadDTO): void;
  /** V0.39: where an update of Fyr is */
  deploy?(p: DeployProgress): void;
  /** V0.38: a merge patch against the thread copy with rev === base */
  threadPatch?(p: ThreadPatch): void;
  /** a project made or changed (V0.23); optional for older callers */
  project?(p: Project): void;
  event(e: FyrEvent): void;
  conn(c: ConnState): void;
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
  t3Candidates(): Promise<T3Candidate[]>;
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
  uploadAttachment(file: File, onProgress: (loaded: number, total: number) => void, signal?: AbortSignal): Promise<UploadResult>;
  attachmentUrl(id: string): string;
  /** An HTML preview (V0.19), for a sandboxed iframe. */
  previewUrl(id: string): string;
  /** The Changes tab (V0.25): the branch diff of the thread's folder. */
  changes(threadId: string): Promise<ChangesDTO>;
  /** One file's unified diff (V0.25). */
  filePatch(threadId: string, path: string): Promise<FilePatchDTO>;
  /** The PR tab (V0.43): the thread's PR as the host reads it; fresh skips the host's 30 s cache. */
  prDetail(threadId: string, fresh?: boolean): Promise<PrReply>;
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

export function createApi(search: string = location.search): Api {
  const scenario = mockScenario(search);
  return scenario ? createMockApi(scenario) : createHttpApi();
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
        if (!r.ok) throw new Error(`state ${r.status}`);
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
      if (gen === generation) retry();
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
    async t3Candidates() {
      const r = await fetch("/api/t3/candidates");
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
    uploadAttachment(file, onProgress, signal) {
      return new Promise<UploadResult>((resolve) => {
        const x = new XMLHttpRequest();
        x.open("POST", "/api/attachments");
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
    attachmentUrl(id) {
      return `/api/attachments/${encodeURIComponent(id)}`;
    },
    previewUrl(id) {
      return `/api/previews/${encodeURIComponent(id)}`;
    },
    async changes(threadId) {
      const r = await fetch(`/api/threads/${encodeURIComponent(threadId)}/changes`);
      const j = (await r.json().catch(() => ({}))) as ChangesDTO & { error?: string };
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      return j;
    },
    async filePatch(threadId, path) {
      const r = await fetch(`/api/threads/${encodeURIComponent(threadId)}/changes?path=${encodeURIComponent(path)}`);
      const j = (await r.json().catch(() => ({}))) as FilePatchDTO & { error?: string };
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      return j;
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

const MOCK_IMAGE =
  "data:image/svg+xml," +
  encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="390" height="844"><rect width="390" height="844" fill="#edeff2"/><rect width="390" height="120" fill="#14212d"/><rect x="24" y="170" width="250" height="18" rx="4" fill="#b4bcc6"/><rect x="24" y="210" width="320" height="14" rx="4" fill="#dde1e6"/><rect x="24" y="240" width="290" height="14" rx="4" fill="#dde1e6"/><circle cx="195" cy="560" r="90" fill="#e8960f" opacity=".5"/></svg>');

function createMockApi(scenario: string): Api {
  const server = createMockServer(scenario, Date.now());
  const mockFiles = new Map<string, string>();
  let watch = initialBuildWatch();
  return {
    mock: true,
    start(h) {
      h.conn("connecting");
      watch = observeBuildId(watch, server.buildId).watch;
      server.subscribe({ thread: h.thread, event: h.event, project: (p) => h.project?.(p) });
      setTimeout(() => {
        h.state(server.state());
        h.conn("live");
      }, 0);
      // V0.38 perf guard (mock only): a burst of n thread updates and n messages for one thread,
      // one pair every 10 ms, the way a busy agent streams.
      (globalThis as { __fyrBurst?: (id: string, n: number) => void }).__fyrBurst = (id, n) => {
        const base = server.state().threads.find((t) => t.id === id);
        if (!base) return;
        for (let i = 1; i <= n; i++) {
          setTimeout(() => {
            h.thread({ ...base, rev: (base.rev ?? 0) + i, activity: `Step ${i} of ${n}`, updatedAt: new Date().toISOString() });
            h.event({ seq: 1_000_000 + i, at: new Date().toISOString(), subject: id, type: "msg", data: { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "text", text: `Burst message ${i}: **bold**, \`code\`, and a list:\n\n- one\n- two` }] } } });
          }, i * 10);
        }
      };
    },
    async command(action, threadId, args) {
      return server.command(newCommandId(), action, threadId, args ?? {});
    },
    async events(threadId, after = 0, limit = 400) {
      return server.events(threadId, after, limit);
    },
    async thread() {
      return null; // the mock sends whole threads
    },
    async fullEvent() {
      return null; // the mock never cuts
    },
    async eventsBefore(threadId, before, limit = 400) {
      const all = server.events(threadId, 0, 1_000_000).filter((e) => e.seq < before);
      return all.slice(Math.max(0, all.length - limit));
    },
    async t3Candidates() {
      return server.t3Candidates();
    },
    async pushKey() {
      return server.pushKey();
    },
    async pushSubscribe(subscription, device) {
      return server.pushSubscribe(subscription, device);
    },
    async pushUnsubscribe(endpoint) {
      return server.pushUnsubscribe(endpoint);
    },
    async pushTest() {
      return server.pushTest();
    },
    async presence() {
      /* the mock has no push */
    },
    async pushStatus(endpoint) {
      return server.pushSubscriptions().some((s) => s.endpoint === endpoint);
    },
    uploadAttachment(file, onProgress, signal) {
      // An in-memory store; progress in four steps so the chip shows it.
      return new Promise<UploadResult>((resolve) => {
        if (file.size > 25 * 1024 * 1024) return resolve({ ok: false, status: 413 });
        let step = 0;
        const tick = () => {
          if (signal?.aborted) return resolve({ ok: false, status: 0, error: "cancelled" });
          step++;
          onProgress(Math.round((file.size * step) / 4), file.size);
          if (step < 4) return void setTimeout(tick, 120);
          const reply = server.putAttachment(file.name || "file", file.type || "application/octet-stream", file.size);
          mockFiles.set(reply.id, URL.createObjectURL(file));
          resolve({ ok: true, reply });
        };
        setTimeout(tick, 120);
      });
    },
    previewUrl(id) {
      return MOCK_PREVIEWS[id] ?? "data:text/html,<p>mock preview</p>";
    },
    async changes(threadId) {
      return mockChanges(threadId);
    },
    async filePatch(_threadId, path) {
      return mockFilePatch(path);
    },
    async prDetail(threadId) {
      await new Promise((r) => setTimeout(r, 120));
      return server.prDetail(threadId);
    },
    attachmentUrl(id) {
      if (MOCK_SHOTS[id]) return MOCK_SHOTS[id];
      // Fixture attachments have no bytes; images get a drawn placeholder.
      return mockFiles.get(id) ?? (id === "fx_img" ? MOCK_IMAGE : "data:text/plain,mock%20attachment%20" + encodeURIComponent(id));
    },
  };
}
