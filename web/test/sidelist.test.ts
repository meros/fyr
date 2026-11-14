// SPEC 0.5 V0.40: the thread list can be hidden; in a narrow desktop window it starts hidden.
import { test } from "node:test";
import assert from "node:assert/strict";
import { LIST_MIN_WIDTH, listShown } from "../src/prefs.ts";

test("the list shows by the owner's choice, else by the window width", () => {
  assert.equal(listShown(null, 1440, true), true, "wide: shown");
  assert.equal(listShown(null, LIST_MIN_WIDTH - 1, true), false, "narrow: hidden by default");
  assert.equal(listShown(true, 800, true), true, "the owner's choice wins");
  assert.equal(listShown(false, 1800, true), false);
  assert.equal(listShown(false, 1800, false), true, "no thread open: the list is all there is");
});
