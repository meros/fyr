// Turns the event timeline into render items. Pure.
import { firstLine, middleTruncate, oneLine, span } from "./format.ts";
import { tildePath } from "./paths.ts";

import type { HostId } from "./hostid.ts";
import { actionEventLine, changeEventLine } from "./offers.ts";
import type { Failure, FyrEvent } from "./types.ts";
import { taskEventText } from "./tasks.ts";
import { toldLine } from "./undeclared.ts";
import { previewCard, type PreviewCard } from "./previews.ts";
import type { ContentUrls } from "./attach.ts";

export interface ToolCall {
  id: string;
  name: string;
  summary: string;
  isError: boolean;
  done: boolean;
  startedAt: number;
  durationMs: number | null;
  /** the human-readable row label (V0.21): the agent's description, else a sentence */
  label: string;
  /** the agent's own description of the call, when it gave one */
  description: string;
  /** the tool input and the result text, for the details view */
  input: unknown;
  output: string;
  /** subagent messages folded under a Task/Agent tool use */
  subMessages: number;
  /** V0.38: the host cut this output; the event with this seq has it whole */
  cutSeq?: number;
  /** V0.subagent-timeline: what the subagent did, in the order it happened: its texts and runs of its tool calls */
  sub: SubStep[];
}

/** One step of a subagent's timeline: a text it wrote, or tool calls it made one after another. */
export type SubStep = { kind: "text"; text: string } | { kind: "calls"; calls: ToolCall[] };

/** V0.subagent-timeline: the tool call with this id anywhere in the items, also inside a subagent's steps. */
export function findCall(items: Item[], id: string): ToolCall | undefined {
  const inCalls = (calls: ToolCall[]): ToolCall | undefined => {
    for (const c of calls) {
      if (c.id === id) return c;
      for (const s of c.sub) {
        const f = s.kind === "calls" ? inCalls(s.calls) : undefined;
        if (f) return f;
      }
    }
    return undefined;
  };
  for (const it of items) {
    const f = it.kind === "tools" ? inCalls(it.calls) : undefined;
    if (f) return f;
  }
  return undefined;
}

/** The prompt a Task/Agent call gave its subagent, "" when there is none. */
export function agentPrompt(c: ToolCall): string {
  const p = (c.input as { prompt?: unknown } | null)?.prompt;
  return typeof p === "string" ? p.trim() : "";
}

/** SPEC 0.5 V0.33: where a message is on its way to the agent; "" for messages from before tracking. */
type Delivery = "" | "sent" | "queued" | "read" | "cancelled" | "lost";

/** V0.41: when the event arrived and which run it belongs to (undefined outside a run). */
interface ItemMeta {
  at?: number;
  run?: number;
}

export type Item = (
  | {
      kind: "user";
      key: string;
      text: string;
      images: number;
      from?: string;
      attachments: { id: string; name: string; mediaType: string; size: number }[];
      /** the message id (V0.33) */
      id?: string;
      delivery: Delivery;
      /** not read yet: it renders after "Agent is here" */
      pending: boolean;
      /** the owner's last message the agent read: the only one that says "Read" */
      lastRead: boolean;
    }
  | { kind: "assistant"; key: string; text: string; quiet?: boolean }
  | { kind: "tools"; key: string; calls: ToolCall[] }
  | { kind: "run"; key: string; failure: Failure | null; interrupted: boolean; durationMs?: number; compact: boolean }
  | { kind: "effect"; key: string; action: string; ok: boolean; text: string; line: string }
  /** V0.run-cards-in-conversation: the agent asked the owner to run this request here; its Run card stands at this place */
  | { kind: "runcard"; key: string; action: string }
  | { kind: "note"; key: string; text: string; line: string }
  /** V0.164 (N9): what Fyr told the agent about heavy work it did not ask for; `text` is the whole notice */
  | { kind: "undeclared"; key: string; text: string; line: string }
  | { kind: "answer"; key: string; text: string; cancelled?: boolean }
  | { kind: "preview"; key: string; card: PreviewCard }
  | { kind: "task"; key: string; text: string; status: string; started: boolean }
  /** V0.45, V0.trans-host-handover: the thread moved to another profile or host */
  | {
      kind: "handover";
      key: string;
      from: string;
      to: string;
      rung: string;
      note?: string;
      atIso: string;
      fromHost?: string;
      fromHostId?: string;
      fromThread?: string;
      toHost?: string;
      toHostId?: string;
      toThread?: string;
    }
) &
  ItemMeta;

