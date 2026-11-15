import { test } from "node:test";
import assert from "node:assert/strict";
import { boardItems, boardProjects, columnOf, columns, needsCount, stackOrder } from "../src/board.ts";
import type { Change, Project, Thread } from "../src/types.ts";
import { thread } from "./helpers.ts";

const at = (m: number) => new Date(Date.UTC(2026, 10, 15, 9, m)).toISOString();
const ch = (p: Partial<Change>): Change => ({ id: "c", key: "k", kind: "infra", title: "x", phase: "working", phaseBy: "agent", createdAt: at(0), updatedAt: at(0), ...p });
const projects = [{ id: "p1", name: "ops" }, { id: "p2", name: "web" }] as Project[];

function threads(): Record<string, Thread> {
  const a = thread("t1", "seen", "seen", { title: "Scale api", projectId: "p1", changes: [ch({ id: "c1", title: "replicas", phase: "landed", conditions: [{ type: "health", status: "fail", source: "agent", at: at(1) }] }), ch({ id: "c2", title: "runbook", phase: "review", updatedAt: at(5) })] });
  const b = thread("t2", "seen", "seen", { title: "Search", projectId: "p2", changes: [ch({ id: "c3", key: "api", title: "api part", phase: "review", group: "search-v2", kind: "code", updatedAt: at(2) }), ch({ id: "c4", title: "web part", phase: "review", group: "search-v2", after: ["api"], updatedAt: at(9) }), ch({ id: "c5", title: "old", phase: "abandoned" })] });
  const c = thread("t3", "archived", "archived", { title: "Archived", archivedAt: at(0), changes: [ch({ id: "c6", title: "hidden" })] });
  return { t1: a, t2: b, t3: c };
}

test("SPEC 0.5 V0.64: a failing or waiting change is in Needs you, whatever its phase", () => {
  assert.equal(columnOf(ch({ phase: "landed", conditions: [{ type: "x", status: "fail", source: "agent", at: at(0) }] })), "needs");
  assert.equal(columnOf(ch({ phase: "review", needs: "owner" })), "needs");
  assert.equal(columnOf(ch({ phase: "live" })), "live");
  assert.equal(columnOf(ch({ phase: "reverted", needs: "owner" })), "ended");
});

test("SPEC 0.5 V0.64: every thread but archived ones; filters by project, text and ended", () => {
  const ts = threads();
  assert.deepEqual(boardItems(ts, projects).map((i) => i.change.id).sort(), ["c1", "c2", "c3", "c4"]);
  assert.deepEqual(boardItems(ts, projects, { projectId: "p1" }).map((i) => i.change.id).sort(), ["c1", "c2"]);
  assert.deepEqual(boardItems(ts, projects, { q: "search" }).map((i) => i.change.id).sort(), ["c3", "c4"], "the thread title matches");
  assert.ok(boardItems(ts, projects, { ended: true }).some((i) => i.change.id === "c5"));
  assert.equal(needsCount(ts), 1);
  assert.deepEqual(boardProjects(ts, projects).map((p) => p.id), ["p1", "p2"]);
});

test("SPEC 0.5 V0.64: columns by stage; a group stays together, its stack base first", () => {
  const cols = columns(boardItems(threads(), projects));
  const by = Object.fromEntries(cols.map((c) => [c.key, c]));
  assert.deepEqual(cols.map((c) => c.key), ["needs", "working", "review", "landed", "live"], "Ended only on request");
  assert.equal(by.needs.count, 1);
  assert.equal(by.review.count, 3);
  const g = by.review.entries.find((e) => e.kind === "group");
  assert.ok(g && g.kind === "group" && g.group === "search-v2");
  assert.deepEqual(g.items.map((i) => i.change.id), ["c3", "c4"], "c4 builds on api (c3): base first, though c4 is newer");
  assert.equal(stackOrder([]).length, 0);
});
