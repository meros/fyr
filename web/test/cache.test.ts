// SPEC 0.5 V0.38: the app keeps the last state and each opened thread's events on the
// device; at start and on open it fetches only the difference.
import { test } from "node:test";
import assert from "node:assert/strict";
import { mergeEventLists, mergeStateDelta, nextHostState } from "../src/cache.ts";
import { chunks, planSync } from "../src/hostsync.ts";
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

// V0.state-index. Found live (16 Nov, under the old ?since= sync): a settled thread stayed in Needs you on two devices,
// because a frame overtaken by a newer one was skipped for good. The diff compares every thread's own rev, so no
// thread can fall between two clocks.
test("a thread whose last frame was overtaken by a newer one is fetched again", () => {
  const settledA = 1794833739804779693;
  const stored = state([thread("a", "asking", "needs", { rev: settledA - 600_000_000_000 }), thread("b", "seen", "seen", { rev: settledA + 80_000_000 })]);
  const plan = planSync(stored, [{ id: "a", rev: settledA, state: "settled" }, { id: "b", rev: settledA + 80_000_000, state: "seen" }]);
  assert.deepEqual(plan.want, ["a"]);
});

test("the plan drops what the host lost, keeps what is current and asks the urgent threads first", () => {
  const stored = state([thread("gone", "seen", "seen", { rev: 1 }), thread("same", "seen", "seen", { rev: 2 }), thread("old", "working", "working", { rev: 3 })]);
  const plan = planSync(stored, [
    { id: "same", rev: 2, state: "seen" },
    { id: "old", rev: 30, state: "done" },
    { id: "new-settled", rev: 50, state: "settled" },
    { id: "new-asking", rev: 10, state: "asking" },
    { id: "new-seen", rev: 40, state: "seen" },
  ]);
  assert.deepEqual(plan.kept.map((t) => t.id).sort(), ["old", "same"], "a thread the host dropped is not kept");
  assert.equal(plan.gone, 1);
  assert.deepEqual(plan.want, ["new-asking", "old", "new-seen", "new-settled"], "asking, then done, then the rest by rev");
  assert.deepEqual(planSync(null, [{ id: "x", rev: 1 }]).want, ["x"], "nothing stored: everything is wanted");
  assert.deepEqual(planSync(stored, [{ id: "same", rev: 2 }, { id: "old", rev: 3 }, { id: "gone", rev: 1 }]).want, [], "nothing changed: nothing to fetch");
});

test("the host's rev wins also when it is older (a restored database)", () => {
  assert.deepEqual(planSync(state([thread("a", "seen", "seen", { rev: 900 })]), [{ id: "a", rev: 100, state: "seen" }]).want, ["a"]);
});

test("a sync in batches", () => {
  assert.deepEqual(chunks([1, 2, 3, 4, 5], 2), [[1, 2], [3, 4], [5]]);
  assert.deepEqual(chunks([], 2), []);
});

test("stored and new events merge by seq; a gap drops the stored copy", () => {
  const ev = (seq: number) => ({ seq, at: "", subject: "t", type: "user", data: {} });
  const cached = [ev(1), ev(2), ev(3)];
  assert.deepEqual(mergeEventLists(cached, [ev(3), ev(4)], 400, false).map((e) => e.seq), [1, 2, 3, 4]);
  assert.deepEqual(mergeEventLists(cached, [ev(10), ev(11)], 400, true).map((e) => e.seq), [10, 11], "more than one page new: the stored copy is stale");
  assert.deepEqual(mergeEventLists([ev(1), ev(2), ev(3), ev(4)], [ev(5)], 3, false).map((e) => e.seq), [3, 4, 5], "keeps the newest");
});

// V0.host-ids (decision 0031 2.5, C7): a host's answer merges into that host's own state, never another's.
test("a host's delta merges into its own state", () => {
  const behind = 1_800_000_000_000_000_000;
  const b = state([thread("t1", "seen", "seen", { rev: behind, title: "B's t1" }), thread("t2", "seen", "seen", { rev: behind - 5 })]);
  const nb = nextHostState(b, state([thread("t2", "done", "needs", { rev: behind + 9, title: "new" })], true));
  assert.deepEqual(nb.threads.map((t) => `${t.id}:${t.title ?? ""}`).sort(), ["t1:B's t1", "t2:new"]);
  assert.equal((nb as { partial?: boolean }).partial, undefined);
  const first = nextHostState(null, state([thread("t9", "seen", "seen", { rev: 3 })]));
  assert.deepEqual(first.threads.map((t) => t.id), ["t9"]);
});
