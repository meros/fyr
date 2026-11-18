// V0.109: the marked row in the thread list is the open thread, whatever path opened it.
import { test } from "node:test";
import assert from "node:assert/strict";
import { flatOrder, sections, type SectionKey } from "../src/store.ts";
import { cutSection, foldedRow, heldLabel, parseSectionOpen, rowAfter, markedRow, stepRow } from "../src/selection.ts";
import { thread } from "./helpers.ts";

const OPEN: Record<SectionKey, number> = { needs: 500, working: 500, slots: 500, unread: 2, seen: 2, snoozed: 0, settled: 0 };

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

test("V0.186: a row in a folded section unfolds nothing", () => {
  assert.equal(cutSection(sec(), "z", OPEN), null);
});

test("a row past the limit of an open section shows the whole section", () => {
  assert.equal(cutSection(sec(), "s3", OPEN), "seen");
});

test("a drawn row, a pinned row and a row no section holds need nothing", () => {
  assert.equal(cutSection(sec(), "s1", OPEN), null, "drawn already");
  assert.equal(cutSection(sec(), "p", OPEN), null, "pinned rows are always drawn");
  assert.equal(cutSection(sec(), "gone", OPEN), null, "hidden by search");
  assert.equal(cutSection(sec(), null, OPEN), null);
});

test("V0.186: the folded section that holds the current thread carries the mark", () => {
  const held = foldedRow(sec(), "z", {});
  assert.equal(held?.section, "settled");
  assert.equal(held?.thread.id, "z");
});

test("V0.186: an unfolded section, a pinned row and an unknown row carry no mark on a header", () => {
  assert.equal(foldedRow(sec(), "z", { settled: true }), null, "unfolded: the row is the mark");
  assert.equal(foldedRow(sec(), "w", {}), null, "open by default");
  assert.equal(foldedRow(sec(), "p", {}), null, "pinned rows are always drawn");
  assert.equal(foldedRow(sec(), "gone", {}), null);
  assert.equal(foldedRow(sec(), null, {}), null);
});

test("V0.186: the mark moves with the thread and the folds stay as they are", () => {
  const open = { working: false, seen: false };
  const before = JSON.stringify(open);
  const inWorking = sections({ m: thread("m", "working", "working") });
  const inSeen = sections({ m: thread("m", "seen", "seen") });
  assert.equal(foldedRow(inWorking, "m", open)?.section, "working");
  assert.equal(foldedRow(inSeen, "m", open)?.section, "seen");
  assert.equal(JSON.stringify(open), before);
});

test("V0.186: the header names the group and the thread it holds", () => {
  assert.equal(heldLabel("Settled", 1, "Fix the map"), "Settled, 1 thread, folded, holds the current thread: Fix the map");
  assert.equal(heldLabel("Seen", 12, "x"), "Seen, 12 threads, folded, holds the current thread: x");
});

test("V0.186: next and previous skip folded sections and leave from the header's place", () => {
  const all = ["p", "a", "w", "s1", "z"];
  const shown = ["p", "a", "s1"]; // Working is folded, and so is Settled
  assert.equal(stepRow(all, shown, "a", 1), "s1", "over the folded Working");
  assert.equal(stepRow(all, shown, "s1", -1), "a");
  assert.equal(stepRow(all, shown, "w", 1), "s1", "from inside a folded section: the next drawn row");
  assert.equal(stepRow(all, shown, "w", -1), "a", "and the one before it");
  assert.equal(stepRow(all, shown, "s1", 1), "s1", "the last drawn row stays");
  assert.equal(stepRow(all, shown, "z", 1), null, "nothing drawn after the folded section: stay");
  assert.equal(stepRow(all, shown, null, 1), "p", "no current thread: the first row");
  assert.equal(stepRow(all, shown, "gone", -1), "p", "a thread search hides: the first row");
});

test("V0.186: settling a thread in a folded section steps to a drawn row near its header", () => {
  const s = sec();
  const all = flatOrder(s);
  const shown = all.filter((id) => id !== "z");
  assert.equal(rowAfter(shown, "z", ["z"], all), "s3", "nothing drawn after Settled: the row before its header");
});

test("V0.186: the folds survive a reload; an open Settled does not, its 30 s are gone (V0.48)", () => {
  assert.deepEqual(parseSectionOpen('{"working":false,"snoozed":true,"settled":true,"seen":false}'), { working: false, snoozed: true, seen: false });
  assert.deepEqual(parseSectionOpen('{"settled":false}'), { settled: false });
  assert.deepEqual(parseSectionOpen('{"nope":true,"needs":"x"}'), {}, "only known sections, only booleans");
  assert.deepEqual(parseSectionOpen("not json"), {});
  assert.deepEqual(parseSectionOpen("[1]"), {});
  assert.deepEqual(parseSectionOpen(null), {});
});

test("the row after the ones that leave, else the one before", () => {
  const order = ["a", "w", "s1", "s2"];
  assert.equal(rowAfter(order, "w", ["w"]), "s1");
  assert.equal(rowAfter(order, "s2", ["s2"]), "s1");
  assert.equal(rowAfter(order, "w", ["w", "s1"]), "s2");
  assert.equal(rowAfter(order, "a", ["a", "w", "s1", "s2"]), null);
  assert.equal(rowAfter(order, "a", ["w"]), null, "the open row stays: nothing to move");
});
