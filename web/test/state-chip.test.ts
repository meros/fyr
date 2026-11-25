import { test } from "node:test";
import assert from "node:assert/strict";
import { rowDetail, rowState, stateChip, stateLine } from "../src/rows.ts";
import type { Decision } from "../src/types.ts";
import { thread } from "./helpers.ts";

// V0.thread-claims: the header chip of both fronts says the row's word, never the state's code name.
test("the chip says the row's word for what needs the owner", () => {
  const ask = thread("a", "asking", "needs", { decisions: [{ id: "d", kind: "ask", questions: [{ question: "Which?", multiSelect: false, options: [] }] } as Decision] });
  assert.deepEqual(stateChip(ask), { word: "1 question", why: "Which?" });
  assert.equal(stateChip(ask).word, rowState(ask));

  const merge = thread("m", "blocked", "needs", { attention: { state: "blocked", section: "needs", reason: "Add login: Merge", group: "approve", word: "Merge", subject: "Add login" } });
  assert.equal(stateChip(merge).word, "Merge");
  assert.equal(stateChip(merge).word, rowState(merge));
  assert.equal(stateLine(merge), "Merge · Add login");

  const stuck = thread("u", "blocked", "needs", { attention: { state: "blocked", section: "needs", reason: "Agent not responding since 08:58: no reply", group: "unblock", word: "Not responding", subject: "since 08:58: no reply" } });
  assert.deepEqual(stateChip(stuck), { word: "Not responding", why: "since 08:58: no reply" });
});

test("a host that sends no word: the chip falls back to Blocked with the reason as the why", () => {
  const old = thread("o", "blocked", "needs", { attention: { state: "blocked", section: "needs", reason: "Agent not responding since 08:58: no reply", group: "unblock" } });
  assert.deepEqual(stateChip(old), { word: "Blocked", why: "Agent not responding since 08:58: no reply" });
});

test("a failed thread says Failed and then the host's reason, on the row and the chip", () => {
  const f = thread("f", "failed", "needs", { attention: { state: "failed", section: "needs", reason: "Failed to connect to the API", group: "unblock" }, failure: { message: "raw" } as never });
  assert.equal(rowState(f), "Failed");
  assert.equal(rowDetail(f), "Failed to connect to the API"); // not trimmed as an echo of the word
  assert.deepEqual(stateChip(f), { word: "Failed", why: "Failed to connect to the API" });
});

test("a wait is Waiting on the chip and its reason in a line; the reason is never doubled", () => {
  const ci = thread("c", "waiting", "working", { attention: { state: "waiting", section: "working", reason: "Waiting for CI on #846" } });
  assert.deepEqual(stateChip(ci), { word: "Waiting", why: "Waiting for CI on #846" });
  assert.equal(stateLine(ci), "Waiting for CI on #846");
  assert.equal(rowState(ci), "Waiting for CI on #846"); // the row has room for it
});

test("working says Working or Starting; finished says Finished, and the line says whether it was read", () => {
  assert.equal(stateChip(thread("w", "working", "working")).word, "Working");
  assert.equal(stateChip(thread("s", "working", "working", { attention: { state: "working", section: "working", reason: "Starting" } })).word, "Starting");
  const done = thread("d", "done", "seen");
  const seen = thread("s", "seen", "seen");
  assert.equal(stateChip(done).word, "Finished");
  assert.equal(stateChip(seen).word, "Finished");
  assert.equal(stateLine(done), "Finished, unread");
  assert.equal(stateLine(seen), "Finished, seen");
});
