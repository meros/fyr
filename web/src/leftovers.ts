// SPEC V0.175: leftover processes (what a thread left running outside its CLI) and the settled threads' worktrees, as
// words for the open thread's strip, the rail, the phone's load sheet and Settings. Pure texts; the components print them.
import type { HostLoad, Lookup } from "./load.ts";

export interface LeftoverKeep {
  thread: string;
  pid?: number;
  match?: string;
  until: string;
  why?: string;
  by: "agent" | "owner";
}
/** One leftover process and its descendants (GET /api/load `leftovers`). */
export interface Leftover {
  id: string;
  thread: string;
  /** no thread of this host has its cgroup */
  orphan?: boolean;
  pid: number;
  name: string;
  command: string;
  procs: number;
  rss: number;
  started: string;
  seen: string;
  /** the end its own `timeout` sets */
  until?: string;
  kept?: LeftoverKeep;
  /** when Fyr ends it, as the policy stands now; absent while it holds (a run, a keep, a timeout) */
  endsAt?: string;
}
/** A settled thread's worktree (GET /api/load `temps`). */
export interface Temp {
  path: string;
  kind: "worktree";
  thread?: string;
  bytes: number;
  safe: boolean;
  why?: string;
}

/** "1.9 GB", "60 MB": 1e9 and 1e6, as the rest of the load. */
export function sizeText(b: number): string {
  return b >= 1e9 ? `${(b / 1e9).toFixed(1)} GB` : `${Math.max(1, Math.round(b / 1e6))} MB`;
}

/** "25 min", "10 h", "2 d": a span from now, rounded down, at least 1 min. */
function inText(iso: string, now: number): string {
  const min = Math.max(0, Math.floor((Date.parse(iso) - now) / 60_000));
  if (min < 60) return `${Math.max(1, min)} min`;
  if (min < 48 * 60) return `${Math.floor(min / 60)} h`;
  return `${Math.floor(min / 1440)} d`;
}

const heavy = (l: Leftover) => l.rss >= 100e6;
const named = (l: Leftover) => (heavy(l) ? `${l.name} (${sizeText(l.rss)})` : l.name);
const byMemory = (a: Leftover, b: Leftover) => b.rss - a.rss || a.pid - b.pid;

/** A thread's leftovers on one host, the heaviest first; null without any. */
export function findLeftovers(list: HostLoad[], id: string): { host: string; items: Leftover[] } | null {
  for (const h of list) {
    const items = (h.leftovers ?? []).filter((l) => l.thread === id && !l.orphan);
    if (items.length) return { host: h.host, items: items.sort(byMemory) };
  }
  return null;
}

/** What holds a leftover, or when it ends: "kept for 3 h: dev server", "its timeout ends it in 10 h", "Fyr ends it in
 *  25 min", "" while a run holds it. by names who kept it (Settings does; the strip, on the thread itself, does not). */
function holdText(l: Leftover, now: number, by = false): string {
  if (l.kept) return `kept for ${inText(l.kept.until, now)}${by ? ` by the ${l.kept.by}` : ""}${l.kept.why && l.kept.by === "agent" ? `: ${l.kept.why}` : ""}`;
  if (l.until && Date.parse(l.until) > now) return `its timeout ends it in ${inText(l.until, now)}`;
  if (l.endsAt) return Date.parse(l.endsAt) <= now ? "ends now" : `ends in ${inText(l.endsAt, now)}`;
  return "";
}

/** The strip on the open thread: "2 leftover processes: chromium (1.9 GB), http.server · Fyr ends them in 25 min". The
 *  soonest end names the whole list's; a keep or a timeout of a single one is named instead. */
export function stripText(items: Leftover[], now: number): string {
  const head = `${items.length} leftover process${items.length === 1 ? "" : "es"}: ${items.map(named).join(", ")}`;
  let tail = "";
  if (items.length === 1) {
    const h = holdText(items[0], now);
    tail = h.startsWith("ends") ? `Fyr ${h.replace("ends", "ends it")}` : h;
  } else {
    const ends = items.map((l) => l.endsAt).filter((x): x is string => !!x && !items.find((l) => l.endsAt === x)?.kept);
    if (ends.length) {
      const soon = ends.sort()[0];
      tail = Date.parse(soon) <= now ? "Fyr ends them now" : `Fyr ends them in ${inText(soon, now)}`;
    }
  }
  return tail ? `${head} · ${tail}` : head;
}

/** The rail's and the phone sheet's count of a host's leftovers; null without any. */
export function hostLeftovers(h: HostLoad): { count: number; text: string; title: string } | null {
  const ls = h.leftovers ?? [];
  if (!ls.length) return null;
  const mem = ls.reduce((s, l) => s + l.rss, 0);
  return {
    count: ls.length,
    text: `${ls.length} leftover process${ls.length === 1 ? "" : "es"} · ${sizeText(mem)}`,
    title: `Processes threads left running: ${[...ls].sort(byMemory).map(named).join(", ")}. Settings → Automation lists them with End and Keep.`,
  };
}

export interface LeftoverRow {
  id: string;
  title: string;
  desc: string;
  /** the thread to open; null for an orphan */
  thread: string | null;
  /** Keep is offered unless the owner or the agent keeps it already */
  canKeep: boolean;
}

/** Settings' rows: "chromium · 9 processes · 1.9 GB" and "Map perf · fyr · ends in 25 min", the heaviest first. */
export function leftoverRows(h: HostLoad, lookup: Lookup, now: number): LeftoverRow[] {
  return [...(h.leftovers ?? [])].sort(byMemory).map((l) => {
    const size = l.rss > 0 ? sizeText(l.rss) : "";
    const title = [l.name, l.procs > 1 ? `${l.procs} processes` : "", size].filter(Boolean).join(" · ");
    const where = l.orphan ? ["in no thread's cgroup"] : [lookup(l.thread).title, lookup(l.thread).project ?? ""];
    const hold = holdText(l, now, true);
    return { id: l.id, title, desc: [...where, hold].filter(Boolean).join(" · "), thread: l.orphan ? null : l.thread, canKeep: !l.kept };
  });
}

export interface TempRow {
  path: string;
  title: string;
  desc: string;
  thread: string | null;
  canRemove: boolean;
}

/** Settings' rows of the settled threads' worktrees: what holds one, or "pushed and clean". */
export function tempRows(h: HostLoad, lookup: Lookup): TempRow[] {
  return (h.temps ?? []).map((t) => ({
    path: t.path,
    title: t.path,
    desc: [t.thread ? lookup(t.thread).title : "", sizeText(t.bytes), t.safe ? "pushed and clean" : `kept: ${t.why ?? "not safe to remove"}`].filter(Boolean).join(" · "),
    thread: t.thread ?? null,
    canRemove: t.safe,
  }));
}
