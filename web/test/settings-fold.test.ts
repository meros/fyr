// V0.automation-folds: the settings folds a device remembers.
import { test } from "node:test";
import assert from "node:assert/strict";
import { foldOpen, parseFolds } from "../src/settings-fold.ts";

test("parseFolds keeps booleans and drops the rest", () => {
  assert.deepEqual(parseFolds(null), {});
  assert.deepEqual(parseFolds("not json"), {});
  assert.deepEqual(parseFolds("[true]"), {});
  assert.deepEqual(parseFolds('{"h:jobs":true,"h:guard":false,"x":1}'), { "h:jobs": true, "h:guard": false });
});

test("foldOpen uses the device's choice, else the default", () => {
  assert.equal(foldOpen({}, "h:jobs", false), false);
  assert.equal(foldOpen({}, "h:automerge", true), true);
  assert.equal(foldOpen({ "h:jobs": true }, "h:jobs", false), true);
  assert.equal(foldOpen({ "h:automerge": false }, "h:automerge", true), false);
});
