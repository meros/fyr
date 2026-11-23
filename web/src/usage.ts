/** V0.usage-ledger (decision 0041): a thread's tokens, as both fronts show them. Tokens only: no money, since a
 *  subscription is not billed per token. The total is a plain sum of the kinds, a count and not a weight; the title
 *  says each kind. */

/** A sum of tokens by kind, as the host sends it (api.Usage). Thinking is part of output. */
export interface Usage {
  input?: number;
  output?: number;
  cacheRead?: number;
  cacheWrite5m?: number;
  cacheWrite1h?: number;
  thinking?: number;
  total?: number;
}

/** 950 → "950", 12_345 → "12k", 1_234_567 → "1.2M", 25_000_000 → "25M". */
export function tokens(n: number | undefined): string {
  const v = Math.max(0, Math.round(n ?? 0));
  if (v < 1000) return String(v);
  if (v < 1_000_000) return `${v < 10_000 ? (v / 1000).toFixed(1).replace(/\.0$/, "") : Math.round(v / 1000)}k`;
  if (v < 1_000_000_000) return `${v < 10_000_000 ? (v / 1e6).toFixed(1).replace(/\.0$/, "") : Math.round(v / 1e6)}M`;
  return `${(v / 1e9).toFixed(1).replace(/\.0$/, "")}B`;
}

/** The chip's words, or "" when the thread has counted nothing. */
export function usageLabel(u: Usage | undefined): string {
  if (!u || !u.total) return "";
  return `${tokens(u.total)} tok`;
}

/** The chip's title: each kind on its own line. */
export function usageTitle(u: Usage | undefined): string {
  if (!u || !u.total) return "";
  const lines = [
    `Tokens this thread used: ${tokens(u.total)}`,
    `Input ${tokens(u.input)} · output ${tokens(u.output)}${u.thinking ? ` (thinking ${tokens(u.thinking)})` : ""}`,
    `Cache read ${tokens(u.cacheRead)} · cache write ${tokens((u.cacheWrite5m ?? 0) + (u.cacheWrite1h ?? 0))}`,
    "A plain sum of the kinds, with subagents and the CLI's own calls",
  ];
  return lines.join("\n");
}

// ---- V0.usage-page: the Usage page in Settings, on both fronts ----

/** One rollup row of a host: its key values by API name, and its sums. */
export interface UsageGroup extends Usage {
  key: Record<string, string>;
  reports?: number;
  webSearch?: number;
  webFetch?: number;
}

export interface UsageNames {
  thread?: Record<string, string>;
  project?: Record<string, { name: string; link?: string }>;
  provider?: Record<string, string>;
  account?: Record<string, string>;
}

/** A host's answer to GET /api/usage, stamped with the host it came from. */
export interface HostUsage {
  hostId: string;
  host: string;
  groups: UsageGroup[];
  names: UsageNames;
}

export interface UsageQuery {
  from?: string;
  to?: string;
  group: string[];
  where?: Record<string, string>;
}

/** The query string of a rollup: group, from, to, then the filters. */
export function usageSearch(q: UsageQuery): string {
  const s = new URLSearchParams();
  if (q.group.length) s.set("group", q.group.join(","));
  if (q.from) s.set("from", q.from);
  if (q.to) s.set("to", q.to);
  for (const [k, v] of Object.entries(q.where ?? {})) s.set(k, v);
  const t = s.toString();
  return t ? `?${t}` : "";
}

export type UsageRange = "today" | "7d" | "30d" | "90d";
export const RANGES: { value: UsageRange; label: string }[] = [
  { value: "today", label: "Today" },
  { value: "7d", label: "7 days" },
  { value: "30d", label: "30 days" },
  { value: "90d", label: "90 days" },
];