interface BuildOptions {
  /** V0.33: ids of the messages the host still holds; others are not waiting, whatever events arrived */
  outbox?: string[];
  /** "Show tool details": every machine line in full (V0.21) */
  details?: boolean;
  /** the thread's folder, for relative paths in labels */
  cwd?: string;
  /** V0.54: the thread's host, which serves its previews */
  host?: HostId;
  /** #41: where previews load from (the Api's); the host's paths when absent */
  urls?: ContentUrls;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function lines(v: unknown): number {
  return typeof v === "string" && v.length ? v.split("\n").length : 0;
}

/** "+42 −18" for edits, from the tool input. */
function diffStat(name: string, input: unknown): string {
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
  if (stat && typeof i.file_path === "string") return `${oneLine(i.file_path, 160)} ${stat}`;
  const pick =
    str(i.command) ||
    str(i.file_path) ||
    str(i.path) ||
    str(i.pattern) ||
    str(i.description) ||
    str(i.url) ||
    str(i.query) ||
    str(i.prompt);
  if (pick) return oneLine(pick, 160);
  if (name && Object.keys(i).length === 0) return "";
  try {
    return oneLine(JSON.stringify(i), 160);
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
  content?: unknown;
}

function blocksOf(data: unknown): Block[] {
  const c = (data as { message?: { content?: unknown } } | null | undefined)?.message?.content;
  if (Array.isArray(c)) return c as Block[];
  if (typeof c === "string") return [{ type: "text", text: c }];
  return [];
}

/** A user message's attachment records, as the host logged them: those without an id are dropped. */
function attachmentsOf(v: unknown): { id: string; name: string; mediaType: string; size: number }[] {
  if (!Array.isArray(v)) return [];
  return v.flatMap((a: Record<string, unknown> | null) => (a && typeof a.id === "string" ? [{ id: a.id, name: str(a.name) || "file", mediaType: str(a.mediaType), size: Number(a.size) || 0 }] : []));
}

/** The thread's preview cards, in log order (for the filmstrip). */
export function previewsOf(events: FyrEvent[], host?: HostId, urls?: ContentUrls): PreviewCard[] {
  const out: PreviewCard[] = [];
  for (const e of events) {
    if (e.type !== "preview") continue;
    const c = previewCard(e.seq, e.at, e.data, host, urls);
    if (c) out.push(c);
  }
  return out;
}

/** V0.event-store: the conversation as a fold over the timeline. `feed` takes the next events, oldest first, and
 *  `items` answers the conversation so far; feeding a timeline in parts gives what feeding it whole gives. `items`
 *  changes nothing the builder keeps, so it can be asked after every feed. */
export function conversationBuilder(opts: Omit<BuildOptions, "outbox"> = {}): { feed(events: readonly FyrEvent[]): void; items(outbox?: string[]): Item[] } {
  const details = !!opts.details;
  const cwd = opts.cwd ?? "";
  const items: Item[] = [];
  // V0.37: inside the turn Fyr forced to update the live view, the agent's text is one quiet line.
  let forcedTurn = false;
  const calls = new Map<string, ToolCall>();
  // V0.33: a message with an id waits here until the agent reads it (or it is
  // cancelled or lost); it then takes its place in the log at that point.
  const held = new Map<string, Extract<Item, { kind: "user" }>>();
  // where each held message was sent: its place if its read event never arrives (V0.33)
  const heldAt = new Map<string, { index: number; meta: ItemMeta }>();
  let group: Extract<Item, { kind: "tools" }> | null = null;
  // V0.41: runs are numbered from 1; `inRun` is false between run.ended and the next run.started.
  let run = 0;
  let inRun = false;
  let now = 0;
  const meta = (): ItemMeta => (inRun ? { at: now, run } : { at: now });

  const pushItem = (it: Item) => {
    group = null;
    // a held message keeps the time it was sent; it joins the run that read it
    items.push({ ...meta(), ...it, ...(inRun ? { run } : {}) });
  };
  const newCall = (b: Block, at: number): ToolCall => ({
    id: b.id ?? "",
    name: b.name ?? "tool",
    summary: summarizeInput(b.name ?? "", b.input),
    label: callLabel(b.name ?? "", b.input, cwd),
    description: descriptionOf(b.input),
    input: b.input,
    output: "",
    isError: false,
    done: false,
    startedAt: at,
    durationMs: null,
    subMessages: 0,
    sub: [],
  });

  const feed = (events: readonly FyrEvent[]) => {
    for (const e of events) {
      const at = Date.parse(e.at) || 0;
      now = at;
      const key = String(e.seq);
      switch (e.type) {
        case "run.started":
          run++;
          inRun = true;
          break;
        case "user": {
          const text = str(e.data?.text);
          // Fyr's own messages to the agent are system notes, not the user's words: hidden unless details are on.
          if (/^\[Fyr\]/.test(text)) {
            if (details) pushItem({ kind: "note", key, text, line: firstLine(text.replace(/^\[Fyr\]\s*/, ""), 100) });
            break;
          }
          const id = str(e.data?.id);
          // V0.128: a message stored with the old View-tab mark ("view-typed", V0.49) is the owner's ordinary message.
          const from = str(e.data?.from);
          const it: Extract<Item, { kind: "user" }> = {
            kind: "user",
            key,
            text: str(e.data?.text),
            images: Number(e.data?.images) || 0,
            from: from && from !== "view-typed" ? from : undefined,
            attachments: attachmentsOf(e.data?.attachments),
            id: id || undefined,
            delivery: id ? "sent" : "",
            pending: false,
            lastRead: false,
          };
          if (id) {
            held.set(id, it);
            heldAt.set(id, { index: items.length, meta: meta() });
          } else pushItem(it);
          break;
        }
        case "message.state": {
          const it = held.get(str(e.data?.id));
          const st = str(e.data?.state);
          if (!it) break;
          if (st === "queued") it.delivery = "queued";
          else if (st === "read" || st === "cancelled" || st === "lost") {
            it.delivery = st;
            held.delete(it.id!);
            pushItem(it);
          }
          break;
        }
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
                const last = host.sub[host.sub.length - 1];
                if (last?.kind === "calls") last.calls.push(c);
                else host.sub.push({ kind: "calls", calls: [c] });
              } else if (b.type === "tool_result") {
                finish(calls.get(b.tool_use_id ?? ""), b, at, d.cut ? e.seq : undefined);
              } else if (b.type === "text" && d.type === "assistant" && b.text?.trim()) {
                host.sub.push({ kind: "text", text: b.text });
              }
            }
            break;
          }
          for (const b of blocks) {
            if (d.type === "assistant" && b.type === "text") {
              if (b.text?.trim()) pushItem({ kind: "assistant", key: `${key}.${items.length}`, text: b.text, ...(forcedTurn ? { quiet: true } : {}) });
            } else if (d.type === "assistant" && b.type === "tool_use") {
              const c = newCall(b, at);
              calls.set(c.id, c);
              if (!group) {
                group = { kind: "tools", key: `${key}.${items.length}`, calls: [], ...meta() };
                items.push(group);
              }
              group.calls.push(c);
            } else if (b.type === "tool_result") {
              finish(calls.get(b.tool_use_id ?? ""), b, at, d.cut ? e.seq : undefined);
            }
            // thinking and image blocks are not shown in v0
          }
          break;
        }
        case "view.forced":
          forcedTurn = true;
          break;
        case "run.ended": {
          forcedTurn = false;
          const d = e.data ?? {};
          pushItem({
            kind: "run",
            key,
            failure: d.failure ?? null,
            interrupted: !!d.interrupted,
            durationMs: typeof d.durationMs === "number" ? d.durationMs : undefined,
            compact: !details,
          });
          inRun = false;
          break;
        }
        case "preview": {
          const card = previewCard(e.seq, e.at, e.data, opts.host, opts.urls);
          if (card) pushItem({ kind: "preview", key, card });
          break;
        }
        case "task": {
          // The Agents strip carries starts and finishes; only a failed agent gets a line by default, and V0.195's quiet wait.
          const failed = e.data?.kind === "ended" && e.data?.status === "failed";
          if (details || failed || e.data?.kind === "quiet") pushItem({ kind: "task", key, text: taskEventText(e.data ?? {}), status: str(e.data?.status), started: e.data?.kind === "started" });
          break;
        }
        case "effect.result": {
          const text = str(e.data?.text);
          pushItem({ kind: "effect", key, action: str(e.data?.action), ok: !!e.data?.ok, text, line: firstLine(text, 100) || (e.data?.ok ? "Done" : "Failed") });
          break;
        }
        case "tool.refused": {
          // V0.133: the agent CLI refused a tool call without asking Fyr (its
          // input was closed); one failed line, so "stopped at your request"
          // is not read as the owner's doing.
          const tool = str(e.data?.tool) || "A tool call";
          const why = str(e.data?.why) || "The agent CLI refused it without asking Fyr; not the owner.";
          pushItem({ kind: "effect", key, action: "refused", ok: false, text: why, line: `${tool} was refused without asking you` });
          break;
        }
        case "agent.unresponsive": {
          // V0.143: the agent process took nothing Fyr wrote; one failed line with the reason.
          const reason = str(e.data?.reason) || "it took nothing Fyr wrote";
          pushItem({ kind: "effect", key, action: "unresponsive", ok: false, text: reason, line: `The agent stopped responding: ${reason}` });
          break;
        }
        case "agent.restarted": {
          // V0.143: Fyr ended the process and resumed the session; it names the background tasks that ended with it.
          const why = str(e.data?.why);
          const tasks = Array.isArray(e.data?.tasks) ? (e.data!.tasks as unknown[]).map(String) : [];
          const text = [why && `Why: ${why}.`, tasks.length ? `Background tasks ended: ${tasks.join(", ")}.` : "", "The unread messages go to the new process."].filter(Boolean).join("\n");
          pushItem({ kind: "effect", key, action: "restarted", ok: true, text, line: tasks.length ? `Agent restarted; ended ${tasks.join(", ")}` : "Agent restarted" });
          break;
        }
        case "change": {
          // V0.59: a change tool call, one line; the Changes tab holds the record.
          const line = changeEventLine(e.data);
          pushItem({ kind: "effect", key, action: "change", ok: true, text: line, line });
          break;
        }
        case "run.requested":
          pushItem({ kind: "runcard", key, action: str(e.data?.action) });
          break;
        case "action": {
          // V0.60: a press and its result; the output is the text.
          const a = actionEventLine(e.data);
          pushItem({ kind: "effect", key, action: "action", ok: a.ok, text: str(e.data?.output) || a.line, line: a.line });
          break;
        }
        case "handover":
          pushItem({
            kind: "handover",
            key,
            from: str(e.data?.from),
            to: str(e.data?.to),
            rung: str(e.data?.rung),
            atIso: e.at,
            ...(str(e.data?.note) ? { note: str(e.data?.note) } : {}),
            ...(str(e.data?.fromHost) ? { fromHost: str(e.data?.fromHost) } : {}),
            ...(str(e.data?.fromHostId) ? { fromHostId: str(e.data?.fromHostId) } : {}),
            ...(str(e.data?.fromThread) ? { fromThread: str(e.data?.fromThread) } : {}),
            ...(str(e.data?.toHost) ? { toHost: str(e.data?.toHost) } : {}),
            ...(str(e.data?.toHostId) ? { toHostId: str(e.data?.toHostId) } : {}),
            ...(str(e.data?.toThread) ? { toThread: str(e.data?.toThread) } : {}),
          });
          break;
        case "effect.error": {
          // V0.45: a handover that could not start; other effect errors stay for tool details.
          const err = str(e.data?.error);
          if (e.data?.kind === "handover") pushItem({ kind: "effect", key, action: "handover", ok: false, text: err, line: `Handover failed: ${firstLine(err, 100)}` });
          // V0.108: a title call that failed; the host's line says the title stays.
          if (e.data?.kind === "title") pushItem({ kind: "effect", key, action: "title", ok: false, text: err, line: firstLine(err, 100) });
          // V0.111: a deploy ask that Fyr did not approve by itself, or that waits for another deploy.
          if (e.data?.kind === "autoDeploy") pushItem({ kind: "effect", key, action: "autoDeploy", ok: false, text: err, line: firstLine(err, 100) });
          break;
        }
        case "slot":
          // V0.164: the notice about undeclared heavy work; the other slot events stay out of the log.
          if (str(e.data?.undeclared)) pushItem({ kind: "undeclared", key, text: str(e.data?.undeclared), line: toldLine(e.data ?? {}) });
          // V0.176: Fyr ended the thread's leftover processes.
          if (str(e.data?.leftovers)) pushItem({ kind: "note", key, text: str(e.data?.leftovers), line: str(e.data?.leftovers).replace(/^Fyr /, "") }); // shown as "Fyr: ended …"
          break;
        case "decision.closed":
          // V0.ask-cancel: a card the owner closed without an answer is its own line, not an answer.
          if (e.data?.cancelled === true) pushItem({ kind: "answer", key, text: str(e.data?.answer), cancelled: true });
          else if (str(e.data?.answer)) pushItem({ kind: "answer", key, text: str(e.data?.answer) });
          break;
        case "decision.autoApproved": {
          // V0.tool-approval-judge: quiet audit record folded with process work (V0.41).
          const tool = str(e.data?.tool);
          const summary = str(e.data?.summary);
          const reason = str(e.data?.reason);
          const model = str(e.data?.model);
          const what = summary && summary !== tool ? `${tool} (${summary})` : tool || "tool call";
          const line = reason ? `Auto-approved ${what} — ${reason}` : `Auto-approved ${what}`;
          const text = model ? `${line} [${model}]` : line;
          pushItem({ kind: "note", key, text, line });
          break;
        }
        default:
          break; // decision.opened and unknown types are not shown in the log
      }
    }
  };

  const result = (outbox?: string[]): Item[] => {
    const out0 = items.slice();
    const waiting = new Map(held);
    // The host's outbox is the truth: a held message it no longer holds was read (its read event may
    // never have reached this device). It goes back where it was sent, not to the end (found live).
    const holds = outbox ? new Set(outbox) : null;
    if (holds) {
      const back = [...waiting.values()].filter((it) => !holds.has(it.id!)).map((it) => ({ it, at: heldAt.get(it.id!)! }));
      for (const { it, at } of back.sort((a, b) => b.at.index - a.at.index)) {
        out0.splice(at.index, 0, { ...at.meta, ...it, delivery: "read", pending: false });
        waiting.delete(it.id!);
      }
    }
    const out = calmTools(out0);
    let last = -1;
    out.forEach((it, i) => {
      if (it.kind === "user" && it.delivery === "read" && !it.from) last = i;
    });
    if (last >= 0) out[last] = { ...(out[last] as Extract<Item, { kind: "user" }>), lastRead: true };
    for (const it of waiting.values()) out.push({ ...it, pending: true });
    return out;
  };
  return { feed, items: result };
}

