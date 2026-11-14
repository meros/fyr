// Turns the event timeline into render items. Pure.

import type { Failure, FyrEvent } from "./types.ts";
import { taskEventText } from "./tasks.ts";

export interface ToolCall {
  id: string;
  name: string;
  summary: string;
  isError: boolean;
  done: boolean;
  startedAt: number;
  durationMs: number | null;
  /** subagent messages folded under a Task/Agent tool use */
  subMessages: number;
  subCalls: ToolCall[];
  subTexts: string[];
}

export type Item =
  | { kind: "user"; key: string; text: string; images: number; attachments: { id: string; name: string; mediaType: string; size: number }[] }
  | { kind: "assistant"; key: string; text: string }
  | { kind: "tools"; key: string; calls: ToolCall[] }
  | { kind: "run"; key: string; failure: Failure | null; interrupted: boolean; costUsd?: number; durationMs?: number }
  | { kind: "effect"; key: string; action: string; ok: boolean; text: string }
  | { kind: "answer"; key: string; text: string }
  | { kind: "task"; key: string; text: string; status: string; started: boolean };

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function oneLine(s: string, max = 160): string {
  const t = s.replace(/\s+/g, " ").trim();
  return t.length > max ? t.slice(0, max - 1) + "…" : t;
}

function lines(v: unknown): number {
  return typeof v === "string" && v.length ? v.split("\n").length : 0;
}

/** "+42 −18" for edits, from the tool input. */
export function diffStat(name: string, input: unknown): string {
  const i = (input && typeof input === "object" ? input : {}) as Record<string, unknown>;
  let add = 0;
  let del = 0;
  if (name === "Edit") {
    add = lines(i.new_string);
    del = lines(i.old_string);
  } else if (name === "MultiEdit" && Array.isArray(i.edits)) {
    for (const e of i.edits as Record<string, unknown>[]) {
      add += lines(e?.new_string);
      del += lines(e?.old_string);
    }
  } else if (name === "Write") {
    add = lines(i.content);
  } else return "";
  return `+${add} −${del}`;
}

/** A short one-line form of a tool input. */
export function summarizeInput(name: string, input: unknown): string {
  const i = (input && typeof input === "object" ? input : {}) as Record<string, unknown>;
  const stat = diffStat(name, input);
  if (stat && typeof i.file_path === "string") return `${oneLine(i.file_path)} ${stat}`;
  const pick =
    str(i.command) ||
    str(i.file_path) ||
    str(i.path) ||
    str(i.pattern) ||
    str(i.description) ||
    str(i.url) ||
    str(i.query) ||
    str(i.prompt);
  if (pick) return oneLine(pick);
  if (name && Object.keys(i).length === 0) return "";
  try {
    return oneLine(JSON.stringify(i));
  } catch {
    return "";
  }
}

interface Block {
  type: string;
  text?: string;
  id?: string;
  name?: string;
  input?: unknown;
  tool_use_id?: string;
  is_error?: boolean;
}

function blocksOf(data: any): Block[] {
  const c = data?.message?.content;
  if (Array.isArray(c)) return c as Block[];
  if (typeof c === "string") return [{ type: "text", text: c }];
  return [];
}

export function buildConversation(events: FyrEvent[]): Item[] {
  const items: Item[] = [];
  const calls = new Map<string, ToolCall>();
  let group: { kind: "tools"; key: string; calls: ToolCall[] } | null = null;

  const pushItem = (it: Item) => {
    group = null;
    items.push(it);
  };
  const newCall = (b: Block, at: number): ToolCall => ({
    id: b.id ?? "",
    name: b.name ?? "tool",
    summary: summarizeInput(b.name ?? "", b.input),
    isError: false,
    done: false,
    startedAt: at,
    durationMs: null,
    subMessages: 0,
    subCalls: [],
    subTexts: [],
  });

  for (const e of events) {
    const at = Date.parse(e.at) || 0;
    const key = String(e.seq);
    switch (e.type) {
      case "user":
        pushItem({
          kind: "user",
          key,
          text: str(e.data?.text),
          images: Number(e.data?.images) || 0,
          attachments: (Array.isArray(e.data?.attachments) ? e.data.attachments : [])
            .filter((a: any) => a && typeof a.id === "string")
            .map((a: any) => ({ id: a.id, name: str(a.name) || "file", mediaType: str(a.mediaType), size: Number(a.size) || 0 })),
        });
        break;
      case "msg": {
        const d = e.data ?? {};
        const parent: string | null = d.parent_tool_use_id ?? null;
        const blocks = blocksOf(d);
        if (parent) {
          const host = calls.get(parent);
          if (!host) break;
          host.subMessages++;
          for (const b of blocks) {
            if (b.type === "tool_use") {
              const c = newCall(b, at);
              calls.set(c.id, c);
              host.subCalls.push(c);
            } else if (b.type === "tool_result") {
              finish(calls.get(b.tool_use_id ?? ""), b, at);
            } else if (b.type === "text" && d.type === "assistant" && b.text?.trim()) {
              host.subTexts.push(b.text);
            }
          }
          break;
        }
        for (const b of blocks) {
          if (d.type === "assistant" && b.type === "text") {
            if (b.text?.trim()) pushItem({ kind: "assistant", key: `${key}.${items.length}`, text: b.text });
          } else if (d.type === "assistant" && b.type === "tool_use") {
            const c = newCall(b, at);
            calls.set(c.id, c);
            if (!group) {
              group = { kind: "tools", key: `${key}.${items.length}`, calls: [] };
              items.push(group);
            }
            group.calls.push(c);
          } else if (b.type === "tool_result") {
            finish(calls.get(b.tool_use_id ?? ""), b, at);
          }
          // thinking and image blocks are not shown in v0
        }
        break;
      }
      case "run.ended": {
        const d = e.data ?? {};
        pushItem({
          kind: "run",
          key,
          failure: d.failure ?? null,
          interrupted: !!d.interrupted,
          costUsd: typeof d.costUsd === "number" ? d.costUsd : undefined,
          durationMs: typeof d.durationMs === "number" ? d.durationMs : undefined,
        });
        break;
      }
      case "task":
        pushItem({ kind: "task", key, text: taskEventText(e.data ?? {}), status: str(e.data?.status), started: e.data?.kind === "started" });
        break;
      case "effect.result":
        pushItem({ kind: "effect", key, action: str(e.data?.action), ok: !!e.data?.ok, text: str(e.data?.text) });
        break;
      case "decision.closed":
        if (str(e.data?.answer)) pushItem({ kind: "answer", key, text: str(e.data?.answer) });
        break;
      default:
        break; // run.started, decision.opened and unknown types are not shown in the log
    }
  }
  return items;
}

function finish(c: ToolCall | undefined, b: Block, at: number) {
  if (!c) return;
  c.done = true;
  c.isError = !!b.is_error;
  c.durationMs = c.startedAt && at ? Math.max(0, at - c.startedAt) : null;
}

export const AGENT_TOOLS = new Set(["Task", "Agent"]);

export type ToolRow = { kind: "call"; call: ToolCall } | { kind: "agents"; calls: ToolCall[] };

/** Consecutive Task/Agent calls in one card read as one row: "3 subagents · explore gestures". */
export function toolRows(calls: ToolCall[]): ToolRow[] {
  const rows: ToolRow[] = [];
  for (const c of calls) {
    const last = rows[rows.length - 1];
    if (AGENT_TOOLS.has(c.name)) {
      if (last?.kind === "agents") last.calls.push(c);
      else rows.push({ kind: "agents", calls: [c] });
    } else rows.push({ kind: "call", call: c });
  }
  return rows;
}
