// SPEC 0.5 V0.207: the list keeps its place.
import { test } from "node:test";
import assert from "node:assert/strict";
import { sections, flatOrder } from "../src/store.ts";
import { anchorRow, placeOf, stillRows, trackArrivals } from "../src/still-list.ts";
import type { Thread } from "../src/types.ts";
import { thread } from "./helpers.ts";

const at = (m: number) => new Date(Date.UTC(2026, 10, 18, 9, m)).toISOString();
const all = (ts: Thread[]) => Object.fromEntries(ts.map((t) => [t.id, t]));
const unread = (id: string, m: number) => thread(id, "done", "seen", { activityAt: at(m) });
const seen = (id: string, read: number, m = 0) => thread(id, "seen", "seen", { activityAt: at(m), view: { seenAt: at(read) } });
const ids = (l: Thread[]) => l.map((t) => t.id);

test("V0.stable-groups: Unread is oldest first by when it became unread; later activity does not move a row", () => {
  let m = trackArrivals({}, all([unread("a", 1), unread("b", 2)]), Date.parse(at(3)));
  const later = all([unread("a", 9), unread("b", 2)]);
  m = trackArrivals(m, later, Date.parse(at(9)));
  assert.deepEqual(ids(sections(later, [], m).unread), ["a", "b"]);
  // Read, then unread again: a new time, so it goes last.
  m = trackArrivals(m, all([seen("a", 10, 9), unread("b", 2)]), Date.parse(at(10)));
  m = trackArrivals(m, all([unread("a", 12), unread("b", 2)]), Date.parse(at(12)));
  assert.deepEqual(ids(sections(all([unread("a", 12), unread("b", 2)]), [], m).unread), ["b", "a"]);
});

test("V0.stable-groups: Working keeps its order when a thread starts a new run; a new thread goes last", () => {
  const w = (id: string, run: number) => thread(id, "working", "working", { runStartedAt: at(run), activityAt: at(run) });
  let m = trackArrivals({}, all([w("a", 1), w("b", 2)]), Date.parse(at(3)));
  assert.deepEqual(ids(sections(all([w("a", 1), w("b", 2)]), [], m).working), ["a", "b"], "first sight: by run start, oldest first");
  // a starts a new run, c arrives with an older run start (an agent started long ago): neither jumps.
  const next = all([w("a", 20), w("b", 2), w("c", 0)]);
  m = trackArrivals(m, next, Date.parse(at(21)));
  assert.deepEqual(ids(sections(next, [], m).working), ["a", "b", "c"]);
});

test("V0.stable-groups: inside a Your move group the order is arrival, not state; a new row goes last", () => {
  const failed = (id: string, t: number) => thread(id, "failed", "needs", { activityAt: at(t) });
  const blocked = (id: string, t: number) => thread(id, "blocked", "needs", { activityAt: at(t), attention: { state: "blocked", section: "needs", reason: "x" } });
  let m = trackArrivals({}, all([failed("f", 1), blocked("b", 2)]), Date.parse(at(3)));
  const next = all([failed("f", 30), blocked("b", 2), failed("n", 4)]);
  m = trackArrivals(m, next, Date.parse(at(31)));
  assert.deepEqual(ids(sections(next, [], m).needs), ["f", "b", "n"]);
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
  assert.deepEqual(flatOrder(r), ["c", "open", "a"]);
  // Settled, it goes.
  const gone = stillRows(sections(all([unread("a", 3), thread("open", "settled", "settled"), unread("c", 1)])), place);
  assert.deepEqual(ids(gone.unread), ["c", "a"]);
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

test("V0.finish-debounce: a thread that stops working stays in Moving for the hold, then shows finished", async () => {
  const { steadyThreads, FINISH_HOLD_MS } = await import("../src/still-list.ts");
  const working = all([thread("a", "working", "working")]);
  const done = all([thread("a", "done", "seen")]);
  let r = steadyThreads({}, null, working, 0);
  assert.equal(r.next, null);
  r = steadyThreads(r.holds, working, done, 1000);
  assert.equal(sectionOfIds(r.threads), "working");
  assert.equal(r.next, 1000 + FINISH_HOLD_MS);
  r = steadyThreads(r.holds, done, done, 1000 + FINISH_HOLD_MS - 1);
  assert.equal(sectionOfIds(r.threads), "working");
  r = steadyThreads(r.holds, done, done, 1000 + FINISH_HOLD_MS);
  assert.equal(sectionOfIds(r.threads), "unread");
  assert.equal(r.next, null);
});

test("V0.finish-debounce: a run that starts again within the hold never shows finished; other moves are at once", async () => {
  const { steadyThreads } = await import("../src/still-list.ts");
  const working = all([thread("a", "working", "working")]);
  const done = all([thread("a", "done", "seen")]);
  let r = steadyThreads({}, working, done, 0);
  r = steadyThreads(r.holds, done, working, 2000);
  assert.equal(sectionOfIds(r.threads), "working");
  assert.deepEqual(r.holds, {});
  // Asking goes to Your move at once.
  const asking = all([thread("a", "asking", "needs")]);
  r = steadyThreads({}, working, asking, 0);
  assert.equal(sectionOfIds(r.threads), "needs");
  // Finished from anything but working is not held.
  r = steadyThreads({}, asking, done, 0);
  assert.equal(sectionOfIds(r.threads), "unread");
});

function sectionOfIds(ts: Record<string, Thread>): string | undefined {
  const s = sections(ts);
  return (["needs", "working", "unread", "seen"] as const).find((k) => s[k].some((t) => t.id === "a"));
}
