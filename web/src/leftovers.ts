// SPEC V0.176: leftover processes (what a thread left running outside its CLI) and the settled threads' worktrees, as
// words for the open thread's strip, the rail, the phone's load sheet and Settings. Pure texts; the components print them.
import { ago } from "./format.ts";
import type { HostId } from "./hostid.ts";
import type { Card } from "./card.ts";
import type { HostLoad, Lookup } from "./host-load.ts";

interface LeftoverKeep {
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
/** A worktree nobody works in (GET /api/load `temps`; V0.176, V0.worktree-sweep). */
export interface Temp {
  path: string;
  kind: "worktree";
  thread?: string;
  bytes: number;
  safe: boolean;
  why?: string;
  /** V0.worktree-sweep: when Fyr removes it, unless someone works in it first */
  removeAt?: string;
}

/** "1.9 GB", "60 MB": 1e9 and 1e6, as the rest of the load. */
function sizeText(b: number): string {
  return b >= 1e9 ? `${(b / 1e9).toFixed(1)} GB` : `${Math.max(1, Math.round(b / 1e6))} MB`;
}

/** "25 min", "10 h", "2 d": a span from now, rounded down, at least 1 min. */
function inText(iso: string, now: number): string {
  return ago(Math.max(60_000, Date.parse(iso) - now));
}

const heavy = (l: Leftover) => l.rss >= 100e6;
const named = (l: Leftover) => (heavy(l) ? `${l.name} (${sizeText(l.rss)})` : l.name);
const plural = (n: number) => `${n} ${n === 1 ? "process" : "processes"}`;
const byMemory = (a: Leftover, b: Leftover) => b.rss - a.rss || a.pid - b.pid;

/** A thread's leftovers on one host, the heaviest first; null without any. V0.kept-not-leftover: a process the agent
 *  kept (keep_process) is its work, not a leftover, and stays off the strip until the keep expires; Settings →
 *  Automation still lists it with End and Keep. */
export function findLeftovers(list: HostLoad[], id: string): { host: string; hostId?: HostId; items: Leftover[] } | null {
  for (const h of list) {
    const items = (h.leftovers ?? []).filter((l) => l.thread === id && !l.orphan && l.kept?.by !== "agent");
    if (items.length) return { host: h.host, ...(h.hostId ? { hostId: h.hostId } : {}), items: items.sort(byMemory) };
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
export function hostLeftovers(h: HostLoad): { count: number; text: string; card: Card } | null {
  const ls = h.leftovers ?? [];
  if (!ls.length) return null;
  const mem = ls.reduce((s, l) => s + l.rss, 0);
  return {
    count: ls.length,
    text: `${ls.length} leftover process${ls.length === 1 ? "" : "es"} · ${sizeText(mem)}`,
    // V0.hover-cards: the rail's hover card and the phone sheet's text: each process and its memory, heaviest first
    card: {
      head: "Still running after their thread",
      summary: `${plural(ls.length)} that threads started and left behind, ${sizeText(mem)} in all.`,
      sections: [{ list: true, rows: [...ls].sort(byMemory).map((l) => ({ label: "", value: l.name, note: [l.procs > 1 ? `${l.procs} processes` : "", l.rss > 0 ? sizeText(l.rss) : ""].filter(Boolean).join(" · ") || undefined })) }],
      source: `End or keep them in Settings → Hosts${h.host ? ` → ${h.host}` : ""}.`, // V0.settings-ia: on the host's page
    },
  };
}

interface LeftoverRow {
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
    // V0.settings-ia: a thread this device cannot look up (it was deleted, or its host is not shown here) has no
    // title (the lookup says `gone`; the row showed its raw id). The row says so, and offers no Open that leads nowhere.
    const t = l.orphan ? null : lookup(l.thread);
    const gone = !!t?.gone;
    const where = !t ? ["of no thread"] : gone ? ["of a thread that is gone"] : [t.title, t.project ?? ""];
    const hold = holdText(l, now, true);
    return { id: l.id, title, desc: [...where, hold].filter(Boolean).join(" · "), thread: t && !gone ? l.thread : null, canKeep: !l.kept };
  });
}

interface TempRow {
  path: string;
  title: string;
  desc: string;
  thread: string | null;
  canRemove: boolean;
}

/** Settings' rows of the worktrees nobody works in: what holds one, or "pushed and clean" and when Fyr removes it. */
export function tempRows(h: HostLoad, lookup: Lookup, now: number): TempRow[] {
  return (h.temps ?? []).map((t) => ({
    path: t.path,
    title: t.path,
    desc: [
      t.thread ? lookup(t.thread).title : "",
      sizeText(t.bytes),
      t.safe ? "pushed and clean" : `kept: ${t.why ?? "not safe to remove"}`,
      t.safe && t.why ? t.why : "",
      t.safe && t.removeAt ? `Fyr removes it in ${inText(t.removeAt, now)}` : "",
    ]
      .filter(Boolean)
      .join(" · "),
    thread: t.thread ?? null,
    canRemove: t.safe,
  }));
}
