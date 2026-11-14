import { test } from "node:test";
import assert from "node:assert/strict";
import { presenceTarget, shouldBeat } from "../src/presence.ts";

// SPEC 0.5 V0.20: report the open thread only while the page is visible.
test("presence target", () => {
  assert.equal(presenceTarget("t1", "visible"), "t1");
  assert.equal(presenceTarget("t1", "hidden"), "");
  assert.equal(presenceTarget(null, "visible"), "");
});

test("beat on change, else every 20 s", () => {
  assert.equal(shouldBeat({ last: "", at: 0 }, "t1", 1000), true);
  assert.equal(shouldBeat({ last: "t1", at: 0 }, "t1", 19_000), false);
  assert.equal(shouldBeat({ last: "t1", at: 0 }, "t1", 20_000), true);
  assert.equal(shouldBeat({ last: "t1", at: 0 }, "", 1000), true, "stopping is sent at once");
  assert.equal(shouldBeat({ last: "", at: 0 }, "", 30_000), false, "nothing shown: no beats");
});
