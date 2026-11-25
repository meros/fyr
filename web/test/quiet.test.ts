// SPEC 0.5 V0.46: quiet hours per project. The host sends quietUntil; the client hides those threads from the inbox
// unless the project filter picks their project or a search is typed, and re-renders at the nearest end.
import { test } from "node:test";
import assert from "node:assert/strict";
import { QUIET_HOLD_MS, QUIET_PRESETS, QUIET_SHOW_MS, cleanWindow, createProjects, loadQuietShow, saveQuietShow, dayText, hasWindow, hideQuiet, isAllDay, isQuiet, nextQuietEnd, quietLine, quietList, quietNote, setAllDay, toggleDay, windowWords } from "../src/quiet.ts";
import { mockQuietUntil } from "../src/mock.ts";
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
  assert.equal(quietLine({ from: "17:00", to: "06:00" }), "Threads hide and send no notifications from 17:00 to 06:00.", "the old single object");
  assert.equal(quietLine([{ from: "17:00", to: "06:00" }]), "Threads hide and send no notifications from 17:00 to 06:00.");
  assert.equal(quietLine([{ from: "17:00", to: "06:00", days: ["mon", "tue", "wed", "thu", "fri"] }]), "Threads hide and send no notifications weeknights 17:00–06:00.");
  assert.equal(quietLine([{ from: "09:00", to: "12:00", days: ["sat", "sun"] }]), "Threads hide and send no notifications weekends 09:00–12:00.");
  assert.equal(quietLine([{ from: "09:00", to: "12:00", days: ["mon", "wed"] }]), "Threads hide and send no notifications Mon, Wed 09:00–12:00.");
  assert.equal(quietLine([{ from: "00:00", to: "00:00", days: ["sat", "sun"] }]), "Threads hide and send no notifications all weekend.");
  assert.equal(quietLine([]), "Off: threads show and notify as usual.");
});

test("day toggles keep the week order; all seven is every day; the last day stays on", () => {
  assert.deepEqual(toggleDay(undefined, "sun"), ["mon", "tue", "wed", "thu", "fri", "sat"]);
  assert.deepEqual(toggleDay(["fri"], "mon"), ["mon", "fri"]);
  assert.deepEqual(toggleDay(["mon"], "mon"), ["mon"], "a window needs a day");
  assert.deepEqual(toggleDay(["mon", "tue", "wed", "thu", "fri", "sat"], "sun"), undefined);
});

// Decision 0014 addendum: several windows, presets, all day, 24-hour words.
const preset = (k: string) => QUIET_PRESETS.find((p) => p.key === k)!.window;

test("the summary line names every window in plain words", () => {
  assert.equal(quietLine([preset("weeknights"), preset("weekend")]), "Threads hide and send no notifications weeknights 17:00–06:00 and all weekend.");
  assert.equal(
    quietLine([preset("weeknights"), { from: "12:00", to: "13:00" }, { from: "00:00", to: "00:00", days: ["wed"] }]),
    "Threads hide and send no notifications weeknights 17:00–06:00, daily 12:00–13:00 and all day Wed.",
  );
  assert.equal(windowWords({ from: "00:00", to: "00:00" }), "all day");
  assert.equal(windowWords({ from: "09:00", to: "17:00", days: ["mon", "tue", "wed", "thu", "fri"] }), "Mon–Fri 09:00–17:00", "a day window is not a night");
  assert.equal(windowWords({ from: "22:00", to: "07:00", days: ["fri", "sat"] }), "Fri–Sat 22:00–07:00");
});

test("day runs read in week order and may wrap past Sunday", () => {
  assert.equal(dayText(["sun", "mon", "tue", "wed", "thu", "fri"]), "Sun–Fri");
  assert.equal(dayText(["fri", "sat", "sun", "mon"]), "Fri–Mon");
  assert.equal(dayText(["mon", "wed", "fri"]), "Mon, Wed, Fri");
  assert.equal(dayText(["sat"]), "Sat");
  assert.equal(dayText(undefined), "");
  assert.equal(dayText(["mon", "tue", "wed", "thu", "fri", "sat", "sun"]), "");
});

