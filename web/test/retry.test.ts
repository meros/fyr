import { test } from "node:test";
import assert from "node:assert/strict";
import { isTransient, sendWithRetry } from "../src/retry.ts";

// SPEC 0.5 V0.13: a restart gives 502, 503, 504 or a network error; those retry.
test("transient answers", () => {
  for (const s of [502, 503, 504, null]) assert.equal(isTransient(s), true, String(s));
  for (const s of [200, 400, 404, 409, 500]) assert.equal(isTransient(s), false, String(s));
});

test("retries the same request until the host is back, within 20 s", async () => {
  const seen: number[] = [];
  const answers = [502, null, 503, 200];
  let i = 0;
  const waits: number[] = [];
  const r = await sendWithRetry(
    async () => {
      const s = answers[i++];
      seen.push(s ?? -1);
      return s;
    },
    async (ms) => void waits.push(ms),
  );
  assert.equal(r.status, 200);
  assert.equal(seen.length, 4);
  assert.ok(waits.reduce((a, b) => a + b, 0) <= 20_000);
});

test("gives up after about 20 s of restarts", async () => {
  const waits: number[] = [];
  const r = await sendWithRetry(async () => 502, async (ms) => void waits.push(ms));
  assert.equal(r.status, 502);
  assert.equal(r.gaveUp, true);
  const total = waits.reduce((a, b) => a + b, 0);
  assert.ok(total >= 15_000 && total <= 20_000, String(total));
});

test("does not retry a real answer", async () => {
  let n = 0;
  const r = await sendWithRetry(async () => (n++, 409), async () => {});
  assert.equal(n, 1);
  assert.equal(r.status, 409);
});
