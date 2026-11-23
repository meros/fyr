// The one fetch of the client (src/http.ts) and the reads of api.ts that go through it: a call times out, a body that
// is not what the screen reads decodes to its empty value instead of throwing, and a failed read never escapes unhandled.
import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { request, replyError } from "../src/http.ts";
import { createApi } from "../src/api.ts";
import { hid } from "./helpers.ts";

const realFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = realFetch;
});

type Seen = { url: string; init: RequestInit };
function stub(answer: (url: string, init: RequestInit) => Response | Promise<Response>): Seen[] {
  const seen: Seen[] = [];
  globalThis.fetch = (async (url: string, init: RequestInit = {}) => {
    seen.push({ url, init });
    return answer(url, init);
  }) as typeof fetch;
  return seen;
}
const body = (text: string, status = 200, headers: Record<string, string> = {}) => new Response(text, { status, headers });
const H = hid("H1");

test("request: a GET asks for JSON, a POST sends X-Fyr and its JSON body", async () => {
  const seen = stub(() => body("{}"));
  await request("/a");
  await request("/b", { method: "POST", body: { x: 1 } });
  assert.deepEqual((seen[0].init.headers as Record<string, string>).Accept, "application/json");
  const h = seen[1].init.headers as Record<string, string>;
  assert.equal(h["X-Fyr"], "1");
  assert.equal(h["Content-Type"], "application/json");
  assert.equal(seen[1].init.body, '{"x":1}');
});

test("request: a body that is not JSON is null, and the status is kept", async () => {
  stub(() => body("<html>", 502));
  const r = await request("/a");
  assert.deepEqual([r.ok, r.status, r.json], [false, 502, null]);
  assert.equal(replyError(r), "HTTP 502");
  assert.equal(replyError({ status: 400, json: { error: "no" } }), "no");
  assert.equal(replyError({ status: 400, json: null }), "HTTP 400");
});

test("request: a call that does not answer in time rejects, and the caller's own cancel does too", async () => {
  stub((_, init) => new Promise<Response>((_res, rej) => init.signal!.addEventListener("abort", () => rej(init.signal!.reason))));
  await assert.rejects(request("/slow", { timeoutMs: 20 }));
  const c = new AbortController();
  const p = request("/slow", { signal: c.signal, timeoutMs: 5000 });
  c.abort();
  await assert.rejects(p);
});

test("every call carries a signal (a timeout) by default", async () => {
  const seen = stub(() => body("{}"));
  await request("/a");
  assert.ok(seen[0].init.signal);
});

