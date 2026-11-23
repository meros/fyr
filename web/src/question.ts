// Question card state and key handling (SPEC 13.2). Pure: the UI feeds keys in and sends the result.

import type { Decision, Question } from "./types.ts";

export interface CardState {
  decisionId: string;
  index: number;
  /** picked option labels per question */
  picks: string[][];
  /** free text of "Other" per question, null when Other is not chosen */
  other: (string | null)[];
  /** the Other field of the active question is open */
  otherOpen: boolean;
  /** V0.ask-cancel: the owner skipped the question: no answer, and the agent is told so */
  skipped: boolean[];
}

/** What a card sends: the questions the owner answered, and the ones left without an answer. */
interface Submit {
  answers: Record<string, string>;
  skipped: string[];
}

interface KeyResult {
  state: CardState;
  handled: boolean;
  /** the answers and skips, when Enter submits */
  submit?: Submit;
  /** the UI should focus the Other text field */
  focusOther?: boolean;
  /** V0.ask-cancel: Shift+Esc closes the card without an answer */
  cancel?: boolean;
}

export function questionsOf(d: Decision): Question[] {
  return d.questions ?? [];
}

export function initCard(d: Decision): CardState {
  const n = questionsOf(d).length;
  return {
    decisionId: d.id,
    index: 0,
    picks: Array.from({ length: n }, () => []),
    other: Array.from({ length: n }, () => null),
    otherOpen: false,
    skipped: Array.from({ length: n }, () => false),
  };
}

export function answerOf(cs: CardState, d: Decision, i: number): string | null {
  const q = questionsOf(d)[i];
  if (!q) return null;
  const other = (cs.other[i] ?? "").trim();
  const picks = cs.picks[i] ?? [];
  if (q.multiSelect) {
    const parts = [...q.options.map((o) => o.label).filter((l) => picks.includes(l))];
    if (other) parts.push(other);
    return parts.length ? parts.join(", ") : null;
  }
  if (cs.other[i] !== null) return other || null;
  return picks[0] ?? null;
}

/** V0.ask-cancel: the question has an answer or was skipped, so the card does not wait on it. */
export function settledOf(cs: CardState, d: Decision, i: number): boolean {
  return answerOf(cs, d, i) !== null || !!cs.skipped[i];
}

/** The answers, or null while a question has neither an answer nor a skip. */
function answersOf(cs: CardState, d: Decision): Record<string, string> | null {
  const qs = questionsOf(d);
  const out: Record<string, string> = {};
  for (let i = 0; i < qs.length; i++) {
    const a = answerOf(cs, d, i);
    if (a === null) {
      if (!cs.skipped[i]) return null;
      continue;
    }
    out[qs[i].question] = a;
  }
  return out;
}

/** V0.ask-cancel: the questions the owner left without an answer. */
function skippedOf(cs: CardState, d: Decision): string[] {
  return questionsOf(d)
    .filter((_, i) => !!cs.skipped[i] && answerOf(cs, d, i) === null)
    .map((q) => q.question);
}

/** What the card sends, or null while a question has neither an answer nor a skip. */
export function submitOf(cs: CardState, d: Decision): Submit | null {
  const answers = answersOf(cs, d);
  return answers ? { answers, skipped: skippedOf(cs, d) } : null;
}

function replaceAt<T>(arr: T[], i: number, v: T): T[] {
  const c = arr.slice();
  c[i] = v;
  return c;
}

/** Pick option `opt` (0-based) of the active question, the same as pressing its number key. */
export function pick(cs: CardState, d: Decision, opt: number): CardState {
  const qs = questionsOf(d);
  const q = qs[cs.index];
  if (!q || opt < 0 || opt >= q.options.length) return cs;
  const label = q.options[opt].label;
  const skipped = replaceAt(cs.skipped, cs.index, false); // an answer takes the skip back
  if (q.multiSelect) {
    const cur = cs.picks[cs.index];
    const next = cur.includes(label) ? cur.filter((l) => l !== label) : [...cur, label];
    return { ...cs, picks: replaceAt(cs.picks, cs.index, next), skipped };
  }
  return {
    ...cs,
    picks: replaceAt(cs.picks, cs.index, [label]),
    other: replaceAt(cs.other, cs.index, null),
    otherOpen: false,
    skipped,
    index: Math.min(cs.index + 1, qs.length - 1),
  };
}

