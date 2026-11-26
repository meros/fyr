// V0.snooze-until-wake, V0.snooze-every-wait: which wait of the strip has "Snooze until then", and the snoozed row's line.
import { test } from "node:test";
import assert from "node:assert/strict";
import { canSnoozeWait, snoozeLine } from "../src/snooze.ts";
import type { Thread } from "../src/types.ts";

const now = Date.parse("2026-01-10T12:00:00Z");
const at = "2026-01-11T08:00:00Z";
const t = (o: Partial<Thread> = {}) => ({ unavailable: {}, attention: { state: "waiting" }, ...o }) as unknown as Thread;

test("every wait snoozes: on a future time, and on no time", () => {
  assert.equal(canSnoozeWait(t(), { rule: "w", text: "wakes 09:00", at }, now), true);
  assert.equal(canSnoozeWait(t(), { rule: "w", text: "waits for checks ok on PR #1" }, now), true);
});

test("a wait on a time that passed has no snooze: it fires now", () => {
  assert.equal(canSnoozeWait(t(), { rule: "w", text: "wakes 09:00", at: "2026-01-10T11:59:00Z" }, now), false);
});

test("a snoozed thread, or one the host does not snooze, has no button", () => {
  const w = { rule: "w", text: "waits for the run to end" };
  assert.equal(canSnoozeWait(t({ attention: { state: "snoozed" } as Thread["attention"] }), w, now), false);
  assert.equal(canSnoozeWait(t({ unavailable: { "thread.snoozeUntilWait": "Settled threads do not snooze" } }), w, now), false);
});

test("a thread snoozed until a wait ends says so, not its 24 h cap", () => {
  assert.equal(snoozeLine(t({ view: { snoozeUntil: at, snoozeWait: "waits for checks ok on PR #1" } }), now), "until the wait ends");
});
