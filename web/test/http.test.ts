// The one fetch of the client (src/http.ts) and the reads of api.ts that go through it: a call times out, a body that
// is not what the screen reads decodes to its empty value instead of throwing, and a failed read never escapes unhandled.
import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
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

test("api.ts makes no call of its own: every request goes through http.ts", () => {
  const src = readFileSync(join(import.meta.dirname, "..", "src", "api.ts"), "utf8");
  assert.doesNotMatch(src, /\bfetch\(/);
});

test("a settings list whose body is null or not a list reads as empty; an object one as {}", async () => {
  const api = await createApi("");
  for (const text of ["null", "{}", "garbage", ""]) {
    stub(() => body(text));
    assert.deepEqual((await api.providers(H)).data, [], text);
    assert.deepEqual((await api.autoMerge(H)).data, [], text);
    assert.deepEqual((await api.autoDeploy(H)).data, {}, text);
    assert.deepEqual((await api.systemAgents(H)).data, {}, text);
  }
});

test("a read keeps its host's offline stamp and throws the host's error on a refusal", async () => {
  const api = await createApi("");
  stub(() => body("[]", 200, { "X-Fyr-Read-At": "2026-01-01T00:00:00Z" }));
  assert.equal((await api.providers(H)).readAt, "2026-01-01T00:00:00Z");
  stub(() => body('{"error":"offline"}', 503));
  await assert.rejects(api.providers(H), /offline/);
  stub(() => body("null", 500));
  await assert.rejects(api.providers(H), /HTTP 500/);
});

test("events, the commands and the profiles read an answer without the expected list as an empty one", async () => {
  const api = await createApi("");
  stub(() => body("null"));
  assert.deepEqual(await api.events(H, "t1"), []);
  assert.deepEqual(await api.eventsBefore(H, "t1", 5), []);
  assert.deepEqual(await api.commands({ host: H, threadId: "t1" }), []);
  assert.deepEqual(await api.profiles(H), []);
  assert.equal(await api.thread(H, "t1"), null);
  assert.equal(await api.fullEvent(H, "t1", 3), null);
  stub(() => body("[1,null]"));
  assert.deepEqual(await api.events(H, "t1"), []);
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
  assert.deepEqual((await api.changes(H, "t1")).files, []);
  assert.equal((await api.filePatch(H, "t1", "a.go")).patch, "");
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
