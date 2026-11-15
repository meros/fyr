import { test } from "node:test";
import assert from "node:assert/strict";
import { dialogAfterClose } from "../src/keys.ts";

// review 2026-11-15: Escape closes a dialog the way its Close button does; the project screen opened from Settings returns there.
test("closing the project screen returns to where it was opened from", () => {
  assert.equal(dialogAfterClose("project", "settings"), "settings");
  assert.equal(dialogAfterClose("project", null), null);
  assert.equal(dialogAfterClose("snooze", "settings"), null, "other dialogs close to nothing");
  assert.equal(dialogAfterClose("settings", "settings"), null);
});
