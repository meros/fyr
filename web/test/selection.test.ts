// V0.109: the marked row in the thread list is the open thread, whatever path opened it.
import { test } from "node:test";
import assert from "node:assert/strict";
import { sections, type SectionKey } from "../src/store.ts";
import { markedRow, revealRow, rowAfter } from "../src/selection.ts";
import { thread } from "./helpers.ts";

const FULL: Record<SectionKey, number> = { needs: 500, working: 500, slots: 500, seen: 2, snoozed: 200, settled: 60 };
const OPEN: Record<SectionKey, number> = { needs: 500, working: 500, slots: 500, seen: 2, snoozed: 0, settled: 0 };

function sec() {
  const at = (m: number) => new Date(Date.UTC(2026, 10, 16, 9, m)).toISOString();
  return sections({
    a: thread("a", "asking", "needs"),
    w: thread("w", "working", "working"),
    s1: thread("s1", "seen", "seen", { activityAt: at(3) }),
    s2: thread("s2", "seen", "seen", { activityAt: at(2) }),
    s3: thread("s3", "seen", "seen", { activityAt: at(1) }),
    z: thread("z", "settled", "settled"),
    p: thread("p", "settled", "settled", { pinnedAt: at(0) }),
  });
}

test("the marked row is the open thread; with nothing open, the last thread opened", () => {
  assert.equal(markedRow("w", "a"), "w", "the open thread wins over an older mark");
  assert.equal(markedRow(null, "a"), "a", "nothing open: the last one stays marked for j/k");
  assert.equal(markedRow(null, null), null);
});

test("a row in a closed section opens that section", () => {
  assert.deepEqual(revealRow(sec(), "z", OPEN, FULL), { section: "settled", expand: false });
});

test("a row past the section's limit shows the whole section", () => {
  assert.deepEqual(revealRow(sec(), "s3", OPEN, FULL), { section: "seen", expand: true });
});

test("a drawn row, a pinned row and a row no section holds need nothing", () => {
  assert.equal(revealRow(sec(), "s1", OPEN, FULL), null, "drawn already");
  assert.equal(revealRow(sec(), "p", OPEN, FULL), null, "pinned rows are always drawn");
  assert.equal(revealRow(sec(), "gone", OPEN, FULL), null, "hidden by search or the filter: no mark, the filter stays");
  assert.equal(revealRow(sec(), null, OPEN, FULL), null);
});

test("the row after the ones that leave, else the one before", () => {
  const order = ["a", "w", "s1", "s2"];
  assert.equal(rowAfter(order, "w", ["w"]), "s1");
  assert.equal(rowAfter(order, "s2", ["s2"]), "s1");
  assert.equal(rowAfter(order, "w", ["w", "s1"]), "s2");
  assert.equal(rowAfter(order, "a", ["a", "w", "s1", "s2"]), null);
  assert.equal(rowAfter(order, "a", ["w"]), null, "the open row stays: nothing to move");
});
