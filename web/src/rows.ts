// The text of an inbox row that needs you: the state word and the line after it. Pure; the desktop
// inbox and the phone inbox draw the same words (review 2026-11-15).

import { currentDecision } from "./decode.ts";
import { describeEffect } from "./effects.ts";
import type { AttentionState, Thread } from "./types.ts";
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

export function stateLabel(s: AttentionState): string {
  return LABEL[s] ?? s;
}

/** The bold word: the open decision's kind while asking, else the attention reason or the state. */
export function rowState(t: Thread): string {
  const st = t.attention.state;
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
