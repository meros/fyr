// V0.119: the one reorder control's pure part.
import { test } from "node:test";
import assert from "node:assert/strict";
import { move, moveBy, moveLabel } from "../src/reorder.ts";

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
