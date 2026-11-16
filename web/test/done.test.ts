// SPEC V0.98: a finished change leaves the board (decision 0024, slice 4).
import { test } from "node:test";
import assert from "node:assert/strict";
import { boardItems, columns, finishedSince, untouchedItems, UNTOUCHED_MS } from "../src/board.ts";
import { doneText, headerPr, isDone, openAndDone } from "../src/offers.ts";
import type { Change, Project, Thread } from "../src/types.ts";
import { thread } from "./helpers.ts";

const at = (h: number) => new Date(Date.UTC(2026, 10, 16, 8 + h, 42)).toISOString();
const ch = (p: Partial<Change>): Change => ({ id: "c", key: "k", kind: "code", title: "x", phase: "working", phaseBy: "observed", createdAt: at(0), updatedAt: at(0), ...p });
const projects: Project[] = [];
const clock = (iso: string) => new Date(iso).toISOString().slice(11, 16);

function threads(): Record<string, Thread> {
  const changes = [
    ch({ id: "open", title: "open work", phase: "review", touchedAt: at(1) }),
    ch({ id: "merged", title: "merged one", phase: "landed", done: { at: at(2), by: "observed", reason: "merged" } }),
    ch({ id: "old", title: "done before", phase: "landed", done: { at: at(-30), by: "observed", reason: "landed before v2" } }),
    ch({ id: "gone", title: "abandoned", phase: "abandoned", phaseBy: "owner", done: { at: at(3), by: "owner", reason: "abandoned" } }),
    ch({ id: "stale", title: "stale", phase: "working", touchedAt: at(-80) }),
    ch({ id: "fail", title: "failing and stale", phase: "review", touchedAt: at(-80), conditions: [{ type: "checks", status: "fail", source: "observed", at: at(-80) }] }),
  ];
  return { t1: thread("t1", "seen", "seen", { title: "Work", changes }) };
}

test("SPEC V0.98 (2): a done change is not on the board's columns", () => {
  const ids = boardItems(threads(), projects).map((i) => i.change.id).sort();
  assert.deepEqual(ids, ["fail", "open", "stale"]);
  assert.ok(isDone(ch({ done: { at: at(0), by: "owner", reason: "done" } })));
  assert.ok(!isDone(ch({ phase: "landed" })), "landed without a mark is not done: the host decides");
});

test("SPEC V0.98 (3): Finished since your last visit lists what was done after boardSeenAt, newest first, with its reason", () => {
  const f = finishedSince(threads(), projects, at(1));
  assert.deepEqual(f.map((i) => i.change.id), ["gone", "merged"]);
  assert.equal(doneText(f[1].change, clock), "merged 10:42");
  assert.equal(doneText(f[0].change, clock), "abandoned by you");
  assert.equal(doneText(ch({ done: { at: at(0), by: "owner", reason: "done" } }), clock), "marked done by you");
  assert.equal(finishedSince(threads(), projects, at(5)).length, 0);
  assert.equal(finishedSince(threads(), projects, null).length, 3, "no visit yet: every done change");
});

test("SPEC V0.98 (4): Untouched for 3 days holds open changes with no touch for 3 days; one that needs the owner stays in Needs you", () => {
  const now = Date.parse(at(1)) + UNTOUCHED_MS - 1;
  const items = boardItems(threads(), projects);
  assert.deepEqual(untouchedItems(items, now).map((i) => i.change.id), ["stale"]);
  const cols = columns(items, false, now);
  const shown = cols.flatMap((c) => c.entries.flatMap((e) => (e.kind === "one" ? [e.item.change.id] : e.items.map((i) => i.change.id))));
  assert.deepEqual(shown.sort(), ["fail", "open"]);
  assert.deepEqual(untouchedItems(items, now + 2).map((i) => i.change.id).sort(), ["open", "stale"]);
});

test("SPEC V0.98 (6): the header shows the newest open change's PR, never a done one", () => {
  const pr = (n: number, state = "OPEN") => ({ url: `https://github.com/o/r/pull/${n}`, repo: "o/r", number: n, state, checks: "pass" });
  const done = { at: at(2), by: "observed" as const, reason: "merged" };
  // The thread's main PR is a done change's: not shown; the open change's PR is.
  const t = { pr: pr(116, "MERGED"), changes: [ch({ id: "a", key: "pr:o/r#116", pr: pr(116, "MERGED"), phase: "landed", done, createdAt: at(3) }), ch({ id: "b", key: "k2", pr: pr(120), phase: "review", createdAt: at(1) })] };
  assert.equal(headerPr(t)?.number, 120);
  assert.equal(headerPr({ pr: pr(116, "MERGED"), changes: [t.changes[0]] }), null, "nothing when none is open");
  const two = { pr: null, changes: [ch({ id: "a", pr: pr(1), phase: "review", createdAt: at(1) }), ch({ id: "b", pr: pr(2), phase: "review", createdAt: at(2) })] };
  assert.equal(headerPr(two)?.number, 2, "the newest open one");
});

test("SPEC V0.98 (3): the thread's Changes tab lists open changes, then Done here", () => {
  const { open, done } = openAndDone(threads().t1);
  assert.deepEqual(open.map((c) => c.id).sort(), ["fail", "open", "stale"]);
  assert.deepEqual(done.map((c) => c.id), ["gone", "merged", "old"]);
});
