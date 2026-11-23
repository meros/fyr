import { test } from "node:test";
import assert from "node:assert/strict";
import { allowArgs, answerArgs, answerThenNext, back, cardKey, goTo, initCard, pick, setOther, settledOf, skip, submitOf, submitState, updateCard, type CardState } from "../src/question.ts";
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
  assert.deepEqual(r.submit, { answers: { "Gesture?": "Swipe", "Devices?": "Pixel", "Name?": "Custom name" }, skipped: [] });
});

test("Enter submits only when every question is answered, else jumps to the first open one", () => {
  let r = keys(initCard(d), "1", "Enter");
  assert.equal(r.submit, undefined);
  assert.equal(r.state.index, 1);
  r = keys(r.state, "2", "4", "Tab", "1", "Enter");
  assert.deepEqual(r.submit, { answers: { "Gesture?": "Swipe", "Devices?": "Pixel, Desktop", "Name?": "A" }, skipped: [] });
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
  assert.deepEqual(r.submit, { answers: { "Gesture?": "Swipe", "Devices?": "iPhone, Pixel, Fold", "Name?": "B" }, skipped: [] });
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
  assert.deepEqual(submitState(cs, d), { label: "Pick an answer", act: "none", answered: 0, skipped: 0, total: 2 });
  cs = pick(cs, d, 0); // answers A and moves to B
  assert.deepEqual(submitState({ ...cs, index: 0 }, d), { label: "Next question", act: "next", answered: 1, skipped: 0, total: 2 });
  cs = pick(cs, d, 1);
  assert.deepEqual(submitState(cs, d), { label: "Send 2 answers", act: "submit", answered: 2, skipped: 0, total: 2 });
  const one = { id: "e", kind: "ask", questions: [{ question: "C?", options: [{ label: "z" }] }] } as unknown as Decision;
  assert.equal(submitState(pick(initCard(one), one, 0), one).label, "Send answer");
});

// review 2026-11-15: a failed answer keeps the thread open; only an accepted one moves to the next that needs you.
test("answerThenNext moves on only when the host took the answer", async () => {
  let moved = 0;
  await answerThenNext(async () => false, () => moved++);
  assert.equal(moved, 0, "a failed thread.answer stays");
  await answerThenNext(async () => true, () => moved++);
  assert.equal(moved, 1);
});

// V0.ask-cancel: a question the owner skips is left without an answer, the card moves on, and the
// send names it so the agent can tell the gap from an answer.
test("Skip leaves a question unanswered, moves on, and the send names it", () => {
  const cs = skip(initCard(d), d);
  assert.deepEqual(cs.skipped, [true, false, false]);
  assert.equal(cs.index, 1, "a skip moves to the next question that needs an answer");
  assert.equal(settledOf(cs, d, 0), true, "a skipped question does not hold the card");
  assert.equal(submitOf(cs, d), null, "the other questions still need an answer");
  let r = keys(cs, "1", "2", "Tab", "1", "Enter");
  assert.deepEqual(r.submit, { answers: { "Devices?": "iPhone, Pixel", "Name?": "A" }, skipped: ["Gesture?"] });
  assert.equal(submitState(r.state, d).label, "Send 2 answers");
  // Taking the answer back for a skipped question, and the skip back with it.
  assert.equal(settledOf(skip(back(cs, d), d), d, 0), false, "Unskip takes the skip back");
  const answered = pick(cs, d, 0);
  assert.equal(answered.skipped[1], false, "an answer takes the skip back");
});

test("Skipping every question leaves only Cancel to send", () => {
  const one = { id: "e", kind: "ask", questions: [{ question: "C?", options: [{ label: "z" }] }] } as unknown as Decision;
  const cs = skip(initCard(one), one);
  assert.deepEqual(cs.skipped, [true]);
  assert.deepEqual(submitState(cs, one), { label: "Cancel question", act: "cancel", answered: 0, skipped: 1, total: 1 });
  // Enter on that card cancels it rather than sending an answer nobody gave.
  const r = cardKey(cs, one, "Enter");
  assert.equal(r.cancel, true);
  assert.equal(r.submit, undefined);
});

test("Shift+S skips the question being shown, and Shift+Esc cancels the card", () => {
  const cs = cardKey(cardKey(initCard(d), d, "S", { shift: true }).state, d, "S", { shift: true }).state; // q0, then q1
  assert.deepEqual(cs.skipped, [true, true, false]);
  const r = cardKey(cs, d, "Escape", { shift: true });
  assert.equal(r.handled, true);
  assert.equal(r.cancel, true);
  assert.equal(cardKey(cs, d, "Escape").handled, false, "a plain Esc keeps its meaning");
});

test("Back and the arrows step between questions, and Back stops at the first", () => {
  let cs = keys(initCard(d), "1").state; // answered q0, now at q1
  assert.equal(cs.index, 1);
  assert.equal(back(cs, d).index, 0);
  assert.equal(back(back(cs, d), d).index, 0, "Back stops at the first question");
  const left = cardKey(back(cs, d), d, "ArrowLeft");
  assert.equal(left.handled, false, "nothing before the first question");
  assert.equal(cardKey(cs, d, "ArrowRight").state.index, 2);
  assert.equal(cardKey(cs, d, "ArrowLeft").state.index, 0);
  const wrapped = keys(initCard(d), "Tab", "Tab", "Tab");
  assert.equal(wrapped.state.index, 2, "Tab stops at the last question");
});

test("the answer payload has one builder: skips go named, an approval carries allow", () => {
  const d = { id: "d1", kind: "ask" } as never;
  assert.deepEqual(answerArgs(d, { answers: [["a"]], skipped: [] } as never), { decisionId: "d1", answers: [["a"]] });
  assert.deepEqual(answerArgs(d, { answers: [["a"], []], skipped: [1] } as never), { decisionId: "d1", answers: [["a"], []], skipped: [1] });
  assert.deepEqual(allowArgs(d, false), { decisionId: "d1", allow: false });
});

test("no front builds a thread.answer payload of its own: app-cards.tsx's useAnswers is the one", async () => {
  const { readdirSync, readFileSync } = await import("node:fs");
  const dir = new URL("../src/", import.meta.url);
  const hits = readdirSync(dir).filter((f) => /^(app|phone|thread)[\w-]*\.tsx?$/.test(f) && /"thread\.answer"/.test(readFileSync(new URL(f, dir), "utf8")));
  assert.deepEqual(hits, ["app-cards.tsx"]);
});

// V0.app-shell-tidy: both fronts change a card through one updater on the card as it is now.
test("updateCard: each change starts from the newest card; a closed decision's card stays absent", () => {
  let cards: Record<string, CardState> = { [d.id]: initCard(d) };
  // Two changes made from one render: a value-style write would lose the first; updaters keep both.
  const a = (cs: CardState) => pick(cs, d, 1);
  const b = (cs: CardState) => goTo(cs, d, 1);
  cards = updateCard(cards, d.id, a);
  cards = updateCard(cards, d.id, b);
  assert.equal(cards[d.id].index, 1);
  assert.deepEqual(cards[d.id].picks[0], ["Tap"], "the pick made before the step is kept");
  const before = cards;
  assert.equal(updateCard(cards, "gone", a), before, "no card: the same object, nothing added");
  assert.equal(Object.keys(updateCard(cards, "gone", a)).length, 1);
});

