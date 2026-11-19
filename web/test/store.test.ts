import { test } from "node:test";
import assert from "node:assert/strict";
import {
  applySnapshot,
  applyThreadPatch,
  flatOrder,
  initialStore,
  lastSeq,
  resumed,
  resumeSeq,
  setConn,
  liveEvent,
  matchesQuery,
  mergeEvents,
  newer,
  recordUndo,
  sections,
  unpinned,
  bulkSettle,
  bulkSettleText,
  presetForKey,
  snoozePresets,
  stateForCache,
  undoCommands,
  undoKindOf,
  undoVerb,
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

// V0.stable-groups: by group (Answer before Unblock), then oldest first inside a group, whatever the state.
test("needs you sorts by group, then oldest first inside the group", () => {
  const ts = [
    thread("done-new", "done", "needs", { activityAt: at(50) }),
    thread("blocked", "blocked", "needs", { activityAt: at(40) }),
    thread("ask-old", "asking", "needs", { activityAt: at(1) }),
    thread("failed", "failed", "needs", { activityAt: at(30) }),
    thread("ask-new", "asking", "needs", { activityAt: at(20) }),
    thread("done-old", "done", "needs", { activityAt: at(2) }),
  ];
  const sec = sections(applySnapshot(initialStore(), snap(ts), 0).threads);
  assert.deepEqual(sec.needs.map((t) => t.id), ["ask-old", "ask-new", "done-old", "failed", "blocked", "done-new"]);
});

// SPEC V0.205 (4): a thread whose asks are all merge asks sorts below questions, above failures.
test("needs you puts merge asks after questions and before failures", () => {
  const merge = { id: "m", kind: "effect", effect: { action: "pr.merge", args: {} } } as const;
  const ask = { id: "q", kind: "ask" } as const;
  const ts = [
    thread("merge-new", "asking", "needs", { activityAt: at(50), decisions: [merge] }),
    thread("failed", "failed", "needs", { activityAt: at(40) }),
    thread("question-old", "asking", "needs", { activityAt: at(1), decisions: [ask] }),
    thread("both", "asking", "needs", { activityAt: at(2), decisions: [merge, ask] }),
  ];
  const sec = sections(applySnapshot(initialStore(), snap(ts), 0).threads);
  assert.deepEqual(sec.needs.map((t) => t.id), ["question-old", "both", "merge-new", "failed"]);
});

// SPEC 0.5 V0.26, V0.stable-groups: Working sorts by run start, oldest first; seen, snoozed, settled by newest activity
test("working sorts by runStartedAt (oldest started first); seen, snoozed, settled by newest activity", () => {
  const ts = [
    thread("w-late", "working", "working", { runStartedAt: at(30), activityAt: at(59) }),
    thread("w-early", "working", "working", { runStartedAt: at(5), activityAt: at(10) }),
    thread("s-old", "seen", "seen", { activityAt: at(1) }),
    thread("s-new", "seen", "seen", { activityAt: at(9) }),
    thread("x-old", "settled", "settled", { activityAt: at(3) }),
    thread("x-new", "settled", "settled", { activityAt: at(8) }),
  ];
  const sec = sections(applySnapshot(initialStore(), snap(ts), 0).threads);
  assert.deepEqual(sec.working.map((t) => t.id), ["w-early", "w-late"]);
  assert.deepEqual(sec.seen.map((t) => t.id), ["s-new", "s-old"]);
  assert.deepEqual(sec.settled.map((t) => t.id), ["x-new", "x-old"]);
  assert.deepEqual(flatOrder(sec), ["s-new", "s-old", "w-early", "w-late", "x-new", "x-old"]);
  assert.deepEqual(flatOrder(sec, { seen: 1 }), ["s-new", "w-early", "w-late", "x-new", "x-old"]);
});

// V0.104: pinned threads sort above every other thread, in the normal order among themselves (section order, then the
// section's own order). They stay in their section's list (counts, badges) and the lists draw them once, on top.
test("pinned threads come first in the flat order and show once", () => {
  const pin = { pinnedAt: at(0) };
  const ts = [
    thread("ask", "asking", "needs", { activityAt: at(5) }),
    thread("ask-pin", "asking", "needs", { activityAt: at(1), ...pin }),
    thread("w-pin", "working", "working", { runStartedAt: at(9), ...pin }),
    thread("w", "working", "working", { runStartedAt: at(20) }),
    thread("s-pin-old", "seen", "seen", { activityAt: at(2), ...pin }),
    thread("s-pin-new", "seen", "seen", { activityAt: at(8), ...pin }),
    thread("s", "seen", "seen", { activityAt: at(30) }),
  ];
  const sec = sections(applySnapshot(initialStore(), snap(ts), 0).threads);
  assert.deepEqual(sec.pinned.map((t) => t.id), ["ask-pin", "s-pin-new", "s-pin-old", "w-pin"]);
  assert.deepEqual(sec.needs.map((t) => t.id), ["ask-pin", "ask"], "a pinned thread still counts in its section");
  assert.deepEqual(unpinned(sec.needs).map((t) => t.id), ["ask"]);
  assert.deepEqual(flatOrder(sec), ["ask-pin", "s-pin-new", "s-pin-old", "w-pin", "ask", "s", "w"]);
  assert.deepEqual(flatOrder(sec, { seen: 0 }), ["ask-pin", "s-pin-new", "s-pin-old", "w-pin", "ask", "w"], "a closed section keeps its pinned rows on top");
});

// SPEC 0.5 V0.26, V0.stable-groups: the Working order is stable, oldest started first; activity never moves a row, a waiting thread sorts by its agents' start
test("working order does not follow activity; waiting threads use the earliest running agent start; id breaks ties", () => {
  const task = (id: string, startedAt: string, status: "running" | "completed" = "running") => ({ id, description: id, status, startedAt });
  const ts = [
    thread("w-b", "working", "working", { runStartedAt: at(10), activityAt: at(11) }),
    thread("w-a", "working", "working", { runStartedAt: at(10), activityAt: at(58) }),
    thread("w-new", "working", "working", { runStartedAt: at(20), activityAt: at(21) }),
    thread("wait", "waiting", "working", { runStartedAt: null, activityAt: at(59), tasks: [task("t1", at(30)), task("t2", at(15)), task("t0", at(1), "completed")] }),
  ];
  let s = applySnapshot(initialStore(), snap(ts), 0);
  assert.deepEqual(sections(s.threads).working.map((t) => t.id), ["w-a", "w-b", "wait", "w-new"]);
  // New activity on w-b (a later update) leaves the order as it was.
  s = upsertThread(s, { ...s.threads["w-b"], activityAt: at(59), updatedAt: at(59), activity: "Reading app.tsx" });
  assert.deepEqual(sections(s.threads).working.map((t) => t.id), ["w-a", "w-b", "wait", "w-new"]);
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

// SPEC 0.5 V0.67: one snooze list for desktop and phone, each with its time; keys 1–5 pick in the order shown.
test("SPEC 0.5 V0.67: one snooze list, in order, with times", () => {
  const morning = snoozePresets(new Date(2026, 10, 14, 9, 41)); // a Saturday
  assert.deepEqual(morning.map((p) => p.label), ["In 1 hour", "In 3 hours", "This evening", "Tomorrow morning", "Monday morning"]);
  assert.deepEqual(morning.map((p) => p.when), ["10:41", "12:41", "today 18:00", "Sun 06:00", "Mon 06:00"]);
  const late = snoozePresets(new Date(2026, 10, 14, 19, 0));
  assert.deepEqual(late.map((p) => p.label), ["In 1 hour", "In 3 hours", "Tomorrow morning", "Monday morning"]);
  const monday = new Date(2026, 10, 16, 9, 0);
  assert.equal(snoozePresets(monday).at(-1)!.until.getDate(), 23, "on a Monday: next week's Monday");
});

test("SPEC 0.5 V0.67: keys 1–5 pick the preset shown at that place", () => {
  const morning = snoozePresets(new Date(2026, 10, 14, 9, 41));
  assert.equal(presetForKey(morning, "3")?.label, "This evening");
  assert.equal(presetForKey(morning, "5")?.label, "Monday morning");
  const late = snoozePresets(new Date(2026, 10, 14, 19, 0));
  assert.equal(presetForKey(late, "4")?.label, "Monday morning", "without This evening 4 is the last");
  assert.equal(presetForKey(late, "5"), null);
  assert.equal(presetForKey(late, "0"), null);
  assert.equal(presetForKey(late, "x"), null);
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

// review 2026-11-15: events that arrive between a new subscription and conn → live must not hide the gap before them.
test("a reconnect refetches each loaded timeline from where it was when the stream dropped", () => {
  const ev = (seq: number, subject = "a") => ({ seq, subject, type: "agent", at: at(0) }) as unknown as import("../src/types.ts").FyrEvent;
  let s = applySnapshot(initialStore(), snap([thread("a", "seen", "seen"), thread("b", "seen", "seen")]), Date.parse(at(0)));
  s = setConn(s, "live");
  s = mergeEvents(s, "a", [ev(1), ev(2)], true);
  s = mergeEvents(s, "b", [ev(7, "b")], true);
  s = setConn(s, "offline");
  s = setConn(s, "connecting");
  // Seq 3–4 were missed; 5 arrives on the new stream before the snapshot.
  s = liveEvent(s, ev(5));
  assert.equal(lastSeq(s, "a"), 5);
  s = setConn(s, "live");
  assert.equal(resumeSeq(s, "a"), 2, "the refetch starts before the gap");
  assert.equal(resumeSeq(s, "b"), 7);
  s = resumed(s, "a", 2);
  assert.equal(resumeSeq(s, "a"), 5, "after the refetch the newest seq again");
  assert.equal(resumeSeq(s, "b"), 7);
  // A second drop before b is refetched keeps b's older point.
  s = mergeEvents(s, "b", [ev(9, "b")]);
  s = setConn(s, "offline");
  assert.equal(resumeSeq(s, "b"), 7);
});

// review 2026-11-15: the device's copy of the state keeps the host list and the deploy, so an offline start has them.
test("the stored state round-trips hosts and deploy", () => {
  const live = applySnapshot(initialStore(), { ...snap([thread("a", "seen", "seen")]), host: "front", hosts: [{ name: "front", online: true }, { name: "nas", online: false }], deploy: { phase: "build" } }, Date.parse(at(0)));
  const stored = stateForCache(live, at(1));
  const offline = applySnapshot(initialStore(), stored, Date.parse(at(2)));
  assert.deepEqual(offline.hosts.map((h) => h.name), ["front", "nas"]);
  assert.equal(offline.deploy?.phase, "build");
  assert.equal(offline.host, "front");
  assert.deepEqual(Object.keys(offline.threads), ["a"]);
});

// review 2026-11-15: one search match for the desktop list and the phone search (title, project, #PR).
test("matchesQuery finds a thread by title, project or PR number", () => {
  const t = thread("a", "seen", "seen", { title: "Fix Login", project: "web", pr: { number: 42 } as never });
  assert.equal(matchesQuery(t, "login"), true);
  assert.equal(matchesQuery(t, " WEB "), true);
  assert.equal(matchesQuery(t, "#42"), true);
  assert.equal(matchesQuery(t, "#4"), true);
  assert.equal(matchesQuery(t, "api"), false);
  assert.equal(matchesQuery(thread("b", "seen", "seen"), "#"), false, "no PR, no #");
});

// review 2026-11-15: one verb for the undo toast, desktop and phone.
test("undoVerb names the deferred action", () => {
  assert.equal(undoVerb("settle"), "Settled");
  assert.equal(undoVerb("archive"), "Archived");
  assert.equal(undoVerb("snooze"), "Snoozed");
});

// V0.162: a bulk settle skips pinned threads (no settle, no unpin) and says how many it kept.
test("a bulk settle leaves pinned threads out and counts them", () => {
  const pin = { pinnedAt: at(0) };
  const ts = [
    thread("a", "seen", "seen"),
    thread("b", "seen", "seen", pin),
    thread("c", "seen", "seen", { unavailable: { "thread.settle": "Already settled" } }),
    thread("d", "done", "needs"),
  ];
  assert.deepEqual(bulkSettle(ts), { ids: ["a", "d"], kept: 1, unread: 0, running: 0 });
  assert.deepEqual(bulkSettle([ts[1]]), { ids: [], kept: 1, unread: 0, running: 0 });
  assert.equal(bulkSettleText(7, 1), "Settled 7 threads; 1 pinned thread kept");
  assert.equal(bulkSettleText(1, 2), "Settled; 2 pinned threads kept");
  assert.equal(bulkSettleText(0, 1), "Nothing settled; 1 pinned thread kept");
  assert.equal(bulkSettleText(3, 0), "Settled 3 threads");
  // V0.203: a bulk settle never stops a run
  assert.deepEqual(bulkSettle([thread("r", "working", "working", { running: true })]), { ids: [], kept: 0, unread: 0, running: 1 });
  assert.equal(bulkSettleText(2, 0, 0, 1), "Settled 2 threads; 1 still running");
});

// SPEC V0.210 (4): a question Fyr read in text comes after the causes that are facts, whatever its age.
test("needs you puts a question read in text last", () => {
  const q = thread("q", "blocked", "needs", { activityAt: at(50), attention: { state: "blocked", section: "needs", reason: "Which?" }, textAsk: { question: "Which?", at: at(50), run: 1 } });
  const hard = thread("b", "blocked", "needs", { activityAt: at(1), attention: { state: "blocked", section: "needs", reason: "CI failed" } });
  const ask = thread("a", "asking", "needs", { activityAt: at(2) });
  assert.deepEqual(sections({ q, b: hard, a: ask }).needs.map((t) => t.id), ["a", "b", "q"]);
});