/** The conversation of a whole timeline. */
export function buildConversation(events: FyrEvent[], opts: BuildOptions = {}): Item[] {
  const b = conversationBuilder(opts);
  b.feed(events);
  return b.items(opts.outbox);
}

/** V0.event-store: a builder kept across renders. A timeline that only grew at its end (every event before is the
 *  same object, as `mergeSeq` keeps them) feeds only the new events; any other change, or other options, builds
 *  again from the start. */
export function conversationCache(): (events: readonly FyrEvent[], opts?: BuildOptions) => Item[] {
  let b: ReturnType<typeof conversationBuilder> | null = null;
  let fed: readonly FyrEvent[] = [];
  let was: BuildOptions = {};
  return (events, opts = {}) => {
    const same = b && opts.details === was.details && opts.cwd === was.cwd && opts.host === was.host && opts.urls === was.urls;
    if (!b || !same || !grewFrom(fed, events)) {
      b = conversationBuilder(opts);
      fed = [];
    }
    b.feed(events.slice(fed.length));
    fed = events;
    was = opts;
    return b.items(opts.outbox);
  };
}

/** `next` is `prev` with events added at its end. */
function grewFrom(prev: readonly FyrEvent[], next: readonly FyrEvent[]): boolean {
  if (next.length < prev.length) return false;
  for (let i = 0; i < prev.length; i++) if (prev[i] !== next[i]) return false;
  return true;
}

