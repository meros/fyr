// The words of an approval or effect card are built once for both fronts (src/approval-view.ts).
import { test } from "node:test";
import assert from "node:assert/strict";
import { approvalView } from "../src/approval-view.ts";
import { askCommit } from "../src/mobile.ts";
import { goTo, initCard, pickStay, skip, submitState } from "../src/question.ts";
import type { Decision } from "../src/types.ts";

const approval = { id: "a1", kind: "approval", tool: "Bash", input: { command: "git status", description: "Show the tree" } } as Decision;
const effect = (action: string, args: Record<string, unknown> = {}) => ({ id: "e1", kind: "effect", effect: { action, args, reason: "" } }) as Decision;

test("a tool approval has the approval body, Allow and Deny with their lines, and the host", () => {
  const v = approvalView(approval, { cwd: "/home/owner/acme", host: "host-a" });
  assert.ok(v.approval);
  assert.equal(v.effect, null);
  assert.equal(v.label, "Approval");
  assert.equal(v.title, v.approval.title);
  assert.equal(v.host, "host-a");
  assert.deepEqual([v.yes, v.no], ["Allow", "Deny"]);
  assert.equal(v.yesDesc, "Run it once.");
  assert.equal(v.noDesc, "The agent is told no and continues.");
});

test("an effect card takes its words from the effect, has no yes line, and keeps the PR in the title", () => {
  const v = approvalView(effect("pr.merge", { pr: "https://example.com/acme/x/pull/15" }), { cwd: "/w", host: "" }, { url: "https://example.com/acme/x/pull/15", number: 15, title: "Fyr v0" });
  assert.equal(v.approval, null);
  assert.equal(v.label, "Merge #15");
  assert.equal(v.title, "Merge pull request #15: Fyr v0");
  assert.equal(v.yes, "Merge");
  assert.equal(v.yesDesc, "");
  assert.equal(v.noDesc, "Nothing happens; the agent is told.");
  assert.equal(v.host, undefined, "an empty host is no host");
});

test("a thread with no host or folder still yields a card, and an unknown effect does not throw", () => {
  assert.doesNotThrow(() => approvalView(approval, {}));
  assert.equal(approvalView(approval, { host: null }).host, undefined);
  const v = approvalView(effect("something.new"), {});
  assert.ok(v.title.length > 0);
});

test("the phone's ask commit and the desktop's submitState agree on what the button does once nothing is left", () => {
  const d: Decision = { id: "d", kind: "ask", questions: [{ question: "A?", options: [{ label: "x" }, { label: "y" }] }, { question: "B?", options: [{ label: "p" }, { label: "q" }] }] } as Decision;
  let cs = initCard(d);
  assert.equal(askCommit(cs, d).act === "none", submitState(cs, d).act === "none");
  cs = skip(goTo(skip(cs, d), d, 1), d);
  assert.equal(askCommit(cs, d).act, "cancel");
  assert.equal(submitState(cs, d).act, "cancel");
  cs = pickStay(goTo(cs, d, 1), d, 0);
  assert.equal(askCommit(cs, d).act, "submit");
  assert.equal(submitState(cs, d).act, "submit");
});
