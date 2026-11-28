// V0.tooltips: the tooltip's words split into the sentence and its keys, its place at the anchor inside the window,
// and the warm start along a row.
import { test } from "node:test";
import assert from "node:assert/strict";
import { TIP_GAP, TIP_MARGIN, WARM_MS, isWarm, placeTip, sameWords, splitTip } from "../src/tooltip.ts";

test("a trailing key hint becomes key caps", () => {
  assert.deepEqual(splitTip("Settle now (e)"), { text: "Settle now", keys: ["e"] });
  assert.deepEqual(splitTip("Leave this question without an answer (⇧S)"), { text: "Leave this question without an answer", keys: ["⇧S"] });
  assert.deepEqual(splitTip("Previous question (⇧Tab or ←)"), { text: "Previous question", keys: ["⇧Tab", "←"] });
  assert.deepEqual(splitTip("Your previous message ([)"), { text: "Your previous message", keys: ["["] });
  assert.deepEqual(splitTip("Close (Esc)"), { text: "Close", keys: ["Esc"] });
  assert.deepEqual(splitTip("Inbox (g1)"), { text: "Inbox", keys: ["g1"] });
  assert.deepEqual(splitTip("Search (Ctrl+K)"), { text: "Search", keys: ["Ctrl+K"] });
});

test("brackets that are words stay words", () => {
  for (const s of ["Token (optional)", "Ran the check (exit 2)", "Node (node 24)", "Run on host-a (now)", "Same project (#N)", "Back"]) {
    assert.deepEqual(splitTip(s), { text: s, keys: [] }, s);
  }
});

const win = { left: 0, top: 0, width: 1280, height: 800 };
const tip = { width: 120, height: 26 };

test("above the anchor, centred, the nub at its middle", () => {
  const a = { left: 600, top: 300, width: 28, height: 28 };
  const p = placeTip(a, tip, win);
  assert.equal(p.side, "above");
  assert.equal(p.top, 300 - TIP_GAP - tip.height);
  assert.equal(p.left, 614 - 60);
  assert.equal(p.nub, 60);
});

test("at the top of the window: below the anchor", () => {
  const p = placeTip({ left: 600, top: 4, width: 28, height: 28 }, tip, win);
  assert.equal(p.side, "below");
  assert.equal(p.top, 32 + TIP_GAP);
});

test("at the right edge: pushed inside, the nub still at the anchor", () => {
  const a = { left: 1262, top: 300, width: 16, height: 16 };
  const p = placeTip(a, tip, win);
  assert.equal(p.left, 1280 - TIP_MARGIN - tip.width);
  assert.equal(p.nub, Math.min(1270 - p.left, tip.width - 10));
});

test("the rail: on the anchor's right, centred on it", () => {
  const a = { left: 14, top: 200, width: 48, height: 40 };
  const p = placeTip(a, tip, win, "right");
  assert.equal(p.side, "right");
  assert.equal(p.left, 62 + TIP_GAP);
  assert.equal(p.top, 220 - 13);
});

test("a tooltip wider than the window fits inside its margins", () => {
  const p = placeTip({ left: 10, top: 300, width: 20, height: 20 }, { width: 400, height: 40 }, { left: 0, top: 0, width: 300, height: 600 });
  assert.equal(p.left, TIP_MARGIN);
});

test("warm: the next tooltip opens at once, only shortly after the last", () => {
  assert.equal(isWarm(0, 100), false);
  assert.equal(isWarm(1000, 1000 + WARM_MS), true);
  assert.equal(isWarm(1000, 1001 + WARM_MS), false);
});

test("a tooltip that repeats the label counts as the same words", () => {
  assert.equal(sameWords("Remove", "Remove"), true);
  assert.equal(sameWords("acme/web", "  acme/web\n"), true);
  assert.equal(sameWords("Open the thread", "Open the thread."), true);
  assert.equal(sameWords("Settle now and stop the run (e)", "Settle\ne"), false);
  assert.equal(sameWords("", ""), false);
});
