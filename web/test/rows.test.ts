import { test } from "node:test";
import assert from "node:assert/strict";
import { rowMeta, rowState } from "../src/rows.ts";
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
  assert.equal(rowState(t), "Approval");
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