/**
 * V0.ask-cancel: skip the active question (dropping what it holds) and move on to the next one that
 * still needs an answer, or take the skip back.
 */
export function skip(cs: CardState, d: Decision): CardState {
  const qs = questionsOf(d);
  if (!qs[cs.index]) return cs;
  const i = cs.index;
  if (cs.skipped[i]) return { ...cs, skipped: replaceAt(cs.skipped, i, false) };
  const next: CardState = {
    ...closeOther(cs),
    picks: replaceAt(cs.picks, i, []),
    other: replaceAt(cs.other, i, null),
    skipped: replaceAt(cs.skipped, i, true),
  };
  for (let k = 1; k <= qs.length; k++) {
    const j = (i + k) % qs.length;
    if (j !== i && !settledOf(next, d, j)) return { ...next, index: j };
  }
  return next;
}

export function openOther(cs: CardState): CardState {
  const other = cs.other[cs.index] ?? "";
  return { ...cs, otherOpen: true, other: replaceAt(cs.other, cs.index, other), skipped: replaceAt(cs.skipped, cs.index, false) };
}

export function setOther(cs: CardState, d: Decision, text: string): CardState {
  const q = questionsOf(d)[cs.index];
  const picks = q && !q.multiSelect && text ? replaceAt(cs.picks, cs.index, []) : cs.picks;
  const skipped = text ? replaceAt(cs.skipped, cs.index, false) : cs.skipped;
  return { ...cs, picks, skipped, other: replaceAt(cs.other, cs.index, text === "" ? null : text) };
}

function closeOther(cs: CardState): CardState {
  const text = (cs.other[cs.index] ?? "").trim();
  return { ...cs, otherOpen: false, other: text ? cs.other : replaceAt(cs.other, cs.index, null) };
}

export function goTo(cs: CardState, d: Decision, index: number): CardState {
  const n = questionsOf(d).length;
  if (index < 0 || index >= n || index === cs.index) return cs;
  return { ...closeOther(cs), index };
}

/** V0.ask-cancel: to the previous question. */
export function back(cs: CardState, d: Decision): CardState {
  return goTo(cs, d, cs.index - 1);
}

/**
 * Keys: 1-9 pick (single-select advances, multi-select toggles), 0 opens Other, Enter submits when every
 * question is answered or skipped (from the Other field, Enter moves to the next unanswered question first),
 * Tab / Shift+Tab and ← / → move between questions, Shift+S skips the question being shown, and Shift+Esc
 * cancels the card (V0.ask-cancel). `inOther` is true when the key comes from the Other field, where keys
 * are text.
 */
export function cardKey(cs: CardState, d: Decision, key: string, opts: { shift?: boolean; inOther?: boolean } = {}): KeyResult {
  const qs = questionsOf(d);
  if (!qs.length) return { state: cs, handled: false };
  if (opts.inOther) {
    if (key === "Escape") return { state: closeOther(cs), handled: true };
    if (key === "Tab") return { state: goTo(cs, d, cs.index + (opts.shift ? -1 : 1)), handled: true };
    if (key !== "Enter" || opts.shift) return { state: cs, handled: false };
  }
  if (/^[1-9]$/.test(key)) {
    const opt = Number(key) - 1;
    if (opt >= qs[cs.index].options.length) return { state: cs, handled: false };
    return { state: pick(cs, d, opt), handled: true };
  }
  if (key === "0") return { state: openOther(cs), handled: true, focusOther: true };
  if (opts.shift && (key === "s" || key === "S")) return { state: skip(cs, d), handled: true };
  if (opts.shift && key === "Escape") return { state: cs, handled: true, cancel: true };
  if (key === "Tab") return { state: goTo(cs, d, cs.index + (opts.shift ? -1 : 1)), handled: true };
  if (key === "ArrowRight") return { state: goTo(cs, d, cs.index + 1), handled: true };
  if (key === "ArrowLeft") return { state: back(cs, d), handled: cs.index > 0 };
  if (key === "Enter") {
    const st = submitState(cs, d);
    // A card with no answer at all sends nothing: Enter cancels it, as its button does.
    if (st.act === "submit") return { state: closeOther(cs), handled: true, submit: submitOf(cs, d)! };
    if (st.act === "cancel") return { state: closeOther(cs), handled: true, cancel: true };
    // Move to the first question that still needs an answer.
    const next = qs.findIndex((_, i) => !settledOf(cs, d, i));
    return { state: goTo(cs, d, next), handled: true };
  }
  return { state: cs, handled: false };
}

