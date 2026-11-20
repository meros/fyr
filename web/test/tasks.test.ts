import { test } from "node:test";
import assert from "node:assert/strict";
import { agentsIcon, agentsSummary, compactCount, endedLabel, groupTasks, liveLine, orderTasks, taskEventText, taskLine, taskStats, visibleTasks, waitLine, workingLine } from "../src/tasks.ts";
import { clockTime } from "../src/format.ts";
import type { TaskDTO, Thread } from "../src/types.ts";
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
    attention: { state: "waiting", section: "working", reason: "2 subagents running" },
    tasks: [
      task("a", "running", 1, { activity: "Reading files", updatedAt: at(3) }),
      task("b", "running", 2, { activity: "Running build", updatedAt: at(8) }),
      task("c", "completed", 0, { activity: "Old", updatedAt: at(9) }),
    ],
  });
  assert.equal(workingLine(t), "2 subagents running · Running build");
  assert.equal(workingLine({ ...t, tasks: [] }), "2 subagents running", "no activity: the reason alone");
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
  // V0.stop-ends-background: agents the owner's Stop ended are not "finished".
  const stopped = [task("a", "completed", 1), task("b", "stopped", 2), task("c", "stopped", 3)];
  assert.equal(agentsSummary(stopped), "3 agents: 1 finished, 2 stopped");
  assert.equal(endedLabel(stopped), "1 finished, 2 stopped");
  assert.equal(agentsIcon(stopped), "seen");
  assert.equal(agentsIcon([task("a", "completed", 1)]), "settled");
  assert.equal(taskStats({ ...ts[0], toolUses: 14, tokens: 143218 }), "14 tools · 143k");
  assert.equal(taskStats({ ...ts[0], toolUses: 9999, tokens: 12_345_678 }), "10k tools · 12.3M");
  assert.equal(taskStats({ ...ts[0], toolUses: 1 }), "1 tool");
  assert.equal(compactCount(980), "980");
  assert.equal(taskLine(task("c", "completed", 1, { summary: "Design delivered" })), "Design delivered");
  assert.equal(taskLine(task("d", "stopped", 1)), "Stopped");
  // V0.stop-ends-background: the CLI's summary of a stopped agent is its description; it reads as stopped, not done.
  assert.equal(taskLine(task("e", "stopped", 1, { description: "Audit sources", summary: "Audit sources" })), "Stopped");
  assert.equal(taskLine(task("f", "failed", 1, { description: "Build", summary: "exit 2" })), "Failed · exit 2");
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

// SPEC 0.5 V0.143: a live agent that takes nothing Fyr writes is not "Starting · 1 agent · 39 min" (the incident's
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

// SPEC 0.5 V0.153: while the host thrashes Fyr waits for it; the live line says so, not "Starting" or a failure.
test("live line while Fyr waits for the machine", () => {
  const now = Date.parse("2026-11-17T13:09:00Z");
  const t = thread("m", "waiting", "working", { running: true, activity: "Starting", machineWait: { since: "2026-11-17T13:05:51Z", why: "memory pressure" } });
  assert.match(liveLine(t, now), /^Waiting for the machine \(memory pressure\) · since \d\d:\d\d$/);
});

test("V0.195: a thread waiting for a background command says which, for how long, and that it went quiet", () => {
  const now = Date.parse("2026-11-18T09:36:00Z");
  const cmd: TaskDTO = { id: "b", description: "Wait for it", type: "local_bash", background: true, status: "running", startedAt: "2026-11-18T09:01:00Z" };
  const base = { running: false, tasks: [cmd], attention: { state: "waiting", section: "working", reason: "Running in background: Wait for it" } } as unknown as Thread;
  assert.equal(waitLine(base, now), "Running in background: Wait for it · 35 min");
  assert.equal(workingLine(base), "Running in background: Wait for it");
  assert.equal(taskLine(cmd), "Running");
  assert.equal(agentsSummary([cmd]), "1 background command");
  const quiet = { ...cmd, quietSince: "2026-11-18T09:19:00Z" };
  const q = { ...base, tasks: [quiet] } as Thread;
  const at = clockTime(quiet.quietSince);
  assert.equal(waitLine(q, now), `Running in background: Wait for it · 35 min · no CPU use since ${at}`);
  assert.equal(workingLine(q), `Running in background: Wait for it · no CPU use since ${at}`);
  assert.equal(taskLine(quiet), `No CPU use since ${at}`);
  assert.equal(waitLine({ ...q, running: true } as Thread, now), "", "a live turn has its own line");
  assert.equal(waitLine({ ...q, tasks: [{ ...cmd, type: "local_agent", background: true }] } as Thread, now), "", "an agent is not a command");
  assert.match(taskEventText({ kind: "quiet", description: "Wait for it", minutes: 5 }), /No CPU use for 5 min while waiting for Wait for it/);
});

test("V0.ci-wait-moves: every row in Moving says why it is there", () => {
  const ci = thread("ci", "waiting", "working", { attention: { state: "waiting", section: "working", reason: "Waiting for CI on #846: 2 of 5 checks done" } });
  assert.equal(workingLine(ci), "Waiting for CI on #846: 2 of 5 checks done");
  const starting = thread("s", "working", "working", { attention: { state: "working", section: "working", reason: "Starting" } });
  assert.equal(workingLine(starting), "Starting");
  const fresh = thread("f", "working", "working", { project: "fyr" });
  assert.equal(workingLine(fresh), "Working", "a run with no activity yet says Working, not the project");
});
