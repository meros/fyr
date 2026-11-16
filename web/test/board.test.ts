import { test } from "node:test";
import assert from "node:assert/strict";
import { boardItems, boardProjects, columnOf, columns, needsCount, stackOrder } from "../src/board.ts";
import type { Change, Project, Thread } from "../src/types.ts";
import { thread } from "./helpers.ts";
import { changeList } from "../src/offers.ts";

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

test("#35: an observed branch is one host record, shown in each of its threads and once on the board", () => {
  const rec = ch({ id: "r1", key: "git:github.com/meros/fyr#v0", kind: "code", title: "v0", phaseBy: "observed", threads: ["t1", "t2"], conditions: [{ type: "x", status: "fail", source: "observed", at: at(0) }] });
  const own = ch({ id: "c1", key: "k", title: "same key, another thread" });
  const ts = {
    t1: thread("t1", "seen", "seen", { title: "One", changes: [rec, own] }),
    t2: thread("t2", "seen", "seen", { title: "Two", changes: [rec, { ...own }] }),
  };
  assert.deepEqual(boardItems(ts, projects).map((i) => i.change.id).sort(), ["c1", "c1", "r1"], "agent changes stay per thread");
  assert.equal(needsCount(ts), 1);
});

test("SPEC 0.5 V0.78, #54: settled and snoozed threads leave the board, except a change that needs the owner", () => {
  const host = ch({ id: "h1", key: "host:fyr@main", kind: "code", title: "main", phase: "landed", threads: ["s1", "o1"] });
  const s = thread("s1", "settled", "settled", { title: "Done work", changes: [host, ch({ id: "s-landed", phase: "landed" }), ch({ id: "s-live", phase: "live" }), ch({ id: "s-fail", phase: "landed", conditions: [{ type: "review", status: "fail", source: "agent", at: at(1) }] })] });
  const z = thread("z1", "snoozed", "snoozed", { title: "Later", changes: [ch({ id: "z-review", phase: "review" }), ch({ id: "z-owner", phase: "review", needs: "owner" })] });
  const o = thread("o1", "seen", "seen", { title: "Open", changes: [host, ch({ id: "o-work", phase: "working" })] });
  const ts = { s1: s, z1: z, o1: o };
  const ids = (f = {}) => boardItems(ts, projects, f).map((i) => i.change.id).sort();
  assert.deepEqual(ids(), ["h1", "o-work", "s-fail", "z-owner"], "settled is hidden by default; Needs you and the open thread's host record stay");
  assert.equal(boardItems(ts, projects).find((i) => i.change.id === "h1")?.thread.id, "o1", "the host record shows under the open thread");
  assert.deepEqual(ids({ settled: true }), ["h1", "o-work", "s-fail", "s-landed", "s-live", "z-owner", "z-review"], "the toggle brings everything back");
  assert.equal(needsCount(ts), 2, "the badge counts the Needs you changes of settled threads too");
});

test("SPEC 0.5 V0.84, #93: a parked change sits in its phase column, out of Needs you and the count", () => {
  const parked = { at: at(3), seen: "f1" };
  assert.equal(columnOf(ch({ phase: "review", needs: "owner", parked })), "review");
  assert.equal(columnOf(ch({ phase: "landed", conditions: [{ type: "x", status: "fail", source: "agent", at: at(0) }], parked })), "landed");
  const ts = {
    t1: thread("t1", "seen", "seen", { title: "Coordinator", changes: [ch({ id: "p1", phase: "working", needs: "owner", parked }), ch({ id: "n1", phase: "review", needs: "owner" })] }),
  };
  assert.equal(needsCount(ts), 1, "only the change that still needs the owner counts");
  const by = Object.fromEntries(columns(boardItems(ts, projects)).map((c) => [c.key, c]));
  assert.equal(by.needs.count, 1);
  assert.equal(by.working.count, 1);
  const away = { s1: thread("s1", "settled", "settled", { title: "Done", changes: [ch({ id: "sp", phase: "review", needs: "owner", parked })] }) };
  assert.deepEqual(boardItems(away, projects).map((i) => i.change.id), [], "a parked change of a settled thread is history, like the rest of it");
});

test("SPEC 0.5 V0.87 (3), #69: one card per change id in a thread, the newest, with its branch", () => {
  const old = ch({ id: "same", key: "doc:x", kind: "doc", branch: "main", updatedAt: at(1) });
  const shown = ch({ id: "same", key: "doc:x", kind: "doc", branch: "fix/y", updatedAt: at(5) });
  const list = changeList({ changes: [old, shown] });
  assert.equal(list.length, 1);
  assert.equal(list[0].branch, "fix/y");
  const ts = { t1: thread("t1", "seen", "seen", { title: "One", changes: [old, shown] }) };
  const items = boardItems(ts, projects);
  assert.equal(items.length, 1, "one card on the board");
  assert.equal(items[0].change.branch, "fix/y");
});

test("SPEC 0.5 V0.87 (3), #69: a host record and the change that follows it are one card on the board", () => {
  const rec = ch({ id: "r1", key: "git:github.com/meros/fyr#fix/y", kind: "code", branch: "fix/y", phaseBy: "observed", threads: ["t1", "t2"], updatedAt: at(1) });
  const follower = ch({ id: "c1", key: "fyr:decision-0021", kind: "doc", branch: "fix/y", observed: "r1", phase: "review", updatedAt: at(2) });
  const ts = {
    t2: thread("t2", "seen", "seen", { title: "Other thread in the folder", changes: [rec] }),
    t1: thread("t1", "seen", "seen", { title: "The work", changes: [follower] }),
  };
  assert.deepEqual(boardItems(ts, projects).map((i) => `${i.thread.id}/${i.change.id}`), ["t1/c1"], "the follower's card, under its thread");
});