test("the presets: weeknights Sun–Fri 17:00–06:00 and all weekend", () => {
  assert.deepEqual(cleanWindow(preset("weeknights")), { from: "17:00", to: "06:00", days: ["mon", "tue", "wed", "thu", "fri", "sun"] });
  assert.deepEqual(preset("weekend"), { from: "00:00", to: "00:00", days: ["sat", "sun"] });
  assert.equal(isAllDay(preset("weekend")), true);
  assert.equal(hasWindow([{ from: "17:00", to: "06:00", days: ["sun", "fri", "thu", "wed", "tue", "mon"] }], preset("weeknights")), true, "day order does not matter");
  assert.equal(hasWindow([{ from: "17:00", to: "06:00" }], preset("weeknights")), false);
  // Friday 17:00 joins the weekend and Monday morning (the host computes this; the mock mirrors it).
  const fri = new Date(2026, 10, 13, 17, 0).getTime();
  assert.equal(mockQuietUntil([preset("weeknights"), preset("weekend")], fri), new Date(2026, 10, 16, 6, 0).toISOString());
  assert.equal(quietNote({ quietUntil: mockQuietUntil([preset("weeknights"), preset("weekend")], fri) }, fri), "Quiet until Mon 06:00");
  assert.equal(mockQuietUntil([preset("weeknights")], new Date(2026, 10, 14, 12, 0).getTime()), undefined, "Saturday noon without the weekend");
});

test("all day is from equal to to; turned off it is the default evening", () => {
  assert.deepEqual(setAllDay({ from: "17:00", to: "06:00", days: ["mon"] }, true), { from: "00:00", to: "00:00", days: ["mon"] });
  assert.deepEqual(setAllDay({ from: "00:00", to: "00:00" }, false), { from: "17:00", to: "06:00" });
});

test("the old single object and the list both read as a list", () => {
  assert.deepEqual(quietList({ from: "17:00", to: "06:00" }), [{ from: "17:00", to: "06:00" }]);
  assert.deepEqual(quietList([{ from: "17:00", to: "06:00" }, { nope: 1 }]), [{ from: "17:00", to: "06:00" }]);
  assert.deepEqual(quietList(undefined), []);
  assert.deepEqual(quietList(null), []);
  assert.deepEqual(cleanWindow({ from: "17:00", to: "06:00", days: ["mon", "tue", "wed", "thu", "fri", "sat", "sun"] }), { from: "17:00", to: "06:00" }, "every day is no days");
});

// SPEC 0.5 V0.74: a quiet project stays out of New thread; a 30 s hold shows it for 30 minutes.
test("New thread leaves quiet projects out, keeps the picked one, and shows them all while unlocked", () => {
  const ps: Project[] = [
    { id: "p_work", name: "Work", quietUntil: until },
    { id: "p_home", name: "Home" },
    { id: "p_late", name: "Late", quietUntil: later },
  ];
  assert.deepEqual(createProjects(ps, now, 0).map((p) => p.id), ["p_home"]);
  assert.deepEqual(createProjects(ps, now, 0, "p_late").map((p) => p.id), ["p_home", "p_late"], "the picked project stays");
  // V0.dialogs-review: and so does the one a restored draft was written for, while its picks wait (the picked one may be undefined then).
  assert.deepEqual(createProjects(ps, now, 0, undefined, "p_late").map((p) => p.id), ["p_home", "p_late"]);
  assert.deepEqual(createProjects(ps, now, 0, "p_work", "p_late").map((p) => p.id), ["p_work", "p_home", "p_late"]);
  assert.deepEqual(createProjects(ps, now, 0, undefined, undefined).map((p) => p.id), ["p_home"]);
  assert.deepEqual(createProjects(ps, now, now + 60_000).map((p) => p.id), ["p_work", "p_home", "p_late"], "unlocked");
  assert.deepEqual(createProjects(ps, now, now).map((p) => p.id), ["p_home"], "the unlock ended");
  assert.deepEqual(createProjects(ps, Date.parse(until), 0).map((p) => p.id), ["p_work", "p_home"], "a window ended");
});

