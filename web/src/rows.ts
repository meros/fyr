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
