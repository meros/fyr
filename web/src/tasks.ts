// Background work (SPEC 0.5 V0.16): subagents and background tasks of a thread. Pure.

import type { TaskDTO, Thread } from "./types.ts";
import { clockTime, relTime } from "./format.ts";

function ts(s: string | null | undefined): number {
  const n = s ? Date.parse(s) : NaN;
  return Number.isNaN(n) ? 0 : n;
}

/** Running tasks first, then newest started first. */
export function orderTasks(tasks: TaskDTO[] | null | undefined): TaskDTO[] {
  return [...(tasks ?? [])].sort((a, b) => {
    const ra = a.status === "running" ? 0 : 1;
    const rb = b.status === "running" ? 0 : 1;
    return ra - rb || ts(b.startedAt) - ts(a.startedAt) || a.id.localeCompare(b.id);
  });
}

export interface TaskGroups {
  /** always shown */
  running: TaskDTO[];
  /** shown behind "N finished" until expanded */
  ended: TaskDTO[];
}

export function groupTasks(tasks: TaskDTO[] | null | undefined): TaskGroups {
  const all = orderTasks(tasks);
  return { running: all.filter((t) => t.status === "running"), ended: all.filter((t) => t.status !== "running") };
}

/** The tasks the strip shows: running ones always, ended ones only when expanded. */
export function visibleTasks(tasks: TaskDTO[] | null | undefined, expanded: boolean): TaskDTO[] {
  const g = groupTasks(tasks);
  return expanded ? [...g.running, ...g.ended] : g.running;
}

export function agentsLabel(n: number): string {
  return n === 1 ? "1 agent" : `${n} agents`;
}

/** "143k", "1.2M", "980". */
export function compactCount(n: number | null | undefined): string {
  const v = Math.max(0, Math.round(n ?? 0));
  if (v < 1000) return String(v);
  if (v < 1_000_000) return `${Math.round(v / 1000)}k`;
  return `${(v / 1_000_000).toFixed(1).replace(/\.0$/, "")}M`;
}

/** "14 tools · 143k" (tokens). */
export function taskStats(t: TaskDTO): string {
  const tools = t.toolUses ?? 0;
  const parts = [`${compactCount(tools)} ${tools === 1 ? "tool" : "tools"}`];
  if (t.tokens) parts.push(compactCount(t.tokens));
  return parts.join(" · ");
}

/** What the task is doing now, or how it ended. */
export function taskLine(t: TaskDTO): string {
  if (t.status === "running") return t.activity || "Starting";
  return t.summary || t.activity || (t.status === "completed" ? "Finished" : t.status === "failed" ? "Failed" : "Stopped");
}

/** "2 agents · Running build": the running count and the newest running task's activity. */
export function agentsSummary(tasks: TaskDTO[] | null | undefined): string {
  const running = groupTasks(tasks).running;
  if (!running.length) {
    const n = (tasks ?? []).length;
    return n ? `${agentsLabel(n)} finished` : "";
  }
  const newest = [...running].sort((a, b) => ts(b.updatedAt) - ts(a.updatedAt))[0];
  return newest.activity ? `${agentsLabel(running.length)} · ${newest.activity}` : agentsLabel(running.length);
}

/** The second part of a Working-section row: waiting reads the reason and the newest activity; working adds "· N agents". */
export function workingLine(t: Thread): string {
  const running = groupTasks(t.tasks).running;
  if (t.attention.state === "waiting") {
    const newest = [...running].sort((a, b) => ts(b.updatedAt) - ts(a.updatedAt))[0];
    const reason = t.attention.reason || agentsLabel(running.length);
    return newest?.activity ? `${reason} · ${newest.activity}` : reason;
  }
  const base = t.activity || t.project;
  return running.length ? `${base} · ${agentsLabel(running.length)}` : base;
}

/** The log line for a `task` event. */
export function taskEventText(d: { kind?: string; description?: string; status?: string; summary?: string }): string {
  const what = d.description || "agent";
  if (d.kind === "started") return `Agent started: ${what}`;
  const verb = d.status === "failed" ? "failed" : d.status === "stopped" ? "stopped" : "finished";
  return `Agent ${verb}: ${d.summary || what}`;
}

/** SPEC 0.5 V0.21: the live line under a working thread, desktop and phone. */
export function liveLine(t: Thread, now: number): string {
  // V0.143: a process that takes nothing Fyr writes is not "Starting" or "Working".
  if (t.unresponsive) return `Not responding since ${clockTime(t.unresponsive.since)} · ${t.unresponsive.reason}`;
  // V0.153: slow because the host thrashes; Fyr waits, nothing failed.
  if (t.machineWait) return `Waiting for the machine (${t.machineWait.why}) · since ${clockTime(t.machineWait.since)}`;
  const running = groupTasks(t.tasks).running.length;
  return [t.activity || "Working", running ? agentsLabel(running) : "", relTime(t.runStartedAt, now)].filter(Boolean).join(" · ");
}
