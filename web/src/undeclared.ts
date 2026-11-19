// SPEC V0.164 (design/rogue-agents/SPEC.md 8): heavy work a thread runs without having asked ("undeclared"), as words:
// the row's mark, the rail's button and menu, the thread's strip, Settings' lines. Pure; the components print them.
// The owner's word is "undeclared"; "slot" and "reservation" never reach the owner (V0.163).

import type { Card } from "./card.ts";
import { guardLine } from "./guard.ts";
import { cpuText, gbText, spanText, undeclaredTip, usedMeasures, type HostLoad, type Lookup, type UndeclaredUse } from "./load.ts";

export { undeclaredTip, type Lookup };

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;
const since = (u: UndeclaredUse, now: number) => spanText((now - Date.parse(u.since)) / 1000);

/** A thread's use and the host it is on, from any host's load, or null. */
export function findUndeclared(list: HostLoad[], id: string): { h: HostLoad; u: UndeclaredUse } | null {
  for (const h of list) {
    const u = h.slots?.undeclared?.find((x) => x.thread === id);
    if (u) return { h, u };
  }
  return null;
}

/** "2.8 GB · 2.1 cores": the measures that are over, memory first. */
function measures(u: UndeclaredUse, pct: "none" | "first" | "all", host: string): string[] {
  const m = usedMeasures(u);
  const out: string[] = [];
  if (m.mem) out.push(`${gbText(u.mem)}${pct === "none" ? "" : ` (${u.memPct} %${pct === "first" ? ` of ${host}` : ""})`}`);
  if (m.cpu) out.push(`${cpuText(u.cpu)}${pct === "none" ? "" : ` (${u.cpuPct} %)`}`);
  return out;
}

export interface RowUndeclared {
  /** "undeclared · 2.8 GB · 2.1 cores", or "undeclared · paused: memory" */
  text: string;
  paused: boolean;
  title: string;
}

/** The row's mark (desktop and phone): it starts the second line of a flagged thread. */
export function rowUndeclared(list: HostLoad[], id: string, now: number): RowUndeclared | null {
  const f = findUndeclared(list, id);
  if (!f) return null;
  const u = f.u;
  const m = measures(u, "none", f.h.host);
  // Decision 0032: a thread the guard froze says how often and for how long; the guard's own words replace "memory".
  const g = guardLine(f.h.slots?.guard, id, now);
  const text = u.paused ? (g ? `undeclared · ${g}` : "undeclared · paused: memory") : ["undeclared", ...m, ...(g ? [g] : [])].join(" · ");
  const what = m.join(" and ");
  // V0.hover-cards: what it means, not the measure's name ("admit", "idle level")
  return { text, paused: u.paused, title: `Heavy work Fyr didn't schedule: ${what} more than at rest, for ${since(u, now)}.` };
}

export interface RailUndeclared {
  count: number;
  /** the thread to open when exactly one is counted */
  only: string | null;
  entries: { id: string; label: string }[];
  /** V0.hover-cards: the button's hover card: each thread, its use and for how long, and what a click does */
  card: Card;
  aria: string;
}

/** The rail's button (desktop only): the flagged threads. Null when none. */
export function railUndeclared(h: HostLoad, lookup: Lookup, now: number): RailUndeclared | null {
  const list = h.slots?.undeclared ?? [];
  if (!list.length) return null;
  const entries = list.map((u) => ({ id: u.thread, label: `${lookup(u.thread).title} · ${measures(u, "none", h.host).join(" · ")}` }));
  const one = list.length === 1;
  return {
    count: list.length,
    only: one ? list[0].thread : null,
    entries,
    card: {
      head: "Heavy work Fyr didn't schedule",
      level: "warm",
      summary: `${one ? "This thread runs" : `These ${list.length} threads run`} heavy jobs without asking first. Fyr counts them against ${h.host}'s heavy-job budget.`,
      sections: [
        {
          list: true,
          rows: list.map((u) => {
            const l = lookup(u.thread);
            return { label: "", value: l.title, note: [l.project, measures(u, "none", h.host).join(" · "), `for ${since(u, now)}`].filter(Boolean).join(" · ") };
          }),
        },
      ],
      source: one ? "Click to open it." : "Click for the list.",
    },
    aria: `${plural(list.length, "thread runs", "threads run")} undeclared heavy work`,
  };
}

export interface StripView {
  head: string;
  /** what the host does about it */
  body: string;
  command: string;
  note: string;
}

/** The strip under an open thread's header (desktop and phone), V0.209: only while Fyr holds the work paused. Work
 *  that merely runs asks nothing of the owner; the row and the rail show it. Null otherwise. */
