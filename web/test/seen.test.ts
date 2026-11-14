import { test } from "node:test";
import assert from "node:assert/strict";
import { initialTracker, markSent, markedUnseen, openView, setAtEnd, setWindowFocus, shouldMarkSeen, SEEN_DELAY_MS } from "../src/seen.ts";
import { thread } from "./helpers.ts";

const done = thread("a", "done", "needs", { activityAt: "2026-11-14T09:00:00Z" });
const T0 = 10_000;

test("marks seen only after 1.5 s open AND scrolled to the end", () => {
  let tr = openView(initialTracker(), "a", "open", T0);
  assert.equal(shouldMarkSeen(tr, done, T0 + 2000), false, "not at the end");
  tr = setAtEnd(tr, true);
  assert.equal(shouldMarkSeen(tr, done, T0 + SEEN_DELAY_MS - 1), false, "too early");
  assert.equal(shouldMarkSeen(tr, done, T0 + SEEN_DELAY_MS), true);
  tr = setAtEnd(tr, false);
  assert.equal(shouldMarkSeen(tr, done, T0 + 5000), false, "scrolled away");
});

test("Space peek never marks seen", () => {
  let tr = setAtEnd(openView(initialTracker(), "a", "peek", T0), true);
  assert.equal(shouldMarkSeen(tr, done, T0 + 60_000), false);
  // Opening the peeked thread starts the 1.5 s clock now.
  tr = openView(tr, "a", "open", T0 + 60_000);
  assert.equal(shouldMarkSeen(tr, done, T0 + 60_500), false);
  assert.equal(shouldMarkSeen(tr, done, T0 + 61_500), true);
});

test("only for attention state done, once per result", () => {
  let tr = setAtEnd(openView(initialTracker(), "a", "open", T0), true);
  assert.equal(shouldMarkSeen(tr, thread("a", "asking", "needs"), T0 + 5000), false);
  assert.equal(shouldMarkSeen(tr, thread("a", "failed", "needs"), T0 + 5000), false);
  assert.equal(shouldMarkSeen(tr, thread("b", "done", "needs"), T0 + 5000), false, "other thread");
  tr = markSent(tr, done);
  assert.equal(shouldMarkSeen(tr, done, T0 + 5000), false, "already sent");
  const next = { ...done, activityAt: "2026-11-14T09:30:00Z" };
  assert.equal(shouldMarkSeen(tr, next, T0 + 5000), true, "a new result");
});

test("window blur stops the clock; refocus restarts it", () => {
  let tr = setAtEnd(openView(initialTracker(), "a", "open", T0), true);
  tr = setWindowFocus(tr, false, T0 + 500);
  assert.equal(shouldMarkSeen(tr, done, T0 + 5000), false);
  tr = setWindowFocus(tr, true, T0 + 5000);
  assert.equal(shouldMarkSeen(tr, done, T0 + 6000), false);
  assert.equal(shouldMarkSeen(tr, done, T0 + 6500), true);
});

test("after mark unseen, no mark seen until the view is left and opened again", () => {
  let tr = markedUnseen(setAtEnd(openView(initialTracker(), "a", "open", T0), true));
  assert.equal(shouldMarkSeen(tr, done, T0 + 10_000), false);
  tr = openView(tr, "b", "open", T0 + 10_000);
  tr = setAtEnd(openView(tr, "a", "open", T0 + 11_000), true);
  assert.equal(shouldMarkSeen(tr, done, T0 + 13_000), true);
});

test("unavailable markSeen is respected", () => {
  const tr = setAtEnd(openView(initialTracker(), "a", "open", T0), true);
  assert.equal(shouldMarkSeen(tr, { ...done, unavailable: { "thread.markSeen": "No result" } }, T0 + 5000), false);
});
