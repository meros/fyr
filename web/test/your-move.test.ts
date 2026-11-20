// SPEC V0.your-move: the inbox says whose move it is.
import { test } from "node:test";
import assert from "node:assert/strict";
import { SECTIONS, sections } from "../src/store.ts";
import { needsGroup, needsHeads } from "../src/rows.ts";
import type { Change, Decision, Thread } from "../src/types.ts";
import { thread } from "./helpers.ts";

const ask = { id: "q", kind: "ask" } as Decision;
const approval = { id: "a", kind: "approval" } as Decision;
const merge = { id: "m", kind: "effect", effect: { action: "pr.merge", args: {} } } as Decision;
const blocked = (id: string, reason: string, extra: Partial<Thread> = {}) => thread(id, "blocked", "needs", { attention: { state: "blocked", section: "needs", reason }, ...extra });
const need = (cause: string, muted = false) => ({ need: { cause, muted } }) as unknown as Change;

test("V0.your-move: the sections say whose move it is, in V0.199's order", () => {
  assert.deepEqual(SECTIONS.map((s) => s.label), ["Your move", "Finished, unread", "Finished, seen", "Moving without you", "Waiting to run", "Snoozed", "Settled"]);
});

test("V0.your-move: each cause lands in its group", () => {
  assert.equal(needsGroup(thread("a", "asking", "needs", { decisions: [ask] })), "answer");
  assert.equal(needsGroup(thread("b", "asking", "needs", { decisions: [merge, ask] })), "answer", "a question in the thread is answered first");
  assert.equal(needsGroup(thread("c", "asking", "needs", { decisions: [merge] })), "approve");
  assert.equal(needsGroup(thread("d", "asking", "needs", { decisions: [approval] })), "approve");
  assert.equal(needsGroup(blocked("e", "PR: Ready", { changes: [need("ready")] })), "approve");
  assert.equal(needsGroup(blocked("f", "Run: rebuild")), "approve");
  assert.equal(needsGroup(blocked("g", "PR: failed", { changes: [need("ready"), need("failure")] })), "unblock");
  assert.equal(needsGroup(thread("h", "failed", "needs")), "unblock");
});

test("V0.your-move: rows sort by group and each group is labelled once", () => {
  const ts = [
    thread("fail", "failed", "needs"),
    thread("merge", "asking", "needs", { decisions: [merge] }),
    thread("q1", "asking", "needs", { decisions: [ask], activityAt: "2026-11-18T10:00:00Z" }),
    thread("q2", "asking", "needs", { decisions: [ask], activityAt: "2026-11-18T09:00:00Z" }),
  ];
  const rows = sections(Object.fromEntries(ts.map((t) => [t.id, t]))).needs;
  assert.deepEqual(rows.map((t) => t.id), ["q2", "q1", "merge", "fail"]);
  assert.deepEqual(needsHeads(rows), { q2: "Answer", merge: "Approve", fail: "Unblock" });
});

test("V0.your-move: a row that left Your move and keeps its place starts no group", () => {
  const rows = [thread("open", "working", "working"), thread("q", "asking", "needs", { decisions: [ask] })];
  assert.deepEqual(needsHeads(rows), { q: "Answer" });
});
