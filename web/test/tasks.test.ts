import { test } from "node:test";
import assert from "node:assert/strict";
import { agentsSummary, compactCount, groupTasks, liveLine, orderTasks, taskEventText, taskLine, taskStats, visibleTasks, workingLine } from "../src/tasks.ts";
import type { TaskDTO } from "../src/types.ts";
import { thread } from "./helpers.ts";

const at = (m: number) => new Date(Date.UTC(2026, 10, 14, 9, m)).toISOString();
const task = (id: string, status: TaskDTO["status"], started: number, extra: Partial<TaskDTO> = {}): TaskDTO => ({ id, description: id, status, startedAt: at(started), updatedAt: at(started), ...extra });

test("tasks: running first, then newest started", () => {
  const ts = [task("old-done", "completed", 1), task("run-old", "running", 2), task("new-failed", "failed", 9), task("run-new", "running", 5), task("stopped", "stopped", 3)];
  assert.deepEqual(orderTasks(ts).map((t) => t.id), ["run-new", "run-old", "new-failed", "stopped", "old-done"]);
  const g = groupTasks(ts);
  assert.deepEqual(g.running.map((t) => t.id), ["run-new", "run-old"]);
  assert.equal(g.ended.length, 3);
  assert.deepEqual(visibleTasks(ts, false).map((t) => t.id), ["run-new", "run-old"], "ended ones collapse");
  assert.equal(visibleTasks(ts, true).length, 5);
  assert.deepEqual(orderTasks(null), []);
});

test("waiting row: the reason plus the newest running task's activity", () => {
  const t = thread("w", "waiting", "working", {
    attention: { state: "waiting", section: "working", reason: "2 agents" },
    tasks: [
      task("a", "running", 1, { activity: "Reading files", updatedAt: at(3) }),
      task("b", "running", 2, { activity: "Running build", updatedAt: at(8) }),
      task("c", "completed", 0, { activity: "Old", updatedAt: at(9) }),
    ],
  });
  assert.equal(workingLine(t), "2 agents · Running build");
  assert.equal(workingLine({ ...t, tasks: [] }), "2 agents", "no activity: the reason alone");
});

test("working row with running tasks adds the agent count", () => {
  const t = thread("x", "working", "working", { activity: "Bash yarn test", tasks: [task("a", "running", 1)] });
  assert.equal(workingLine(t), "Bash yarn test · 1 agent");
  assert.equal(workingLine({ ...t, tasks: [task("a", "completed", 1)] }), "Bash yarn test");
});

test("strip summary, stats and lines", () => {
  const ts = [task("a", "running", 1, { activity: "Running build", updatedAt: at(5) }), task("b", "running", 2, { activity: "Reading", updatedAt: at(4) })];
  assert.equal(agentsSummary(ts), "2 agents · Running build");
  assert.equal(agentsSummary([task("a", "completed", 1)]), "1 agent finished");
  assert.equal(taskStats({ ...ts[0], toolUses: 14, tokens: 143218 }), "14 tools · 143k");
  assert.equal(taskStats({ ...ts[0], toolUses: 9999, tokens: 12_345_678 }), "10k tools · 12.3M");
  assert.equal(taskStats({ ...ts[0], toolUses: 1 }), "1 tool");
  assert.equal(compactCount(980), "980");
  assert.equal(taskLine(task("c", "completed", 1, { summary: "Design delivered" })), "Design delivered");
  assert.equal(taskLine(task("d", "stopped", 1)), "Stopped");
});

test("task events become log lines", () => {
  assert.equal(taskEventText({ kind: "started", description: "Design mobile" }), "Agent started: Design mobile");
  assert.equal(taskEventText({ kind: "ended", description: "Design mobile", status: "completed", summary: "Design delivered" }), "Agent finished: Design delivered");
  assert.equal(taskEventText({ kind: "ended", description: "Build", status: "failed" }), "Agent failed: Build");
});

// SPEC 0.5 V0.21: the live line while a thread works, the same on desktop and phone:
// what it does now, how many agents run, for how long. Never the bare word alone when
// there is an activity.
test("live line while working", () => {
  const now = Date.parse("2026-01-01T12:12:00Z");
  const t = thread("t", "working", "working", { running: true, runStartedAt: "2026-01-01T12:00:00Z", activity: "Run the map tests" });
  assert.equal(liveLine(t, now), "Run the map tests · 12 min");
  const withAgents = thread("w", "working", "working", { running: true, runStartedAt: "2026-01-01T12:00:00Z", activity: "", tasks: [{ id: "a", description: "x", status: "running" }] });
  assert.equal(liveLine(withAgents, now), "Working · 1 agent · 12 min");
});

// SPEC 0.5 V0.142: a live agent that takes nothing Fyr writes is not "Starting · 1 agent · 39 min" (the incident's
// line): the live line says since when it is silent and why, on desktop and phone.
test("live line of an agent that does not respond", () => {
  const now = Date.parse("2026-11-17T09:48:00Z");
  const t = thread("s", "blocked", "needs", {
    running: true,
    runStartedAt: "2026-11-17T09:09:00Z",
    activity: "Starting",
    tasks: [{ id: "b", description: "Run Cloud SQL proxy to the clone", status: "running" }],
    unresponsive: { since: "2026-11-17T07:58:42Z", at: "2026-11-17T09:10:48Z", reason: "it did not take a message within 1 min" },
  });
  const line = liveLine(t, now);
  assert.match(line, /^Not responding since \d\d:\d\d · it did not take a message within 1 min$/);
  assert.doesNotMatch(line, /Starting/);
});
