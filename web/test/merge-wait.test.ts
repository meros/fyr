// SPEC V0.112: a merge waits for CI. No check reported on a repo with CI, or a check running, is not green: the ask's
// Merge and the PR tab's Merge are disabled with the reason, on desktop and phone alike (one component each).
import { test } from "node:test";
import assert from "node:assert/strict";
import { askYes, needLook } from "../src/offers.ts";
import { chipStatus, mergeStatus, type PrDetail } from "../src/pr.ts";

const head = "029f9c7640b795c9e9edcd8e875f6490e753b593";
const wait = "CI has not passed on 029f9c7 yet: Merge waits for the checks";

test("SPEC V0.112: a merge ask on pending checks shows Merge disabled, with the host's reason", () => {
  const n = { cause: "ask" as const, ask: "d_merge", action: "pr.merge", head, text: "Merge #126", wait };
  assert.deepEqual(askYes(n), { label: "Merge 029f9c7", wait });
  assert.equal(needLook(n).line, `Merge #126 · ${wait}`);
  assert.deepEqual(askYes({ ...n, wait: undefined }), { label: "Merge 029f9c7", wait: undefined }, "green: the yes runs");
  assert.deepEqual(askYes({ cause: "ask", ask: "d2", text: "Approve" }), { label: "Yes", wait: undefined });
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
