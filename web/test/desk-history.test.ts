import { test } from "node:test";
import assert from "node:assert/strict";
import { deskEntry, deskUrl, isDeskEntry, openStep, popStep, seedTab, tabStep } from "../src/desk-history.ts";
import { navFromHistory } from "../src/nav.ts";

test("V0.145: the owner opening another thread adds one entry", () => {
  const a = deskEntry("t_a", 0);
  assert.equal(openStep(a, "t_b", "owner"), "push");
  assert.equal(openStep(a, "t_a", "owner"), "none", "the same thread again adds none");
});

test("V0.145: Fyr opening a thread adds none; the page's first entry is replaced", () => {
  const a = deskEntry("t_a", 3);
  assert.equal(openStep(a, "t_b", "auto"), "none", "back, forward and the first thread on load add none");
  assert.equal(openStep(null, "t_b", "auto"), "replace");
  assert.equal(openStep(null, "t_b", "owner"), "replace");
  assert.equal(openStep({ viewer: true }, "t_b", "owner"), "replace", "an entry that is not ours");
});

test("V0.145: a tab is written into the open thread's entry", () => {
  const a = deskEntry("t_a", 2);
  assert.deepEqual(tabStep(a, "t_a", "view"), deskEntry("t_a", 2, "view"));
  assert.equal(tabStep(deskEntry("t_a", 2, "view"), "t_a", "view"), null, "the same tab: nothing");
  assert.equal(tabStep(a, "t_b", "view"), null, "another thread's tab: nothing");
  assert.equal(tabStep(null, "t_a", "view"), null);
});

test("V0.145: the URL keeps the other parameters and drops a dialog", () => {
  assert.equal(deskUrl("?mock=rail&hosts=2", "t_a"), "/?mock=rail&hosts=2&open=t_a");
  assert.equal(deskUrl("?mock=rail&open=t_x&tab=pr", "t_a"), "/?mock=rail&open=t_a");
  assert.equal(deskUrl("?open=t_x&dialog=settings", "t_a", "view"), "/?open=t_a&tab=view");
});

test("V0.145: a pop opens that thread; one on screen already or not ours does nothing", () => {
  const none = () => false;
  assert.deepEqual(popStep(deskEntry("t_a", 0), 1, "t_b", none), { open: "t_a" });
  assert.deepEqual(popStep(deskEntry("t_b", 1), 0, "t_a", none), { open: "t_b" });
  assert.equal(popStep(deskEntry("t_a", 0), 1, "t_a", none), null, "a preview viewer's entry over the same thread");
  assert.equal(popStep(null, 1, "t_a", none), null);
  assert.equal(popStep({ screen: { name: "inbox" }, sheet: null }, 1, "t_a", none), null, "a phone entry");
});

test("V0.145: a gone thread is skipped in the direction of travel, never out of Fyr", () => {
  const gone = (id: string) => id === "t_gone";
  assert.deepEqual(popStep(deskEntry("t_gone", 1), 2, "t_c", gone), { skip: -1 });
  assert.deepEqual(popStep(deskEntry("t_gone", 2), 1, "t_a", gone), { skip: 1 });
  assert.equal(popStep(deskEntry("t_gone", 0), 1, "t_b", gone), null, "below the first entry is the page before Fyr");
});

test("V0.145: a desktop entry is a phone Nav too", () => {
  const e = deskEntry("t_a", 4, "view");
  assert.ok(isDeskEntry(e));
  assert.deepEqual(navFromHistory(e, "").screen, { name: "thread", id: "t_a", tab: "view" });
  assert.ok(!isDeskEntry({ screen: { name: "thread", id: "t_a" }, sheet: null }), "a phone entry has no place");
});

test("V0.145: the first entry keeps a deep link's tab for that thread only", () => {
  assert.equal(seedTab("?mock=1&open=t_a&tab=pr", "t_a"), "pr");
  assert.equal(seedTab("?mock=1&open=t_a&tab=pr", "t_b"), undefined);
  assert.equal(seedTab("?mock=1", "t_a"), undefined);
});
