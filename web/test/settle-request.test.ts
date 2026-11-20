import { test } from "node:test";
import assert from "node:assert/strict";
import { agentSettleLine, agentSettleOf, patchFor } from "../src/projects.ts";
import { settleChip } from "../src/runcard.ts";
import { needsGroup, rowDetail, rowState } from "../src/rows.ts";
import { thread } from "./helpers.ts";

test("V0.settle-request: the Settle chip shows the agent's why once the run ended, until the thread settles", () => {
  const ask = { at: "2026-11-19T10:00:00Z", why: "PR #12 merged" };
  assert.deepEqual(settleChip({ settleAsk: ask, running: false }), { label: "Settle: PR #12 merged", why: "PR #12 merged" });
  assert.equal(settleChip({ settleAsk: ask, running: true }), null);
  assert.equal(settleChip({ settleAsk: ask, running: false, settledAt: "2026-11-19T10:01:00Z" }), null);
  assert.equal(settleChip({ settleAsk: null, running: false }), null);
  assert.equal(settleChip({ settleAsk: { at: ask.at }, running: false })?.label, "Settle: the agent is done");
});

test("V0.settle-request: a project asks unless set to auto, and saves ask as null", () => {
  assert.equal(agentSettleOf({}), "ask");
  assert.equal(agentSettleOf({ agentSettle: "auto" }), "auto");
  assert.deepEqual(patchFor("agentSettle", "auto"), { agentSettle: "auto" });
  assert.deepEqual(patchFor("agentSettle", "ask"), { agentSettle: null });
  assert.notEqual(agentSettleLine("ask"), agentSettleLine("auto"));
});

test("V0.settle-offer-your-move: a settle offer is in Your move under Approve, and its row reads Settle", () => {
  const t = thread("s", "blocked", "needs", {
    attention: { state: "blocked", section: "needs", reason: "Settle: PR #12 merged" },
    settleAsk: { at: "2026-11-19T10:00:00Z", why: "PR #12 merged" },
  });
  assert.equal(needsGroup(t), "approve");
  assert.equal(rowState(t), "Settle");
  assert.equal(rowDetail(t).startsWith("PR #12 merged"), true);
});
