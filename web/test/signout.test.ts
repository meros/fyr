// V0.157: a revoked device reloads into the signed-out page, once.
import { test } from "node:test";
import assert from "node:assert/strict";
import { isSignedOut, mayReload } from "../src/signout.ts";

test("only the front's signed-out 401 counts", () => {
  assert.equal(isSignedOut(401, { ok: false, signedOut: true }), true);
  assert.equal(isSignedOut(401, { ok: false }), false);
  assert.equal(isSignedOut(403, { signedOut: true }), false);
  assert.equal(isSignedOut(401, null), false);
  assert.equal(isSignedOut(200, { signedOut: true }), false);
});

test("a reload at most once in 10 s", () => {
  assert.equal(mayReload(null, 1000), true);
  assert.equal(mayReload(1000, 5000), false);
  assert.equal(mayReload(1000, 11_001), true);
});
