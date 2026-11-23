// SPEC V0.119 (3): "Saved" shows for 2 s after a command answered ok, on a fake clock (node:test's mock timers), so the
// check does not wait the wall time out (V0.single-sources; the layout test only sees it come and go).
import { test } from "node:test";
import assert from "node:assert/strict";
import { SAVED_MS, savedClock } from "../src/saved.ts";

test("Saved shows at a mark and hides 2 s after the newest one", (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const seen: boolean[] = [];
  const c = savedClock((v) => seen.push(v));
  assert.equal(SAVED_MS, 2000);
  c.mark();
  assert.deepEqual(seen, [true]);
  t.mock.timers.tick(1999);
  assert.deepEqual(seen, [true], "still shown just before 2 s");
  t.mock.timers.tick(1);
  assert.deepEqual(seen, [true, false]);
  // A second save restarts the 2 s.
  c.mark();
  t.mock.timers.tick(1500);
  c.mark();
  t.mock.timers.tick(1999);
  assert.deepEqual(seen, [true, false, true, true]);
  t.mock.timers.tick(1);
  assert.deepEqual(seen, [true, false, true, true, false]);
});

test("a page that closes drops the pending hide", (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const seen: boolean[] = [];
  const c = savedClock((v) => seen.push(v));
  c.mark();
  c.stop();
  t.mock.timers.tick(SAVED_MS * 2);
  assert.deepEqual(seen, [true]);
});
