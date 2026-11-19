// SPEC 0.5 V0.207: the list keeps its place.
import { test } from "node:test";
import assert from "node:assert/strict";
import { sections, flatOrder } from "../src/store.ts";
import { anchorRow, placeOf, stillRows, trackUnread } from "../src/still-list.ts";
import type { Thread } from "../src/types.ts";
import { thread } from "./helpers.ts";

const at = (m: number) => new Date(Date.UTC(2026, 10, 18, 9, m)).toISOString();
const all = (ts: Thread[]) => Object.fromEntries(ts.map((t) => [t.id, t]));
const unread = (id: string, m: number) => thread(id, "done", "seen", { activityAt: at(m) });
const seen = (id: string, read: number, m = 0) => thread(id, "seen", "seen", { activityAt: at(m), view: { seenAt: at(read) } });
const ids = (l: Thread[]) => l.map((t) => t.id);

test("V0.207: Unread is newest first by when it became unread; later activity does not move a row", () => {
  let m = trackUnread({}, all([unread("a", 1), unread("b", 2)]));
  const later = all([unread("a", 9), unread("b", 2)]);
  m = trackUnread(m, later);
  assert.deepEqual(ids(sections(later, [], m).unread), ["b", "a"]);
  // Read, then unread again: a new time.
  m = trackUnread(m, all([seen("a", 10, 9), unread("b", 2)]));
  m = trackUnread(m, all([unread("a", 12), unread("b", 2)]));
  assert.deepEqual(ids(sections(all([unread("a", 12), unread("b", 2)]), [], m).unread), ["a", "b"]);
});

test("V0.207: Seen is by when it was read, whatever ran since", () => {
  assert.deepEqual(ids(sections(all([seen("old", 1, 50), seen("new", 5, 2)])).seen), ["new", "old"]);
});

test("V0.207: the open row keeps its place after it is read", () => {
  const before = sections(all([unread("a", 3), unread("open", 2), unread("c", 1)]));
  const place = placeOf(before, "open");
  assert.deepEqual(place, { id: "open", section: "unread", index: 1 });
  const live = sections(all([unread("a", 3), seen("open", 9, 2), unread("c", 1)]));
  const r = stillRows(live, place);
  assert.deepEqual(flatOrder(r), ["a", "open", "c"]);
  // Settled, it goes.
  const gone = stillRows(sections(all([unread("a", 3), thread("open", "settled", "settled"), unread("c", 1)])), place);
  assert.deepEqual(ids(gone.unread), ["a", "c"]);
});

test("V0.open-row-only: every other row moves at once, around the open row", () => {
  const before = sections(all([unread("a", 3), unread("open", 2), unread("c", 1)]));
  const place = placeOf(before, "open");
  const live = sections(all([thread("a", "working", "working"), seen("open", 9, 2), seen("c", 8, 1), thread("n", "asking", "needs")]));
  const r = stillRows(live, place);
  assert.deepEqual(ids(r.needs), ["n"]);
  assert.deepEqual(ids(r.unread), ["open"]);
  assert.deepEqual(ids(r.seen), ["c"]);
  assert.deepEqual(ids(r.working), ["a"]);
  // With no thread open the list is the live list.
  assert.equal(stillRows(live, null), live);
});

test("V0.207: the pinned block follows the drawn rows", () => {
  const p = unread("p", 1);
  p.pinnedAt = at(1);
  const place = placeOf(sections(all([p])), "p");
  const live = sections(all([{ ...p, attention: { state: "seen", section: "seen", reason: "" } } as Thread]));
  assert.deepEqual(ids(stillRows(live, place).pinned), ["p"]);
});

test("V0.207: the anchor is the hovered row, else the open row in view, else the first in view; none at the top", () => {
  const o = { hovered: null, open: "o", firstVisible: "f", visible: (x: string) => x === "o", scrollTop: 50 };
  assert.equal(anchorRow({ ...o, hovered: "h", scrollTop: 0 }), "h");
  assert.equal(anchorRow(o), "o");
  assert.equal(anchorRow({ ...o, visible: () => false }), "f");
  assert.equal(anchorRow({ ...o, scrollTop: 0 }), null);
});
