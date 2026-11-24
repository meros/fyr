// V0.list-motion: the plan for one change of the inbox list.
import assert from "node:assert/strict";
import test from "node:test";
import { FILTER_ENTER_DELAY_MS, FILTER_WAIT_MS, MAX_CHANGED, planLayout, STAGGER_MAX_MS, STAGGER_MS, type Snap } from "../src/list-motion.ts";
import { LIST_SPRING, unitPath } from "../src/motion.ts";

const view = { top: 0, bottom: 800 };
const o = { filter: false, view, moveMs: 320 };
const m = (e: Record<string, Snap>) => new Map(Object.entries(e));

test("bands, group labels and rows all move, on one plan: nothing snaps while its neighbours glide", () => {
  const before = m({ "sec:needs": { top: 0, height: 28 }, "gh:Unblock": { top: 32, height: 20 }, "row:a": { top: 52, height: 60, group: "needs" }, "sec:working": { top: 120, height: 28 } });
  const after = m({ "sec:needs": { top: 0, height: 28 }, "sec:working": { top: 40, height: 28 }, "row:a": { top: 72, height: 40, group: "working" } });
  const p = planLayout(before, after, o);
  assert.deepEqual(
    p.moves.map((x) => [x.key, x.dy]),
    [
      ["sec:working", 80],
      ["row:a", -20],
    ],
  );
  assert.deepEqual(p.exits, [{ key: "gh:Unblock", kind: "drop" }]);
  // the row changed section: it is lifted over the band it crosses
  assert.equal(p.moves.find((x) => x.key === "row:a")?.lift, true);
  assert.equal(p.moves.find((x) => x.key === "sec:working")?.lift, false);
});

test("an arrival fades in a moment after its neighbours start to make room", () => {
  const p = planLayout(m({ "row:a": { top: 0, height: 40 } }), m({ "row:b": { top: 0, height: 40 }, "row:a": { top: 40, height: 40 } }), o);
  assert.deepEqual(p.enters, [{ key: "row:b", delay: 60 }]);
  assert.deepEqual(p.moves[0], { key: "row:a", dy: -40, delay: 0, lift: false });
});

test("a row that goes into a folded section tucks into its band, and the band's count ticks when it arrives", () => {
  const before = m({ "row:a": { top: 100, height: 40, group: "seen" }, "sec:settled": { top: 200, height: 28 }, "n:settled": { top: 206, height: 16, text: "527" } });
  const after = m({ "sec:settled": { top: 160, height: 28 }, "n:settled": { top: 166, height: 16, text: "528" } });
  const p = planLayout(before, after, { ...o, tuckTo: (k) => (k === "row:a" ? "sec:settled" : null) });
  assert.deepEqual(p.exits, [{ key: "row:a", kind: "tuck", to: "sec:settled", dy: 174 - 120 }]);
  assert.deepEqual(p.rolls, [{ key: "n:settled", from: "527", to: "528", up: true, delay: 224 }]);
});

test("a count rolls down when it falls, early in the move; counts never move by themselves", () => {
  const p = planLayout(m({ "n:working": { top: 10, height: 16, text: "4" } }), m({ "n:working": { top: 50, height: 16, text: "3" } }), o);
  assert.deepEqual(p.moves, []);
  assert.deepEqual(p.rolls, [{ key: "n:working", from: "4", to: "3", up: false, delay: 80 }]);
});

test("V0.list-motion (7): a filter fades out in place, waits, moves, then fades in from the top down", () => {
  const before = m({ "row:x": { top: 0, height: 40 }, "row:a": { top: 40, height: 40 } });
  const after = m({ "row:a": { top: 0, height: 40 }, "row:b": { top: 40, height: 40 }, "row:c": { top: 80, height: 40 }, empty: { top: 120, height: 60 } });
  const p = planLayout(before, after, { ...o, filter: true, tuckTo: () => "sec:settled" });
  assert.deepEqual(p.exits, [{ key: "row:x", kind: "fade" }]);
  assert.deepEqual(p.moves, [{ key: "row:a", dy: 40, delay: FILTER_WAIT_MS, lift: false }]);
  assert.deepEqual(
    p.enters.map((e) => e.delay),
    [FILTER_ENTER_DELAY_MS, FILTER_ENTER_DELAY_MS + STAGGER_MS, FILTER_ENTER_DELAY_MS + STAGGER_MAX_MS],
  );
});

test("the stagger of a filter ends by its cap however many rows come in", () => {
  const after = m(Object.fromEntries(Array.from({ length: 30 }, (_, i) => [`row:${i}`, { top: i * 20, height: 20 }])));
  const p = planLayout(m({}), after, { ...o, filter: true });
  assert.equal(Math.max(...p.enters.map((e) => e.delay)), FILTER_ENTER_DELAY_MS + STAGGER_MAX_MS);
});

test("too many changes at once: the list cross-fades as a whole and nothing moves", () => {
  const after = m(Object.fromEntries(Array.from({ length: MAX_CHANGED + 1 }, (_, i) => [`row:${i}`, { top: i * 10, height: 10 }])));
  const p = planLayout(m({}), after, o);
  assert.equal(p.whole, true);
  assert.deepEqual([p.moves, p.enters, p.exits], [[], [], []]);
});

test("elements off the screen are not animated, and a long move starts just past the edge", () => {
  const before = m({ "row:far": { top: 2000, height: 40 }, "row:in": { top: 3000, height: 40 }, "row:gone": { top: 5000, height: 40 } });
  const after = m({ "row:far": { top: 2400, height: 40 }, "row:in": { top: 100, height: 40 } });
  const p = planLayout(before, after, o);
  assert.deepEqual(p.moves, [{ key: "row:in", dy: 800, delay: 0, lift: false }]);
  assert.deepEqual(p.exits, []);
});

test("the list spring lands without overshoot, within half a second", () => {
  const u = unitPath(LIST_SPRING);
  assert.equal(u[0], 1);
  assert.equal(u[u.length - 1], 0);
  assert.ok(Math.min(...u) >= -0.005);
  assert.ok(((u.length - 1) / 60) * 1000 <= 500);
});

test("a fold drops its rows: a row tucks only into another section's band", () => {
  const before = m({ "sec:working": { top: 0, height: 28 }, "row:a": { top: 30, height: 40, group: "working" } });
  const after = m({ "sec:working": { top: 0, height: 28 } });
  const p = planLayout(before, after, { ...o, tuckTo: () => "sec:working" });
  assert.deepEqual(p.exits, [{ key: "row:a", kind: "drop" }]);
});
