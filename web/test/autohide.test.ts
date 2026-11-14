// SPEC 0.5 V0.48: Settled opened from its header closes again after 30 s; using it restarts the
// time; it stays open while Settled is the selected section in the rail.
import { test } from "node:test";
import assert from "node:assert/strict";
import { AUTO_HIDE_MS, autoHide } from "../src/inbox-autohide.ts";

test("settled closes 30 s after it was opened or last used", () => {
  const t0 = 1_000_000;
  assert.equal(AUTO_HIDE_MS, 30_000);
  assert.deepEqual(autoHide({ open: true, deadline: t0 + AUTO_HIDE_MS, active: "needs" }, t0 + 10_000), { close: false, wait: 20_000 });
  assert.deepEqual(autoHide({ open: true, deadline: t0 + AUTO_HIDE_MS, active: "needs" }, t0 + 30_000), { close: true, wait: 0 });
  assert.deepEqual(autoHide({ open: true, deadline: t0, active: "settled" }, t0 + 60_000), { close: false, wait: null }, "selected in the rail: stays");
  assert.deepEqual(autoHide({ open: true, deadline: null, active: null }, t0), { close: false, wait: null }, "opened from the rail: no timer");
  assert.deepEqual(autoHide({ open: false, deadline: t0, active: null }, t0 + 99_000), { close: false, wait: null });
});