/** A tap on the phone: pick without moving on; the view moves on after 250 ms for single-select (design/mobile). */
export function pickStay(cs: CardState, d: Decision, opt: number): CardState {
  const next = pick(cs, d, opt);
  return { ...next, index: cs.index };
}

/** Choose Other on the phone: opens the field; a single-select question drops its pick. */
export function chooseOther(cs: CardState, d: Decision): CardState {
  const q = questionsOf(d)[cs.index];
  const opened = openOther(cs);
  return q && !q.multiSelect ? { ...opened, picks: replaceAt(opened.picks, cs.index, []) } : opened;
}

/** SPEC U15: what the card's button does now: nothing yet, the next open question, send, or cancel
 *  (V0.ask-cancel: every question skipped sends nothing, so the card can only be cancelled). */
export function submitState(cs: CardState, d: Decision): { label: string; act: "none" | "next" | "submit" | "cancel"; answered: number; skipped: number; total: number } {
  const qs = questionsOf(d);
  const answered = qs.filter((_, i) => answerOf(cs, d, i) !== null).length;
  const skipped = qs.filter((_, i) => answerOf(cs, d, i) === null && !!cs.skipped[i]).length;
  const total = qs.length;
  if (answered + skipped === total) {
    if (answered === 0) return { label: "Cancel question", act: "cancel", answered, skipped, total };
    if (skipped > 0) return { label: answered === 1 ? "Send answer" : `Send ${answered} answers`, act: "submit", answered, skipped, total };
    return { label: total === 1 ? "Send answer" : `Send ${total} answers`, act: "submit", answered, skipped, total };
  }
  if (settledOf(cs, d, cs.index)) return { label: "Next question", act: "next", answered, skipped, total };
  return { label: "Pick an answer", act: "none", answered, skipped, total };
}

/** The first question without an answer or a skip, after the current one (wrapping). */
export function nextOpen(cs: CardState, d: Decision): number {
  const n = questionsOf(d).length;
  for (let k = 1; k <= n; k++) {
    const i = (cs.index + k) % n;
    if (!settledOf(cs, d, i)) return i;
  }
  return cs.index;
}

/** The line the card shows of its progress (V0.ask-cancel adds the skips). */
export function progressText(st: { answered: number; skipped: number; total: number }): string {
  if (st.total === 1) return st.answered ? "Answered" : st.skipped ? "Skipped" : "";
  const s = `${st.answered} of ${st.total} answered`;
  return st.skipped ? `${s} · ${st.skipped} skipped` : s;
}

/** The `thread.answer` arguments for a submitted card (V0.ask-cancel: a question the owner skipped goes with the card,
 *  named, so the agent hears it). The one place the payload is built, for both fronts. */
export function answerArgs(d: Decision, send: Submit): Record<string, unknown> {
  return { decisionId: d.id, answers: send.answers, ...(send.skipped.length ? { skipped: send.skipped } : {}) };
}

/** The `thread.answer` arguments for a tool approval. */
export function allowArgs(d: Decision, allow: boolean): Record<string, unknown> {
  return { decisionId: d.id, allow };
}

/** Send the answer, then open the next thread that needs you, only when the host took it (as the phone does). */
export async function answerThenNext(answer: () => Promise<boolean>, next: () => void): Promise<void> {
  if (await answer()) next();
}
