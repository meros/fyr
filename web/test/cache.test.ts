// SPEC 0.5 V0.38: the app keeps the last state and each opened thread's events on the
// device; at start and on open it fetches only the difference.
import { test } from "node:test";
import assert from "node:assert/strict";
import { hostSince, mergeEventLists, mergeStateDelta, nextHostState, pruneGone, sinceRev } from "../src/cache.ts";
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

test("the since rev asks a minute early, never late", () => {
  const s = state([thread("a", "seen", "seen", { rev: 1794666946312324400 }), thread("b", "seen", "seen", { rev: 1794666946312320000 })]);
  assert.ok(sinceRev(s) < 1794666946312320000, "never late");
  assert.equal(sinceRev(state([])), 0);
});

// Found live (16 Nov): a settled thread stayed in Needs you on two devices. The stream holds a thread's frame up
// to 100 ms while other threads' frames go out at once, so a device can hold a newer rev of thread B before the
// last frame of thread A. When the stream drops in that window, ?since= from B's rev skipped A's update for good.
test("a thread whose last frame was overtaken by a newer one is asked for again", () => {
  const settledA = 1794833739804779693; // A settled
  const laterB = settledA + 80_000_000; // B changed 80 ms later and reached the device first
  const stored = state([thread("a", "asking", "needs", { rev: settledA - 600_000_000_000 }), thread("b", "seen", "seen", { rev: laterB })]);
  assert.ok(sinceRev(stored) < settledA, `since ${sinceRev(stored)} skips A's frame at ${settledA}`);
});

test("stored and new events merge by seq; a gap drops the stored copy", () => {
  const ev = (seq: number) => ({ seq, at: "", subject: "t", type: "user", data: {} });
  const cached = [ev(1), ev(2), ev(3)];
  assert.deepEqual(mergeEventLists(cached, [ev(3), ev(4)], 400, false).map((e) => e.seq), [1, 2, 3, 4]);
  assert.deepEqual(mergeEventLists(cached, [ev(10), ev(11)], 400, true).map((e) => e.seq), [10, 11], "more than one page new: the stored copy is stale");
  assert.deepEqual(mergeEventLists([ev(1), ev(2), ev(3), ev(4)], [ev(5)], 3, false).map((e) => e.seq), [3, 4, 5], "keeps the newest");
});

// V0.host-ids (decision 0031 2.5, C7): each host's since comes from its own stored state, and its partial answer
// merges into that state, so a host whose clock runs ahead never makes the device skip another host's threads.
test("each host is asked since its own newest rev and its delta merges into its own state", () => {
  const ahead = 1_900_000_000_000_000_000, behind = 1_800_000_000_000_000_000;
  const a = state([thread("t1", "seen", "seen", { rev: ahead })]);
  const b = state([thread("t1", "seen", "seen", { rev: behind, title: "B's t1" }), thread("t2", "seen", "seen", { rev: behind - 5 })]);
  assert.ok(hostSince(b) < behind && hostSince(a) > behind, "B is asked from its own clock, not A's");
  assert.equal(hostSince(null), 0, "nothing stored: the whole state");
  const nb = nextHostState(b, state([thread("t2", "done", "needs", { rev: behind + 9, title: "new" })], true));
  assert.deepEqual(nb.threads.map((t) => `${t.id}:${t.title ?? ""}`).sort(), ["t1:B's t1", "t2:new"], "the delta lands in B's own state; A's t1 is not B's");
  assert.equal((nb as { partial?: boolean }).partial, undefined);
  const first = nextHostState(null, state([thread("t9", "seen", "seen", { rev: 3 })]));
  assert.deepEqual(first.threads.map((t) => t.id), ["t9"]);
});

test("V0.state-prune: stored threads the host no longer lists are dropped; without a list none are", () => {
  const merged = state([thread("a", "seen", "seen", { rev: 1 }), thread("b", "seen", "seen", { rev: 2 }), thread("c", "seen", "seen", { rev: 3 })]);
  assert.deepEqual(pruneGone(merged, ["a", "c", "d"]).threads.map((t) => t.id), ["a", "c"]);
  assert.equal(pruneGone(merged, undefined), merged);
  assert.equal(pruneGone(merged, ["a", "b", "c"]), merged, "nothing gone: the same object");
});
