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
}

export interface KeyResult {
  state: CardState;
  handled: boolean;
  /** all answers, when Enter submits */
  submit?: Record<string, string>;
  /** the UI should focus the Other text field */
  focusOther?: boolean;
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

export function answersOf(cs: CardState, d: Decision): Record<string, string> | null {
  const qs = questionsOf(d);
  const out: Record<string, string> = {};
  for (let i = 0; i < qs.length; i++) {
    const a = answerOf(cs, d, i);
    if (a === null) return null;
    out[qs[i].question] = a;
  }
  return out;
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
  if (q.multiSelect) {
    const cur = cs.picks[cs.index];
    const next = cur.includes(label) ? cur.filter((l) => l !== label) : [...cur, label];
    return { ...cs, picks: replaceAt(cs.picks, cs.index, next) };
  }
  return {
    ...cs,
    picks: replaceAt(cs.picks, cs.index, [label]),
    other: replaceAt(cs.other, cs.index, null),
    otherOpen: false,
    index: Math.min(cs.index + 1, qs.length - 1),
  };
}

export function openOther(cs: CardState): CardState {
  const other = cs.other[cs.index] ?? "";
  return { ...cs, otherOpen: true, other: replaceAt(cs.other, cs.index, other) };
}

export function setOther(cs: CardState, d: Decision, text: string): CardState {
  const q = questionsOf(d)[cs.index];
  const picks = q && !q.multiSelect && text ? replaceAt(cs.picks, cs.index, []) : cs.picks;
  return { ...cs, picks, other: replaceAt(cs.other, cs.index, text === "" ? null : text) };
}

export function closeOther(cs: CardState): CardState {
  const text = (cs.other[cs.index] ?? "").trim();
  return { ...cs, otherOpen: false, other: text ? cs.other : replaceAt(cs.other, cs.index, null) };
}

export function goTo(cs: CardState, d: Decision, index: number): CardState {
  const n = questionsOf(d).length;
  if (index < 0 || index >= n || index === cs.index) return cs;
  return { ...closeOther(cs), index };
}

/**
 * Keys: 1-9 pick (single-select advances, multi-select toggles), 0 opens Other, Enter submits when every
 * question is answered (from the Other field, Enter moves to the next unanswered question first),
 * Tab / Shift+Tab move between questions. `inOther` is true when the key comes from the Other field.
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
  if (key === "Tab") return { state: goTo(cs, d, cs.index + (opts.shift ? -1 : 1)), handled: true };
  if (key === "Enter") {
    const all = answersOf(cs, d);
    if (all) return { state: closeOther(cs), handled: true, submit: all };
    // Move to the first unanswered question.
    const next = qs.findIndex((_, i) => answerOf(cs, d, i) === null);
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

/** SPEC U15: what the card's button does now: nothing yet, the next open question, or send. */
export function submitState(cs: CardState, d: Decision): { label: string; act: "none" | "next" | "submit"; answered: number; total: number } {
  const qs = questionsOf(d);
  const done = qs.map((_, i) => answerOf(cs, d, i) !== null);
  const answered = done.filter(Boolean).length;
  const total = qs.length;
  if (answered === total) return { label: total === 1 ? "Send answer" : `Send ${total} answers`, act: "submit", answered, total };
  if (done[cs.index]) return { label: "Next question", act: "next", answered, total };
  return { label: "Pick an answer", act: "none", answered, total };
}

/** The first question without an answer, after the current one (wrapping). */
export function nextOpen(cs: CardState, d: Decision): number {
  const n = questionsOf(d).length;
  for (let k = 1; k <= n; k++) {
    const i = (cs.index + k) % n;
    if (answerOf(cs, d, i) === null) return i;
  }
  return cs.index;
}
