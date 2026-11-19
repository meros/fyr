// V0.119: the one reorder control's pure part.
import { test } from "node:test";
import assert from "node:assert/strict";
import { dropIndex, edgeSpeed, move, moveBy, moveLabel, shiftOf, slotOffset, stepOf } from "../src/reorder.ts";

const ids = ["a", "b", "c", "d"];

test("move: first, last, same place, out of range", () => {
  assert.deepEqual(move(ids, 0, 3), ["b", "c", "d", "a"]);
  assert.deepEqual(move(ids, 3, 0), ["d", "a", "b", "c"]);
  assert.deepEqual(move(ids, 1, 2), ["a", "c", "b", "d"]);
  assert.deepEqual(move(ids, 2, 2), ids);
  assert.deepEqual(move(ids, -1, 2), ids);
  assert.deepEqual(move(ids, 1, 9), ids);
  assert.notEqual(move(ids, 0, 1), ids, "a new list, the old one untouched");
  assert.deepEqual(ids, ["a", "b", "c", "d"]);
});

test("moveBy stops at the ends", () => {
  assert.deepEqual(moveBy(ids, "b", -1), ["b", "a", "c", "d"]);
  assert.deepEqual(moveBy(ids, "b", 1), ["a", "c", "b", "d"]);
  assert.deepEqual(moveBy(ids, "a", -1), ids);
  assert.deepEqual(moveBy(ids, "d", 1), ids);
  assert.deepEqual(moveBy(ids, "zz", 1), ids);
});

test("the grip's label", () => {
  assert.equal(moveLabel("Opus 5.5", 0, 4), "Move Opus 5.5, 1 of 4");
  assert.equal(moveLabel("X", 1, 4), "Move X, 2 of 4");
});

// V0.reorder-one: the drag's geometry. Three rows of 40 px with 4 px between them: tops 0, 44, 88.
const tops = [0, 44, 88];
const bottoms = [40, 84, 128];
const mids = [20, 64, 108];

test("dropIndex: where the held row lands", () => {
  assert.equal(dropIndex(mids, 0, 20), 0, "not moved");
  assert.equal(dropIndex(mids, 0, 70), 1, "past the second row's middle");
  assert.equal(dropIndex(mids, 0, 500), 2, "below the list: last");
  assert.equal(dropIndex(mids, 2, -50), 0, "above the list: first");
  assert.equal(dropIndex(mids, 2, 60), 1);
});

test("shiftOf: the rows between slide one step, the rest stay", () => {
  // row 0 held over place 2: rows 1 and 2 slide up
  assert.deepEqual([0, 1, 2].map((j) => shiftOf(j, 0, 2, 44)), [0, -44, -44]);
  // row 2 held over place 0: rows 0 and 1 slide down
  assert.deepEqual([0, 1, 2].map((j) => shiftOf(j, 2, 0, 44)), [44, 44, 0]);
  assert.deepEqual([0, 1, 2].map((j) => shiftOf(j, 1, 1, 44)), [0, 0, 0]);
});

test("stepOf and slotOffset: the row's height and gap, and its way to its new place", () => {
  assert.equal(stepOf(tops, bottoms, 0), 44);
  assert.equal(stepOf(tops, bottoms, 2), 44, "the last row takes the gap before it");
  assert.equal(stepOf([0], [40], 0), 40, "one row: no gap");
  assert.equal(slotOffset(tops, bottoms, 0, 2), 88);
  assert.equal(slotOffset(tops, bottoms, 2, 0), -88);
  assert.equal(slotOffset(tops, bottoms, 1, 1), 0);
  // rows of other heights: the held row's bottom meets the bottom of the row it passes
  assert.equal(slotOffset([0, 20, 80], [20, 80, 100], 0, 1), 60);
});

test("edgeSpeed: the list scrolls near its edges only, faster closer", () => {
  assert.equal(edgeSpeed(300, 0, 600), 0);
  assert.ok(edgeSpeed(2, 0, 600) < 0 && edgeSpeed(2, 0, 600) < edgeSpeed(40, 0, 600));
  assert.ok(edgeSpeed(598, 0, 600) > 0);
  assert.equal(edgeSpeed(-100, 0, 600), -16, "past the edge: full speed, no more");
  assert.equal(edgeSpeed(50, 0, 0), 0, "a scroller with no height");
});
