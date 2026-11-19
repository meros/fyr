// Decision 0032, V0.188: what the memory guard did, as words and as geometry. Pure: the components print them.
// A "lane" is one thread over the last stretch of time: when its work ran and when the guard held it frozen, on a
// band that marks when the host's memory was short.

import { spanText, type HostLoad } from "./load.ts";

export interface GuardEvent {
  at: string;
  /** "short" and "recovered" are the host's memory; "freeze" and "thaw" a thread's work; V0.rerun-classes: "kill" of a
   *  rerunnable job, "capped" when its class was killed 3 times in an hour and is frozen instead; V0.guard-tidy: "kept"
   *  when frozen work stays frozen past the 10 minute limit because the agents' memory stalls critically */
  kind: "short" | "recovered" | "freeze" | "thaw" | "kill" | "capped" | "kept";
  work?: "undeclared" | "job";
  thread?: string;
  /** the freeze's number in its episode */
  n?: number;
  /** why a thaw happened */
  reason?: "memory" | "gone" | "limit" | "ended" | "ceiling";
  headroom?: number;
  stall?: string;
  /** the rerunnable class of a kill or a cap */
  class?: string;
}

/** V0.rerun-classes: a rerunnable class the guard killed in the last hour. */
export interface GuardRerun {
  class: string;
  kills: number;
  cappedUntil?: string;
}

export interface GuardThread {
  thread: string;
  n: number;
  frozen: boolean;
  frozenAt?: string;
  nextAt?: string;
  capped?: boolean;
}

export interface Guard {
  short: boolean;
  shortSince?: string;
  max: number;
  refreezeSec: number;
  /** null from a host before V0.190 whose guard has done nothing yet */
  events: GuardEvent[] | null;
  threads?: GuardThread[];
  rerun?: GuardRerun[];
}

/** How far back the chart looks. */
export const GUARD_WINDOW = 15 * 60 * 1000;

/** A stretch of a lane, as shares of the window (0 left, 1 now). */
export interface Span {
  a: number;
  b: number;
}

export interface Lane {
  thread: string;
  title: string;
  /** when the guard held the work frozen */
  frozen: Span[];
  /** a freeze starts here, numbered in its episode */
  marks: { x: number; n: number }[];
  /** the work is frozen right now */
  live: boolean;
}

export interface GuardChart {
  /** when the host's memory was short */
  short: Span[];
  lanes: Lane[];
  /** the text alternative: what the picture says */
  alt: string;
}

const clip = (a: number, b: number, from: number, to: number): Span | null => {
  const x = Math.max(a, from);
  const y = Math.min(b, to);
  if (y <= x) return null;
  return { a: (x - from) / (to - from), b: (y - from) / (to - from) };
};

/** The lanes and the band of the guard's history over the last GUARD_WINDOW, from its events. A freeze with no thaw yet
 *  runs to now. Threads with nothing in the window have no lane; the lanes are in the order the threads first froze. */
export function guardChart(g: Guard | undefined, now: number, titleOf: (thread: string) => string): GuardChart {
  const from = now - GUARD_WINDOW;
  const empty: GuardChart = { short: [], lanes: [], alt: "The memory guard did nothing in the last 15 minutes." };
  if (!g) return empty;
  const ev = [...(g.events ?? [])].sort((x, y) => Date.parse(x.at) - Date.parse(y.at));
  const short: Span[] = [];
  let shortFrom: number | null = null;
  const lanes = new Map<string, Lane>();
  const open = new Map<string, number>();
  const lane = (thread: string): Lane => {
    let l = lanes.get(thread);
    if (!l) lanes.set(thread, (l = { thread, title: titleOf(thread), frozen: [], marks: [], live: false }));
    return l;
  };
  for (const e of ev) {
    const t = Date.parse(e.at);
    if (e.kind === "short") shortFrom ??= t;
    else if (e.kind === "recovered" && shortFrom !== null) {
      const s = clip(shortFrom, t, from, now);
      if (s) short.push(s);
      shortFrom = null;
    } else if (e.kind === "freeze" && e.thread) {
      open.set(e.thread, t);
      const l = lane(e.thread);
      if (t >= from) l.marks.push({ x: (t - from) / GUARD_WINDOW, n: e.n ?? 0 });
    } else if ((e.kind === "thaw" || e.kind === "kill") && e.thread) {
      const a = open.get(e.thread);
      if (a !== undefined) {
        const s = clip(a, t, from, now);
        if (s) lane(e.thread).frozen.push(s);
        open.delete(e.thread);
      }
    }
  }
  if (shortFrom !== null) {
    const s = clip(shortFrom, now, from, now);
    if (s) short.push(s);
  }
  for (const [thread, a] of open) {
    const l = lane(thread);
    const s = clip(a, now, from, now);
    if (s) l.frozen.push(s);
    l.live = true;
  }
  const out = [...lanes.values()].filter((l) => l.frozen.length || l.marks.length);
  const frozen = out.filter((l) => l.live).length;
  const parts = [
    short.length ? `Memory was short ${short.length === 1 ? "once" : `${short.length} times`} in the last 15 minutes` : "Memory was not short in the last 15 minutes",
    out.length ? `${out.length} thread${out.length === 1 ? "" : "s"} frozen at least once` : "nothing frozen",
    frozen ? `${frozen} frozen now` : "",
  ].filter(Boolean);
  return { short, lanes: out, alt: `${parts.join("; ")}.` };
}