/** A local day, YYYY-MM-DD (each host files its rows under its own local day). */
export function localDay(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

/** The days a range covers, oldest first, ending today. */
export function rangeDays(r: UsageRange, now: number): string[] {
  const n = r === "today" ? 1 : r === "7d" ? 7 : r === "30d" ? 30 : 90;
  const out: string[] = [];
  const d = new Date(now);
  d.setHours(12, 0, 0, 0); // a daylight-saving change never skips or repeats a day
  for (let i = n - 1; i >= 0; i--) {
    const x = new Date(d);
    x.setDate(d.getDate() - i);
    out.push(localDay(x));
  }
  return out;
}

export type UsageBy = "model" | "account" | "provider" | "project" | "thread" | "source" | "trigger" | "part" | "billing" | "host";
export const BYS: { value: UsageBy; label: string }[] = [
  { value: "model", label: "Model" },
  { value: "account", label: "Account" },
  { value: "provider", label: "Provider" },
  { value: "project", label: "Project" },
  { value: "thread", label: "Thread" },
  { value: "source", label: "Source" },
  { value: "trigger", label: "Started by" },
  { value: "part", label: "Agent" },
  { value: "billing", label: "Billing" },
  { value: "host", label: "Host" },
];

const SOURCES: Record<string, string> = {
  turn: "Agent turns",
  "job:title": "Titles",
  "job:search": "Search",
  "job:question": "Question check",
  "job:approval": "Approval judge",
  "job:setup": "Project setup",
  calibration: "Overhead calibration",
};
const TRIGGERS: Record<string, string> = {
  owner: "Your message",
  thread: "Another thread",
  view: "Live view",
  handover: "Handover",
  notice: "Fyr notice",
  wake: "Wake-up",
  background: "Agent on its own",
  "": "Not a turn",
};
const PARTS: Record<string, string> = { main: "Main agent", other: "Subagents and CLI calls" };
const BILLING: Record<string, string> = { subscription: "Subscription", "api-key": "API key", endpoint: "Other endpoint", none: "None", "": "Unknown" };

/** A merged row: one entity across hosts. */
export interface UsageRow extends Required<Pick<Usage, "input" | "output" | "cacheRead" | "cacheWrite5m" | "cacheWrite1h" | "thinking">> {
  id: string;
  label: string;
  /** a second line: the host, for a thing that lives on one */
  sub?: string;
  total: number;
  reports: number;
  /** each host's own value of the key for this row (a linked project has one per host) */
  hosts: Record<string, string>;
}

/** What the page is narrowed to: a key, its label, and each host's value. A model, a source, a trigger, a part or a
 *  billing is the same on every host (global); a project, a provider, an account or a host is asked of its hosts only. */
export interface UsageFilter {
  key: UsageBy;
  label: string;
  hosts: Record<string, string>;
  global: boolean;
}

const GLOBAL: ReadonlySet<string> = new Set(["model", "source", "trigger", "part", "billing"]);

/** The filter a tap on a row sets; a thread row opens its thread instead, so it has none. */
export function filterOf(by: UsageBy, r: UsageRow): UsageFilter | null {
  if (by === "thread") return null;
  return { key: by, label: r.label, hosts: r.hosts, global: GLOBAL.has(by) };
}

/** Each host's filter, or undefined to ask every host: a global key asks all of them for its one value, a host's key
 *  only the hosts that have it. */
export function perHostWhere(f: UsageFilter | null): Record<string, Record<string, string>> | undefined {
  if (!f || f.global) return undefined;
  const out: Record<string, Record<string, string>> = {};
  for (const [h, v] of Object.entries(f.hosts)) out[h] = f.key === "host" ? {} : { [f.key]: v };
  return out;
}

/** The filter every host gets: a global key's one value. */
export function globalWhere(f: UsageFilter | null): Record<string, string> | undefined {
  if (!f || !f.global) return undefined;
  const v = Object.values(f.hosts)[0];
  return v === undefined ? undefined : { [f.key]: v };
}

/** "claude-sonnet-5-5" → "Sonnet 5.5"; other names are kept. */
export function modelLabel(m: string): string {
  const x = /^claude-([a-z]+)-(\d+)-(\d+)/.exec(m);
  if (!x) return m || "Unknown";
  return `${x[1][0].toUpperCase()}${x[1].slice(1)} ${x[2]}.${x[3]}`;
}

/** Joins the hosts' answers into one list by the group key, biggest first. A thread, a provider and an account belong
 *  to their host; a linked project is one row across hosts; a model, a day, a source or a trigger is one row
 *  everywhere. */
export function mergeUsage(hosts: HostUsage[], by: UsageBy | "day" | "hour"): UsageRow[] {
  const many = hosts.length > 1;
  const rows = new Map<string, UsageRow>();
  for (const h of hosts) {
    for (const g of h.groups) {
      const v = by === "host" ? h.host : (g.key[by] ?? "");
      let id = v;
      let label = v;
      let sub: string | undefined;
      switch (by) {
        case "host":
          id = h.hostId;
          break;
        case "model":
          label = modelLabel(v);
          break;
        case "source":
          label = SOURCES[v] ?? v;
          break;
        case "trigger":
          label = TRIGGERS[v] ?? v;
          break;
        case "part":
          label = PARTS[v] ?? v;
          break;
        case "billing":
          label = BILLING[v] ?? v;
          break;
        case "project": {
          const p = h.names.project?.[v];
          id = p?.link ? `link:${p.link}` : `${h.hostId}/${v}`;
          label = v ? (p?.name ?? v) : "No project";
          break;
        }
        case "thread":
          id = `${h.hostId}/${v}`;
          label = v ? (h.names.thread?.[v] || "Thread no longer here") : "No thread";
          sub = many ? h.host : undefined;
          break;
        case "provider":
          id = `${h.hostId}/${v}`;
          label = h.names.provider?.[v] ?? (v || "Unknown");
          sub = many ? h.host : undefined;
          break;
        case "account":
          id = `${h.hostId}/${v}`;
          label = h.names.account?.[v] ?? (v ? "A removed provider" : "Unknown");
          sub = many ? h.host : undefined;
          break;
      }
      const r = rows.get(id) ?? { id, label, sub, hosts: {}, input: 0, output: 0, cacheRead: 0, cacheWrite5m: 0, cacheWrite1h: 0, thinking: 0, total: 0, reports: 0 };
      r.hosts[h.hostId] = v;
      r.input += g.input ?? 0;
      r.output += g.output ?? 0;
      r.cacheRead += g.cacheRead ?? 0;
      r.cacheWrite5m += g.cacheWrite5m ?? 0;
      r.cacheWrite1h += g.cacheWrite1h ?? 0;
      r.thinking += g.thinking ?? 0;
      r.reports += g.reports ?? 0;
      r.total = r.input + r.output + r.cacheRead + r.cacheWrite5m + r.cacheWrite1h;
      rows.set(id, r);
    }
  }
  return [...rows.values()].sort((a, b) => b.total - a.total || a.label.localeCompare(b.label));
}

/** The tokens of each day of the range, zero for a day with none. */
export function perDay(hosts: HostUsage[], days: string[]): { day: string; total: number }[] {
  const by = new Map(mergeUsage(hosts, "day").map((r) => [r.id, r.total]));
  return days.map((day) => ({ day, total: by.get(day) ?? 0 }));
}

/** Fyr's own calls: every source that is not an agent's turn. */
export function overheadRows(hosts: HostUsage[]): UsageRow[] {
  return mergeUsage(hosts, "source").filter((r) => r.id !== "turn");
}

/** "Input 40 · output 5.2k · cache read 1.2M · cache write 30k" */
export function kindsLine(r: Usage): string {
  return `Input ${tokens(r.input)} · output ${tokens(r.output)} · cache read ${tokens(r.cacheRead)} · cache write ${tokens((r.cacheWrite5m ?? 0) + (r.cacheWrite1h ?? 0))}`;
}

/** A host's answer, read defensively: a missing list or map is empty. */
export function decodeUsage(raw: unknown, hostId: string, host: string): HostUsage {
  const j = (raw && typeof raw === "object" ? raw : {}) as { groups?: unknown; names?: unknown };
  const groups = Array.isArray(j.groups) ? (j.groups as UsageGroup[]).map((g) => ({ ...g, key: g && typeof g.key === "object" && g.key ? g.key : {} })) : [];
  const names = (j.names && typeof j.names === "object" ? j.names : {}) as UsageNames;
  return { hostId, host, groups, names };
}

/** V0.usage-page: today's tokens by local hour, midnight to the hour now, from the hosts' UTC hours ("2026-11-23T17").
 *  A UTC hour goes to the local hour it starts in (a zone with a half-hour offset gives each to one local hour). Hours
 *  after now are left out. */
export function todayHours(hosts: HostUsage[], now: number): { at: string; total: number; now: boolean }[] {
  const start = new Date(now);
  start.setHours(0, 0, 0, 0);
  const cur = new Date(now).getHours();
  const out = Array.from({ length: cur + 1 }, (_, h) => ({ at: `${String(h).padStart(2, "0")}:00`, total: 0, now: h === cur }));
  for (const r of mergeUsage(hosts, "hour")) {
    const t = Date.parse(`${r.id}:00:00Z`);
    if (!Number.isFinite(t) || t < start.getTime() || t > now) continue;
    const h = new Date(t).getHours();
    if (out[h]) out[h].total += r.total;
  }
  return out;
}

/** The days to ask for today's hours: yesterday and today, so a UTC hour that is today here is in the range. */
export function todayRange(now: number): { from: string; to: string } {
  const d = new Date(now);
  d.setHours(12, 0, 0, 0);
  const y = new Date(d);
  y.setDate(d.getDate() - 1);
  return { from: localDay(y), to: localDay(d) };
}
