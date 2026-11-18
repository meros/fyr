// SPEC 0.5 V0.48, V0.196: Snoozed and Settled opened from their header close again after 10 s; using one restarts its
// time; it stays open while the current thread is in it.
import { test } from "node:test";
import assert from "node:assert/strict";
import { AUTO_HIDE_MS, AUTO_HIDE_SECTIONS, hideClocks, hideStep } from "../src/inbox-autohide.ts";

const t0 = 1_000_000;
const open = () => true;

test("V0.196: Snoozed and Settled close 10 s after they were opened or last used", () => {
  assert.equal(AUTO_HIDE_MS, 10_000);
  assert.deepEqual([...AUTO_HIDE_SECTIONS], ["snoozed", "settled"]);
  const at = { settled: t0 + AUTO_HIDE_MS };
  assert.deepEqual(hideStep({ at, open, active: "needs" }, t0 + 4_000), { close: [], wait: 6_000 });
  assert.deepEqual(hideStep({ at, open, active: "needs" }, t0 + 10_000), { close: ["settled"], wait: null });
});

test("V0.196: each section runs on its own time", () => {
  const at = { snoozed: t0 + 3_000, settled: t0 + 8_000 };
  assert.deepEqual(hideStep({ at, open, active: null }, t0), { close: [], wait: 3_000 });
  assert.deepEqual(hideStep({ at, open, active: null }, t0 + 3_000), { close: ["snoozed"], wait: 5_000 });
  assert.deepEqual(hideStep({ at, open, active: null }, t0 + 9_000), { close: ["snoozed", "settled"], wait: null });
});

test("V0.196: what does not run down", () => {
  const at = { snoozed: t0, settled: t0 };
  assert.deepEqual(hideStep({ at: { settled: t0 }, open, active: "settled" }, t0 + 60_000), { close: [], wait: null }, "the current thread is in it");
  assert.deepEqual(hideStep({ at: {}, open, active: null }, t0), { close: [], wait: null }, "named in the rail or the strip: no deadline");
  assert.deepEqual(hideStep({ at, open: () => false, active: null }, t0 + 99_000), { close: [], wait: null }, "already folded");
  assert.deepEqual(hideStep({ at: { seen: t0 }, open, active: null }, t0 + 1), { close: [], wait: null }, "only these two sections close by themselves");
});

test("V0.196: the current thread's section shows no clock", () => {
  const at = { snoozed: t0, settled: t0 };
  assert.deepEqual(hideClocks(at, "settled"), { snoozed: t0 });
  assert.deepEqual(hideClocks(at, null), at);
});
