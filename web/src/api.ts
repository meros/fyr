// The only module that talks HTTP and SSE to fyrd (docs/API-v0.md). With ?mock=1 it serves fixtures instead.

import type { CommandReply, FyrEvent, StateDTO, T3Candidate, ThreadDTO } from "./types.ts";
import { initialBuildWatch, observeBuildId } from "./buildid.ts";
import { createMockServer, mockScenario } from "./mock.ts";
import { reloadForNewBuild } from "./pwa.ts";

export type ConnState = "connecting" | "live" | "offline";

export interface ApiHandlers {
  state(dto: StateDTO): void;
  thread(t: ThreadDTO): void;
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
  t3Candidates(): Promise<T3Candidate[]>;
  /** Web Push (SPEC V0.15): the host's VAPID public key, base64url. */
  pushKey(): Promise<string>;
  pushSubscribe(subscription: PushSubscriptionJSON, device: string): Promise<PushReply>;
  pushUnsubscribe(endpoint: string): Promise<PushReply>;
  pushTest(): Promise<PushReply>;
}

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
        const r = await fetch("/api/state", { headers: { Accept: "application/json" } });
        if (!r.ok) throw new Error(`state ${r.status}`);
        const dto = (await r.json()) as StateDTO & { buildId?: string };
        if (gen !== generation) return;
        if (checkBuild(dto.buildId)) return;
        backoff = 500;
        h.state(dto);
        h.conn("live");
      } catch {
        if (gen === generation) retry();
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

  const post = async (body: unknown): Promise<CommandReply> => {
    const r = await fetch("/api/command", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Fyr": "1" },
      body: JSON.stringify(body),
    });
    let reply: CommandReply;
    try {
      reply = (await r.json()) as CommandReply;
    } catch {
      reply = { ok: false, error: `HTTP ${r.status}` };
    }
    if (!r.ok && reply.ok !== false) reply = { ok: false, error: `HTTP ${r.status}` };
    return reply;
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
      connect();
    },
    async command(action, threadId, args) {
      const body = { commandId: newCommandId(), action, threadId, args: args ?? {} };
      try {
        return await post(body);
      } catch {
        // Network failure: the same commandId is safe to send again (I7).
        try {
          await new Promise((r) => setTimeout(r, 800));
          return await post(body);
        } catch (e) {
          return { ok: false, error: "Fyr is not reachable" };
        }
      }
    },
    async events(threadId, after = 0, limit = 400) {
      const r = await fetch(`/api/threads/${encodeURIComponent(threadId)}/events?after=${after}&limit=${limit}`);
      if (!r.ok) throw new Error(`events ${r.status}`);
      const j = (await r.json()) as { events?: FyrEvent[] };
      return j.events ?? [];
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
  };
}

function createMockApi(scenario: string): Api {
  const server = createMockServer(scenario, Date.now());
  let watch = initialBuildWatch();
  return {
    mock: true,
    start(h) {
      h.conn("connecting");
      watch = observeBuildId(watch, server.buildId).watch;
      server.subscribe({ thread: h.thread, event: h.event });
      setTimeout(() => {
        h.state(server.state());
        h.conn("live");
      }, 0);
    },
    async command(action, threadId, args) {
      return server.command(newCommandId(), action, threadId, args ?? {});
    },
    async events(threadId, after = 0, limit = 400) {
      return server.events(threadId, after, limit);
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
  };
}
