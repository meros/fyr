// V0.snooze-until-wake: which wait of the strip has "Snooze until then".
import { test } from "node:test";
import assert from "node:assert/strict";
import { snoozeUntilWait } from "../src/snooze.ts";
import type { Thread } from "../src/types.ts";

const now = Date.parse("2026-01-10T12:00:00Z");
const at = "2026-01-11T08:00:00Z";
const t = (o: Partial<Thread> = {}) => ({ unavailable: {}, attention: { state: "waiting" }, ...o }) as unknown as Thread;

test("a wait on a future time snoozes until that time", () => {
  assert.equal(snoozeUntilWait(t(), { rule: "w", text: "wakes 09:00", at }, now), at);
});

test("a wait on no time, or on a time that passed, has no snooze", () => {
  assert.equal(snoozeUntilWait(t(), { rule: "w", text: "waits for the run to end" }, now), null);
  assert.equal(snoozeUntilWait(t(), { rule: "w", text: "wakes 09:00", at: "2026-01-10T11:59:00Z" }, now), null);
});

test("a snoozed thread, or one the host does not snooze, has no button", () => {
  assert.equal(snoozeUntilWait(t({ attention: { state: "snoozed" } as Thread["attention"] }), { rule: "w", text: "wakes 09:00", at }, now), null);
  assert.equal(snoozeUntilWait(t({ unavailable: { "thread.snooze": "Settled threads do not snooze" } }), { rule: "w", text: "wakes 09:00", at }, now), null);
});
