import { test } from "node:test";
import assert from "node:assert/strict";
import { keyHints } from "../src/keys.ts";

// SPEC 0.5 V0.67: the key hint names as many snooze keys as the menu shows.
test("SPEC 0.5 V0.67: the snooze hint counts the presets shown", () => {
  const c = { dialog: "snooze", zone: "list", hasSelection: true, picked: 0, view: null, card: null, cardMulti: false, running: false, failed: false, undo: false, inText: false } as const;
  assert.deepEqual(keyHints({ ...c, snoozeCount: 5 })[0], ["1–5", "snooze"]);
  assert.deepEqual(keyHints({ ...c, snoozeCount: 4 })[0], ["1–4", "snooze"]);
});