/** V0.33: the quiet word under a message: Waiting, Sent, Read (the last read one only), Cancelled, Not delivered. */
export function deliveryLabel(it: Extract<Item, { kind: "user" }>): string {
  switch (it.delivery) {
    case "queued":
      return "Waiting";
    case "sent":
      return "Sent";
    case "read":
      return it.lastRead ? "Read" : "";
    case "cancelled":
      return "Cancelled";
    case "lost":
      return "Not delivered";
  }
  return "";
}

/** V0.21: drop calls that have their own card or are plumbing (unless they failed); drop groups left empty. */
function calmTools(items: Item[]): Item[] {
  const out: Item[] = [];
  for (const it of items) {
    if (it.kind !== "tools") {
      out.push(it);
      continue;
    }
    const calls = it.calls.filter((c) => !hiddenCall(c));
    if (calls.length) out.push({ ...it, calls });
  }
  return out;
}

function finish(c: ToolCall | undefined, b: Block, at: number, cutSeq?: number) {
  if (!c) return;
  if (cutSeq) c.cutSeq = cutSeq;
  c.done = true;
  c.isError = !!b.is_error;
  c.output = resultText(b.content);
  c.durationMs = c.startedAt && at ? Math.max(0, at - c.startedAt) : null;
}

const AGENT_TOOLS = new Set(["Task", "Agent"]);

