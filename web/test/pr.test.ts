// SPEC 0.5 V0.43: the PR tab's pure logic: merge status words, the chip, check grouping, ask-the-agent messages.
import { test } from "node:test";
import assert from "node:assert/strict";
import { checkDuration, checksSection, chipStatus, fixCheckMessage, groupChecks, mergeStatus, passedLabel, prChip, reviewAskLabel, reviewMessage, updatedLabel, type PrCheck, type PrDetail } from "../src/pr.ts";

const base: PrDetail = {
  url: "https://github.com/acme/app/pull/88",
  repo: "acme/app",
  number: 88,
  title: "Cards",
  state: "OPEN",
  base: "main",
  head: "fix/cards",
  mergeable: "MERGEABLE",
  mergeState: "CLEAN",
  reviewDecision: "",
  checks: [],
  threads: [],
};
const check = (name: string, state: PrCheck["state"], extra: Partial<PrCheck> = {}): PrCheck => ({ name, state, ...extra });

test("merge status words, important first", () => {
  assert.deepEqual(mergeStatus(base), { text: "Ready to merge", tone: "ok", canMerge: true });
  assert.equal(mergeStatus({ ...base, state: "MERGED" }).text, "Merged");
  assert.equal(mergeStatus({ ...base, state: "MERGED" }).canMerge, false);
  assert.equal(mergeStatus({ ...base, state: "CLOSED" }).text, "Closed");
  assert.equal(mergeStatus({ ...base, draft: true, mergeState: "DRAFT" }).text, "Draft: not ready for review");
  const conflict = mergeStatus({ ...base, mergeable: "CONFLICTING", mergeState: "DIRTY", checks: [check("lint", "fail")] });
  assert.deepEqual(conflict, { text: "Conflicts with main", tone: "fail", canMerge: false, reason: "Conflicts with main" });
  const failing = mergeStatus({ ...base, mergeState: "BLOCKED", checks: [check("lint", "fail"), check("unit", "pass")] });
  assert.equal(failing.text, "Blocked: 1 failing check");
  assert.equal(failing.canMerge, false);
  assert.equal(mergeStatus({ ...base, mergeState: "BLOCKED", checks: [check("a", "fail"), check("b", "fail")] }).text, "Blocked: 2 failing checks");
  // A failing check that is not required (UNSTABLE) does not block.
  const unstable = mergeStatus({ ...base, mergeState: "UNSTABLE", checks: [check("lint", "fail")] });
  assert.equal(unstable.canMerge, true);
  assert.equal(unstable.text, "Ready to merge · 1 failing check is not required");
  assert.equal(mergeStatus({ ...base, mergeState: "BLOCKED", reviewDecision: "REVIEW_REQUIRED" }).text, "Review required");
  assert.equal(mergeStatus({ ...base, mergeState: "BLOCKED", reviewDecision: "CHANGES_REQUESTED" }).text, "Changes requested");
  assert.equal(mergeStatus({ ...base, mergeState: "BEHIND" }).text, "Behind main");
  const running = mergeStatus({ ...base, mergeState: "BLOCKED", checks: [check("e2e", "running"), check("unit", "pass")] });
  assert.equal(running.text, "Checks running: 1");
  assert.equal(running.canMerge, false);
  assert.equal(mergeStatus({ ...base, mergeState: "BLOCKED" }).text, "Blocked by branch rules");
});

test("the chip uses the same words as the tab", () => {
  const pr = { url: base.url, number: 88, state: "OPEN" };
  assert.equal(chipStatus({ ...pr, checks: "fail" }).text, "Checks failing");
  assert.equal(chipStatus({ ...pr, checks: "pass", conflict: true }).text, "Conflicts with the base branch");
  assert.equal(chipStatus({ ...pr, checks: "pass", review: "REVIEW_REQUIRED" }).text, "Review required");
  assert.equal(chipStatus({ ...pr, checks: "pending" }).text, "Checks running");
  assert.equal(chipStatus({ ...pr, checks: "pass" }).text, "Ready to merge");
  assert.deepEqual(prChip({ ...pr, checks: "fail" }), { text: "#88 failing", cls: "fail", title: "#88 · Checks failing" });
  assert.equal(prChip({ ...pr, checks: "pass", conflict: true }).text, "#88 conflicts");
  assert.equal(prChip({ ...pr, checks: "pass", review: "CHANGES_REQUESTED" }).text, "#88 review");
  assert.equal(prChip({ ...pr, checks: "pass" }).text, "#88 ready");
  assert.equal(prChip({ ...pr, checks: "pending" }).text, "#88 running");
  assert.equal(prChip({ ...pr, state: "MERGED" }).text, "#88 merged");
  assert.equal(prChip({ ...pr, state: "MERGED" }, true).text, "merged");
  assert.equal(prChip({ ...pr, checks: "pass", title: "Cards" }).title, "#88 · Ready to merge · Cards");
});

