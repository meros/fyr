// SPEC V0.101: the client reads the host's need {cause, head} and derives nothing; the inbox row's beam and button.
import { test } from "node:test";
import assert from "node:assert/strict";
import { allowArgs, canRefresh, changeChip, checkedText, declineArgs, needLook, needsOwner, rowNeed } from "../src/offers.ts";
import { columnOf } from "../src/board.ts";
import type { Change } from "../src/types.ts";
import { thread } from "./helpers.ts";

const at = "2026-11-16T10:00:00Z";
const change = (x: Partial<Change>): Change => ({ id: "c_1", key: "k", kind: "code", title: "Fix", phase: "review", phaseBy: "observed", createdAt: at, updatedAt: at, ...x });
const head = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678";

test("SPEC V0.101 (1): the client reads need only: a failed condition without a need is not Needs you", () => {
  const failing = change({ conditions: [{ type: "checks", status: "fail", reason: "CI failed", source: "observed", at }] });
  assert.equal(needsOwner(failing), false, "the agent may be on it: the host decides");
  assert.equal(columnOf(failing), "review");
  assert.equal(needsOwner(change({ need: { cause: "failure", text: "CI failed", head } })), true);
  assert.equal(needsOwner(change({ need: { cause: "fixing", text: "Agent is fixing CI", muted: true } })), false, "muted");
  assert.equal(needsOwner(change({ ready: "Merge" })), false, "ready alone is not read; need carries it");
  assert.equal(needsOwner(change({ need: { cause: "ready", text: "Merge", head } })), true);
});

test("SPEC V0.101 (6): the inbox row's button: Merge bound to the head for Ready, Answer for an ask, none for a muted need", () => {
  assert.deepEqual(needLook({ cause: "ready", text: "Merge", head }), { button: "Merge a1b2c3d", line: "Green and waiting for your merge.", beam: true });
  assert.equal(needLook({ cause: "ready", text: "Review and merge", head }).button, "Review and merge a1b2c3d");
  assert.equal(needLook({ cause: "ask", ask: "d1", text: "Approve the design" }).button, "Answer");
  assert.equal(needLook({ cause: "fixing", muted: true, text: "Agent is fixing CI" }).beam, false);
  const t = thread("t1", "blocked", "needs", {
    changes: [change({ id: "c_old", updatedAt: "2026-11-16T09:00:00Z", need: { cause: "ask", ask: "d1" } }), change({ id: "c_new", need: { cause: "ready", text: "Merge", head } }), change({ id: "c_fix", updatedAt: "2026-11-16T11:00:00Z", need: { cause: "fixing", muted: true } })],
  });
  const r = rowNeed(t);
  assert.equal(r?.change.id, "c_new", "the newest change with a cause that is not muted");
  assert.equal(rowNeed(thread("t2", "seen", "seen", { changes: [change({})] })), null);
});

test("SPEC V0.101 (2), (3): the ask's answers; Refresh now and its freshness line", () => {
  assert.deepEqual(allowArgs({ ask: "d1" }), { decisionId: "d1", allow: true });
  assert.deepEqual(declineArgs({ ask: "d1" }), { decisionId: "d1", allow: false });
  assert.equal(canRefresh(change({ pr: { url: "https://github.com/o/r/pull/1", number: 1 } })), true);
  assert.equal(canRefresh(change({ kind: "doc" })), false, "nothing Fyr reads");
  assert.equal(canRefresh(change({ branch: "fix/x", done: { at, by: "observed", reason: "merged" } })), false, "done");
  assert.equal(checkedText("2026-11-16T10:00:00Z", Date.parse("2026-11-16T10:00:30Z")), "checked 30 s ago");
  assert.equal(checkedText("2026-11-16T10:00:00Z", Date.parse("2026-11-16T10:05:00Z")), "checked 5 min ago");
  const chip = changeChip({ changes: [change({ kind: "doc", need: { cause: "ask", ask: "d1" } })] });
  assert.ok(chip?.text.includes("needs you"));
});