type ToolRow = { kind: "call"; call: ToolCall } | { kind: "agents"; calls: ToolCall[] };

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

// ---- V0.21: calm tool output ----

/** Calls whose result has its own card in the log: the call itself never shows (unless it failed). */
const CARD_TOOLS = new Set(["mcp__fyr__view_publish", "mcp__fyr__view_state", "mcp__fyr__view_unchanged", "mcp__fyr__show", "mcp__fyr__effect_request", "mcp__fyr__pr_link", "mcp__fyr__settle_when", "mcp__fyr__ask", "AskUserQuestion"]);
/** Plumbing: tool loading and bookkeeping. Extend here. */
const PLUMBING_TOOLS = new Set(["ToolSearch", "TodoWrite", "TodoRead", "ListMcpResourcesTool", "ReadMcpResourceTool"]);
/** Shell bookkeeping: a Bash call whose whole command is one of these. */
const PLUMBING_COMMANDS = [/^\s*settask\b/];

export function hiddenCall(c: Pick<ToolCall, "name" | "isError" | "input">): boolean {
  if (c.isError) return false; // a failed hidden call is shown
  if (CARD_TOOLS.has(c.name) || PLUMBING_TOOLS.has(c.name)) return true;
  if (c.name === "Bash") {
    const cmd = (c.input as { command?: unknown } | undefined)?.command;
    if (typeof cmd === "string" && PLUMBING_COMMANDS.some((r) => r.test(cmd))) return true;
  }
  return false;
}

/** A tool_result's content as text. */
function resultText(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content))
    return content
      .map((b) => (b && typeof b === "object" && (b as Block).type === "text" ? str((b as Block).text) : b && typeof b === "object" && (b as Block).type === "image" ? "[image]" : ""))
      .filter(Boolean)
      .join("\n");
  return "";
}

const SERVERS: Record<string, string> = { github: "GitHub", gitlab: "GitLab", playwright: "Playwright", fyr: "", slack: "Slack", linear: "Linear", sentry: "Sentry" };
const BUILTIN: Record<string, string> = { WebSearch: "Web search", WebFetch: "Web fetch", MultiEdit: "Edit", NotebookEdit: "Notebook edit", NotebookRead: "Notebook read", TodoWrite: "Todo list", AskUserQuestion: "Question", BashOutput: "Shell output", KillShell: "Stop shell", KillBash: "Stop shell" };

/** Tool names as words: mcp__fyr__show → Show, mcp__github__get_pr → GitHub get pr, mcp__x__y → X y. */
export function friendlyName(name: string): string {
  const m = /^mcp__([^_]+(?:_[^_]+)*?)__(.+)$/.exec(name);
  if (m) {
    const server = m[1].toLowerCase();
    const words = m[2].replace(/[_-]+/g, " ").trim().toLowerCase();
    const label = server in SERVERS ? SERVERS[server] : m[1].charAt(0).toUpperCase() + m[1].slice(1).replace(/[_-]+/g, " ");
    const text = label ? `${label} ${words}` : words;
    return text.charAt(0).toUpperCase() + text.slice(1);
  }
  return BUILTIN[name] ?? name;
}

type Kind = { key: string; one: string; many: (n: number) => string };
const KINDS: Record<string, Kind> = {
  read: { key: "read", one: "read 1 file", many: (n) => `read ${n} files` },
  edit: { key: "edit", one: "edited 1 file", many: (n) => `edited ${n} files` },
  bash: { key: "bash", one: "ran 1 command", many: (n) => `ran ${n} commands` },
  search: { key: "search", one: "searched the code", many: (n) => `searched the code ${n} times` },
  web: { key: "web", one: "searched the web", many: (n) => `searched the web ${n} times` },
  fetch: { key: "fetch", one: "fetched 1 page", many: (n) => `fetched ${n} pages` },
  agent: { key: "agent", one: "started 1 subagent", many: (n) => `started ${n} subagents` },
};
const KIND_OF: Record<string, string> = { Read: "read", NotebookRead: "read", Edit: "edit", MultiEdit: "edit", Write: "edit", NotebookEdit: "edit", Bash: "bash", BashOutput: "bash", Grep: "search", Glob: "search", LS: "search", WebSearch: "web", WebFetch: "fetch", Task: "agent", Agent: "agent" };