test("checks: failed first, then running, passed and skipped folded", () => {
  const g = groupChecks([check("unit", "pass"), check("e2e", "running"), check("lint", "fail"), check("docs", "skipped"), check("build", "fail"), check("api", "pass")]);
  assert.deepEqual(
    g.failed.map((c) => c.name),
    ["build", "lint"],
  );
  assert.deepEqual(
    g.running.map((c) => c.name),
    ["e2e"],
  );
  assert.deepEqual(
    g.passed.map((c) => c.name),
    ["api", "unit"],
  );
  assert.deepEqual(
    g.skipped.map((c) => c.name),
    ["docs"],
  );
  assert.equal(passedLabel(g), "2 passed, 1 skipped");
  assert.equal(passedLabel(groupChecks([check("a", "pass")])), "1 passed");
});

test("check duration", () => {
  const now = Date.parse("2026-11-14T09:00:00Z");
  assert.equal(checkDuration(check("a", "pass", { startedAt: "2026-11-14T08:58:00Z", completedAt: "2026-11-14T08:59:30Z" }), now), "1m 30s");
  assert.equal(checkDuration(check("a", "running", { startedAt: "2026-11-14T08:58:00Z" }), now), "running 2m 0s");
  assert.equal(checkDuration(check("a", "running"), now), "");
});

test("ask the agent: a failed check", () => {
  assert.equal(fixCheckMessage(check("lint", "fail", { url: "https://github.com/acme/app/actions/runs/1/job/11" })), "The check lint failed: https://github.com/acme/app/actions/runs/1/job/11. Find the cause and fix it.");
  assert.equal(fixCheckMessage(check("lint", "fail")), "The check lint failed. Find the cause and fix it.");
});

test("ask the agent: review comments, listed and trimmed", () => {
  const long = "x".repeat(400);
  const msg = reviewMessage([
    { path: "web/src/Card.tsx", line: 42, outdated: false, comments: [{ author: "anna", body: "Use the\n\ntoken here." }, { author: "octo", body: "Will do." }] },
    { path: "web/src/Grid.tsx", line: 7, outdated: true, comments: [{ author: "ghost", body: long }] },
  ]);
  const lines = msg.split("\n");
  assert.equal(lines[0], "Address these 2 unresolved review comments:");
  assert.equal(lines[2], "1. web/src/Card.tsx:42 (anna): Use the token here.");
  assert.ok(lines[3].startsWith("2. web/src/Grid.tsx:7, outdated (ghost): xxx"));
  assert.ok(lines[3].endsWith("…"));
  assert.ok(lines[3].length < 360);
  assert.match(msg, /Fix what they ask for, or tell me why not\.$/);
  assert.equal(reviewAskLabel(1), "Ask the agent to address 1 review comment");
  assert.equal(reviewAskLabel(2), "Ask the agent to address 2 review comments");
});

test("updated stamp", () => {
  const now = Date.parse("2026-11-14T09:00:00Z");
  assert.equal(updatedLabel("2026-11-14T08:59:55Z", now), "updated 5 s ago");
  assert.equal(updatedLabel("2026-11-14T08:58:00Z", now), "updated 2 min ago");
  assert.equal(updatedLabel(undefined, now), "");
});

test("SPEC 0.5 V0.70 (3): after a merge or close, failed checks are history: muted, under Checks at merge, no ask", () => {
  assert.deepEqual(checksSection("OPEN"), { heading: "Checks", history: false });
  assert.deepEqual(checksSection(undefined), { heading: "Checks", history: false });
  assert.deepEqual(checksSection("MERGED"), { heading: "Checks at merge", history: true });
  assert.deepEqual(checksSection("CLOSED"), { heading: "Checks at merge", history: true });
});
