import { test } from "node:test";
import assert from "node:assert/strict";
import { activeSection, allowCommit, askCommit, newDefaults, parseLastUsed, phoneSnoozePresets, stepLabel, stripCount } from "../src/mobile.ts";
import { chooseOther, goTo, initCard, pickStay, setOther } from "../src/question.ts";
import type { Decision } from "../src/types.ts";

const d: Decision = {
  id: "d",
  kind: "ask",
  questions: [
    { question: "Gesture?", options: [{ label: "Swipe" }, { label: "Tap" }] },
    { question: "Screens?", multiSelect: true, options: [{ label: "Map" }, { label: "Listing" }] },
  ],
};

test("a phone tap picks without moving on (the view moves on after 250 ms)", () => {
  const cs = pickStay(initCard(d), d, 0);
  assert.equal(cs.index, 0);
  assert.deepEqual(cs.picks[0], ["Swipe"]);
});

test("the commit button names its result", () => {
  let cs = initCard(d);
  assert.deepEqual(askCommit(cs, d), { label: "Answer · 2 of 2 left", enabled: false, act: "none" });
  cs = pickStay(cs, d, 0);
  assert.equal(askCommit(cs, d).label, "Answer · 1 of 2 left", "single-select moves on by itself");
  cs = goTo(cs, d, 1);
  cs = pickStay(cs, d, 1);
  assert.deepEqual(askCommit(cs, d), { label: "Answer 2 questions", enabled: true, act: "submit" });
  // Other on a single-select question that is not the last: Next question.
  let o = chooseOther(initCard(d), d);
  o = setOther(o, d, "Only on the handle");
  assert.deepEqual(askCommit(o, d), { label: "Next question", enabled: true, act: "next" });
  // A multi-select question that is not the last never moves on by itself.
  const d2: Decision = { ...d, questions: [d.questions![1], d.questions![0]] };
  const m = pickStay(initCard(d2), d2, 0);
  assert.equal(askCommit(m, d2).act, "next");
  const one: Decision = { ...d, questions: [d.questions![0]] };
  assert.equal(askCommit(pickStay(initCard(one), one, 1), one).label, "Answer");
});

test("approval and Fyr action commits", () => {
  const ap: Decision = { id: "a", kind: "approval", tool: "Bash" };
  const ef: Decision = { id: "e", kind: "effect", effect: { action: "pr.merge" } };
  assert.deepEqual(allowCommit(ap, null), { label: "Allow or deny", enabled: false, act: "none" });
  assert.equal(allowCommit(ap, "allow").label, "Allow");
  assert.equal(allowCommit(ap, "deny").label, "Deny");
  // SPEC 0.5 V0.27: an effect's buttons are verbs.
  assert.equal(allowCommit(ef, "allow").label, "Merge");
  assert.equal(allowCommit(ef, "deny").label, "Not now");
  assert.deepEqual(allowCommit(ef, null), { label: "Pick one", enabled: false, act: "none" });
});

test("step dots up to 5 questions, counts in the strip", () => {
  assert.equal(stepLabel(0, 2).dots, true);
  assert.deepEqual(stepLabel(0, 9999), { dots: false, text: "Question 1 of 9999" });
  assert.equal(stripCount(9999), "9999");
  assert.equal(stripCount(10000), "10k+");
});

test("snooze presets: this evening only before 17:00", () => {
  const morning = phoneSnoozePresets(new Date(2026, 10, 14, 9, 41)); // a Saturday
  assert.deepEqual(morning.map((p) => p.label), ["In 1 hour", "In 3 hours", "This evening", "Tomorrow morning", "Monday morning"]);
  assert.deepEqual(morning.map((p) => p.when), ["10:41", "12:41", "today 18:00", "Sun 06:00", "Mon 06:00"]);
  const late = phoneSnoozePresets(new Date(2026, 10, 14, 19, 0));
  assert.ok(!late.some((p) => p.label === "This evening"));
});

test("New screen defaults: last used on this device, else the host's most recent", () => {
  const projects = [
    { cwd: "/a", name: "a", lastUsed: "2026-11-01" },
    { cwd: "/b", name: "b", lastUsed: "2026-11-10" },
  ];
  const profiles = [
    { id: "claude", name: "Claude", color: "#000", models: ["opus", "sonnet"], defaultModel: "opus" },
    { id: "work", name: "Work", color: "#111", models: ["sonnet"] },
  ];
  assert.deepEqual(newDefaults({}, projects, profiles), { cwd: "/b", profile: "claude", model: "opus" });
  assert.deepEqual(newDefaults(parseLastUsed('{"cwd":"/a","profile":"work","model":"sonnet"}'), projects, profiles), { cwd: "/a", profile: "work", model: "sonnet" });
  assert.deepEqual(newDefaults({ profile: "gone", model: "x" }, projects, profiles), { cwd: "/b", profile: "claude", model: "opus" });
  assert.deepEqual(parseLastUsed("not json"), {});
});

test("the active strip segment follows the scroll", () => {
  const tops = [
    { key: "needs", top: 0 },
    { key: "working", top: 400 },
    { key: "seen", top: 700 },
  ];
  assert.equal(activeSection(tops, 0), "needs");
  assert.equal(activeSection(tops, 398), "working");
  assert.equal(activeSection(tops, 1000), "seen");
});