test("the hold takes 30 s and shows quiet projects for 30 minutes on this device", () => {
  assert.equal(QUIET_HOLD_MS, 30_000);
  assert.equal(QUIET_SHOW_MS, 30 * 60_000);
  const mem = new Map<string, string>();
  const kv = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => void mem.set(k, v), removeItem: (k: string) => void mem.delete(k) };
  assert.equal(loadQuietShow(kv), 0);
  const end = saveQuietShow(kv, now);
  assert.equal(end, now + QUIET_SHOW_MS);
  assert.equal(loadQuietShow(kv), end);
  saveQuietShow(kv, null);
  assert.equal(loadQuietShow(kv), 0, "Hide ends it");
  mem.set("fyr.quietShowUntil", "junk");
  assert.equal(loadQuietShow(kv), 0);
  assert.equal(loadQuietShow(null), 0);
});

// V0.119: quiet hours save 600 ms after the last change, and at once when the page closes.
import { mock } from "node:test";
import { QUIET_SAVE_MS, quietSaver } from "../src/quiet.ts";

test("the quiet-hours save keeps only the last value", () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    const sent: unknown[] = [];
    const s = quietSaver<number>((v) => sent.push(v), QUIET_SAVE_MS);
    s.push(1);
    mock.timers.tick(300);
    s.push(2);
    mock.timers.tick(300);
    s.push(3);
    mock.timers.tick(599);
    assert.deepEqual(sent, [], "nothing before 600 ms of quiet");
    mock.timers.tick(1);
    assert.deepEqual(sent, [3]);
    s.push(4);
    s.flush();
    assert.deepEqual(sent, [3, 4], "flush sends at once");
    s.flush();
    mock.timers.tick(1000);
    assert.deepEqual(sent, [3, 4], "nothing is sent twice");
    assert.equal(QUIET_SAVE_MS, 600);
  } finally {
    mock.timers.reset();
  }
});

// V0.quiet-now: a quiet stretch started by hand.
test("quiet now by hand: choices, the line, and the mock joins it with a window", async () => {
  const { manualUntil, nextClock, quietNowChoices, quietNowLine } = await import("../src/quiet.ts");
  const noon = new Date(2026, 10, 13, 12, 0).getTime(); // a Friday
  assert.equal(nextClock("15:30", noon), new Date(2026, 10, 13, 15, 30).getTime());
  assert.equal(nextClock("08:00", noon), new Date(2026, 10, 14, 8, 0).getTime(), "a time passed today is tomorrow");
  assert.deepEqual(
    quietNowChoices(noon).map((c) => c.label),
    ["1 hour", "3 hours", "Until tomorrow 08:00"],
  );
  assert.equal(quietNowChoices(new Date(2026, 10, 13, 2, 0).getTime())[2].label, "Until 08:00", "before 08:00 it is today");
  const three = new Date(2026, 10, 13, 15, 0).toISOString();
  assert.equal(manualUntil({ quietNow: three }, noon), Date.parse(three));
  assert.equal(manualUntil({ quietNow: three }, Date.parse(three)), 0, "ended");
  assert.equal(quietNowLine({ quietNow: three }, noon), "Quiet by hand until 15:00.");
  assert.equal(quietNowLine({ quietNow: new Date(2026, 10, 14, 8, 0).toISOString() }, noon), "Quiet by hand until tomorrow 08:00.");
  assert.equal(quietNowLine({}, noon), "Hide its threads and hold notifications from now, once.");
  assert.equal(mockQuietUntil(undefined, noon, three), three);
  const night = [{ from: "17:00", to: "06:00" }];
  assert.equal(mockQuietUntil(night, noon, new Date(2026, 10, 13, 17, 0).toISOString()), new Date(2026, 10, 14, 6, 0).toISOString(), "runs into the night window");
  assert.equal(mockQuietUntil(night, noon, new Date(2026, 10, 13, 11, 0).toISOString()), undefined, "a time passed is off");
});
