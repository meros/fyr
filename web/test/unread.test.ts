// V0.189: a finished thread the owner has not looked at is Unread, not Seen. The client derives it from the attention
// the host already sends, so a host of any version since V0.101 lands in the right section.
import { test } from "node:test";
import assert from "node:assert/strict";
import { SECTIONS, bulkSettle, bulkSettleText, flatOrder, isOpen, sectionOf, sections, selectAllIds, sweptUnread } from "../src/store.ts";
import { keyHints } from "../src/keys.ts";
import { seenToggle, threadMenu } from "../src/menu.ts";
import { changeChip } from "../src/offers.ts";
import { filterEmpty, inView } from "../src/filters.ts";
import { decodeSectionOpen, foldedRow } from "../src/selection.ts";
import type { Change, Thread } from "../src/types.ts";
import { thread } from "./helpers.ts";

const byId = (...ts: Thread[]) => Object.fromEntries(ts.map((t) => [t.id, t]));
const unread = (id: string, extra = {}) => thread(id, "done", "seen", extra);
const seen = (id: string, extra = {}) => thread(id, "seen", "seen", extra);
const ids = (l: Thread[]) => l.map((t) => t.id);

test("a finished unread thread is in Unread, not Seen", () => {
  const sec = sections(byId(unread("u"), seen("s")));
  assert.deepEqual(ids(sec.unread), ["u"]);
  assert.deepEqual(ids(sec.seen), ["s"]);
  assert.deepEqual(ids(sec.needs), [], "V0.101 stands: a finished thread does not go back to Needs you");
});

test("an older host's done thread lands in Unread: only state and section are read", () => {
  const dto = { state: "done", section: "seen" } as Thread["attention"];
  assert.equal(sectionOf({ ...seen("old"), attention: dto }), "unread");
});

test("opening (Mark seen) moves it to Seen, Mark unseen moves it back", () => {
  const before = unread("a");
  assert.equal(sectionOf(before), "unread");
  const after = { ...before, attention: { state: "seen", section: "seen", reason: "" } } as Thread;
  assert.equal(sectionOf(after), "seen");
  assert.equal(sectionOf({ ...after, attention: before.attention }), "unread");
});

test("the order puts Unread directly above Seen, and the g keys keep their meaning", () => {
  assert.deepEqual(SECTIONS.map((s) => s.key), ["needs", "unread", "seen", "working", "slots", "snoozed", "settled"]);
  const g = Object.fromEntries(SECTIONS.filter((s) => s.g).map((s) => [s.key, s.g]));
  assert.deepEqual(g, { needs: 1, working: 2, seen: 3, snoozed: 4, settled: 5, unread: 6 });
  const help = keyHints({ zone: "list", view: null, card: null, hasSelection: false, picked: 0, failed: false, undo: false, running: false, inText: false } as never);
  assert.ok(help.some(([k]) => k === "g1–6"), "the shortcuts help lists g6");
});

test("j/k walks Unread before Seen; Unread folds on its own and is open by default", () => {
  const sec = sections(byId(seen("s"), unread("u")));
  assert.deepEqual(flatOrder(sec), ["u", "s"]);
  assert.equal(isOpen("unread", {}), true);
  assert.equal(isOpen("seen", { unread: false }), true);
  assert.equal(inView(unread("u"), { unread: false }), false);
  assert.equal(inView(seen("s"), { unread: false }), true);
  assert.equal(filterEmpty(byId(unread("u")), { hostsOff: ["x"], project: null }, { unread: false }), "1 more in folded section.");
});

test("Unread's fold is remembered on its own, and a folded Unread holds the current thread (V0.186)", () => {
  assert.deepEqual(decodeSectionOpen({ unread: false, seen: true }), { unread: false, seen: true });
  const sec = sections(byId(unread("u"), seen("s")));
  assert.equal(foldedRow(sec, "u", { unread: false })?.section, "unread");
  assert.equal(foldedRow(sec, "u", { seen: false }), null, "folding Seen says nothing about a thread in Unread");
  assert.equal(foldedRow(sec, "s", { unread: false }), null);
});

