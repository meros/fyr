import { test } from "node:test";
import assert from "node:assert/strict";
import { wakeChip } from "../src/offers.ts";

// V0.wakeups-via-watch (3): the agent's armed wake-up as a chip, in the owner's local time.
const local = (y: number, mo: number, d: number, h: number, mi: number) => new Date(y, mo, d, h, mi).getTime();

test("no wake-up, no chip", () => {
  assert.equal(wakeChip({}, Date.now()), null);
  assert.equal(wakeChip({ wakes: [] }, Date.now()), null);
});

test("today: the time alone; the prompt only in the title (V0.watch-waits)", () => {
  const now = local(2026, 10, 20, 1, 0);
  const at = new Date(local(2026, 10, 20, 6, 0)).toISOString();
  const c = wakeChip({ wakes: [{ rule: "w_1", at, note: "Check the deploy\nthen report on #12" }] }, now)!;
  assert.equal(c.text, "wakes 06:00");
  assert.equal(c.rule, "w_1");
  assert.match(c.title, /then report on #12/, "the full prompt in the title");
});

test("another day: the date too; no prompt: the time alone; more than one: the soonest, the rest counted", () => {
  const now = local(2026, 10, 20, 23, 0);
  const at = new Date(local(2026, 10, 21, 6, 0)).toISOString();
  const later = new Date(local(2026, 10, 22, 6, 0)).toISOString();
  const day = new Date(at).toLocaleDateString(undefined, { day: "numeric", month: "short" });
  const c = wakeChip({ wakes: [{ rule: "w_1", at }, { rule: "w_2", at: later, note: "x" }] }, now)!;
  assert.equal(c.text, `wakes ${day} 06:00 +1`);
  assert.match(c.title, /\+1 more/);
});

test("V0.quota-reset-wake: a quota wake names its reset window", () => {
  const now = local(2026, 10, 20, 14, 0);
  const at = new Date(local(2026, 10, 20, 18, 0)).toISOString();
  const c = wakeChip({ wakes: [{ rule: "w_q", at, provider: "claude", window: "5 h", note: "Continue work" }] }, now)!;
  assert.equal(c.text, "wakes 18:00, when the 5 h window resets");
  assert.equal(c.rule, "w_q");
  assert.match(c.title, /when the 5 h window resets/);
  assert.match(c.title, /Continue work/);
});