export function stripView(h: HostLoad, u: UndeclaredUse): StripView | null {
  if (!u.paused) return null;
  const free = h.headroom !== undefined ? ` (${Math.round(h.headroom * 100)} % free)` : "";
  return {
    head: `Paused by Fyr: memory is short on ${h.host}${free}.`,
    body: "Frozen, not killed; it resumes when memory has recovered for 30 s, or after 10 min unless memory stalls.",
    command: u.command,
    note: [...measures(u, "none", h.host).slice(0, 1), "undeclared"].join(" · "),
  };
}

// ---- Settings → Automation → Heavy jobs ----

export interface UndeclaredRow {
  thread: string;
  title: string;
  project: string;
  command: string;
  usage: string;
}

/** "Undeclared now": a row per flagged thread. */
export function undeclaredRows(h: HostLoad, now: number, lookup: Lookup): UndeclaredRow[] {
  return (h.slots?.undeclared ?? []).map((u) => {
    const l = lookup(u.thread);
    return {
      thread: u.thread,
      title: l.title,
      project: l.project ?? "",
      command: u.command,
      usage: [...measures(u, "all", h.host), since(u, now)].join(" · "),
    };
  });
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const dayText = (iso: string) => {
  const d = new Date(Date.parse(iso));
  return Number.isNaN(d.getTime()) ? "" : `${d.getDate()} ${MONTHS[d.getMonth()]}`;
};

/** "Learned heavy jobs": the pattern, its kind and when Fyr learned it (or, before V0.209, the owner added it). */
export function addedRows(h: HostLoad): { pattern: string; line: string }[] {
  return (h.heavyAdded ?? []).map((a) => ({
    pattern: a.pattern,
    line: a.learned ? `${a.kind} · learned ${dayText(a.at)}` : `${a.kind} · added ${dayText(a.at)} · ${a.refused === 0 ? "not refused since" : a.refused === 1 ? "refused once since" : `refused ${a.refused} times since`}`,
  }));
}

export type ThresholdKey = "cpu" | "memory" | "after";
export interface ThresholdRow {
  key: ThresholdKey;
  title: string;
  now: string;
  value: string;
  unit: string;
}

const cap = (n: number) => (n >= 10 ? String(Math.round(n)) : n.toFixed(1).replace(/\.0$/, ""));

/** "When a thread counts as heavy": three fields, each with what it is on this host now. Null without the host's settings. */
export function thresholdRows(h: HostLoad): ThresholdRow[] | null {
  const s = h.undeclaredSettings;
  if (!s) return null;
  const c = h.capacity;
  return [
    { key: "cpu", title: "CPU above its idle level", now: c ? `Now ${(s.cpu * c.cpu).toFixed(1)} of ${h.host}'s ${cap(c.cpu)} cores` : "A share of what this host gives its agents", value: String(Math.round(s.cpu * 100)), unit: "% of this host" },
    { key: "memory", title: "Memory above its idle level", now: c ? `Now ${((s.memory * c.memory) / 1e9).toFixed(1)} GB of ${h.host}'s ${cap(c.memory / 1e9)} GB` : "A share of what this host gives its agents", value: String(Math.round(s.memory * 100)), unit: "% of this host" },
    { key: "after", title: "For at least", now: "Shorter bursts (a typecheck, one test file) never count", value: String(Math.round(s.after)), unit: "seconds" },
  ];
}

/** A threshold field's text as the setting host.undeclaredSettings takes it: whole percent to a share, seconds 30 to 120. */
export function parseThreshold(key: ThresholdKey, text: string): Record<string, number> | { error: string } {
  const t = text.trim();
  if (!/^\d+$/.test(t)) return { error: "Enter a whole number" };
  const n = Number(t);
  if (key === "after") return n >= 30 && n <= 120 ? { after: n } : { error: "Between 30 and 120 seconds" };
  return n >= 1 && n <= 100 ? { [key]: n / 100 } : { error: "Between 1 and 100 %" };
}

/** The error text of a parseThreshold result, or null when it parsed. */
export const thresholdError = (p: ReturnType<typeof parseThreshold>): string | null => (typeof p.error === "string" ? p.error : null);

// ---- the conversation's machine line ----

/** N9: what the host told the agent (a thread event of type "slot" with `undeclared`): one line, the whole text on
 *  unfolding. The measures named are those the host sent above zero. */
export function toldLine(d: { mem?: unknown; cpu?: unknown }): string {
  const mem = typeof d.mem === "number" && d.mem > 0 ? gbText(d.mem) : "";
  const cpu = typeof d.cpu === "number" && d.cpu > 0 ? cpuText(d.cpu) : "";
  const what = [mem, cpu].filter(Boolean).join(", ");
  return `Fyr told the agent: heavy work it did not ask for${what ? ` (${what})` : ""}; ask before a heavy job`;
}
