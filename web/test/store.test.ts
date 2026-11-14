import { test } from "node:test";
import assert from "node:assert/strict";
import {
  applySnapshot,
  applyThreadPatch,
  flatOrder,
  initialStore,
  liveEvent,
  mergeEvents,
  newer,
  recordUndo,
  sections,
  snoozePresets,
  undoCommands,
  undoKindOf,
  upsertThread,
  UNDO_MS,
} from "../src/store.ts";
import type { StateDTO } from "../src/types.ts";
import { thread } from "./helpers.ts";

const at = (m: number) => new Date(Date.UTC(2026, 10, 14, 9, m)).toISOString();

function snap(threads = [] as ReturnType<typeof thread>[]): StateDTO {
  return { now: at(0), user: "owner", threads, actions: [], profiles: [], projects: [] };
}

test("upsert places threads into sections by attention.section", () => {
  let s = applySnapshot(initialStore(), snap(), Date.parse(at(0)));
  s = upsertThread(s, thread("a", "asking", "needs"));
  s = upsertThread(s, thread("w", "working", "working", { running: true, runStartedAt: at(1) }));
  s = upsertThread(s, thread("z", "snoozed", "snoozed"));
  s = upsertThread(s, thread("arch", "archived", "archived"));
  const sec = sections(s.threads);
  assert.deepEqual(sec.needs.map((t) => t.id), ["a"]);
  assert.deepEqual(sec.working.map((t) => t.id), ["w"]);
  assert.deepEqual(sec.snoozed.map((t) => t.id), ["z"]);
  assert.equal(Object.values(sec).flat().some((t) => t.id === "arch"), false, "archived is hidden");
  // Moving to another section on a later upsert.
  s = upsertThread(s, thread("a", "settled", "settled", { updatedAt: at(5) }));
  const sec2 = sections(s.threads);
  assert.deepEqual(sec2.needs, []);
  assert.deepEqual(sec2.settled.map((t) => t.id), ["a"]);
});

test("needs you sorts by state precedence asking, failed, blocked, done, then newest activity", () => {
  const ts = [
    thread("done-new", "done", "needs", { activityAt: at(50) }),
    thread("blocked", "blocked", "needs", { activityAt: at(40) }),
    thread("ask-old", "asking", "needs", { activityAt: at(1) }),
    thread("failed", "failed", "needs", { activityAt: at(30) }),
    thread("ask-new", "asking", "needs", { activityAt: at(20) }),
    thread("done-old", "done", "needs", { activityAt: at(2) }),
  ];
  const sec = sections(applySnapshot(initialStore(), snap(ts), 0).threads);
  assert.deepEqual(sec.needs.map((t) => t.id), ["ask-new", "ask-old", "failed", "blocked", "done-new", "done-old"]);
});

// SPEC 0.5 V0.26: Working sorts by run start, newest first; seen, snoozed, settled by newest activity
test("working sorts by runStartedAt (newest started first); seen, snoozed, settled by newest activity", () => {
  const ts = [
    thread("w-late", "working", "working", { runStartedAt: at(30), activityAt: at(59) }),
    thread("w-early", "working", "working", { runStartedAt: at(5), activityAt: at(10) }),
    thread("s-old", "seen", "seen", { activityAt: at(1) }),
    thread("s-new", "seen", "seen", { activityAt: at(9) }),
    thread("x-old", "settled", "settled", { activityAt: at(3) }),
    thread("x-new", "settled", "settled", { activityAt: at(8) }),
  ];
  const sec = sections(applySnapshot(initialStore(), snap(ts), 0).threads);
  assert.deepEqual(sec.working.map((t) => t.id), ["w-late", "w-early"]);
  assert.deepEqual(sec.seen.map((t) => t.id), ["s-new", "s-old"]);
  assert.deepEqual(sec.settled.map((t) => t.id), ["x-new", "x-old"]);
  assert.deepEqual(flatOrder(sec), ["w-late", "w-early", "s-new", "s-old", "x-new", "x-old"]);
  assert.deepEqual(flatOrder(sec, { seen: 1 }), ["w-late", "w-early", "s-new", "x-new", "x-old"]);
});

// SPEC 0.5 V0.26: the Working order is stable; activity never moves a row, a waiting thread sorts by its agents' start
test("working order does not follow activity; waiting threads use the earliest running agent start; id breaks ties", () => {
  const task = (id: string, startedAt: string, status: "running" | "completed" = "running") => ({ id, description: id, status, startedAt });
  const ts = [
    thread("w-b", "working", "working", { runStartedAt: at(10), activityAt: at(11) }),
    thread("w-a", "working", "working", { runStartedAt: at(10), activityAt: at(58) }),
    thread("w-new", "working", "working", { runStartedAt: at(20), activityAt: at(21) }),
    thread("wait", "waiting", "working", { runStartedAt: null, activityAt: at(59), tasks: [task("t1", at(30)), task("t2", at(15)), task("t0", at(1), "completed")] }),
  ];
  let s = applySnapshot(initialStore(), snap(ts), 0);
  assert.deepEqual(sections(s.threads).working.map((t) => t.id), ["w-new", "wait", "w-a", "w-b"]);
  // New activity on w-b (a later update) leaves the order as it was.
  s = upsertThread(s, { ...s.threads["w-b"], activityAt: at(59), updatedAt: at(59), activity: "Reading app.tsx" });
  assert.deepEqual(sections(s.threads).working.map((t) => t.id), ["w-new", "wait", "w-a", "w-b"]);
});

