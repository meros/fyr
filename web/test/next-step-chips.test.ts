// SPEC V0.next-step-chips: an optional next step is a chip under the newest message; an optional run request is never in
// the dock, and its card stands in the conversation only once it ran.
import { test } from "node:test";
import assert from "node:assert/strict";
import { cardShows, chips, dockRun, waitingRuns } from "../src/runcard.ts";
import type { Offer, OfferRun } from "../src/types.ts";

const req = (id: string, p: Partial<Offer> = {}): Offer => ({ id, on: "thread", label: "Run", run: "script", request: true, command: `echo ${id}`, why: id, by: "agent", digest: id, ...p });
const prompt = (id: string, p: Partial<Offer> = {}): Offer => ({ id, on: "thread", label: id, run: "agent", prompt: id, by: "agent", digest: id, ...p });
const ran = (action: string): OfferRun => ({ action, label: "Run", by: "owner", at: "2026-11-19T10:00:00Z", ok: true });

test("V0.next-step-chips (2): the agent's prompts and the optional requests not yet run are the chips", () => {
  const t = { offers: [prompt("fix"), req("block"), req("opt", { optional: true }), req("opt-ran", { optional: true }), prompt("fyr.merge", { by: "observed" })], runs: [ran("opt-ran")] };
  const c = chips(t);
  assert.deepEqual(c.prompts.map((o) => o.id), ["fix"]);
  assert.deepEqual(c.runs.map((o) => o.id), ["opt"]);
});

test("V0.next-step-chips (3): an optional request is never in the dock and never counts as waiting", () => {
  const t = { offers: [req("opt", { optional: true })], runs: [], decisions: [] };
  assert.equal(dockRun(t), null);
  assert.equal(waitingRuns(t), 0);
  assert.equal(dockRun({ ...t, offers: [req("block"), req("opt", { optional: true })] })?.o.id, "block");
});

test("V0.next-step-chips (4): an optional request's card shows once it ran; a blocking one always", () => {
  assert.equal(cardShows(req("opt", { optional: true }), []), false);
  assert.equal(cardShows(req("opt", { optional: true }), [ran("opt")]), true);
  assert.equal(cardShows(req("block"), []), true);
});
