// The text of an inbox row that needs you: the state word and the line after it. Pure; the desktop
// inbox and the phone inbox draw the same words (review 2026-11-15).

import { currentDecision } from "./decode.ts";
import { describeEffect } from "./effects.ts";
import type { AttentionState, Project, Thread } from "./types.ts";
import { rowSummary } from "./view.ts";

const LABEL: Record<AttentionState, string> = {
  asking: "Asking",
  failed: "Failed",
  blocked: "Blocked",
  done: "Done",
  working: "Working",
  waiting: "Waiting",
  seen: "Seen",
  snoozed: "Snoozed",
  settled: "Settled",
  archived: "Archived",
};

/** V0.135: a row shows its project as the icon before the title. The name goes on the second line, muted, only when
 *  the icon is not an image (a monogram or a glyph alone may not say which project it is) and no project filter is
 *  on (then every row is that project). */
export function rowProjectName(p: Pick<Project, "name" | "image"> | undefined, name: string, filter: string | null): string | null {
  if (filter || p?.image) return null;
  return (p?.name ?? name) || null;
}

export function stateLabel(s: AttentionState): string {
  return LABEL[s] ?? s;
}

/** V0.210: the thread is in Needs you only because Fyr read a question in the agent's text. */
export function textAskShown(t: Pick<Thread, "attention" | "textAsk">): boolean {
  return t.attention.state === "blocked" && !!t.textAsk && t.attention.reason === t.textAsk.question;
}

/** V0.your-move: what the owner has to do, the groups of Your move in their order. Questions Fyr only read in the text come
 *  last, as decision 0033 asks: they are an inference, the others are facts. */
export const NEEDS_GROUPS = [
  { key: "answer", label: "Answer" },
  { key: "approve", label: "Approve" },
  { key: "unblock", label: "Unblock" },
  { key: "text", label: "Questions in text" },
] as const;
export type NeedsGroup = (typeof NEEDS_GROUPS)[number]["key"];

/** V0.your-move: a question to answer, something to say yes to (an approval, a merge, a ready PR, a command to run), or
 *  something broken. Reads only what the host computed. */
export function needsGroup(t: Thread): NeedsGroup {
  if (textAskShown(t)) return "text";
  const st = t.attention.state;
  if (st === "asking") return t.decisions.length === 0 || t.decisions.some((d) => d.kind === "ask") ? "answer" : "approve";
  if (st === "blocked") {
    if (t.attention.reason?.startsWith("Run: ")) return "approve"; // V0.149 (5): the host's words for a run request
    const needs = t.changes.flatMap((c) => (c.need && !c.need.muted ? [c.need] : []));
    if (needs.length > 0 && needs.every((n) => n.cause === "ready")) return "approve";
  }
  return "unblock";
}

export function needsGroupRank(t: Thread): number {
  return NEEDS_GROUPS.findIndex((g) => g.key === needsGroup(t));
}

/** V0.your-move: the group label each drawn row carries above it, by thread id: on the first row of each group. A row that
 *  left Your move and keeps its place (the open row) starts no group, and the open row whose group changed stays where
 *  it is, so no label shows twice. */
export function needsHeads(rows: Thread[]): Record<string, string> {
  const out: Record<string, string> = {};
  const seen = new Set<NeedsGroup>();
  for (const t of rows) {
    if (t.attention.section !== "needs") continue;
    const g = needsGroup(t);
    if (seen.has(g)) continue;
    seen.add(g);
    out[t.id] = NEEDS_GROUPS.find((x) => x.key === g)!.label;
  }
  return out;
}

/** The bold word: the open decision's kind while asking, else the attention reason or the state. */
export function rowState(t: Thread): string {
  const st = t.attention.state;
  if (textAskShown(t)) return "Question in text"; // V0.210: says that this is Fyr's reading; the line says what
  if (st === "asking") {
    const d = currentDecision(t);
    if (d?.kind === "ask") {
      const n = d.questions?.length ?? 1;
      return n === 1 ? "1 question" : `${n} questions`;
    }
    if (d?.kind === "approval") return "Approval";
    if (d?.kind === "effect") return describeEffect(d, t.pr).label;
  }
  return t.attention.reason || stateLabel(st);
}

/** The line after it. `keys`: a failed row names the retry key (the desktop has a keyboard). */
export function rowMeta(t: Thread, o: { keys?: boolean } = {}): string {
  const st = t.attention.state;
  if (textAskShown(t)) return t.textAsk!.question;
  const d = currentDecision(t);
  if (st === "asking" && d) {
    // The asker is named on the card (U25); the row keeps the question, as the design draws it.
    if (d.kind === "ask") return d.questions?.[0]?.question ?? "";
    if (d.kind === "approval") {
      const cmd = typeof d.input?.command === "string" ? ` ${d.input.command}` : "";
      return `Allow ${d.tool ?? "tool"}${cmd}`;
    }
    if (d.kind === "effect") {
      const x = describeEffect(d, t.pr);
      return `${x.title}${x.why ? " · " + x.why : ""}`;
    }
  }
  if (st === "failed") return `${t.failure?.message ?? ""}${o.keys ? " · r to retry" : ""}`;
  if (st === "working") return t.activity ?? "";
  return rowSummary(t);
}
