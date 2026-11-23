// Pure helpers for the phone layout (design/mobile/README.md).

import { describeEffect } from "./effects.ts";
import type { Decision } from "./types.ts";
import { answerOf, questionsOf, settledOf, type CardState } from "./question.ts";

/** Single-select picks move on after this delay; multi-select and the last question never do. */
export const ADVANCE_MS = 250;
/** The phone's Undo toast. One second shorter than the desktop's UNDO_MS (store.ts): a thumb reaches the toast sooner than a
 *  pointer crosses the window, and the toast sits over the bottom bar the next tap lands on, so it must clear sooner. */
export const PHONE_UNDO_MS = 5000;

export interface Commit {
  label: string;
  enabled: boolean;
  /** what the button does */
  act: "submit" | "next" | "none" | "cancel";
}

/** Why this is not derived from question.ts submitState: the phone button is sticky and always shown, so it
 *  also has a disabled "Answer · n of m left" state, it counts a question as settled by its pick alone
 *  (single-select auto-advances after ADVANCE_MS, so it never offers "Next question" for one), and it
 *  words the result "Answer" where the desktop says "Send answer". The counts (answered, skipped, left)
 *  and the cancel-when-nothing-answered rule are the same as submitState's.
 *  The sticky commit button always names its result (README, Decisions). V0.ask-cancel: a skipped
 *  question counts as passed, and a card with no answer at all can only be cancelled. */
export function askCommit(cs: CardState, d: Decision): Commit {
  const qs = questionsOf(d);
  const n = qs.length;
  const answered = qs.filter((_, i) => answerOf(cs, d, i) !== null).length;
  const left = qs.filter((_, i) => !settledOf(cs, d, i)).length;
  const skipped = n - left - answered;
  if (left === 0) {
    if (answered === 0) return { label: "Cancel question", enabled: true, act: "cancel" };
    const label = n === 1 ? "Answer" : skipped ? `Answer ${answered}, skip ${skipped}` : `Answer ${n} questions`;
    return { label, enabled: true, act: "submit" };
  }
  const q = qs[cs.index];
  const cur = settledOf(cs, d, cs.index);
  const autoAdvances = q && !q.multiSelect && cs.other[cs.index] === null;
  if (cur && cs.index < n - 1 && !autoAdvances) return { label: "Next question", enabled: true, act: "next" };
  return { label: n === 1 ? "Answer" : `Answer · ${left} of ${n} left`, enabled: false, act: "none" };
}

export type Choice = "allow" | "deny" | null;

export function allowCommit(d: Decision, choice: Choice): Commit {
  const approval = d.kind === "approval";
  const e = approval ? null : describeEffect(d);
  if (choice === "allow") return { label: e ? e.yes : "Allow", enabled: true, act: "submit" };
  if (choice === "deny") return { label: e ? e.no : "Deny", enabled: true, act: "submit" };
  return { label: e ? "Pick one" : "Allow or deny", enabled: false, act: "none" };
}

/** Step dots up to 5 questions; beyond that only the words, "Question 1 of 9999" (the desktop has no dots, it prints
 *  progressText). */
export function stepLabel(index: number, n: number): { dots: boolean; text: string } {
  return { dots: n <= 5, text: `Question ${index + 1} of ${n}` };
}

/** Counts in the strip: 4 digits fit; 10000 and more show as 10k+. */
export function stripCount(n: number): string {
  return n >= 10000 ? "10k+" : String(n);
}

/** Which section the list is scrolled to: the last section whose header is at or above the top. */
export function activeSection<K extends string>(tops: { key: K; top: number }[], scrollTop: number): K | null {
  let cur: K | null = tops[0]?.key ?? null;
  for (const t of tops) if (t.top <= scrollTop + 4) cur = t.key;
  return cur;
}
