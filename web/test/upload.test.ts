// V0.api-contract: an upload gives up when it stalls, and its cancel listener goes when it ends.
import { test, afterEach, mock } from "node:test";
import assert from "node:assert/strict";
import { upload } from "../src/http.ts";

/** An XMLHttpRequest the test drives: it answers only when told. */
class FakeXhr {
  static last: FakeXhr | null = null;
  upload: { onprogress: ((e: { loaded: number; total: number; lengthComputable: boolean }) => void) | null } = { onprogress: null };
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onabort: (() => void) | null = null;
  status = 0;
  responseText = "";
  headers: Record<string, string> = {};
  aborted = false;
  constructor() {
    FakeXhr.last = this;
  }
  open() {}
  setRequestHeader(k: string, v: string) {
    this.headers[k] = v;
  }
  send() {}
  abort() {
    this.aborted = true;
    this.onabort?.();
  }
  answer(status: number, body: string) {
    this.status = status;
    this.responseText = body;
    this.onload?.();
  }
}

/** An AbortSignal that counts its listeners. */
function countedSignal() {
  const ctl = new AbortController();
  let n = 0;
  const add = ctl.signal.addEventListener.bind(ctl.signal);
  const remove = ctl.signal.removeEventListener.bind(ctl.signal);
  ctl.signal.addEventListener = ((...a: Parameters<typeof add>) => (n++, add(...a))) as typeof add;
  ctl.signal.removeEventListener = ((...a: Parameters<typeof remove>) => (n--, remove(...a))) as typeof remove;
  return { ctl, listeners: () => n };
}

const realXhr = (globalThis as { XMLHttpRequest?: unknown }).XMLHttpRequest;
afterEach(() => {
  mock.timers.reset();
  (globalThis as { XMLHttpRequest?: unknown }).XMLHttpRequest = realXhr;
});
const install = () => ((globalThis as { XMLHttpRequest?: unknown }).XMLHttpRequest = FakeXhr);
const file = new Blob(["hello"], { type: "text/plain" });

test("an answered upload resolves with its status and body, sends X-Fyr, and removes its cancel listener", async () => {
  install();
  const { ctl, listeners } = countedSignal();
  const seen: number[] = [];
  const p = upload("/a", file, { "X-Fyr-Filename": "a.txt" }, (l) => seen.push(l), ctl.signal, 1000);
  assert.equal(listeners(), 1);
  const x = FakeXhr.last!;
  assert.equal(x.headers["X-Fyr"], "1");
  assert.equal(x.headers["X-Fyr-Filename"], "a.txt");
  x.upload.onprogress!({ loaded: 3, total: 5, lengthComputable: true });
  x.answer(200, '{"id":"at_1"}');
  assert.deepEqual(await p, { status: 200, json: { id: "at_1" } });
  assert.deepEqual(seen, [3]);
  assert.equal(listeners(), 0);
});

// The clock is the test's (mock timers): no wall time decides a stall.
test("an upload with no progress for the stall time is given up as stalled", async () => {
  install();
  mock.timers.enable({ apis: ["setTimeout"] });
  const { ctl, listeners } = countedSignal();
  const p = upload("/a", file, {}, () => {}, ctl.signal, 1000);
  mock.timers.tick(999);
  assert.equal(FakeXhr.last!.aborted, false);
  mock.timers.tick(1);
  const r = await p;
  assert.deepEqual(r, { status: null, json: null, stalled: true });
  assert.ok(FakeXhr.last!.aborted);
  assert.equal(listeners(), 0);
});

test("progress keeps a slow upload alive; the caller's cancel ends it as cancelled", async () => {
  install();
  const { ctl, listeners } = countedSignal();
  mock.timers.enable({ apis: ["setTimeout"] });
  const p = upload("/a", file, {}, () => {}, ctl.signal, 1000);
  const x = FakeXhr.last!;
  for (let i = 0; i < 4; i++) {
    mock.timers.tick(900);
    x.upload.onprogress!({ loaded: i, total: 5, lengthComputable: true });
  }
  assert.equal(x.aborted, false, "3.6 s with progress every 0.9 s is not a stall");
  ctl.abort();
  assert.deepEqual(await p, { status: 0, json: null });
  assert.equal(listeners(), 0);
});

test("a network error resolves with no status, and never rejects", async () => {
  install();
  const p = upload("/a", file, {}, () => {});
  FakeXhr.last!.onerror!();
  assert.deepEqual(await p, { status: null, json: null });
});
