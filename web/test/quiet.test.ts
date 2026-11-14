// SPEC 0.5 V0.46: quiet hours per project. The host sends quietUntil; the client hides those threads from the inbox
// unless the project filter picks their project or a search is typed, and re-renders at the nearest end.
import { test } from "node:test";
import assert from "node:assert/strict";
import { hideQuiet, isQuiet, nextQuietEnd, quietLine, quietNote, toggleDay } from "../src/quiet.ts";
import { sections } from "../src/store.ts";
import type { Project } from "../src/types.ts";
import { thread } from "./helpers.ts";

const now = Date.parse("2026-11-13T20:00:00Z");
const until = "2026-11-14T05:00:00Z";
const later = "2026-11-14T07:00:00Z";
const threads = {
  a: thread("a", "asking", "needs", { projectId: "p_work", quietUntil: until }),
  b: thread("b", "done", "needs", { projectId: "p_home" }),
  c: thread("c", "working", "working", { projectId: "p_work", quietUntil: until }),
  d: thread("d", "seen", "seen", { projectId: "p_late", quietUntil: later }),
};

test("a thread is quiet only before its quietUntil", () => {
  assert.equal(isQuiet(threads.a, now), true);
  assert.equal(isQuiet(threads.b, now), false);
  assert.equal(isQuiet(threads.a, Date.parse(until)), false, "back at the end of the window");
  assert.equal(isQuiet({ quietUntil: "not a time" }, now), false);
});

test("the inbox hides quiet threads; the filter and a search show them", () => {
  assert.deepEqual(Object.keys(hideQuiet(threads, false, now)), ["b"]);
  assert.deepEqual(Object.keys(hideQuiet(threads, true, now)).sort(), ["a", "b", "c", "d"]);
  assert.deepEqual(Object.keys(hideQuiet(threads, false, Date.parse(until))).sort(), ["a", "b", "c"], "the first window ended");
});

test("counts and the Needs you badge leave quiet threads out", () => {
  const sec = sections(hideQuiet(threads, false, now));
  assert.equal(sec.needs.length, 1);
  assert.equal(sec.working.length, 0);
  assert.equal(sec.seen.length, 0);
});

test("one timer: the nearest quietUntil still ahead", () => {
  const projects = [{ id: "p_x", name: "x", quietUntil: "2026-11-14T04:00:00Z" }] as Project[];
  assert.equal(nextQuietEnd(threads, [], now), Date.parse(until));
  assert.equal(nextQuietEnd(threads, projects, now), Date.parse("2026-11-14T04:00:00Z"));
  assert.equal(nextQuietEnd(threads, [], Date.parse(until)), Date.parse(later));
  assert.equal(nextQuietEnd(threads, [], Date.parse(later)), null);
});

test("the filter note and the project screen line", () => {
  const p = { id: "p_work", name: "Work", quietUntil: new Date(2026, 10, 14, 6, 0).toISOString() } as Project;
  assert.equal(quietNote(p, new Date(2026, 10, 13, 20, 0).getTime()), "Quiet until 06:00");
  assert.equal(quietNote(p, new Date(2026, 10, 12, 20, 0).getTime()), "Quiet until Sat 06:00", "more than a day ahead names the day");
  assert.equal(quietNote(p, new Date(2026, 10, 14, 6, 0).getTime()), null);
  assert.equal(quietNote(undefined, now), null);
  assert.equal(quietLine({ from: "17:00", to: "06:00" }), "Threads hide and send no notifications from 17:00 to 06:00.");
  assert.equal(quietLine({ from: "17:00", to: "06:00", days: ["mon", "tue", "wed", "thu", "fri"] }), "Threads hide and send no notifications from 17:00 to 06:00, Mon–Fri.");
  assert.equal(quietLine({ from: "09:00", to: "12:00", days: ["sat", "sun"] }), "Threads hide and send no notifications from 09:00 to 12:00, Sat–Sun.");
  assert.equal(quietLine({ from: "09:00", to: "12:00", days: ["mon", "wed"] }), "Threads hide and send no notifications from 09:00 to 12:00, Mon, Wed.");
  assert.equal(quietLine({ from: "00:00", to: "00:00", days: ["sat", "sun"] }), "Threads hide and send no notifications all day, Sat–Sun.");
});

test("day toggles keep the week order; all seven is every day; the last day stays on", () => {
  assert.deepEqual(toggleDay(undefined, "sun"), ["mon", "tue", "wed", "thu", "fri", "sat"]);
  assert.deepEqual(toggleDay(["fri"], "mon"), ["mon", "fri"]);
  assert.deepEqual(toggleDay(["mon"], "mon"), ["mon"], "a window needs a day");
  assert.deepEqual(toggleDay(["mon", "tue", "wed", "thu", "fri", "sat"], "sun"), undefined);
});
