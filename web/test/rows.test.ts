import { test } from "node:test";
import assert from "node:assert/strict";
import { rowMeta, rowProjectName, rowState, stateNamesPr, trimEcho, clipName } from "../src/rows.ts";
import type { Decision } from "../src/types.ts";
import { thread } from "./helpers.ts";

const asking = (d: Partial<Decision>) => thread("a", "asking", "needs", { decisions: [{ id: "d1", kind: "ask", ...d } as Decision] });

// review 2026-11-15: the desktop and the phone inbox draw a row that needs you with the same words.
test("an ask row counts its questions and shows the first", () => {
  const t = asking({ kind: "ask", questions: [{ question: "Which?", multiSelect: false, options: [] }, { question: "Why?", multiSelect: false, options: [] }] });
  assert.equal(rowState(t), "2 questions");
  assert.equal(rowMeta(t), "Which?");
});

test("an approval row names the tool and the command", () => {
  const t = asking({ kind: "approval", tool: "Bash", input: { command: "ls" } });
  assert.equal(rowState(t), "Allow Bash");
  assert.equal(rowMeta(t), "Allow Bash ls");
  assert.equal(rowMeta(asking({ kind: "approval" })), "Allow tool");
});

test("an effect row shows its title and why", () => {
  const t = asking({ kind: "effect", effect: { action: "pr.merge", args: { pr: "https://github.com/o/r/pull/12" }, reason: "checks are green" } } as Partial<Decision>);
  assert.equal(rowMeta(t), "Merge pull request #12 · checks are green");
});

test("a failed row names the retry key only with a keyboard", () => {
  const t = thread("f", "failed", "needs", { failure: { message: "exit 1" } as never });
  assert.equal(rowState(t), "Failed");
  assert.equal(rowMeta(t, { keys: true }), "exit 1 · r to retry");
  assert.equal(rowMeta(t), "exit 1");
});

test("other rows take the attention reason and the summary", () => {
  const t = thread("b", "blocked", "needs", { summary: "waiting on CI" });
  t.attention.reason = "checks failed";
  assert.equal(rowState(t), "checks failed");
  assert.equal(rowMeta(t), "waiting on CI");
});

// V0.135: the row's project is its icon; the name shows on the second line only when the icon is not an image and
// no project filter is on.
test("a row names its project only without an image icon and without a project filter", () => {
  assert.equal(rowProjectName({ name: "Fyr", image: "fy1" }, "Fyr", null), null);
  assert.equal(rowProjectName({ name: "dotfiles" }, "dotfiles", null), "dotfiles");
  assert.equal(rowProjectName({ name: "dotfiles" }, "dotfiles", "p_dot"), null);
  assert.equal(rowProjectName(undefined, "loose", null), "loose");
  assert.equal(rowProjectName(undefined, "", null), null);
});

// SPEC V0.210 (5): both fronts draw these words: the marker first, then what Fyr read.
test("a question read in text is marked as such and shows the question", () => {
  const t = thread("q", "blocked", "needs", { attention: { state: "blocked", section: "needs", reason: "Which logo?" }, textAsk: { question: "Which logo?", at: "2026-11-18T10:00:00Z", run: 1 } });
  assert.equal(rowState(t), "Question in text");
  assert.equal(rowMeta(t), "Which logo?");
});

// SPEC V0.210 (4): a flag under a harder cause does not rename that cause.
test("a blocked row with another reason keeps it", () => {
  const t = thread("q", "blocked", "needs", { attention: { state: "blocked", section: "needs", reason: "Fix: CI failed" }, textAsk: { question: "Which logo?", at: "2026-11-18T10:00:00Z", run: 1 } });
  assert.equal(rowState(t), "Fix: CI failed");
});

// V0.row-calm: the line under the title does not say twice what its state word says.
test("the detail leaves out what the state word already says", () => {
  assert.equal(trimEcho("Merge #4170", "Merge pull request #4170 · CI is green"), "CI is green");
  assert.equal(trimEcho("Merge #4170", "Merge pull request #4170"), "");
  assert.equal(trimEcho("Update Fyr", "Update Fyr to a new version · Hardening"), "Hardening");
  assert.equal(trimEcho("API error", "API error 529: overloaded"), "529: overloaded");
  assert.equal(trimEcho("Merge #41", "Merge pull request #4170"), "Merge pull request #4170");
  assert.equal(trimEcho("2 questions", "Which?"), "Which?");
});

test("a row's PR chip is left out when the state word names that PR", () => {
  const t = asking({ kind: "effect", effect: { action: "pr.merge", args: { pr: "https://github.com/o/r/pull/12" } } } as Partial<Decision>);
  assert.equal(rowState(t), "Merge #12");
  assert.equal(stateNamesPr({ ...t, pr: { number: 12 } as never }), true);
  assert.equal(stateNamesPr({ ...t, pr: { number: 123 } as never }), false);
  assert.equal(stateNamesPr({ ...asking({ kind: "ask" }), pr: { number: 12 } as never }), false);
});

test("a Needs you reason puts its short word first and the change on the line", () => {
  const ch = [{ id: "c", title: "Placement rows load in one read" }] as never;
  const t = thread("b", "blocked", "needs", { attention: { state: "blocked", section: "needs", reason: "Placement rows load in one read: Merge" }, changes: ch });
  assert.equal(rowState(t), "Merge");
  assert.ok(rowMeta(t).startsWith("Placement rows load in one read"));
  const run = thread("r", "blocked", "needs", { attention: { state: "blocked", section: "needs", reason: "Run: install the hook" } });
  assert.equal(rowState(run), "Run");
  const other = thread("o", "blocked", "needs", { attention: { state: "blocked", section: "needs", reason: "Waiting: for CI" } });
  assert.equal(rowState(other), "Waiting: for CI");
});

test("a long project name is cut in the text, short ones stay whole", () => {
  assert.equal(clipName("acme-platform-infrastructure"), "acme-platform…");
  assert.equal(clipName("dotfiles"), "dotfiles");
  assert.equal(clipName("exactly-14-ch!"), "exactly-14-ch!");
});
