// V0.state-index: the client's sync against a stubbed front and host: the index first, then the threads in batches,
// the ones that wait for the owner before the rest, and the whole state from a host that has no index.
import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { createApi, type Api } from "../src/api.ts";
import type { Snapshot } from "../src/hostfeed.ts";

const realFetch = globalThis.fetch;
const realES = (globalThis as { EventSource?: unknown }).EventSource;
afterEach(() => {
  globalThis.fetch = realFetch;
  (globalThis as { EventSource?: unknown }).EventSource = realES;
});

const json = (o: unknown) => new Response(JSON.stringify(o), { status: 200, headers: { "Content-Type": "application/json" } });
const raw = (i: number, state = "seen") => ({ id: `t${i}`, title: `T${i}`, cwd: "/x", rev: i + 1, attention: { state, section: state === "asking" ? "needs" : "seen" } });
const meta = { now: "2026-01-01T00:00:00Z", user: "owner", actions: [], profiles: [], projects: [] };

/** A front with one host; `host` answers that host's /state. Returns the urls asked and the snapshots drawn. */
async function run(host: (q: URLSearchParams) => unknown): Promise<{ asked: string[]; drawn: Snapshot[]; done: () => Promise<void> }> {
  const asked: string[] = [];
  globalThis.fetch = (async (url: string) => {
    asked.push(String(url));
    const u = new URL(String(url), "http://x");
    if (/\/h\/H1\/state$/.test(u.pathname)) return json(host(u.searchParams));
    return json({ now: meta.now, user: "owner", hosts: [{ id: "H1", name: "host-a", online: true }] });
  }) as typeof fetch;
  class ES {
    static last: ES;
    on = new Map<string, (m: { data: string }) => void>();
    constructor() {
      ES.last = this;
    }
    addEventListener(n: string, f: (m: { data: string }) => void) {
      this.on.set(n, f);
    }
    close() {}
  }
  (globalThis as { EventSource?: unknown }).EventSource = ES;
  const api = await createApi("");
  const drawn: Snapshot[] = [];
  api.start({ state: (s: Snapshot) => void drawn.push(s), conn: () => {}, thread: () => {}, event: () => {} } as unknown as Parameters<Api["start"]>[0]);
  await new Promise((r) => setTimeout(r, 0));
  ES.last.on.get("hello")!({ data: "{}" });
  const done = async () => {
    for (let i = 0; i < 200; i++) {
      await new Promise((r) => setTimeout(r, 10));
      if (drawn.length && Object.values(drawn[drawn.length - 1].states)[0]?.threads.length === 100) return;
    }
  };
  return { asked, drawn, done };
}

test("a cold device gets the urgent thread in its first batch and all the rest in batches of at most 40", async () => {
  const all = Array.from({ length: 100 }, (_, i) => raw(i, i === 7 ? "asking" : "seen"));
  const { asked, drawn, done } = await run((q) => {
    if (q.get("index") === "1") return { ...meta, partial: true, threads: [], index: all.map((t) => ({ id: t.id, rev: t.rev, state: t.attention.state })) };
    const ids = (q.get("ids") ?? "").split(",");
    return { ...meta, partial: true, threads: all.filter((t) => ids.includes(t.id)) };
  });
  await done();
  const first = Object.values(drawn[0].states)[0].threads;
  assert.equal(first.length, 40, "the first draw is one batch, not an empty list");
  assert.ok(first.some((t) => t.rid === "t7" || t.id.endsWith("t7")), "the thread that waits for the owner is in it");
  assert.equal(Object.values(drawn[drawn.length - 1].states)[0].threads.length, 100);
  const idsAsked = asked.filter((u) => u.includes("ids=")).map((u) => new URL(u, "http://x").searchParams.get("ids")!.split(",").length);
  assert.ok(idsAsked.length === 3 && idsAsked.every((n) => n <= 40), `batches ${idsAsked}`);
  assert.ok(!asked.some((u) => /\/state$/.test(u) && u.includes("/h/")), "the whole state is never read");
});

test("a host without an index answers its whole state, and that is taken as it is", async () => {
  const all = Array.from({ length: 100 }, (_, i) => raw(i));
  const { asked, drawn, done } = await run(() => ({ ...meta, threads: all }));
  await done();
  assert.equal(Object.values(drawn[drawn.length - 1].states)[0].threads.length, 100);
  assert.equal(asked.filter((u) => u.includes("ids=")).length, 0, "nothing more is asked of it");
});