test("an older upsert never overwrites a newer thread; an equal one does (time-driven attention)", () => {
  let s = upsertThread(initialStore(), thread("a", "done", "needs", { updatedAt: at(10) }));
  s = upsertThread(s, thread("a", "working", "working", { updatedAt: at(5) }));
  assert.equal(s.threads.a.attention.state, "done");
  s = upsertThread(s, thread("a", "seen", "seen", { updatedAt: at(10) }));
  assert.equal(s.threads.a.attention.state, "seen");
});

test("snapshot keeps a newer stream upsert that arrived first, and drops threads it does not list", () => {
  let s = upsertThread(initialStore(), thread("a", "done", "needs", { updatedAt: at(20) }));
  s = upsertThread(s, thread("gone", "seen", "seen"));
  s = applySnapshot(s, snap([thread("a", "working", "working", { updatedAt: at(10) })]), 0);
  assert.equal(s.threads.a.attention.state, "done");
  assert.equal(s.threads.gone, undefined);
  assert.equal(s.loaded, true);
});

test("events merge by seq; live events only for loaded threads", () => {
  const e = (seq: number, subject = "a") => ({ seq, at: at(seq), subject, type: "user", data: { text: String(seq) } });
  let s = liveEvent(initialStore(), e(1));
  assert.equal(s.events.a, undefined);
  s = mergeEvents(s, "a", [e(3), e(1), e(2)], true);
  s = liveEvent(s, e(2));
  s = liveEvent(s, e(4));
  assert.deepEqual(s.events.a.map((x) => x.seq), [1, 2, 3, 4]);
});

test("undo of settle, archive and snooze sends the inverse within the window", () => {
  const now = 1_000_000;
  for (const [action, inverse] of [
    ["thread.settle", "thread.unsettle"],
    ["thread.archive", "thread.unarchive"],
    ["thread.snooze", "thread.unsnooze"],
  ] as const) {
    const kind = undoKindOf(action)!;
    const u = recordUndo(kind, ["a", "b"], now);
    assert.deepEqual(undoCommands(u, now + 1000), [
      { action: inverse, threadId: "a" },
      { action: inverse, threadId: "b" },
    ]);
    assert.equal(undoCommands(u, now + UNDO_MS), null, "expired after the window");
  }
  assert.equal(undoKindOf("thread.send"), null);
  assert.equal(undoCommands(null, now), null);
  assert.equal(recordUndo("settle", ["a", "b", "c"], now).label, "Settled 3 threads");
});

test("snooze presets: 3 h, tomorrow 06:00, next Monday 06:00", () => {
  const fri = new Date(2026, 10, 13, 15, 30); // Friday
  const [h3, tom, mon] = snoozePresets(fri);
  assert.equal(h3.until.getTime() - fri.getTime(), 3 * 3600_000);
  assert.deepEqual([tom.until.getDate(), tom.until.getHours(), tom.until.getMinutes()], [14, 6, 0]);
  assert.deepEqual([mon.until.getDay(), mon.until.getDate(), mon.until.getHours()], [1, 16, 6]);
  const monday = new Date(2026, 10, 16, 9, 0);
  assert.equal(snoozePresets(monday)[2].until.getDate(), 23, "on a Monday: next week's Monday");
});

// API v0: rev decides which copy wins, also when updatedAt says otherwise.
test("upsert keeps the higher rev", () => {
  const base = thread("r1", "done", "needs");
  const a = { ...base, rev: 10, updatedAt: "2026-11-14T10:00:00Z", title: "new" };
  const b = { ...base, rev: 9, updatedAt: "2026-11-14T11:00:00Z", title: "old" };
  assert.equal(newer(a, b), false);
  assert.equal(newer(b, a), true);
});

// SPEC 0.5 V0.38: a thread patch applies only on the copy it was made against;
// otherwise the client asks for the whole thread and shows nothing wrong meanwhile.
test("thread patches apply on the matching rev only", () => {
  const t0 = thread("t1", "working", "working", { rev: 5, title: "a", activity: "Reading x", tasks: [{ id: "k", description: "d", status: "running" }] });
  let s = upsertThread(initialStore(), t0);
  const r = applyThreadPatch(s, { id: "t1", rev: 6, base: 5, patch: { rev: 6, activity: null, tasks: [{ id: "k", description: "d", status: "completed" }] } });
  assert.equal(r.needFull, false);
  assert.equal(r.store.threads.t1.rev, 6);
  assert.equal(r.store.threads.t1.activity, undefined, "a removed field is gone");
  assert.equal(r.store.threads.t1.tasks?.[0].status, "completed");
  assert.equal(r.store.threads.t1.title, "a");
  const stale = applyThreadPatch(r.store, { id: "t1", rev: 9, base: 8, patch: { title: "b" } });
  assert.equal(stale.needFull, true);
  assert.equal(stale.store.threads.t1.title, "a", "a patch on another base changes nothing");
  assert.equal(applyThreadPatch(r.store, { id: "nope", rev: 1, base: 0, patch: {} }).needFull, true);
});
