// V0.automation-folds: the settings folds a device remembers.
import { test } from "node:test";
import assert from "node:assert/strict";
import { foldNow, storedFold, writeFold } from "../src/settings-fold.ts";
import { memoryStorage, withStorage } from "./helpers.ts";

const KEY = "fyr.settingsFold";

test("storedFold keeps the stored booleans and reads anything else as never pressed", () => {
  for (const raw of ["not json", "[true]", "null", '{"h:jobs":1}']) withStorage(memoryStorage({ [KEY]: raw }), () => assert.equal(storedFold("h:jobs"), undefined, raw));
  withStorage(memoryStorage({ [KEY]: '{"h:jobs":false,"x":1}' }), () => {
    assert.equal(storedFold("h:jobs"), false);
    writeFold("h:guard", true);
    assert.equal(storedFold("h:guard"), true);
    assert.equal(storedFold("h:jobs"), false, "a write keeps the other folds");
  });
});

// V0.settings-ia: a default read once at mount left Setup checks shut when its report came late, and a section a link
// opened could not be closed.
test("V0.settings-ia: foldNow: the press of this visit, then force, then the device, then the default as it is now", () => {
  const base = { touched: null, stored: undefined, byDefault: false };
  assert.equal(foldNow(base), false);
  assert.equal(foldNow({ ...base, byDefault: true }), true, "a default that turns true opens it while the device remembers nothing");
  assert.equal(foldNow({ ...base, stored: false, byDefault: true }), false, "the device's choice wins over the default");
  assert.equal(foldNow({ ...base, stored: false, force: true }), true, "a link that lands on it, or something wrong now, opens it all the same");
  assert.equal(foldNow({ ...base, stored: true, force: true, touched: false }), false, "and one press closes it");
  assert.equal(foldNow({ ...base, touched: true }), true);
  withStorage(memoryStorage({ [KEY]: '{"h:jobs":false}' }), () => {
    assert.equal(storedFold("h:jobs"), false);
    assert.equal(storedFold("h:guard"), undefined, "never pressed here");
  });
});