/** The thread's guard state in a few words for its row: "paused 2/8 · 41 s" while frozen, "next slice in 24 s" while the
 *  work runs between two freezes, "let run after 8 pauses" once the cap is reached; null when the guard never froze it. */
export function guardLine(g: Guard | undefined, thread: string, now: number): string | null {
  const t = g?.threads?.find((x) => x.thread === thread);
  if (!g || !t || t.n === 0) return null;
  if (t.frozen) return `paused ${t.n}/${g.max}${t.frozenAt ? ` · ${spanText((now - Date.parse(t.frozenAt)) / 1000)}` : ""}`;
  if (t.capped) return `let run after ${g.max} pauses`;
  if (t.nextAt && Date.parse(t.nextAt) > now) return `runs · next pause possible in ${spanText((Date.parse(t.nextAt) - now) / 1000)}`;
  return null;
}

/** One line for Settings: "Memory is short (3 min) · 1 thread frozen" or "Memory is fine". */
export function guardSummary(g: Guard | undefined, now: number): string {
  if (!g) return "The guard has not run yet.";
  const frozen = g.threads?.filter((x) => x.frozen).length ?? 0;
  const head = g.short && g.shortSince ? `Memory is short (${spanText((now - Date.parse(g.shortSince)) / 1000)})` : g.short ? "Memory is short" : "Memory is fine";
  return frozen ? `${head} · ${frozen} thread${frozen === 1 ? "" : "s"} frozen` : head;
}

/** The guard of the first host that has one. */
export function findGuard(list: HostLoad[]): { h: HostLoad; g: Guard } | null {
  for (const h of list) {
    const g = (h.slots as { guard?: Guard } | undefined)?.guard;
    if (g) return { h, g };
  }
  return null;
}

const WHY: Record<string, string> = {
  memory: "memory was fine for 30 s",
  gone: "its processes ended",
  limit: "the 10 minute limit",
  ended: "the work ended or was admitted",
  ceiling: "frozen 30 minutes, the most the guard holds work", // V0.guard-tidy
};

/** The guard's decisions newest first, one line each, for the list under the chart. */
export function guardLog(g: Guard | undefined, titleOf: (thread: string) => string, clock: (iso: string) => string, limit = 8): { at: string; text: string }[] {
  if (!g) return [];
  const rows = [...(g.events ?? [])].reverse().map((e) => {
    const who = e.thread ? titleOf(e.thread) : "";
    const room = e.headroom !== undefined ? ` (${Math.round(e.headroom * 100)} % free)` : "";
    let text = "";
    if (e.kind === "short") text = `Memory became short${room}`;
    else if (e.kind === "recovered") text = `Memory recovered${room}`;
    else if (e.kind === "kill") text = `Killed ${e.work === "job" ? "the job" : "undeclared work"} of ${who} (${e.class ?? "rerunnable"}) to free memory; it runs again when admitted${room}`;
    else if (e.kind === "capped") text = `Stopped killing ${e.class ?? "a class"} for an hour: killed 3 times; it is frozen instead`;
    else if (e.kind === "kept") text = `Kept ${e.work === "job" ? "the job" : "undeclared work"} of ${who} frozen past 10 minutes: the agents' memory stalls critically`;
    else if (e.kind === "freeze") text = `${e.work === "job" ? "Paused the job of" : "Paused undeclared work of"} ${who}${e.n ? ` (${e.n}/${g.max})` : ""}${room}`;
    else text = `Resumed ${who}: ${WHY[e.reason ?? "memory"] ?? "memory was fine"}`;
    return { at: clock(e.at), text };
  });
  return rows.slice(0, limit);
}

/** V0.rerun-classes (Settings → Automation, desktop and phone): one row per class the owner marked rerunnable, with what
 *  the guard did to it in the last hour. */
export function rerunRows(h: HostLoad, clock: (iso: string) => string): { class: string; line: string }[] {
  const kills = (h.slots as { guard?: Guard } | undefined)?.guard?.rerun ?? [];
  return (h.rerun?.classes ?? []).map((c) => {
    const k = kills.find((x) => x.class === c);
    if (!k) return { class: c, line: "Not killed in the last hour" };
    const times = k.kills === 1 ? "once" : `${k.kills} times`;
    return { class: c, line: k.cappedUntil ? `Killed ${times} in the last hour; frozen instead until ${clock(k.cappedUntil)}` : `Killed ${times} in the last hour` };
  });
}
