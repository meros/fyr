import { test } from "node:test";
import assert from "node:assert/strict";
import { answerThenNext, cardKey, initCard, pick, setOther, type CardState, submitState } from "../src/question.ts";
import type { Decision } from "../src/types.ts";

const d: Decision = {
  id: "d1",
  kind: "ask",
  questions: [
    { question: "Gesture?", multiSelect: false, options: [{ label: "Swipe" }, { label: "Tap" }, { label: "Both" }] },
    { question: "Devices?", multiSelect: true, options: [{ label: "iPhone" }, { label: "Pixel" }, { label: "iPad" }, { label: "Desktop" }] },
    { question: "Name?", multiSelect: false, options: [{ label: "A" }, { label: "B" }] },
  ],
};

function keys(cs: CardState, ...ks: string[]) {
  let r = { state: cs, handled: false } as ReturnType<typeof cardKey>;
  for (const k of ks) r = cardKey(r.state, d, k);
  return r;
}

test("1-4 pick in a single-select question and advance", () => {
  const r = keys(initCard(d), "2");
  assert.equal(r.handled, true);
  assert.deepEqual(r.state.picks[0], ["Tap"]);
  assert.equal(r.state.index, 1);
});

test("keys beyond the options are not handled", () => {
  const r = cardKey(initCard(d), d, "4");
  assert.equal(r.handled, false);
});

test("multi-select toggles with 1-4 and Tab moves to the next question", () => {
  let r = keys(initCard(d), "1", "1", "3", "4", "4");
  assert.equal(r.state.index, 1, "toggling does not advance");
  assert.deepEqual(r.state.picks[1], ["iPhone", "iPad"]);
  r = cardKey(r.state, d, "Tab");
  assert.equal(r.state.index, 2);
  r = cardKey(r.state, d, "Tab", { shift: true });
  assert.equal(r.state.index, 1);
});

test("0 opens Other; its text is the answer", () => {
  let r = keys(initCard(d), "1", "2", "Tab", "0");
  assert.equal(r.focusOther, true);
  assert.equal(r.state.otherOpen, true);
  assert.equal(r.state.index, 2);
  const s = setOther(r.state, d, "Custom name");
  r = cardKey(s, d, "Enter", { inOther: true });
  assert.deepEqual(r.submit, { "Gesture?": "Swipe", "Devices?": "Pixel", "Name?": "Custom name" });
});

test("Enter submits only when every question is answered, else jumps to the first open one", () => {
  let r = keys(initCard(d), "1", "Enter");
  assert.equal(r.submit, undefined);
  assert.equal(r.state.index, 1);
  r = keys(r.state, "2", "4", "Tab", "1", "Enter");
  assert.deepEqual(r.submit, { "Gesture?": "Swipe", "Devices?": "Pixel, Desktop", "Name?": "A" });
});

test("in the Other field digits are text, Esc closes, Enter submits", () => {
  const cs = keys(initCard(d), "0").state;
  assert.equal(cardKey(cs, d, "1", { inOther: true }).handled, false);
  const closed = cardKey(cs, d, "Escape", { inOther: true });
  assert.equal(closed.state.otherOpen, false);
  assert.equal(closed.state.other[0], null, "empty Other is dropped");
});

test("multi-select with Other joins picks and text", () => {
  let cs = keys(initCard(d), "1", "1", "2").state; // q0 Swipe, q1 toggles iPhone, Pixel
  cs = cardKey(cs, d, "0").state;
  cs = setOther(cs, d, "Fold");
  const r = keys(cs, "Tab", "2", "Enter");
  assert.deepEqual(r.submit, { "Gesture?": "Swipe", "Devices?": "iPhone, Pixel, Fold", "Name?": "B" });
});

test("clearing the Other text drops it, so the question is unanswered again", () => {
  let cs = setOther(initCard(d), d, "x");
  assert.equal(cs.other[0], "x");
  cs = setOther(cs, d, "");
  assert.equal(cs.other[0], null);
  assert.equal(cardKey(cs, d, "Enter").submit, undefined);
});

// SPEC U15: the question card always shows how to finish: a button that says what it does.
test("the card's submit button follows the answers", () => {
  const d = { id: "d", kind: "ask", questions: [
    { question: "A?", options: [{ label: "x" }, { label: "y" }] },
    { question: "B?", options: [{ label: "p" }, { label: "q" }] },
  ] } as unknown as Decision;
  let cs = initCard(d);
  assert.deepEqual(submitState(cs, d), { label: "Pick an answer", act: "none", answered: 0, total: 2 });
  cs = pick(cs, d, 0); // answers A and moves to B
  assert.deepEqual(submitState({ ...cs, index: 0 }, d), { label: "Next question", act: "next", answered: 1, total: 2 });
  cs = pick(cs, d, 1);
  assert.deepEqual(submitState(cs, d), { label: "Send 2 answers", act: "submit", answered: 2, total: 2 });
  const one = { id: "e", kind: "ask", questions: [{ question: "C?", options: [{ label: "z" }] }] } as unknown as Decision;
  assert.equal(submitState(pick(initCard(one), one, 0), one).label, "Send answer");
});

// review 2026-11-15: a failed answer keeps the thread open; only an accepted one moves to the next that needs you.
test("answerThenNext moves on only when the host took the answer", async () => {
  let moved = 0;
  await answerThenNext(async () => 0, () => moved++);
  assert.equal(moved, 0, "a failed thread.answer stays");
  await answerThenNext(async () => 1, () => moved++);
  assert.equal(moved, 1);
});