test("a snooze that wakes on a finished, unread thread lands in Unread; one already seen in Seen (V0.185)", () => {
  assert.equal(sectionOf(thread("w", "snoozed", "snoozed")), "snoozed");
  assert.equal(sectionOf({ ...thread("w", "snoozed", "snoozed"), attention: unread("w").attention }), "unread");
  assert.equal(sectionOf({ ...thread("w", "snoozed", "snoozed"), attention: seen("w").attention }), "seen");
});

test("a pinned unread thread is in the pinned block, and Unread still holds it", () => {
  const sec = sections(byId(unread("p", { pinnedAt: "2026-11-17T09:00:00Z" })));
  assert.deepEqual(ids(sec.pinned), ["p"]);
  assert.deepEqual(ids(sec.unread), ["p"]);
});

test("a sweep leaves Unread alone and says so; threads picked one by one settle even when unread", () => {
  const pin = { pinnedAt: "2026-11-17T09:00:00Z" };
  const ts = [seen("s"), unread("u"), seen("p", pin), unread("up", pin)];
  assert.deepEqual(bulkSettle(ts, true), { ids: ["s"], kept: 2, unread: 1, running: 0 }, "⇧E on a section");
  assert.deepEqual(bulkSettle(ts), { ids: ["s", "u"], kept: 2, unread: 0, running: 0 }, "the owner's own picks");
  assert.equal(bulkSettleText(0, 0, 2), "Nothing settled; 2 unread threads kept");
  assert.equal(bulkSettleText(3, 0, 1), "Settled 3 threads; 1 unread thread kept");
  assert.equal(bulkSettleText(3, 1, 1), "Settled 3 threads; 1 pinned and 1 unread threads kept");
});

test("the phone's Select all leaves unread rows unpicked and counts them; a row tapped after it is the owner's pick", () => {
  const pin = { pinnedAt: "2026-11-17T09:00:00Z" };
  const ts = [seen("s"), unread("u"), unread("v"), unread("up", pin)];
  const picked = selectAllIds(ts);
  assert.deepEqual(picked, ["s", "up"], "a pinned thread stays picked: the settle counts it as pinned (V0.162)");
  assert.equal(sweptUnread(ts, new Set(picked)), 2);
  assert.equal(sweptUnread(ts, new Set([...picked, "u"])), 1);
});

test("the menus offer Mark seen on an unread thread and Mark unseen on a seen one", () => {
  const label = (t: Thread) => (threadMenu(t).find((x) => "id" in x && /^mark/.test(x.id)) as { label: string }).label;
  assert.equal(label(unread("u")), "Mark seen");
  assert.equal(label(seen("s")), "Mark unseen");
  assert.equal(seenToggle(unread("u")).action, "thread.markSeen");
  assert.equal(seenToggle(seen("s")).action, "thread.markUnseen");
});

const change = (p: Partial<Change>): Change => ({ id: "c", key: "k", kind: "code", title: "Fix", phase: "working", phaseBy: "agent", createdAt: "2026-11-14T08:00:00Z", updatedAt: "2026-11-14T08:00:00Z", ...p }) as Change;

test("the chip of an open change does not say working: the agent may have stopped", () => {
  const c = changeChip({ changes: [change({})], pr: null }, true)!;
  assert.equal(c.text, "change open");
  assert.doesNotMatch(c.title, /working/);
  assert.equal(changeChip({ changes: [change({})], pr: null })!.text, "Fix · change open");
  assert.equal(changeChip({ changes: [change({ label: "applied" })], pr: null }, true)!.text, "applied", "the agent's own word stays");
  assert.equal(changeChip({ changes: [change({ phase: "review" })], pr: null }, true)!.text, "review");
});
