// V0.no-interrupt: what the host or the clock does never takes over what the owner is doing.
import { test } from "node:test";
import assert from "node:assert/strict";
import { answerTakesComposer } from "../src/compose.ts";
import { ARRIVAL_GRACE_MS, TYPING_QUIET_MS, cardTakesKeys, framePage, moveOnAfterAnswer, ownerBusy, pushedValue, unpickedTab } from "../src/no-interrupt.ts";
import { defaultTab } from "../src/view.ts";

test("V0.no-interrupt-rest (1): a view that appears while the thread is open does not take the owner off Conversation", () => {
  assert.equal(unpickedTab(true, true), "view", "there when opened: opens on View (V0.37)");
  assert.equal(unpickedTab(true, false), "conv");
  assert.equal(unpickedTab(false, false), "conv");
  const withView = { live: { pageId: "p1", state: {}, updatedAt: "", checkedAt: "", headline: "" }, liveView: true, pr: undefined } as unknown as Parameters<typeof defaultTab>[0];
  assert.equal(defaultTab(withView, undefined, false), "conv");
  assert.equal(defaultTab(withView, "view", false), "view", "a pick wins");
  assert.equal(defaultTab(withView, undefined), "view", "callers that do not say keep V0.37");
});

test("V0.no-interrupt-rest (2): a republished page waits while the owner is in the frame", () => {
  assert.equal(framePage("p1", "p2", true), "p1");
  assert.equal(framePage("p1", "p2", false), "p2");
  assert.equal(framePage("p2", "p2", true), "p2");
});

test("V0.no-interrupt-rest (3): a late yes to an answer moves on only if the owner stayed and pressed nothing", () => {
  assert.equal(moveOnAfterAnswer({ stillOpen: true, keyAfter: false }), true);
  assert.equal(moveOnAfterAnswer({ stillOpen: false, keyAfter: false }), false);
  assert.equal(moveOnAfterAnswer({ stillOpen: true, keyAfter: true }), false);
});

test("V0.no-interrupt (1): a question that arrives over a draft leaves the composer to the draft", () => {
  const asked = { text: "", chosen: false };
  assert.equal(answerTakesComposer("half a sentence", asked), false);
  assert.equal(answerTakesComposer("   ", asked), true, "an empty field is the answer field");
  assert.equal(answerTakesComposer("", asked), true);
  assert.equal(answerTakesComposer("draft", { text: "", chosen: true }), true, "Other… (0) takes the field on purpose");
  assert.equal(answerTakesComposer("draft", { text: "my answer", chosen: false }), true, "an answer begun stays the field's");
  assert.equal(answerTakesComposer("draft", null), false);
});

test("V0.no-interrupt (3): a card that just arrived takes no key; one shown since the thread opened does", () => {
  assert.equal(cardTakesKeys(0, 5), true, "there when the thread opened");
  assert.equal(cardTakesKeys(10_000, 10_000 + ARRIVAL_GRACE_MS - 1), false);
  assert.equal(cardTakesKeys(10_000, 10_000 + ARRIVAL_GRACE_MS), true);
  assert.equal(cardTakesKeys(null, 10_000), false);
});

test("V0.no-interrupt (4): a reload for a new build waits while the owner types, fills a field or has a dialog open", () => {
  const idle = { hidden: false, dialogOpen: false, fieldText: null, lastKeyAt: 0, now: 100_000 };
  assert.equal(ownerBusy(idle), false);
  assert.equal(ownerBusy({ ...idle, fieldText: "" }), false, "an empty focused composer does not hold the update");
  assert.equal(ownerBusy({ ...idle, fieldText: "half" }), true);
  assert.equal(ownerBusy({ ...idle, dialogOpen: true }), true);
  assert.equal(ownerBusy({ ...idle, lastKeyAt: idle.now - TYPING_QUIET_MS + 1 }), true);
  assert.equal(ownerBusy({ ...idle, lastKeyAt: idle.now - TYPING_QUIET_MS }), false);
  assert.equal(ownerBusy({ ...idle, hidden: true, dialogOpen: true, fieldText: "x", lastKeyAt: idle.now }), false, "a hidden page reloads");
});

test("V0.no-interrupt (5): a pushed value replaces a settings field only while the owner has not changed it", () => {
  assert.equal(pushedValue("old", "old", "new"), "new");
  assert.equal(pushedValue("mine", "old", "new"), "mine");
});
