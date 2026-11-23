// The agents strip under the thread header (SPEC V0.16, V0.subagent-timeline): one fixed-height row per task.
import { useMemo, useState } from "preact/hooks";
import type { FyrEvent, TaskDTO } from "./types.ts";
import { buildConversation, findCall, type ToolCall } from "./conversation.ts";
import { StateIcon } from "./icons.tsx";
import { relTime } from "./format.ts";
import { agentsIcon, agentsSummary, endedLabel, groupTasks, taskLine, taskStats } from "./tasks.ts";
import { SubTimeline } from "./tool-calls-ui.tsx";

export const TASK_ICON: Record<string, "working" | "settled" | "failed" | "seen"> = {
  running: "working",
  completed: "settled",
  failed: "failed",
  stopped: "seen",
};

/** V0.subagent-timeline: a row whose Agent call is in the loaded conversation opens that subagent's timeline under it. */
function TaskRow({ t, now, call, open, onOpen }: { t: TaskDTO; now: number; call?: ToolCall; open?: boolean; onOpen?: () => void }) {
  const line = taskLine(t);
  const row = (
    <div
      class={`tk ${t.status}${call ? " click" : ""}`}
      title={`${t.description}\n${line}`}
      {...(call ? { role: "button", tabIndex: 0, "aria-expanded": !!open, onClick: onOpen, onKeyDown: (e: KeyboardEvent) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), onOpen?.()) } : {})}
    >
      <StateIcon state={TASK_ICON[t.status] ?? "seen"} size={14} />
      <span class="tkt">
        <span class="tkd ell">{t.description || "Agent"}</span>
        <span class="tkl ell">{line}</span>
      </span>
      <span class="tks">{taskStats(t)}</span>
      <span class="tka">{relTime(t.startedAt, now)}</span>
    </div>
  );
  if (!call || !open) return row;
  return (
    <>
      {row}
      <div class="sub tksub">
        <SubTimeline c={call} />
      </div>
    </>
  );
}

/** Running tasks always show; ended ones fold behind "N finished" ("1 finished, 3 stopped"). On a phone the whole strip is one line until opened. */
export function AgentsStrip({ tasks, now, events, cwd }: { tasks: TaskDTO[]; now: number; events?: FyrEvent[]; cwd?: string }) {
  const [showEnded, setShowEnded] = useState(false);
  const [open, setOpen] = useState(false);
  const [opened, setOpened] = useState<string | null>(null);
  const g = groupTasks(tasks);
  // V0.subagent-timeline: the Agent calls the rows can open, from the loaded events (an earlier page may not have them).
  const wanted = tasks.map((t) => t.toolUseId ?? "").join(",");
  const calls = useMemo(() => {
    const m = new Map<string, ToolCall>();
    if (!wanted.replace(/,/g, "")) return m;
    const items = buildConversation(events ?? [], { cwd });
    for (const id of wanted.split(",")) {
      const c = id ? findCall(items, id) : undefined;
      if (c) m.set(id, c);
    }
    return m;
  }, [events, wanted, cwd]);
  const row = (t: TaskDTO) => {
    const call = t.toolUseId ? calls.get(t.toolUseId) : undefined;
    return <TaskRow t={t} now={now} key={t.id} call={call} open={opened === t.id} onOpen={() => setOpened(opened === t.id ? null : t.id)} />;
  };
  return (
    <div class={`agents${open ? " open" : ""}`}>
      <button class="agsum ell" onClick={() => setOpen(!open)} aria-expanded={open}>
        <StateIcon state={agentsIcon(tasks)} size={14} />
        <span class="ell">{agentsSummary(tasks)}</span>
        <span class="chev">{open ? "▾" : "▸"}</span>
      </button>
      <div class={`agrows${opened ? " deep" : ""}`}>
        {g.running.map(row)}
        {g.ended.length > 0 && (
          <button class="tkmore" onClick={() => setShowEnded(!showEnded)} aria-expanded={showEnded}>
            {endedLabel(g.ended)} {showEnded ? "▾" : "▸"}
          </button>
        )}
        {showEnded && g.ended.map(row)}
      </div>
    </div>
  );
}
