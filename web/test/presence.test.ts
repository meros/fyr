import { test } from "node:test";
import assert from "node:assert/strict";
import { presenceTarget, shouldBeat } from "../src/presence.ts";

// SPEC 0.5 V0.20: report the open thread only while the page is visible.
test("presence target", () => {
  assert.equal(presenceTarget("t1", "visible"), "t1");
  assert.equal(presenceTarget("t1", "hidden"), "");
  assert.equal(presenceTarget(null, "visible"), "");
});

test("beat on change, else every 20 s while visible", () => {
  assert.equal(shouldBeat({ last: "", visible: true, at: 0 }, "t1", true, 1000), true);
  assert.equal(shouldBeat({ last: "t1", visible: true, at: 0 }, "t1", true, 19_000), false);
  assert.equal(shouldBeat({ last: "t1", visible: true, at: 0 }, "t1", true, 20_000), true);
  assert.equal(shouldBeat({ last: "t1", visible: true, at: 0 }, "", false, 1000), true, "stopping is sent at once");
  // V0.push-summary-batch: the inbox on screen beats too, so the front knows the owner is here.
  assert.equal(shouldBeat({ last: "", visible: true, at: 0 }, "", true, 19_000), false);
  assert.equal(shouldBeat({ last: "", visible: true, at: 0 }, "", true, 20_000), true);
  assert.equal(shouldBeat({ last: "", visible: false, at: 0 }, "", true, 1000), true, "coming back to the inbox is sent at once");
  assert.equal(shouldBeat({ last: "", visible: true, at: 0 }, "", false, 1000), true, "hiding the inbox is sent at once");
  assert.equal(shouldBeat({ last: "", visible: false, at: 0 }, "", false, 30_000), false, "hidden: no beats");
});
