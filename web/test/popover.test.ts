// V0.popover-place: a popover opens at its anchor, on the other side when its own is short, lined up with the anchor
// before it is pushed inside the window, capped (it scrolls) when neither side holds it, and its nub points at the anchor.
import { test } from "node:test";
import assert from "node:assert/strict";
import { GAP, MARGIN, anchorGone, placePopover } from "../src/popover.ts";

const win = { left: 0, top: 0, width: 1280, height: 800 };
const card = { width: 340, height: 200 };

test("below: under the anchor, its left edges lined up, the nub at the anchor's middle", () => {
  const a = { left: 600, top: 56, width: 100, height: 24 };
  const p = placePopover(a, card, win, "below");
  assert.equal(p.side, "below");
  assert.equal(p.left, 600);
  assert.equal(p.top, 80 + GAP);
  assert.equal(p.nub, 50);
});

test("below, near the right edge: the card's right edge lines up with the anchor's, not the window's", () => {
  // the quota chip at the end of the header: before, the card was pushed against the window, away from its chip
  const a = { left: 1062, top: 56, width: 120, height: 24 };
  const p = placePopover(a, card, win, "below");
  assert.equal(p.left + card.width, 1182);
  assert.equal(p.nub, 1122 - p.left);
});

test("below, an anchor wider than the window's room: pushed inside the margin", () => {
  const a = { left: 1262, top: 56, width: 16, height: 24 };
  const p = placePopover(a, card, win, "below");
  assert.equal(p.left, 1280 - MARGIN - card.width);
  assert.ok(p.nub <= card.width - 14, "the nub stays clear of the corner");
});

test("below, at the bottom of the window: opens above", () => {
  const a = { left: 100, top: 700, width: 100, height: 24 };
  const p = placePopover(a, card, win, "below");
  assert.equal(p.side, "above");
  assert.equal(p.top + card.height, 700 - GAP);
});

test("below, neither side holds it: the side with more room, capped", () => {
  const a = { left: 100, top: 300, width: 100, height: 24 };
  const p = placePopover(a, { width: 340, height: 900 }, win, "below");
  assert.equal(p.side, "below");
  assert.equal(p.maxHeight, 800 - MARGIN - (324 + GAP));
});

test("right: beside the anchor, top lined up; a card taller than the room under it moves up, the nub still at the anchor", () => {
  const a = { left: 5, top: 556, width: 64, height: 24 };
  const tall = { width: 340, height: 440 };
  const p = placePopover(a, tall, win, "right");
  assert.equal(p.side, "right");
  assert.equal(p.left, 69 + GAP);
  assert.equal(p.top, 800 - MARGIN - 440);
  assert.equal(p.nub, 556 + 12 - p.top);
});

test("right, no room on the right: opens on the left", () => {
  const a = { left: 1100, top: 100, width: 100, height: 24 };
  const p = placePopover(a, card, win, "right");
  assert.equal(p.side, "left");
  assert.equal(p.left, 1100 - GAP - card.width);
});

test("taller than the window: capped to it", () => {
  const p = placePopover({ left: 5, top: 10, width: 64, height: 24 }, { width: 340, height: 2000 }, win, "right");
  assert.equal(p.top, MARGIN);
  assert.equal(p.maxHeight, 800 - 2 * MARGIN);
});

test("the visible area may start below the page's top (a phone's keyboard, a pinch)", () => {
  const area = { left: 0, top: 300, width: 390, height: 400 };
  const p = placePopover({ left: 20, top: 650, width: 80, height: 24 }, { width: 340, height: 120 }, area, "below");
  assert.equal(p.side, "above");
  assert.ok(p.top >= 300 + MARGIN);
  assert.ok(p.left + 340 <= 390 - MARGIN + 0.5);
});

test("anchorGone: off the area, collapsed or missing", () => {
  assert.equal(anchorGone(null, win), true);
  assert.equal(anchorGone({ left: 0, top: 0, width: 0, height: 0 }, win), true);
  assert.equal(anchorGone({ left: 10, top: -40, width: 50, height: 24 }, win), true);
  assert.equal(anchorGone({ left: 10, top: 810, width: 50, height: 24 }, win), true);
  assert.equal(anchorGone({ left: 10, top: 10, width: 50, height: 24 }, win), false);
});
