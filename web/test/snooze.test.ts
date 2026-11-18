import { test } from "node:test";
import assert from "node:assert/strict";
import { SNOOZE_DONE_ACTION, snoozeChoiceForKey, snoozeChoices, snoozeCommand, snoozedHint, snoozeKeyCount, snoozeLine } from "../src/snooze.ts";
import { snoozePresets, undoKindOf } from "../src/store.ts";
import { thread } from "./helpers.ts";

const NOW = new Date(2026, 10, 14, 9, 41);
const working = (id: string) => thread(id, "working", "working", { running: true });
const idle = (id: string) => thread(id, "seen", "seen", { unavailable: { [SNOOZE_DONE_ACTION]: "Nothing is working" } });

// V0.185: one list for desktop and phone; the new choice comes last, so keys 1–5 keep their meaning.
test("V0.185: until work is done is the last snooze choice and names its maximum", () => {
  const list = snoozeChoices(NOW, [working("a")]);
  assert.deepEqual(list.slice(0, -1).map((c) => c.label), snoozePresets(NOW).map((p) => p.label));
  const last = list.at(-1)!;
  assert.equal(last.label, "Until work is done");
  assert.equal(last.when, "at most 24 h");
  assert.equal(last.off, undefined);
  assert.equal(snoozeChoiceForKey(list, String(list.length)), last);
  assert.equal(snoozeKeyCount(list), list.length);
});

test("V0.185: a thread with no work going gets the choice off, with the host's reason, and no key", () => {
  const list = snoozeChoices(NOW, [idle("a")]);
  const last = list.at(-1)!;
  assert.equal(last.off, "Nothing is working");
  assert.equal(snoozeChoiceForKey(list, String(list.length)), null);
  assert.equal(snoozeKeyCount(list), list.length - 1);
});

test("V0.185: several threads: the command goes to those that can take it", () => {
  const list = snoozeChoices(NOW, [idle("a"), working("b")]);
  const last = list.at(-1)!;
  assert.equal(last.off, undefined);
  assert.deepEqual(snoozeCommand(last, ["a", "b"]), { action: SNOOZE_DONE_ACTION, ids: ["b"], args: undefined });
  assert.equal(undoKindOf(SNOOZE_DONE_ACTION), "snooze");
});

test("V0.185: a timed choice still sends its time to every thread", () => {
  const first = snoozeChoices(NOW, [idle("a")])[0];
  assert.deepEqual(snoozeCommand(first, ["a"]), { action: "thread.snooze", ids: ["a"], args: { until: first.until.toISOString() } });
});

test("V0.185: the row and the section say until work is done, not the maximum time", () => {
  const until = new Date(NOW.getTime() + 3600_000).toISOString();
  const bound = thread("a", "snoozed", "snoozed", { view: { snoozeUntil: until, snoozeWhenDone: true } });
  const timed = thread("b", "snoozed", "snoozed", { view: { snoozeUntil: until } });
  assert.equal(snoozeLine(bound, NOW.getTime()), "until work is done");
  assert.match(snoozeLine(timed, NOW.getTime()), /^until \d/);
  assert.equal(snoozedHint([bound], NOW.getTime()), "until work is done");
  assert.match(snoozedHint([bound, timed], NOW.getTime()), /^next: \d/);
  assert.equal(snoozedHint([], NOW.getTime()), "none");
});