/** "Read 3 files · ran 2 commands · searched the web · edited 1 file", in order of first use. */
export function groupSummary(calls: (Pick<ToolCall, "name"> & { description?: string })[]): string {
  const counts = new Map<string, { kind?: Kind; label?: string; described?: string; n: number }>();
  for (const c of calls) {
    const k = KIND_OF[c.name];
    const id = k ?? `other:${c.name}`;
    const cur = counts.get(id) ?? (k ? { kind: KINDS[k], n: 0 } : { label: friendlyName(c.name), n: 0 });
    // Commands are named by the agent's first description: "Ran the Go tests and 2 more".
    if (k === "bash" && !cur.described && c.description) cur.described = pastTense(c.description);
    cur.n++;
    counts.set(id, cur);
  }
  const parts = [...counts.values()].map((v) => {
    if (v.described) return lowerFirst(v.n === 1 ? v.described : `${v.described} and ${v.n - 1} more`);
    return v.kind ? (v.n === 1 ? v.kind.one : v.kind.many(v.n)) : v.n === 1 ? `used ${v.label}` : `used ${v.label} ${v.n} times`;
  });
  const text = parts.join(" · ");
  return text.charAt(0).toUpperCase() + text.slice(1);
}

function lowerFirst(s: string): string {
  // Keep proper nouns and acronyms ("Go", "CI") as written after the first word.
  return s.charAt(0).toLowerCase() + s.slice(1);
}

interface GroupStats {
  failed: number;
  running: number;
  /** #91: the last call failed (a failure earlier in the group that a later call got past is not the outcome) */
  lastFailed: boolean;
  /** wall time from the first start to the last finish, when every call finished */
  durationMs: number | null;
}

export function groupStats(calls: ToolCall[]): GroupStats {
  const failed = calls.filter((c) => c.isError).length;
  const running = calls.filter((c) => !c.done).length;
  const lastFailed = calls.length > 0 && calls[calls.length - 1].isError;
  if (running || !calls.length) return { failed, running, lastFailed, durationMs: null };
  const start = Math.min(...calls.map((c) => c.startedAt));
  const end = Math.max(...calls.map((c) => c.startedAt + (c.durationMs ?? 0)));
  return { failed, running, lastFailed, durationMs: start ? Math.max(0, end - start) : null };
}

/** #91: how a folded group reads. Red only when it ended in a failure; amber when a later call got past one. */
type GroupTone = "failed" | "recovered" | "running" | "ok";
export function groupTone(st: Pick<GroupStats, "failed" | "running" | "lastFailed">): GroupTone {
  if (st.running) return "running";
  if (st.lastFailed) return "failed";
  return st.failed ? "recovered" : "ok";
}

/** A tool group splits into folded runs of plain calls and the subagent rows, which stay as they are (V0.16). */
type Segment = { kind: "fold"; calls: ToolCall[] } | { kind: "agents"; calls: ToolCall[] };
export function segments(calls: ToolCall[]): Segment[] {
  const out: Segment[] = [];
  for (const r of toolRows(calls)) {
    const last = out[out.length - 1];
    if (r.kind === "agents") out.push({ kind: "agents", calls: r.calls });
    else if (last?.kind === "fold") last.calls.push(r.call);
    else out.push({ kind: "fold", calls: [r.call] });
  }
  return out;
}

/** V0.26: an opened tool group shows at most this many calls (the last ones) before "+N more tools". */
export const VISIBLE_CALLS = 3;

type CallView = { kind: "call"; call: ToolCall } | { kind: "more"; count: number; label: string };

/** The rows of an opened group: the last calls and every failed call; the rest behind one "+N more tools" row. */
export function capCalls(calls: ToolCall[], expanded: boolean, max = VISIBLE_CALLS): CallView[] {
  const all = calls.map((call): CallView => ({ kind: "call", call }));
  if (expanded || calls.length <= max) return all;
  const keep = calls.map((c, i) => c.isError || i >= calls.length - max);
  const hidden = keep.filter((k) => !k).length;
  if (!hidden) return all;
  const shown = all.filter((_, i) => keep[i]);
  return [{ kind: "more", count: hidden, label: `+${hidden} more ${hidden === 1 ? "tool" : "tools"}` }, ...shown];
}

const OUTPUT_LINES = 200;
/** Long output is cut at 200 lines behind "Show all". */
export function cutOutput(text: string, max = OUTPUT_LINES): { text: string; hidden: number } {
  const lines = text.split("\n");
  if (lines.length <= max) return { text, hidden: 0 };
  return { text: lines.slice(0, max).join("\n"), hidden: lines.length - max };
}

/** The full input of a call, for the details view: the command for Bash, else readable JSON. */
export function fullInput(name: string, input: unknown): string {
  const i = (input && typeof input === "object" ? input : {}) as Record<string, unknown>;
  if (name === "Bash" && typeof i.command === "string") return i.command;
  try {
    return JSON.stringify(input ?? {}, null, 2);
  } catch {
    return String(input);
  }
}

