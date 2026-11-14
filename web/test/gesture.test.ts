import { test } from "node:test";
import assert from "node:assert/strict";
import { EDGE_PX, idle, longPressCancelled, settleLabel, swipeAvail, swipeEnd, swipeMove, swipeOffset, swipeStart } from "../src/gesture.ts";
import { thread } from "./helpers.ts";

const W = 390;
const ok = { right: { ok: true, label: "Settle" }, left: { ok: true, label: "Snooze…" } };

test("a swipe starts after 12 px under 30 degrees; vertical movement is a scroll", () => {
  let s = swipeStart(100, 300);
  s = swipeMove(s, 108, 301);
  assert.equal(s.phase, "pending", "under 12 px");
  s = swipeMove(s, 115, 303);
  assert.equal(s.phase, "swipe");
  assert.equal(s.dx, 15);
  let v = swipeMove(swipeStart(100, 300), 108, 315);
  assert.equal(v.phase, "scroll", "steep: the list scrolls");
  v = swipeMove(v, 300, 315);
  assert.equal(v.phase, "scroll", "locked: a later horizontal move does not start a swipe");
  assert.equal(swipeMove(swipeStart(100, 300), 112, 308).phase, "scroll", "34 degrees is too steep");
});

test("the left 24 px stay free for the iOS Back edge", () => {
  const s = swipeStart(EDGE_PX - 1, 300);
  assert.equal(s.phase, "edge");
  assert.equal(swipeMove(s, 250, 300).phase, "edge");
  assert.deepEqual(swipeEnd(swipeMove(s, 250, 300), W, ok), { kind: "none" });
});

test("right settles at half the width, left opens snooze at 30 %", () => {
  const right = (dx: number) => swipeEnd(swipeMove(swipeStart(100, 0), 100 + dx, 0), W, ok);
  assert.equal(right(194).kind, "none");
  assert.equal(right(195).kind, "settle");
  const left = (dx: number) => swipeEnd(swipeMove(swipeStart(300, 0), 300 - dx, 0), W, ok);
  assert.equal(left(110).kind, "none");
  assert.equal(left(117).kind, "snooze");
  assert.deepEqual(swipeEnd(idle, W, ok), { kind: "none" }, "a tap is not a swipe");
});

test("an unavailable direction shows the reason, resists and snaps back", () => {
  const asking = thread("a", "asking", "needs", { unavailable: { "thread.settle": "A question is open" } });
  const av = swipeAvail(asking);
  assert.deepEqual(av.right, { ok: false, label: "Answer first" });
  const s = swipeMove(swipeStart(100, 0), 340, 0);
  assert.deepEqual(swipeEnd(s, W, av), { kind: "none", reason: "Answer first" });
  assert.ok(swipeOffset(s, av) <= 96, "rubber band");
  assert.equal(swipeOffset(s, ok), 240);
  const noSnooze = swipeAvail(thread("b", "seen", "seen", { unavailable: { "thread.snooze": "Already snoozed" } }));
  assert.deepEqual(swipeEnd(swipeMove(swipeStart(300, 0), 100, 0), W, noSnooze), { kind: "none", reason: "Already snoozed" });
});

test("the reveal names the exact Action", () => {
  assert.equal(settleLabel(thread("a", "done", "needs")), "Settle");
  assert.equal(settleLabel(thread("a", "working", "working", { running: true })), "Settle when done");
  assert.equal(settleLabel(thread("a", "seen", "seen", { pr: { url: "u", number: 1, state: "OPEN" } })), "Settle when merged");
  assert.equal(settleLabel(thread("a", "seen", "seen", { pr: { url: "u", number: 1, state: "MERGED" } })), "Settle");
  assert.equal(swipeAvail(thread("a", "done", "needs")).right.label, "Settle");
});

test("long press is cancelled by more than 8 px of movement", () => {
  assert.equal(longPressCancelled(0, 0, 5, 5), false);
  assert.equal(longPressCancelled(0, 0, 9, 0), true);
});
