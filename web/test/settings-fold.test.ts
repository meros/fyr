// V0.automation-folds: the settings folds a device remembers.
import { test } from "node:test";
import assert from "node:assert/strict";
import { foldOpen, readFold, writeFold } from "../src/settings-fold.ts";
import { memoryStorage, withStorage } from "./helpers.ts";

const KEY = "fyr.settingsFold";

test("readFold keeps the stored booleans and reads anything else as the default", () => {
  for (const raw of ["not json", "[true]", "null", '{"h:jobs":1}']) withStorage(memoryStorage({ [KEY]: raw }), () => assert.equal(readFold("h:jobs", true), true, raw));
  withStorage(memoryStorage({ [KEY]: '{"h:jobs":false,"x":1}' }), () => {
    assert.equal(readFold("h:jobs", true), false);
    writeFold("h:guard", true);
    assert.equal(readFold("h:guard", false), true);
    assert.equal(readFold("h:jobs", true), false, "a write keeps the other folds");
  });
});

test("foldOpen uses the device's choice, else the default", () => {
  assert.equal(foldOpen({}, "h:jobs", false), false);
  assert.equal(foldOpen({}, "h:automerge", true), true);
  assert.equal(foldOpen({ "h:jobs": true }, "h:jobs", false), true);
  assert.equal(foldOpen({ "h:automerge": false }, "h:automerge", true), false);
});
