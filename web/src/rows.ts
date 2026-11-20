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
  return clipName((p?.name ?? name) || "") || null;
}

/** V0.row-calm: a long project name is cut in the text, not by the box, so no gap is left before the "·" after it. */
export function clipName(n: string, max = 14): string {
  return n.length > max ? n.slice(0, max - 1).trimEnd() + "…" : n;
}

export function stateLabel(s: AttentionState): string {
  return LABEL[s] ?? s;
}

/** V0.your-move: what the owner has to do, the groups of Your move in their order. (V0.text-ask-nudge: a question Fyr
 *  only read in the agent's text is no longer a group; Fyr tells the agent, which asks on a card.) */
export const NEEDS_GROUPS = [
  { key: "answer", label: "Answer" },
  { key: "approve", label: "Approve" },
  { key: "unblock", label: "Unblock" },
] as const;
export type NeedsGroup = (typeof NEEDS_GROUPS)[number]["key"];

/** V0.your-move: a question to answer, something to say yes to (an approval, a merge, a ready PR, a command to run), or
 *  something broken. Reads only what the host computed. */
export function needsGroup(t: Thread): NeedsGroup {
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
  if (st === "asking") {
    const d = currentDecision(t);
    if (d?.kind === "ask") {
      const n = d.questions?.length ?? 1;
      return n === 1 ? "1 question" : `${n} questions`;
    }
    // V0.row-calm: the word names the action, as "Merge #12" does; the line under it keeps the command.
    if (d?.kind === "approval") return `Allow ${d.tool ?? "tool"}`;
    if (d?.kind === "effect") return describeEffect(d, t.pr).label;
  }
  return blockedReason(t)?.word ?? (t.attention.reason || stateLabel(st));
}

/** V0.row-calm: the host's Needs you reason is "<change title>: <need>" (V0.101) or "Run: <why>" (V0.149). The row
 *  puts the short word ("Merge", "Run") in the state's place and the rest on the line, so a long change title in the
 *  state colour does not fill the line. */
export function blockedReason(t: Pick<Thread, "attention" | "changes">): { word: string; what: string } | null {
  const r = t.attention.reason;
  if (t.attention.state !== "blocked" || !r) return null;
  const i = r.indexOf(": ");
  if (i < 0) return null;
  const head = r.slice(0, i);
  const tail = r.slice(i + 2);
  if (head === "Run") return { word: head, what: tail };
  if ((t.changes ?? []).some((c) => c.title === head) && tail) return { word: tail, what: head };
  return null;
}

/** V0.row-calm: the state word names the row's PR ("Merge #4170"), so a "#4170 ready" chip under it says nothing new. */
export function stateNamesPr(t: Thread): boolean {
  return !!t.pr && new RegExp(`#${t.pr.number}\\b`).test(rowState(t));
}

/** V0.row-calm: the detail after the state word, without what the state word already says. Its first part is left
 *  out when it names the same PR ("Merge #12" over "Merge pull request #12 · checks are green"), and a detail that
 *  starts with the state word loses that part, or only the word when nothing follows ("API error 529: overloaded" reads "529: overloaded"). */
export function rowDetail(t: Thread, o: { keys?: boolean } = {}): string {
  return trimEcho(rowState(t), rowMeta(t, o));
}

export function trimEcho(state: string, meta: string): string {
  const parts = meta.split(" · ");
  const ref = state.match(/#\d+\b/)?.[0];
  const head = parts[0];
  if (ref && new RegExp(`${ref}\\b`).test(head)) parts.shift();
  else if (state && head.toLowerCase().startsWith(state.toLowerCase())) {
    const rest = head.slice(state.length).replace(/^[\s:·,–-]+/, "");
    // With more parts after it the echo goes whole ("Update Fyr to a new version · Hardening" reads "Hardening");
    // alone, its tail is the news ("529: overloaded").
    if (rest && parts.length === 1) parts[0] = rest;
    else parts.shift();
  }
  return parts.join(" · ");
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
  const b = blockedReason(t);
  if (b) return [b.what, rowSummary(t)].filter(Boolean).join(" · ");
  return rowSummary(t);
}