// ---- V0.21: human-readable labels ----

function descriptionOf(input: unknown): string {
  const d = (input as { description?: unknown } | undefined)?.description;
  return typeof d === "string" ? oneLine(d, 120) : "";
}

/** A path relative to the thread's folder (or ~ for home), shortened in the middle. */
export function relPath(path: string, cwd: string, max = 40): string {
  let p = path;
  const base = cwd.replace(/\/+$/, "");
  if (base && (p === base || p.startsWith(base + "/"))) p = p.slice(base.length + 1) || ".";
  else p = tildePath(p);
  return middleTruncate(p, max);
}

function short(v: string, max = 40): string {
  return middleTruncate(oneLine(v, 400), max);
}

/** A row label: the agent's description when there is one, else a sentence from the tool and its key input. */
export function callLabel(name: string, input: unknown, cwd: string): string {
  const desc = descriptionOf(input);
  if (desc) return desc;
  const i = (input && typeof input === "object" ? input : {}) as Record<string, unknown>;
  const path = str(i.file_path) || str(i.notebook_path) || str(i.path);
  switch (name) {
    case "Read":
    case "NotebookRead":
      return path ? `Read ${relPath(path, cwd)}` : "Read a file";
    case "Edit":
    case "MultiEdit":
    case "NotebookEdit": {
      const st = diffStat(name, input);
      return `Edited ${path ? relPath(path, cwd) : "a file"}${st ? ` (${st})` : ""}`;
    }
    case "Write":
      return `Wrote ${path ? relPath(path, cwd) : "a file"}`;
    case "Grep":
      return `Searched for "${short(str(i.pattern), 32)}"${path ? ` in ${relPath(path, cwd, 28)}` : ""}`;
    case "Glob":
      return `Found files matching ${short(str(i.pattern), 36)}`;
    case "LS":
      return `Listed ${path ? relPath(path, cwd) : "the folder"}`;
    case "WebSearch":
      return `Searched the web for "${short(str(i.query), 36)}"`;
    case "WebFetch": {
      const u = str(i.url).replace(/^https?:\/\//, "");
      return `Fetched ${u ? middleTruncate(u, 40) : "a page"}`;
    }
    case "Bash": {
      const head = commandHead(str(i.command));
      return head ? `Ran ${head}` : "Ran a command";
    }
    case "Task":
    case "Agent":
      return "Started a subagent";
    default: {
      const key = str(i.title) || str(i.query) || str(i.url) || path || str(i.name);
      return key ? `${friendlyName(name)} ${short(key, 32)}` : friendlyName(name);
    }
  }
}

/** The first words of a shell command, readable: "yarn test", "go test", "git rebase". */
function commandHead(cmd: string): string {
  const first = cmd.split(/&&|\|\||;|\|/)[0].trim();
  const words = first.split(/\s+/).filter((w) => !/^[A-Z_]+=/.test(w));
  const head: string[] = [];
  for (const w of words) {
    if (head.length >= 2 || w.startsWith("-") || /[\/"'$`]/.test(w)) break;
    head.push(w);
  }
  return middleTruncate(head.join(" "), 32);
}

const PAST: Record<string, string> = {
  run: "ran", check: "checked", build: "built", install: "installed", list: "listed", show: "showed", find: "found", get: "got",
  fetch: "fetched", test: "tested", start: "started", stop: "stopped", create: "created", update: "updated", read: "read",
  view: "viewed", verify: "verified", open: "opened", push: "pushed", commit: "committed", rebase: "rebased", deploy: "deployed",
  kill: "killed", wait: "waited", search: "searched", count: "counted", print: "printed", compile: "compiled", format: "formatted",
  lint: "linted", inspect: "inspected", look: "looked", copy: "copied", move: "moved", remove: "removed", delete: "deleted",
  make: "made", write: "wrote", add: "added", clean: "cleaned", restart: "restarted", watch: "watched", take: "took", capture: "captured",
};

/** "Run the Go tests" → "Ran the Go tests"; unknown verbs are kept as written. */
export function pastTense(description: string): string {
  const m = /^(\w+)(.*)$/s.exec(description);
  if (!m) return description;
  const p = PAST[m[1].toLowerCase()];
  return p ? p.charAt(0).toUpperCase() + p.slice(1) + m[2] : description;
}

/** V0.quota-wait, V0.overload-retry: the words of a failure that is a wait, not the owner's move: the provider's quota
 *  is out until its reset, or the provider failed in passing (overloaded, a 5xx) and Fyr retries by itself. undefined
 *  for a failure that is the owner's. */
export function failureWait(cls: string | undefined): string | undefined {
  return cls === "quota" ? "Waiting for quota" : cls === "overloaded" ? "Waiting to retry" : undefined;
}

/** SPEC 0.5 V0.21: the words for the end of a run, and what one tap adds (a failure's duration).
 *  V0.quota-in-thread: no cost. The providers are subscriptions; the CLI's dollar figure is what the API would charge.
 *  V0.quota-wait: a run the provider's quota stopped waits for the reset; it did not break. */
export function runLabel(it: Extract<Item, { kind: "run" }>): { text: string; more: string } {
  const d = it.durationMs !== undefined ? span(it.durationMs) : "";
  const wait = it.failure && failureWait(it.failure.class);
  if (it.failure && wait) return { text: `${wait}: ${it.failure.message}`, more: d };
  if (it.failure) return { text: `Failed (${it.failure.class}): ${it.failure.message}`, more: d };
  const text = it.interrupted ? (d ? `Stopped after ${d}` : "Stopped") : d ? `Finished in ${d}` : "Finished";
  return { text, more: "" };
}

/** V0.38: the whole output of one call, from the full event the host returns. */
export function fullOutputOf(ev: { data?: unknown }, toolUseId: string): string | null {
  const d = (ev?.data ?? {}) as { message?: { content?: Block[] } };
  for (const b of d.message?.content ?? []) {
    if (b.type === "tool_result" && b.tool_use_id === toolUseId) return resultText(b.content);
  }
  return null;
}

// ---- V0.41: pacing. What the owner reads (outcome) at full weight; the work (process) quiet and folded ----

/** outcome: read at full weight; process: quiet; end: the small line that closes a run. */
type Role = "outcome" | "process" | "end";

export type Entry =
  | { kind: "item"; key: string; item: Item; role: Role }
  | { kind: "fold"; key: string; items: Item[]; text: string; steps: number; failed: number; running: number; tone: GroupTone; durationMs: number | null };

function turnOf(it: Item, users: number): string {
  return it.run !== undefined ? `r${it.run}` : `o${users}`;
}

/**
 * The role of each item. Outcome: the owner's messages, the agent's last message of a run (the
 * newest one while the run goes on), previews, and failures (a failed run, effect or agent).
 * Process: everything the agent did on the way there, its earlier messages included.
 */
export function roles(items: Item[]): Role[] {
  const lastText = new Map<string, number>();
  let users = 0;
  const turns = items.map((it, i) => {
    if (it.kind === "user" && it.run === undefined) users++;
    const t = turnOf(it, users);
    if (it.kind === "assistant" && !it.quiet) lastText.set(t, i);
    return t;
  });
  return items.map((it, i): Role => {
    switch (it.kind) {
      case "user":
      case "preview":
      case "handover":
      case "runcard": // never folded away: it holds buttons and a result
        return "outcome";
      case "assistant":
        return !it.quiet && lastText.get(turns[i]) === i ? "outcome" : "process";
      case "run":
        // V0.quota-wait, V0.overload-retry: a wait is not an outcome the owner acts on.
        return it.failure && !failureWait(it.failure.class) ? "outcome" : "end";
      case "effect":
        return it.ok ? "process" : "outcome";
      case "task":
        return it.status === "failed" ? "outcome" : "process";
      default:
        return "process";
    }
  });
}

/** The run still going: the newest run with items and no end yet. */
export function liveRun(items: Item[]): number | undefined {
  let max: number | undefined;
  for (const it of items) if (it.run !== undefined && (max === undefined || it.run > max)) max = it.run;
  if (max === undefined) return undefined;
  return items.some((it) => it.kind === "run" && it.run === max) ? undefined : max;
}

/**
 * V0.41: consecutive process items fold into one line ("Read 3 files · ran 2 commands · 2 min"),
 * one tap from every step. Not folded: a single step, a stretch of system lines only (they are one
 * line each already), the newest stretch of the live run (what is happening now), and everything
 * when tool details are on.
 */
export function pace(items: Item[], opts: { details?: boolean } = {}): Entry[] {
  const rs = roles(items);
  const live = liveRun(items);
  const out: Entry[] = [];
  let stretch: Item[] = [];
  const flush = (atEnd: boolean) => {
    const s = stretch;
    stretch = [];
    if (!s.length) return;
    const work = s.some((it) => it.kind === "tools" || it.kind === "assistant");
    const liveTail = atEnd && live !== undefined && s[s.length - 1].run === live;
    if (opts.details || s.length < 2 || !work || liveTail) {
      for (const it of s) out.push({ kind: "item", key: it.key, item: it, role: "process" });
      return;
    }
    out.push(foldOf(s));
  };
  items.forEach((it, i) => {
    if (rs[i] === "process") {
      stretch.push(it);
      return;
    }
    flush(false);
    out.push({ kind: "item", key: it.key, item: it, role: rs[i] });
  });
  flush(true);
  return out;
}

function foldOf(s: Item[]): Extract<Entry, { kind: "fold" }> {
  const calls = s.flatMap((it) => (it.kind === "tools" ? it.calls : []));
  const steps = calls.length + s.filter((it) => it.kind !== "tools").length;
  let start = Infinity;
  let end = 0;
  for (const it of s) {
    if (it.at) {
      start = Math.min(start, it.at);
      end = Math.max(end, it.at);
    }
    if (it.kind === "tools")
      for (const c of it.calls) {
        if (c.startedAt) start = Math.min(start, c.startedAt);
        end = Math.max(end, c.startedAt + (c.durationMs ?? 0));
      }
  }
  const st = groupStats(calls);
  return {
    kind: "fold",
    key: `${s[0].key}.fold`,
    items: s,
    text: calls.length ? groupSummary(calls) : `${steps} steps`,
    steps,
    failed: st.failed,
    running: st.running,
    tone: groupTone(st),
    durationMs: start !== Infinity && end >= start && !st.running ? end - start : null,
  };
}
