// SPEC 0.5 V0.207: the list keeps its place.
import { test } from "node:test";
import assert from "node:assert/strict";
import { sections, flatOrder } from "../src/store.ts";
import { NO_HAND, anchorRow, holding, placeOf, stillRows, trackUnread, waitingMoves } from "../src/still-list.ts";
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

test("V0.207: during a hold rows keep section and order, content stays live, other new rows wait", () => {
  const before = all([unread("u", 3), seen("s", 2), thread("w", "working", "working", { runStartedAt: at(1), running: true })]);
  const held = sections(before);
  const u2 = thread("u", "working", "working", { running: true, runStartedAt: at(9), title: "fresh" });
  const live = sections(all([u2, seen("s", 2), thread("w", "seen", "seen", { view: { seenAt: at(8) } }), unread("new", 9)]));
  const r = stillRows(live, held, null);
  assert.deepEqual(ids(r.unread), ["u"]);
  assert.equal(r.unread[0].title, "fresh");
  assert.deepEqual(ids(r.working), ["w"]);
  assert.deepEqual(ids(r.seen), ["s"]);
});

test("V0.your-move: the hold counts each waiting move once, and not the open row", () => {
  const held = sections(all([unread("a", 3), unread("b", 2), unread("c", 1)]));
  const moved = sections(all([unread("a", 3), unread("c", 1), thread("b", "working", "working", { running: true })]));
  assert.equal(waitingMoves(stillRows(moved, held, null), moved, null), 1, "b goes to Moving; a and c only close up");
  assert.equal(waitingMoves(stillRows(moved, held, null), moved, "b"), 0);
  const same = sections(all([unread("a", 3), unread("b", 2), unread("c", 1)]));
  assert.equal(waitingMoves(stillRows(same, held, null), same, null), 0);
  const fresh = sections(all([unread("a", 3), unread("b", 2), unread("c", 1), unread("new", 9)]));
  assert.equal(waitingMoves(stillRows(fresh, held, null), fresh, null), 1);
});

test("V0.207, V0.your-move: a new Your-move row comes in at once at the top of its group, also from another section", () => {
  const held = sections(all([thread("n", "asking", "needs"), unread("u", 1)]));
  const live = sections(all([thread("n", "asking", "needs"), thread("u", "asking", "needs", { activityAt: at(5) }), thread("x", "failed", "needs", { activityAt: at(4) })]));
  const r = stillRows(live, held, null);
  assert.deepEqual(ids(r.needs), ["u", "n", "x"]);
  assert.deepEqual(ids(r.unread), []);
});

test("V0.207: settle and snooze act at once during a hold, and the queue stays live", () => {
  const held = sections(all([unread("a", 1), unread("b", 2), seen("q", 1)]));
  const live = sections(all([thread("a", "settled", "settled"), thread("b", "snoozed", "snoozed"), seen("q", 1)]), [{ owner: "q" } as never]);
  const r = stillRows(live, held, null);
  assert.deepEqual(ids(r.unread), []);
  assert.deepEqual(ids(r.settled), ["a"]);
  assert.deepEqual(ids(r.snoozed), ["b"]);
  assert.deepEqual(ids(r.seen), []);
  assert.deepEqual(ids(r.slots), ["q"]);
});

test("V0.207: the open row keeps its place after it is read, with no hold", () => {
  const before = sections(all([unread("a", 3), unread("open", 2), unread("c", 1)]));
  const place = placeOf(before, "open");
  assert.deepEqual(place, { id: "open", section: "unread", index: 1 });
  const live = sections(all([unread("a", 3), seen("open", 9, 2), unread("c", 1)]));
  const r = stillRows(live, null, place);
  assert.deepEqual(flatOrder(r), ["a", "open", "c"]);
  // Settled, it goes.
  const gone = stillRows(sections(all([unread("a", 3), thread("open", "settled", "settled"), unread("c", 1)])), null, place);
  assert.deepEqual(ids(gone.unread), ["a", "c"]);
});

test("V0.207: the row the owner marks seen goes to Seen at once, also during a hold", () => {
  const held = sections(all([unread("u", 2), unread("v", 1), seen("s", 1)]));
  const live = sections(all([seen("u", 9, 2), unread("v", 1), seen("s", 1)]));
  const r = stillRows(live, held, null, "u");
  assert.deepEqual(ids(r.unread), ["v"]);
  assert.deepEqual(ids(r.seen), ["u", "s"]);
});

test("V0.207: a row back from the queue, Snoozed or Settled shows at once during a hold", () => {
  const held = sections(all([thread("z", "snoozed", "snoozed"), seen("q", 2), seen("s", 1)]), [{ owner: "q" } as never]);
  const live = sections(all([seen("z", 3), seen("q", 2), seen("s", 1)]));
  assert.deepEqual(ids(stillRows(live, held, null).seen), ["z", "q", "s"]);
});

test("V0.207: the pinned block follows the drawn rows", () => {
  const p = unread("p", 1);
  p.pinnedAt = at(1);
  const held = sections(all([p]));
  const live = sections(all([{ ...p, attention: { state: "seen", section: "seen", reason: "" } } as Thread]));
  assert.deepEqual(ids(stillRows(live, held, null).pinned), ["p"]);
});

test("V0.207: the hold follows the hand", () => {
  assert.equal(holding(NO_HAND, 100), false);
  assert.equal(holding({ ...NO_HAND, inside: true }, 100), true);
  assert.equal(holding({ ...NO_HAND, touching: true }, 100), true);
  assert.equal(holding({ ...NO_HAND, until: 101 }, 100), true);
  assert.equal(holding({ ...NO_HAND, until: 100 }, 100), false);
});

test("V0.207: the anchor is the hovered row, else the open row in view, else the first in view; none at the top", () => {
  const o = { hovered: null, open: "o", firstVisible: "f", visible: (x: string) => x === "o", scrollTop: 50 };
  assert.equal(anchorRow({ ...o, hovered: "h", scrollTop: 0 }), "h");
  assert.equal(anchorRow(o), "o");
  assert.equal(anchorRow({ ...o, visible: () => false }), "f");
  assert.equal(anchorRow({ ...o, scrollTop: 0 }), null);
});
