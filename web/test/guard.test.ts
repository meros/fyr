// SPEC V0.188 (decision 0032): the memory guard's chart and words. Pure texts and geometry; the component only prints them.
import { test } from "node:test";
import assert from "node:assert/strict";
import { mockLoad, withGuard } from "../src/mock.ts";
import { GUARD_WINDOW, findGuard, guardChart, guardLine, guardLog, guardSummary } from "../src/guard.ts";
import { rowUndeclared } from "../src/undeclared.ts";
import { withUndeclared } from "../src/mock.ts";

const NOW = Date.parse("2026-11-18T08:00:00");
const title = (t: string) => ({ t_mapperf: "Map perf", t_babysit: "Babysit PR #4170" })[t] ?? t;
const guarded = () => withGuard(mockLoad(), "1", NOW);
const g = () => findGuard(guarded())!.g;

test("G1 the chart has a lane per frozen thread, in the order they first froze, and a band for each short stretch", () => {
  const c = guardChart(g(), NOW, title);
  assert.deepEqual(c.lanes.map((l) => l.title), ["Map perf", "Babysit PR #4170"]);
  assert.equal(c.short.length, 2);
  // the first stretch: 14 min ago to 12 min ago of 15
  assert.ok(Math.abs(c.short[0].a - (1 - 14 / 15)) < 1e-9 && Math.abs(c.short[0].b - (1 - 12 / 15)) < 1e-9);
  // the second is still on: it runs to now
  assert.equal(c.short[1].b, 1);
});

test("G2 a freeze with no thaw yet runs to now and its lane is live; marks carry the freeze's number", () => {
  const c = guardChart(g(), NOW, title);
  const m = c.lanes[0];
  assert.equal(m.live, true);
  assert.deepEqual(m.marks.map((x) => x.n), [1, 2, 3]);
  assert.equal(m.frozen.length, 3);
  assert.equal(m.frozen[2].b, 1);
  assert.equal(c.lanes[1].live, false);
  assert.equal(c.lanes[1].frozen.length, 1);
});

test("G3 everything is a share of the window and inside it", () => {
  const c = guardChart(g(), NOW, title);
  for (const s of [...c.short, ...c.lanes.flatMap((l) => l.frozen)]) assert.ok(s.a >= 0 && s.b <= 1 && s.a < s.b, JSON.stringify(s));
  for (const l of c.lanes) for (const m of l.marks) assert.ok(m.x >= 0 && m.x <= 1);
  assert.equal(GUARD_WINDOW, 15 * 60 * 1000);
});

test("G4 events older than the window are clipped, not drawn outside", () => {
  const old = { ...g(), events: g().events!.map((e) => ({ ...e, at: new Date(Date.parse(e.at) - 10 * 60 * 1000).toISOString() })) };
  const c = guardChart(old, NOW, title);
  for (const s of [...c.short, ...c.lanes.flatMap((l) => l.frozen)]) assert.ok(s.a >= 0 && s.b <= 1);
  for (const l of c.lanes) for (const m of l.marks) assert.ok(m.x >= 0);
});

test("G5 no guard, or nothing in the window, is an empty chart with words", () => {
  assert.deepEqual(guardChart(undefined, NOW, title).lanes, []);
  const quiet = guardChart({ short: false, max: 8, refreezeSec: 60, events: [] }, NOW, title);
  assert.equal(quiet.lanes.length, 0);
  assert.equal(quiet.alt, "Memory was not short in the last 15 minutes; nothing frozen.");
});

test("G6 the text alternative says what the picture says", () => {
  assert.equal(guardChart(g(), NOW, title).alt, "Memory was short 2 times in the last 15 minutes; 2 threads frozen at least once; 1 frozen now.");
});

test("G7 a thread's line: frozen n of max and for how long; the wait to the next pause; let run at the cap", () => {
  assert.equal(guardLine(g(), "t_mapperf", NOW), "paused 3/8 · 2 min");
  assert.equal(guardLine(g(), "t_babysit", NOW), "runs · next pause possible in 24 s");
  const capped = findGuard(withGuard(mockLoad(), "capped", NOW))!.g;
  capped.threads![0].frozen = false;
  assert.equal(guardLine(capped, "t_mapperf", NOW), "let run after 8 pauses");
  assert.equal(guardLine(g(), "t_other", NOW), null);
  assert.equal(guardLine(undefined, "t_mapperf", NOW), null);
});

test("G8 the row of a frozen thread starts with the guard's words; without a guard it keeps V0.164's", () => {
  const withG = withGuard(withUndeclared(mockLoad(), "paused", NOW), "1", NOW);
  assert.equal(rowUndeclared(withG, "t_mapperf", NOW)?.text, "undeclared · paused 3/8 · 2 min");
  const without = withUndeclared(mockLoad(), "paused", NOW);
  assert.equal(rowUndeclared(without, "t_mapperf", NOW)?.text, "undeclared · paused: memory");
});

test("G9 the summary line", () => {
  assert.equal(guardSummary(g(), NOW), "Memory is short (8 min) · 1 thread frozen");
  assert.equal(guardSummary({ short: false, max: 8, refreezeSec: 60, events: [] }, NOW), "Memory is fine");
  assert.equal(guardSummary(undefined, NOW), "The guard has not run yet.");
});

test("G10 the log is newest first, names the thread and says why it resumed", () => {
  const rows = guardLog(g(), title, (iso) => iso.slice(11, 16), 4);
  assert.equal(rows.length, 4);
  assert.match(rows[0].text, /^Paused undeclared work of Map perf \(3\/8\) \(4 % free\)$/);
  assert.match(rows[1].text, /^Resumed Map perf: memory was fine for 30 s$/);
  assert.match(rows[2].text, /^Resumed Babysit PR #4170: its processes ended$/);
});

test("G11 a host whose guard has done nothing may send events: null; Automation still shows", () => {
  const calm = { short: false, max: 8, refreezeSec: 60, events: null };
  assert.deepEqual(guardChart(calm, NOW, title).lanes, []);
  assert.deepEqual(guardLog(calm, title, (iso) => iso), []);
  assert.equal(guardSummary(calm, NOW), "Memory is fine");
});
