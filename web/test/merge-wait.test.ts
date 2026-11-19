// SPEC V0.112, V0.136: a merge waits for CI. No check reported on a repo with CI, or a check running, is not green: the
// PR tab's Merge is disabled with the reason, and an agent's merge ask waits (muted, no button) until CI is green.
import { test } from "node:test";
import assert from "node:assert/strict";
import { askYes, needLook, needsOwner } from "../src/offers.ts";
import { columnOf } from "../src/board.ts";
import { chipStatus, mergeStatus, type PrDetail } from "../src/pr.ts";

const head = "029f9c7640b795c9e9edcd8e875f6490e753b593";

test("SPEC V0.136: a merge ask that waits for CI is a muted line with no button, out of Needs you and the board's Needs you", () => {
  const c = { id: "c1", key: "pr:x#846", kind: "code", title: "#846", phase: "review" as const, phaseBy: "observed" as const, createdAt: "", updatedAt: "",
    need: { cause: "waiting" as const, ask: "d_merge", action: "pr.merge", head, text: "Waiting for CI: 2 of 5 checks done", muted: true } };
  assert.deepEqual(needLook(c.need), { button: "", line: "Waiting for CI: 2 of 5 checks done on 029f9c7. Fyr asks you to merge when it is green.", beam: false });
  assert.equal(needsOwner(c), false);
  assert.equal(columnOf(c), "review");
  assert.deepEqual(askYes({ cause: "ask", ask: "d_merge", action: "pr.merge", head }), { label: "Merge", title: "Merges 029f9c7" }, "shown only when green: the yes runs; V0.merge-label: the head in its title");
  assert.deepEqual(askYes({ cause: "ask", ask: "d2", text: "Approve" }), { label: "Yes" });
});

const base: PrDetail = {
  url: "https://github.com/meros/fyr/pull/126",
  repo: "meros/fyr",
  number: 126,
  title: "Icons",
  state: "OPEN",
  base: "main",
  mergeable: "MERGEABLE",
  mergeState: "CLEAN",
  headSha: head,
  checks: [],
  checked: "pending",
  threads: [],
};

test("SPEC V0.112: the PR tab never offers Merge while no check reported or one runs", () => {
  assert.deepEqual(mergeStatus(base), { text: "No check has reported yet", tone: "busy", canMerge: false, reason: "No check has reported yet" });
  const running = mergeStatus({ ...base, checks: [{ name: "test", state: "running" }] });
  assert.equal(running.text, "Checks running: 1");
  assert.equal(running.canMerge, false, "a CLEAN merge state does not make a running check green");
  assert.equal(mergeStatus({ ...base, checked: "none" }).canMerge, true, "a repo with no CI merges");
  assert.equal(mergeStatus({ ...base, checked: "pass", checks: [{ name: "test", state: "pass" }] }).canMerge, true);
  assert.equal(chipStatus({ url: base.url, number: 126, state: "OPEN", checks: "pending" }).canMerge, false);
});
