// Pure helpers for the phone layout (design/mobile/README.md).

import type { Decision, Profile, Project } from "./types.ts";
import { answerOf, questionsOf, type CardState } from "./question.ts";

/** Single-select picks move on after this delay; multi-select and the last question never do. */
export const ADVANCE_MS = 250;
/** The phone's Undo toast. */
export const PHONE_UNDO_MS = 5000;

export interface Commit {
  label: string;
  enabled: boolean;
  /** what the button does */
  act: "submit" | "next" | "none";
}

/** The sticky commit button always names its result (README, Decisions). */
export function askCommit(cs: CardState, d: Decision): Commit {
  const qs = questionsOf(d);
  const n = qs.length;
  const left = qs.filter((_, i) => answerOf(cs, d, i) === null).length;
  if (left === 0) return { label: n === 1 ? "Answer" : `Answer ${n} questions`, enabled: true, act: "submit" };
  const q = qs[cs.index];
  const cur = answerOf(cs, d, cs.index) !== null;
  const autoAdvances = q && !q.multiSelect && cs.other[cs.index] === null;
  if (cur && cs.index < n - 1 && !autoAdvances) return { label: "Next question", enabled: true, act: "next" };
  return { label: n === 1 ? "Answer" : `Answer · ${left} of ${n} left`, enabled: false, act: "none" };
}

export type Choice = "allow" | "deny" | null;

export function allowCommit(d: Decision, choice: Choice): Commit {
  const approval = d.kind === "approval";
  if (choice === "allow") return { label: approval ? "Allow" : "Approve", enabled: true, act: "submit" };
  if (choice === "deny") return { label: approval ? "Deny" : "Decline", enabled: true, act: "submit" };
  return { label: approval ? "Allow or deny" : "Approve or decline", enabled: false, act: "none" };
}

/** Step dots up to 5 questions; beyond that, "1 of 9999". */
export function stepLabel(index: number, n: number): { dots: boolean; text: string } {
  return n <= 5 ? { dots: true, text: `Question ${index + 1} of ${n}` } : { dots: false, text: `Question ${index + 1} of ${n}` };
}

/** Counts in the strip: 4 digits fit; 10000 and more show as 10k+. */
export function stripCount(n: number): string {
  return n >= 10000 ? "10k+" : String(n);
}

export interface PhonePreset {
  label: string;
  until: Date;
  /** "15:41", "today 18:00", "Sun 06:00" */
  when: string;
}

function hm(d: Date): string {
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

/** The phone snooze sheet: in 1 hour, in 3 hours, this evening (only before 17:00), tomorrow morning, Monday morning. */
export function phoneSnoozePresets(now: Date): PhonePreset[] {
  const out: PhonePreset[] = [];
  const in1 = new Date(now.getTime() + 3600_000);
  const in3 = new Date(now.getTime() + 3 * 3600_000);
  out.push({ label: "In 1 hour", until: in1, when: hm(in1) });
  out.push({ label: "In 3 hours", until: in3, when: hm(in3) });
  if (now.getHours() < 17) {
    const ev = new Date(now);
    ev.setHours(18, 0, 0, 0);
    out.push({ label: "This evening", until: ev, when: "today 18:00" });
  }
  const tm = new Date(now);
  tm.setDate(now.getDate() + 1);
  tm.setHours(6, 0, 0, 0);
  out.push({ label: "Tomorrow morning", until: tm, when: `${DAYS[tm.getDay()]} 06:00` });
  const mon = new Date(now);
  mon.setDate(now.getDate() + (((8 - now.getDay()) % 7) || 7));
  mon.setHours(6, 0, 0, 0);
  out.push({ label: "Monday morning", until: mon, when: `${DAYS[mon.getDay()]} 06:00` });
  return out;
}

// ---- New thread: defaults remembered per device ----

export interface LastUsed {
  cwd?: string;
  profile?: string;
  model?: string;
}

export function parseLastUsed(raw: string | null): LastUsed {
  try {
    const j = JSON.parse(raw ?? "");
    if (j && typeof j === "object") return { cwd: typeof j.cwd === "string" ? j.cwd : undefined, profile: typeof j.profile === "string" ? j.profile : undefined, model: typeof j.model === "string" ? j.model : undefined };
  } catch {
    /* none stored */
  }
  return {};
}

/** The folder, profile and model a New screen starts with: the last used on this device when still valid, else the host's most recent. */
export function newDefaults(last: LastUsed, projects: Project[], profiles: Profile[]): { cwd: string; profile: string; model: string } {
  const recent = [...projects].sort((a, b) => (b.lastUsed ?? "").localeCompare(a.lastUsed ?? ""));
  const cwd = last.cwd || recent[0]?.cwd || "";
  const prof = profiles.find((p) => p.id === last.profile) ?? profiles[0];
  const model = prof && last.model && prof.models.includes(last.model) ? last.model : (prof?.defaultModel ?? prof?.models[0] ?? "");
  return { cwd, profile: prof?.id ?? "", model };
}

/** Which section the list is scrolled to: the last section whose header is at or above the top. */
export function activeSection<K extends string>(tops: { key: K; top: number }[], scrollTop: number): K | null {
  let cur: K | null = tops[0]?.key ?? null;
  for (const t of tops) if (t.top <= scrollTop + 4) cur = t.key;
  return cur;
}
