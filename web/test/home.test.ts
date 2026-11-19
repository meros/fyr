import { test } from "node:test";
import assert from "node:assert/strict";
import { HOME_LOOK_DEFAULT_MS, HOME_LOOK_MAX_MS, homeLog, homeMoves, homeNeeds, homeRunning, homeSummary, lookWindow, runningLine } from "../src/home.ts";
import { homeUrl } from "../src/desk-history.ts";
import { parseUrl, urlFor } from "../src/nav.ts";
import type { Change, Offer } from "../src/types.ts";
import { thread } from "./helpers.ts";

const at = (m: number) => new Date(Date.UTC(2026, 10, 19, 9, m)).toISOString();
const merge: Offer = { id: "fyr.merge", label: "Merge", run: "fyr", effect: "pr.merge", by: "observed", digest: "d1" } as Offer;
const ch = (p: Partial<Change>): Change => ({ id: "c", key: "k", kind: "code", title: "x", phase: "review", phaseBy: "agent", createdAt: at(0), updatedAt: at(0), ...p });

test("V0.status-home: Your move keeps the inbox's groups, Answer before Approve before Unblock, then the newest", () => {
  const ask = thread("ask", "asking", "needs", { decisions: [{ id: "d", kind: "ask", questions: [{ question: "Which?", options: [] }] }], activityAt: at(1) });
  const fail = thread("fail", "failed", "needs", { failure: { class: "x", message: "boom" }, activityAt: at(9) });
  const old = thread("fail-old", "failed", "needs", { failure: { class: "x", message: "boom" }, activityAt: at(2) });
  const work = thread("work", "working", "working");
  assert.deepEqual(homeNeeds([fail, work, old, ask, ask]).map((t) => t.id), ["ask", "fail", "fail-old"]);
});

test("V0.status-home: a thread with one green PR merges from the status screen; the others open the thread", () => {
  const ready = thread("ready", "blocked", "needs", {
    changes: [ch({ id: "c1", ready: "Merge", pr: { url: "https://github.com/o/r/pull/7", number: 7, head: "abc1234", state: "OPEN" }, offers: [merge], need: { cause: "ready", head: "abc1234" } })],
  });
  const ask = thread("ask", "asking", "needs", { decisions: [{ id: "d", kind: "ask", questions: [{ question: "Which?", options: [] }] }] });
  const broke = thread("broke", "failed", "needs", { failure: { class: "x", message: "boom" } });
  const [m1, m2, m3] = homeMoves([ready, ask, broke], []);
  assert.equal(m1.verb, "Merge");
  assert.deepEqual(m1.merge?.args, { on: "c1", action: "fyr.merge", digest: "d1", head: "abc1234" });
  assert.equal(m2.verb, "Answer");
  assert.equal(m2.merge, undefined);
  assert.equal(m3.verb, "Open");
});

test("V0.status-home: what moves: working threads, running ones first and the longest running first", () => {
  const a = thread("a", "working", "working", { running: true, runStartedAt: at(5), activity: "Running tests" });
  const b = thread("b", "working", "working", { running: true, runStartedAt: at(1) });
  const ci = thread("ci", "working", "working", { attention: { state: "working", section: "working", reason: "Waiting for CI" } });
  const seen = thread("s", "seen", "seen");
  assert.deepEqual(homeRunning([ci, a, seen, b]).map((t) => t.id), ["b", "a", "ci"]);
  assert.equal(runningLine(a), "Running tests");
  assert.equal(runningLine(ci), "Waiting for CI");
});

test("V0.status-home: Since you looked: merged changes, settles and failures after the last look, newest first", () => {
  const since = Date.parse(at(10));
  const t = thread("t", "settled", "settled", {
    title: "Search",
    settledAt: at(30),
    changes: [ch({ id: "c1", title: "fast search", pr: { url: "u", number: 9 }, done: { at: at(20), by: "observed", reason: "merged" } }), ch({ id: "c2", title: "old", done: { at: at(5), by: "observed", reason: "merged" } })],
  });
  const f = thread("f", "failed", "needs", { title: "Build", failure: { class: "x", message: "exit 1", at: at(25) } });
  const arch = thread("z", "archived", "archived", { archivedAt: at(40), settledAt: at(40) });
  const log = homeLog({ t, f, z: arch }, [], since);
  assert.deepEqual(
    log.map((e) => [e.word, e.text]),
    [
      ["Settled", "Search"],
      ["Failed", "Build: exit 1"],
      ["Merged", "#9 fast search"],
    ],
  );
});

test("V0.status-home: the window starts at the last look, a day without one, a week at most", () => {
  const now = Date.parse(at(0));
  assert.equal(lookWindow(null, now), now - HOME_LOOK_DEFAULT_MS);
  assert.equal(lookWindow(now - 3600_000, now), now - 3600_000);
  assert.equal(lookWindow(now - 30 * 86400_000, now), now - HOME_LOOK_MAX_MS);
  assert.equal(lookWindow(now + 1000, now), now - HOME_LOOK_DEFAULT_MS, "a clock from the future is no look");
});

test("V0.status-home: the summary line", () => {
  assert.equal(homeSummary({ running: 3, needs: 2, mergedToday: 5 }), "3 running · 2 need you · 5 merged today");
  assert.equal(homeSummary({ running: 0, needs: 1, mergedToday: 0 }), "1 needs you");
  assert.equal(homeSummary({ running: 0, needs: 0, mergedToday: 2 }), "Nothing running · nothing needs you · 2 merged today");
});

test("V0.status-home: closing the thread drops open and tab from the URL and keeps the rest", () => {
  assert.equal(homeUrl("?open=h/t1&tab=pr&mock=1"), "/?mock=1");
  assert.equal(homeUrl("?open=t1"), "/");
});

test("V0.status-home: the phone's Status screen has its own URL", () => {
  assert.equal(urlFor({ screen: { name: "status" }, sheet: null }), "/?status");
  assert.deepEqual(parseUrl("?status"), { screen: { name: "status" }, sheet: null });
});

test("V0.day-chart: the day counts open changes from midnight: one carried over, one opened and merged, one still open", async () => {
  const { homeDay, openAt, hourTicks } = await import("../src/home.ts");
  const now = new Date(2026, 10, 19, 15, 0).getTime();
  const h = (n: number) => new Date(2026, 10, 19, n, 0).toISOString();
  const t = thread("t", "working", "working", {
    createdAt: h(9),
    changes: [
      ch({ id: "old", title: "old", createdAt: "2026-11-18T10:00:00Z", done: { at: h(11), by: "observed", reason: "merged" }, pr: { url: "u", number: 4 } }),
      ch({ id: "new", title: "new", createdAt: h(10) }),
      ch({ id: "gone", title: "gone", createdAt: "2026-11-17T10:00:00Z", done: { at: "2026-11-17T12:00:00Z", by: "owner", reason: "done" } }),
    ],
  });
  const d = homeDay({ t }, [], now);
  assert.deepEqual(d.steps.map((s) => s.open), [1, 2, 1]);
  assert.equal(d.opened, 1);
  assert.equal(d.finished, 1);
  assert.equal(d.open, 1);
  assert.equal(d.peak, 2);
  assert.equal(d.started, 1);
  assert.deepEqual(d.merges.map((m) => m.text), ["#4 old"]);
  assert.equal(openAt(d, Date.parse(h(10)) + 1), 2);
  assert.equal(hourTicks(d.from, now).length, 4); // 15 h: every 3 h, 03 to 12
});
