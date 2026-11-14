import { test } from "node:test";
import assert from "node:assert/strict";
import { initialBuildWatch, observeBuildId } from "../src/buildid.ts";

test("same buildId after reconnect: no reload", () => {
  let w = observeBuildId(initialBuildWatch(), "b1").watch;
  const r = observeBuildId(w, "b1");
  assert.equal(r.reload, false);
});

test("different buildId: reload once", () => {
  const w = observeBuildId(initialBuildWatch(), "b1").watch;
  const r = observeBuildId(w, "b2");
  assert.equal(r.reload, true);
  assert.equal(observeBuildId(r.watch, "b3").reload, false, "only once");
});

test("missing buildId: never reload, and it does not count as the first", () => {
  let w = initialBuildWatch();
  for (const missing of [undefined, null, ""]) {
    const r = observeBuildId(w, missing);
    assert.equal(r.reload, false);
    w = r.watch;
  }
  assert.equal(w.first, null);
  w = observeBuildId(w, "b1").watch;
  assert.equal(observeBuildId(w, undefined).reload, false);
});
