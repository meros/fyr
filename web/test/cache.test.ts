// SPEC 0.5 V0.38: the app keeps the last state and each opened thread's events on the
// device; at start and on open it fetches only the difference.
import { test } from "node:test";
import assert from "node:assert/strict";
import { mergeEventLists, mergeStateDelta, sinceRev } from "../src/cache.ts";
import { thread } from "./helpers.ts";
import type { StateDTO } from "../src/types.ts";

const state = (threads: ReturnType<typeof thread>[], partial = false) => ({ now: "n", user: "owner", threads, actions: [], profiles: [], projects: [], partial }) as unknown as StateDTO & { partial?: boolean };

test("a partial state replaces only the threads it carries", () => {
  const stored = state([thread("a", "seen", "seen", { rev: 1, title: "A" }), thread("b", "seen", "seen", { rev: 2, title: "B" })]);
  const delta = state([thread("b", "done", "needs", { rev: 5, title: "B2" })], true);
  const m = mergeStateDelta(stored, delta);
  assert.deepEqual(m.threads.map((t) => `${t.id}:${t.title}`).sort(), ["a:A", "b:B2"]);
  const full = state([thread("c", "seen", "seen", { rev: 9 })]);
  assert.deepEqual(mergeStateDelta(stored, full).threads.map((t) => t.id), ["c"], "a full state replaces everything");
});

test("the since rev asks a little early, never late", () => {
  const s = state([thread("a", "seen", "seen", { rev: 1794666946312324400 }), thread("b", "seen", "seen", { rev: 1794666946312320000 })]);
  assert.ok(sinceRev(s) < 1794666946312324400 && sinceRev(s) > 1794666946312320000);
  assert.equal(sinceRev(state([])), 0);
});

test("stored and new events merge by seq; a gap drops the stored copy", () => {
  const ev = (seq: number) => ({ seq, at: "", subject: "t", type: "user", data: {} });
  const cached = [ev(1), ev(2), ev(3)];
  assert.deepEqual(mergeEventLists(cached, [ev(3), ev(4)], 400, false).map((e) => e.seq), [1, 2, 3, 4]);
  assert.deepEqual(mergeEventLists(cached, [ev(10), ev(11)], 400, true).map((e) => e.seq), [10, 11], "more than one page new: the stored copy is stale");
  assert.deepEqual(mergeEventLists([ev(1), ev(2), ev(3), ev(4)], [ev(5)], 3, false).map((e) => e.seq), [3, 4, 5], "keeps the newest");
});
