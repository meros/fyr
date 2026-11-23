// SPEC V0.next-step-dock: what waits on the owner stands in one dock above the composer; a question or approval first,
// then the newest run request not yet run.
import { test } from "node:test";
import assert from "node:assert/strict";
import { cardDomId, dockRun, firstCommandLine, waitingRuns } from "../src/runcard.ts";
import type { Decision, Offer, OfferRun } from "../src/types.ts";

const req = (id: string): Offer => ({ id, on: "thread", label: "Run", run: "script", request: true, command: `echo ${id}`, why: id, by: "agent", digest: id });
const ran = (action: string): OfferRun => ({ action, label: "Run", by: "owner", at: "2026-11-19T10:00:00Z", ok: true });
const ask = { id: "d1", kind: "ask" } as unknown as Decision;

test("V0.next-step-dock (3): the newest request not yet run stands in the dock, with how many wait", () => {
  const t = { offers: [req("a"), req("b"), { ...req("x"), request: false }], runs: [], decisions: [] };
  assert.deepEqual(dockRun(t), { o: t.offers[1], waiting: 2 });
});

test("V0.next-step-dock (4): a request that ran leaves the dock; none left is no dock", () => {
  assert.equal(dockRun({ offers: [req("a"), req("b")], runs: [ran("b")], decisions: [] })?.o.id, "a");
  assert.equal(dockRun({ offers: [req("a")], runs: [ran("a")], decisions: [] }), null);
  assert.equal(dockRun({ offers: [], runs: [], decisions: [] }), null);
});

test("V0.next-step-dock (3): a question or approval comes first; the requests count in its +N more", () => {
  const t = { offers: [req("a"), req("b")], runs: [ran("a")], decisions: [ask] };
  assert.equal(dockRun(t), null);
  assert.equal(waitingRuns(t), 1);
});

test("V0.next-step-dock (3): the dock shows the command's first line, … when there are more", () => {
  assert.equal(firstCommandLine("nixos-rebuild switch"), "nixos-rebuild switch");
  assert.equal(firstCommandLine("cd /etc\nnixos-rebuild switch\n"), "cd /etc …");
  assert.equal(firstCommandLine("one\n"), "one");
});

test("V0.next-step-dock (3): Show in conversation finds the card by a safe element id", () => {
  assert.equal(cardDomId("run-1"), "runc-run-1");
  assert.equal(cardDomId("a b/c"), "runc-a_b_c");
});
