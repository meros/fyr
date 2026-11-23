// V0.usage-page: a mock usage ledger, rolled up as a host would answer GET /api/usage. Deterministic: the same days,
// threads and models every run, so the layout test sees the same page.
import { localDay, type HostUsage, type UsageGroup, type UsageQuery } from "./usage.ts";

interface Rec {
  daysAgo: number;
  /** the local hour of day it happened in */
  hourOfDay?: number;
  source: string;
  trigger: string;
  thread: string;
  project: string;
  provider: string;
  account: string;
  billing: string;
  model: string;
  part: string;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite1h: number;
}

function ledger(lab: boolean): Rec[] {
  const out: Rec[] = [];
  const threads = lab ? ["t_lab1"] : ["t_mapperf", "t_lasso", "t_w3", "t_s4"];
  for (let d = 0; d < 40; d++) {
    threads.forEach((thread, i) => {
      if ((d + i) % 3 === 2) return; // not every thread every day
      const k = 1 + ((d * 7 + i * 3) % 5);
      const base = { daysAgo: d, hourOfDay: (8 + d * 5 + i * 3) % 14, thread, project: thread === "t_s4" ? "" : lab ? "p_lab" : "p_web", provider: "claude", account: lab ? "a_lab" : "a_desk", billing: "subscription" };
      out.push({ ...base, source: "turn", trigger: i % 2 ? "notice" : "owner", model: "claude-opus-5-5", part: "main", input: 40 * k, output: 6_000 * k, cacheRead: 900_000 * k, cacheWrite1h: 40_000 * k });
      out.push({ ...base, source: "turn", trigger: i % 2 ? "notice" : "owner", model: "claude-haiku-5-5", part: "other", input: 3_000 * k, output: 90 * k, cacheRead: 0, cacheWrite1h: 0 });
      if (i === 0) out.push({ ...base, source: "turn", trigger: "background", model: "claude-opus-5-5", part: "other", input: 20 * k, output: 9_000 * k, cacheRead: 1_200_000 * k, cacheWrite1h: 60_000 * k });
      out.push({ ...base, source: "job:title", trigger: "", model: "claude-haiku-5-5", part: "main", input: 2_500, output: 12, cacheRead: 0, cacheWrite1h: 0 });
    });
    if (!lab) out.push({ daysAgo: d, source: "job:approval", trigger: "", thread: "t_mapperf", project: "p_web", provider: "claude", account: "a_desk", billing: "subscription", model: "claude-haiku-5-5", part: "main", input: 4_000, output: 60, cacheRead: 0, cacheWrite1h: 0 });
  }
  return out;
}

const NAMES = {
  thread: { t_mapperf: "Map perf", t_lasso: "Lasso polygon perf", t_w3: "Listing cards v2", t_s4: "Pick a dishwasher under 8 000 kr", t_lab1: "Train the ranker" },
  project: { p_web: { name: "acme/web", link: "l_web" }, p_lab: { name: "acme/web", link: "l_web" } },
  provider: { claude: "Claude" },
  account: { a_desk: "Claude", a_lab: "Claude" },
};

/** One host's answer to the query; lab is the second host of a two-host mock. */
export function mockUsage(q: UsageQuery, now: number, hostId: string, host: string, lab = false): HostUsage {
  const at = (ago: number, h: number) => {
    const d = new Date(now);
    d.setHours(h, 0, 0, 0);
    d.setDate(d.getDate() - ago);
    return d;
  };
  // Today's records spread over the hours so far (8:00 to now), as a working day; the UTC hour is what a host files.
  const nowH = new Date(now).getHours();
  const rows = ledger(lab)
    .map((r) => {
      const h = r.daysAgo === 0 ? (r.hourOfDay ?? 9) % (nowH + 1) : Math.min((r.hourOfDay ?? 9) + 8, 23);
      const t = at(r.daysAgo, h);
      return { ...r, day: localDay(t), hour: t.toISOString().slice(0, 13) };
    })
    .filter((r) => (!q.from || r.day >= q.from) && (!q.to || r.day <= q.to))
    .filter((r) => Object.entries(q.where ?? {}).every(([k, v]) => String((r as Record<string, unknown>)[k] ?? "") === v));
  const groups = new Map<string, UsageGroup>();
  for (const r of rows) {
    const key: Record<string, string> = {};
    for (const k of q.group) key[k] = String((r as Record<string, unknown>)[k] ?? "");
    const id = JSON.stringify(key);
    const g = groups.get(id) ?? { key, reports: 0, input: 0, output: 0, cacheRead: 0, cacheWrite5m: 0, cacheWrite1h: 0, thinking: 0 };
    g.reports! += 1;
    g.input! += r.input;
    g.output! += r.output;
    g.cacheRead! += r.cacheRead;
    g.cacheWrite1h! += r.cacheWrite1h;
    groups.set(id, g);
  }
  return { hostId, host, groups: [...groups.values()], names: NAMES };
}