test("no file of the client makes a call of its own: every request goes through http.ts (the service worker has its own)", () => {
  const dir = join(import.meta.dirname, "..", "src");
  const files = readdirSync(dir, { recursive: true, encoding: "utf8" }).filter((f) => /\.tsx?$/.test(f) && f !== "http.ts" && f !== "sw.ts");
  assert.ok(files.includes("api.ts"), "the walk finds the client's files");
  for (const f of files) assert.doesNotMatch(readFileSync(join(dir, f), "utf8"), /\bfetch\(/, f);
});

test("a settings list whose body is null or not a list reads as empty; an object one as {}", async () => {
  const api = await createApi("");
  for (const text of ["null", "{}", "garbage", ""]) {
    stub(() => body(text));
    assert.deepEqual(await api.providers(H), { ok: true, data: [] }, text);
    assert.deepEqual(await api.autoMerge(H), { ok: true, data: [] }, text);
    assert.deepEqual(await api.autoDeploy(H), { ok: true, data: {} }, text);
    assert.deepEqual(await api.systemAgents(H), { ok: true, data: {} }, text);
  }
});

test("a read keeps its host's offline stamp and answers the host's error on a refusal", async () => {
  const api = await createApi("");
  stub(() => body("[]", 200, { "X-Fyr-Read-At": "2026-01-01T00:00:00Z" }));
  assert.deepEqual(await api.providers(H), { ok: true, data: [], readAt: "2026-01-01T00:00:00Z" });
  stub(() => body('{"error":"offline"}', 503));
  assert.deepEqual(await api.providers(H), { ok: false, error: "offline" });
  stub(() => body("null", 500));
  assert.deepEqual(await api.providers(H), { ok: false, error: "HTTP 500" });
});

// V0.api-contract: no method rejects. Every read that answers a Result answers a failed one when the network fails, the
// host refuses, or the body is not what the read needs.
test("no read rejects: a network failure, a refusal or a body the read cannot use is a failed Result", async () => {
  const api = await createApi("");
  const reads = {
    events: () => api.events(H, "t1"),
    eventsBefore: () => api.eventsBefore(H, "t1", 5),
    pushKey: () => api.pushKey(),
    changes: () => api.changes(H, "t1"),
    filePatch: () => api.filePatch(H, "t1", "a.go"),
    searchThreads: () => api.searchThreads(H, "q", ["t1"]),
    fsList: () => api.fsList(H, "~"),
    fsRecent: () => api.fsRecent(H),
    fsRepos: () => api.fsRepos(H),
    fsMkdir: () => api.fsMkdir(H, "~", "new"),
    providers: () => api.providers(H),
    providersFound: () => api.providersFound(H),
    autoMerge: () => api.autoMerge(H),
    autoDeploy: () => api.autoDeploy(H),
    updates: () => api.updates(H),
    setup: () => api.setup!(H),
    systemAgents: () => api.systemAgents(H),
    authState: () => api.authState(),
    peerState: () => api.peerState(),
    commands: () => api.commands({ host: H, threadId: "t1" }),
    handoverPlan: () => api.handoverPlan!(H, "t1"),
  };
  const cases: [string, () => void, RegExp][] = [
    ["network", () => stub(() => Promise.reject(new TypeError("network"))), /^Fyr is not reachable$/],
    ["refused", () => stub(() => body('{"error":"no such thread"}', 404)), /^no such thread$/],
  ];
  for (const [why, arm, want] of cases) {
    arm();
    for (const [name, read] of Object.entries(reads)) {
      const r = await read();
      assert.equal(r.ok, false, `${name} ${why}`);
      if (!r.ok) assert.match(r.error, want, `${name} ${why}`);
    }
  }
  // A body without what the read must have: no key, no folder made, no plan.
  stub(() => body("{}"));
  assert.deepEqual(await api.pushKey(), { ok: false, error: "The host sent no push key" });
  assert.equal((await api.fsMkdir(H, "~", "x")).ok, false);
  assert.equal((await api.handoverPlan!(H, "t1")).ok, false);
  stub(() => body('{"publicKey":"k"}'));
  assert.deepEqual(await api.pushKey(), { ok: true, data: "k" });
  stub(() => body('{"path":"/home/a/x"}'));
  assert.deepEqual(await api.fsMkdir(H, "~", "x"), { ok: true, data: "/home/a/x" });
});

test("events, the commands and the profiles read an answer without the expected list as an empty one", async () => {
  const api = await createApi("");
  stub(() => body("null"));
  assert.deepEqual(await api.events(H, "t1"), { ok: true, data: [] });
  assert.deepEqual(await api.eventsBefore(H, "t1", 5), { ok: true, data: [] });
  assert.deepEqual(await api.commands({ host: H, threadId: "t1" }), { ok: true, data: [] });
  assert.deepEqual(await api.profiles(H), []);
  assert.equal(await api.thread(H, "t1"), null);
  assert.equal(await api.fullEvent(H, "t1", 3), null);
  stub(() => body("[1,null]"));
  assert.deepEqual(await api.events(H, "t1"), { ok: true, data: [] });
});

test("a thread read goes through decodeThread: lists are lists", async () => {
  const api = await createApi("");
  stub(() => body(JSON.stringify({ id: "t1", title: "T" })));
  const t = (await api.thread(H, "t1"))!;
  assert.equal(t.id, "t1");
  assert.deepEqual((t as { changes: unknown }).changes, []);
});

test("changes and file patches decode; a literal null body is an empty reply", async () => {
  const api = await createApi("");
  stub(() => body("null"));
  const c = await api.changes(H, "t1");
  assert.ok(c.ok);
  assert.deepEqual(c.data.files, []);
  const p = await api.filePatch(H, "t1", "a.go");
  assert.ok(p.ok);
  assert.equal(p.data.patch, "");
});

test("command, mutate, push and presence never reject, and a hung host is given up on", async () => {
  const api = await createApi("");
  stub(() => {
    throw new TypeError("network");
  });
  assert.deepEqual(await api.pushTest(), { ok: false, error: "Fyr is not reachable" });
  assert.equal((await api.authAct("revoke", {})).ok, false);
  await api.presence(null, "d");
  assert.equal(await api.pushStatus("e"), null);
  stub(() => body("not json", 200));
  assert.deepEqual(await api.pushTest(), { ok: true, error: undefined, delivered: undefined });
});

test("the stream's page listeners go with stop()", async () => {
  const adds: string[] = [];
  const removes: string[] = [];
  const g = globalThis as { window?: unknown; EventSource?: unknown };
  const saved = [g.window, g.EventSource];
  g.window = { addEventListener: (k: string) => adds.push(k), removeEventListener: (k: string) => removes.push(k) };
  g.EventSource = class {
    close() {}
    addEventListener() {}
  };
  try {
    stub(() => body("{}"));
    const api = await createApi("");
    api.start({ state() {}, thread() {}, event() {}, conn() {} } as never);
    assert.deepEqual(adds.sort(), ["pagehide", "pageshow"]);
    api.stop();
    assert.deepEqual(removes.sort(), ["pagehide", "pageshow"]);
  } finally {
    [g.window, g.EventSource] = saved;
  }
});

test("the reads a screen fires and forgets (thread, fullEvent, profiles) never reject, on a network error or a timeout", async () => {
  const api = await createApi("");
  stub(() => {
    throw new TypeError("network");
  });
  assert.equal(await api.thread(H, "t1"), null);
  assert.equal(await api.fullEvent(H, "t1", 1), null);
  assert.deepEqual(await api.profiles(H), []);
});
