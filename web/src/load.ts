// SPEC 0.5 V0.80: the machine load (rail) and each working thread's CPU (rows), as words. Pure.

export interface ThreadLoad {
  /** a thread id, or "self-deploy" */
  id: string;
  /** cores over the last read; absent on a host's first read */
  cpu?: number;
  memory: number;
  /** decision 0029 S1: the cgroup's cpu.pressure some avg10, its memory.peak, the class of its slot's job now */
  cpuPressure?: number;
  peak?: number;
  job?: string;
  /** decision 0029 S5: the memory guard froze this thread's job */
  paused?: boolean;
}
/** Decision 0029 S1: one measured job class (its newest run's measures, the median wall time of its last ten). */
export interface JobClass {
  class: string;
  runs: number;
  cores: number;
  demand: number;
  peak: number;
  /** seconds */
  wall: number;
  at: string;
  /** decision 0029 S3: the class's cost on this host now (older hosts) and how many runs it is learned from */
  cost?: number;
  learned?: number;
  /** V0.163: the class's need as percentages of this host */
  cpuPct?: number;
  memPct?: number;
}
export interface HostLoad {
  host: string;
  at?: string;
  cores: number;
  /** 1, 5 and 15 minutes */
  load: [number, number, number];
  /** /proc/pressure/cpu some avg10, percent */
  cpuPressure?: number;
  /** false: the agents share the daemon's cgroup and threads is empty */
  cgroups: boolean;
  agents?: { cpu?: number; cpuMax: number; memory: number; memoryMax: number };
  threads: ThreadLoad[];
  /** the host's heavy jobs (V0.86; V0.163 in percent). The API calls it `heavy`; an older host sends `slots`;
   *  normalizeLoad puts either here, so the client reads one field. */
  slots?: Slots;
  heavy?: Slots;
  /** Decision 0029 S1 (#185), absent without a provider (no /proc: macOS for now). */
  provider?: string;
  capacity?: { cpu: number; memory: number };
  /** s: how much slower a new job runs now, 1.0 on an idle host */
  slowdown?: number;
  /** the share of capacity.memory left, 0 to 1 */
  headroom?: number;
  /** PSI memory full avg10, percent, and its level */
  memoryPressure?: number;
  memoryStall?: "ok" | "elevated" | "critical";
  agentsCpuPressure?: number;
  jobs?: JobClass[];
  /** V0.164: the thresholds of undeclared heavy work, the commands behind it that no pattern names yet, and the
   *  patterns the owner added; absent on a host without V0.164 */
  undeclaredSettings?: UndeclaredSettings;
  suggestions?: HeavySuggestion[];
  heavyAdded?: HeavyAdded[];
}
/** V0.164: when a thread counts as heavy: shares of what the host gives its agents, and for how many seconds. */
export interface UndeclaredSettings {
  on: boolean;
  cpu: number;
  memory: number;
  after: number;
}
/** V0.164: a command that ran as undeclared work and matches no heavy pattern. */
export interface HeavySuggestion {
  id: string;
  command: string;
  pattern: string;
  kind: "test" | "build" | "e2e";
  seen: number;
  threads: number;
  lastThread: string;
  lastAt: string;
  /** bytes and cores above the idle level, the largest seen; seconds the longest */
  memory: number;
  cpu: number;
  seconds: number;
}
export interface HeavyAdded {
  pattern: string;
  kind: string;
  at: string;
  /** how often the gate refused it since */
  refused: number;
}
/** V0.164: a thread that runs heavy work Fyr did not admit (above its idle level). The host sets overCpu and overMem
 *  true for a measure that is over its threshold or at least a tenth of it; the front names only those. */
export interface UndeclaredUse {
  thread: string;
  cpu: number;
  mem: number;
  cpuPct: number;
  memPct: number;
  since: string;
  command: string;
  told?: string;
  dismissed: boolean;
  paused: boolean;
  queued: boolean;
  overCpu: boolean;
  overMem: boolean;
  /** the command already matches a heavy pattern: no "Add as heavy job…" */
  matched: boolean;
}
/** One owner of a slot or a place in the queue: a thread id, or "self-deploy". */
export interface SlotHolder {
  owner: string;
  /** "test", "build" or "e2e" */
  kind: string;
  since?: string;
  priority?: boolean;
  /** decision 0029 S2: the holder's share of the budget (1.0 a job, 0.1 an idle hold); older hosts */
  cost?: number;
  /** V0.163: the job's need as percentages of the host, CPU and memory, and pct the larger */
  cpuPct?: number;
  memPct?: number;
  pct?: number;
}
export interface Slots {
  n: number;
  held: SlotHolder[];
  waiting: SlotHolder[];
  /** decision 0029 S2: the holders' costs summed (absent without admission by a budget), and why the head waits */
  used?: number;
  why?: string;
  /** decision 0029 S4: the adapted budget (absent while it is n) and why */
  budget?: number;
  budgetWhy?: string;
  /** V0.163, percent of what the host gives its agents: in use, the budget and what is free in it per resource; usedPct
   *  is the tighter resource's use of its budget (the rail's bar) and tight names it; binding is what holds the head
   *  back; needPct is the head's larger need. A host that counts jobs sends only usedPct. */
  cpuPct?: number;
  memPct?: number;
  cpuBudgetPct?: number;
  memBudgetPct?: number;
  cpuFreePct?: number;
  memFreePct?: number;
  usedPct?: number;
  tight?: "cpu" | "memory";
  binding?: "cpu" | "memory";
  needPct?: number;
  /** V0.164: the threads that run heavy work without having asked, and their share of the host (already in cpuPct,
   *  memPct and usedPct) */
  undeclared?: UndeclaredUse[];
  undeclaredCpuPct?: number;
  undeclaredMemPct?: number;
}

// ---- V0.164: undeclared heavy work, the words the rail's tooltip and undeclared.ts share ----

/** The title and project of a thread, by id; "" project when it has none. */
export type Lookup = (id: string) => { title: string; project?: string };

/** "2.8 GB": bytes with one decimal, GB = 1e9. */
export const gbText = (b: number) => `${(b / 1e9).toFixed(1)} GB`;

/** The measures of a use that the front names: the one that is over (or at least a tenth of its threshold), both when
 *  the host says neither. Memory first. */
export function usedMeasures(u: Pick<UndeclaredUse, "overCpu" | "overMem">): { mem: boolean; cpu: boolean } {
  return u.overCpu || u.overMem ? { mem: u.overMem, cpu: u.overCpu } : { mem: true, cpu: true };
}

/** "50 s", "3 min", "1 h 5 min": how long a thread has run undeclared. */
export function spanText(sec: number): string {
  const s = Math.max(0, Math.round(sec));
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} min`;
  return m % 60 ? `${Math.floor(m / 60)} h ${m % 60} min` : `${Math.floor(m / 60)} h`;
}

/** The rail meter's tooltip line for the undeclared threads: names, projects, use and time. "" when none. */
export function undeclaredTip(h: HostLoad, lookup: Lookup, now: number): string {
  const list = h.slots?.undeclared ?? [];
  if (!list.length) return "";
  const parts = list.map((u) => {
    const m = usedMeasures(u);
    const what = [m.mem && gbText(u.mem), m.cpu && cpuText(u.cpu)].filter(Boolean).join(" and ");
    const l = lookup(u.thread);
    return `${l.title}${l.project ? ` (${l.project})` : ""}, ${what} for ${spanText((now - Date.parse(u.since)) / 1000)}`;
  });
  return `Undeclared: ${parts.join("; ")}`;
}

/** V0.163: `heavy` (the API's name) or an older host's `slots`, read as one field. */
export function normalizeLoad(list: HostLoad[]): HostLoad[] {
  return list.map((h) => (h.heavy && !h.slots ? { ...h, slots: h.heavy } : h));
}

/** A thread at or above this many cores is coloured. */
export const HOT_CORES = 2;
/** CPU pressure at or above this share colours the rail figure. */
export const HOT_PRESSURE = 40;

export interface LoadPart {
  text: string;
  hot: boolean;
}

const one = (n: number) => n.toFixed(1);
const gb = (b: number) => (b / 1e9).toFixed(1);

/** The rail's figure: the 1-minute load ("6.0", "11"); hot past the cores or at 40 % CPU pressure. */
export function railLoad(h: HostLoad): LoadPart {
  const l = h.load[0];
  return { text: l >= 10 ? String(Math.round(l)) : one(l), hot: l > h.cores || (h.cpuPressure ?? 0) >= HOT_PRESSURE };
}

/** V0.99: the rail ring's level. calm under 70 % of the cores, warm up to the cores, hot as railLoad. */
export type LoadLevel = "calm" | "warm" | "hot";
export const WARM_SHARE = 0.7;

export interface RailMeter {
  text: string;
  /** the 1-minute load over the cores, 0 to 1; a full ring past the cores */
  share: number;
  level: LoadLevel;
}

/** Decision 0029 S1: the slowdown that colours the ring warm, and hot. */
export const WARM_SLOWDOWN = 1.5;
export const HOT_SLOWDOWN = 3;

/** Decision 0029 S1: the ring's level from the slowdown and the memory stall level; null when the host measures
 *  neither (no provider), so the V0.99 rule holds. */
export function signalLevel(h: HostLoad): LoadLevel | null {
  if (h.slowdown === undefined && h.memoryStall === undefined) return null;
  const s = h.slowdown ?? 1;
  if (s >= HOT_SLOWDOWN || h.memoryStall === "critical") return "hot";
  if (s >= WARM_SLOWDOWN || h.memoryStall === "elevated") return "warm";
  return "calm";
}

/** The rail's ring: the figure of railLoad, the share of the cores it fills and its colour level (decision 0029:
 *  from the slowdown and the memory stall where the host measures them). */
export function railMeter(h: HostLoad): RailMeter {
  const f = railLoad(h);
  const share = h.cores > 0 ? Math.min(1, Math.max(0, h.load[0] / h.cores)) : 1;
  const level = signalLevel(h) ?? (f.hot ? "hot" : share >= WARM_SHARE ? "warm" : "calm");
  // V0.152: the figure is the load as a share of the cores, on the arc's scale, so hosts of any size read alike.
  return { text: h.cores > 0 ? `${Math.round(Math.max(0, h.load[0]) / h.cores * 100)}%` : f.text, share, level };
}

/** The rail's tooltip lines. */
export function loadDetail(h: HostLoad): string[] {
  const out = [`${h.host}: load ${h.load.map(one).join(" · ")} (1, 5, 15 min) on ${h.cores} cores`];
  if (h.slowdown !== undefined) out.push(`Slowdown ${one(h.slowdown)}× (a new job runs ${one(h.slowdown)}× slower than on an idle host)`);
  if (h.cpuPressure !== undefined) out.push(`CPU pressure ${Math.round(h.cpuPressure)} % (10 s)`);
  if (h.agentsCpuPressure !== undefined) out.push(`Agents' CPU pressure ${Math.round(h.agentsCpuPressure)} % (10 s)`);
  if (h.headroom !== undefined && h.capacity) {
    const stall = h.memoryStall ? ` · stall ${h.memoryStall}${h.memoryPressure !== undefined ? ` (${one(h.memoryPressure)} %)` : ""}` : "";
    out.push(`Memory ${Math.round(h.headroom * 100)} % free of ${gb(h.capacity.memory)} GB for agents${stall}`);
  }
  if (h.provider) out.push(`Signals: ${h.provider}`);
  const a = h.agents;
  if (!h.cgroups || !a) {
    out.push("Agents share the daemon's cgroup (no per-thread figures)");
    return out.concat(slotDetail(h));
  }
  const cpu = a.cpu === undefined ? "" : a.cpuMax ? `${one(a.cpu)} of ${a.cpuMax} cores` : `${one(a.cpu)} cores, no cap`;
  const mem = a.memoryMax ? `${gb(a.memory)} of ${gb(a.memoryMax)} GB` : `${gb(a.memory)} GB`;
  out.push(`Agents ${[cpu, mem].filter(Boolean).join(" · ")}`);
  return out.concat(slotDetail(h));
}

/** "0.4 cores", "1.0 core", "12 cores". */
export function cpuText(c: number): string {
  if (c >= 10) return `${Math.round(c)} cores`;
  const t = one(c);
  return `${t} ${t === "1.0" ? "core" : "cores"}`;
}

/** A thread's CPU from any host's load, or null before its second read. */
export function threadCPU(list: HostLoad[], id: string): LoadPart | null {
  for (const h of list) {
    const t = h.threads?.find((x) => x.id === id);
    if (t) return t.cpu === undefined ? null : { text: cpuText(t.cpu), hot: t.cpu >= HOT_CORES };
  }
  return null;
}

// SPEC 0.5 V0.86: the slots for heavy jobs.

/** V0.163: what heavy jobs use of the host, in words: CPU and memory in use and the budget; each running job's share;
 *  who waits and why. A host that counts jobs says that instead. */
export function heavyDetail(h: HostLoad): string[] {
  const s = h.slots;
  if (!s) return [];
  if (s.cpuPct === undefined || s.memPct === undefined) {
    const used = s.usedPct ?? Math.round((s.held.length / Math.max(1, s.n)) * 100);
    return [`Heavy jobs: ${used} % of this host's count in use (${s.held.length} of ${s.n} jobs; no load measures)${s.waiting.length ? `, ${s.waiting.length} waiting` : ""}`];
  }
  // V0.164: the undeclared share of each resource, inside the used one
  const ucpu = s.undeclaredCpuPct ? ` (undeclared ${s.undeclaredCpuPct} %)` : "";
  const umem = s.undeclaredMemPct ? ` (undeclared ${s.undeclaredMemPct} %)` : "";
  const out = [`Heavy jobs: CPU ${s.cpuPct} %${ucpu} · memory ${s.memPct} %${umem} of this host in use; the budget is CPU ${s.cpuBudgetPct ?? 100} % · memory ${s.memBudgetPct ?? 100} %${s.budgetWhy ? ` (${s.budgetWhy})` : ""}`];
  if (s.held.length) out.push(`Running: ${s.held.map((x) => `${x.kind} ≈ ${x.cpuPct ?? x.pct ?? 0} % CPU · ${x.memPct ?? 0} % memory`).join("; ")}`);
  if (s.waiting.length) out.push(`${s.waiting.length} waiting${s.why ? `; the next ${s.why}` : ""}`);
  return out;
}

function slotDetail(h: HostLoad): string[] {
  return heavyDetail(h);
}

export interface RailBudget {
  text: string;
  /** V0.152: "full", "1 waits" or "" */
  note: string;
  /** used over the budget, 0 to 1; with undeclared work (V0.164) the admitted part of it */
  share: number;
  /** V0.164: the undeclared part, drawn hatched after `share`; absent without undeclared work */
  und?: number;
  hot: boolean;
  waiting: number;
}

/** Decision 0029 S2: the rail's slot budget in use as a bar ("1.1/2"), hot while anyone waits; null without
 *  admission by a budget (then the pips show). */
export function railBudget(h: HostLoad): RailBudget | null {
  const s = h.slots;
  if (!s) return null;
  // V0.163: the tighter resource's use of its budget, in percent; a host that counts jobs: its share of the count.
  const used = s.usedPct ?? Math.round((s.held.length / Math.max(1, s.n)) * 100);
  const waiting = s.waiting.length;
  const note = waiting ? `${waiting} ${waiting === 1 ? "waits" : "wait"}` : used >= 99.5 ? "full" : "";
  const whole = Math.round(Math.min(1, Math.max(0, used / 100)) * 100) / 100;
  // V0.164: the tighter resource's undeclared share is the hatched part of the same bar.
  const u = s.tight === "memory" ? s.undeclaredMemPct : s.undeclaredCpuPct;
  const und = u ? Math.min(whole, Math.round(Math.max(0, u)) / 100) : 0;
  return { text: `${Math.round(used)} %`, note, share: und ? Math.round((whole - und) * 100) / 100 : whole, ...(und ? { und } : {}), hot: waiting > 0, waiting };
}

/** V0.152, V0.163: the heavy-job meter's own tooltip: what it is, CPU and memory in use, each job's share, who waits
 *  and why. */
export function slotTip(h: HostLoad, names?: { lookup: Lookup; now: number }): string {
  const out = heavyDetail(h);
  const und = names ? undeclaredTip(h, names.lookup, names.now) : "";
  return (und ? [...out, und] : out).join("\n");
}

/** V0.163: Settings → Heavy jobs: the budget as percentages of what the host gives its agents. */
export function budgetLine(h: HostLoad): string {
  const s = h.slots;
  if (!s || s.cpuBudgetPct === undefined) return "";
  const cap = h.capacity ? ` (${h.capacity.cpu} cores, ${gb(h.capacity.memory)} GB)` : "";
  return `CPU ${s.cpuBudgetPct} % · memory ${s.memBudgetPct ?? 100} % of what this host gives its agents${cap}`;
}

/** V0.152: a short distinct name per host for the rail: the tokens all names share at the start or the end go
 *  ("meros-laptop", "meros-server" → "laptop", "server"); the full name if that leaves one empty or two alike. */
export function shortHostNames(names: string[]): Map<string, string> {
  const out = new Map(names.map((n) => [n, n]));
  if (names.length < 2) return out;
  const toks = names.map((n) => n.split(/(?<=[-_.])/));
  const strip = (t: string) => t.replace(/[-_.]$/, "");
  let a = 0;
  while (toks.every((t) => t.length > a + 1 && strip(t[a]) === strip(toks[0][a]))) a++;
  let b = 0;
  while (toks.every((t) => t.length > a + b + 1 && strip(t[t.length - 1 - b]) === strip(toks[0][toks[0].length - 1 - b]))) b++;
  const short = toks.map((t) => strip(t.slice(a, t.length - b).join("")));
  if (short.some((x) => !x) || new Set(short).size !== short.length) return out;
  names.forEach((n, i) => out.set(n, short[i]));
  return out;
}


/** Decision 0029 S1: the job a thread's slot runs, "make test · 3.2 GB", once the gate hook saw its command; null
 *  otherwise. The row shows it first on its line, since the line is cut at its end. */
export function threadJob(list: HostLoad[], id: string): string | null {
  for (const h of list) {
    const held = h.slots?.held.find((x) => x.owner === id);
    if (!held) continue;
    const t = h.threads?.find((x) => x.id === id);
    const share = held.pct !== undefined ? ` ≈ ${held.pct} %` : "";
    // Decision 0029 S5: a frozen job says so first, whether or not its command is known.
    if (t?.paused) return `${t.job ?? `${held.kind} job`} · paused: memory`;
    return t?.job ? `${t.job}${share}` : null;
  }
  return null;
}

/** A thread's heavy job (V0.163, in percent of the host): "build job ≈ 46 %" while admitted; the queue's head says why it
 *  waits ("waiting: needs ≈ 45 % CPU, 40 % free"), the others their place and need; null when it has none. */
export function threadSlot(list: HostLoad[], id: string): string | null {
  for (const h of list) {
    const s = h.slots;
    if (!s) continue;
    const held = s.held.find((x) => x.owner === id);
    if (held) {
      const t = h.threads?.find((x) => x.id === id);
      const share = held.pct !== undefined ? ` ≈ ${held.pct} %` : "";
      return t?.job ? `${t.job}${share}` : `${held.kind} job${share}`;
    }
    const i = s.waiting.findIndex((x) => x.owner === id);
    if (i === 0 && s.why) return `waiting: ${s.why}`;
    if (i >= 0) return `waiting (${i} ahead)${s.waiting[i].pct !== undefined ? ` · needs ≈ ${s.waiting[i].pct} %` : ""}`;
  }
  return null;
}

/** V0.99, V0.140: a ring's label (its title and aria-label): the load, the cores and the slots; with several hosts it
 *  starts with the host's name. */
export function railLabel(h: HostLoad, several: boolean): string {
  const m = railMeter(h);
  const b = railBudget(h);
  const of = h.slots?.tight ? ` of their ${h.slots.tight === "memory" ? "memory" : "CPU"} budget` : " of their budget";
  const heavy = b ? `, heavy jobs use ${b.text}${of}${b.waiting ? `, ${b.waiting} waiting` : ""}` : "";
  const s = `oad ${railLoad(h).text} on ${h.cores} cores${h.cores > 0 ? ` (${m.text} of the cores)` : ""}${heavy}`;
  return several ? `${h.host}: l${s}` : `L${s}`;
}

const duration = (sec: number) => {
  const m = Math.floor(sec / 60);
  const r = Math.round(sec % 60);
  return m ? `${m} min ${r} s` : `${r} s`;
};

/** Decision 0029 S1: Settings → Automation's line per measured job class: its runs, the cores its newest run used
 *  (and would have used when starved), its memory peak, and its typical wall time. */
export function jobLines(h: HostLoad): { class: string; text: string; cost: string; learned: boolean }[] {
  return (h.jobs ?? []).map((j) => {
    const cores = cpuText(j.cores) + (j.demand >= j.cores + 0.05 ? `, would use ${one(j.demand)}` : "");
    // Decision 0029 S3, V0.163: the learned need, CPU and memory as percentages of this host; under three runs a class
    // still counts as at least the prior.
    const n = j.learned ?? 0;
    const need = j.cpuPct !== undefined ? `≈ ${j.cpuPct} % CPU · ${j.memPct ?? 0} % memory of this host` : j.cost !== undefined ? `cost ${one(j.cost)}` : "";
    const cost = !need || !n ? "" : n >= 3 ? `${need} · learned from ${n} runs` : `${need} · ${n} of 3 runs to learn`;
    return { class: j.class, text: `${j.runs} ${j.runs === 1 ? "run" : "runs"} · ${cores} · peak ${gb(j.peak)} GB · ${duration(j.wall)}`, cost, learned: n > 0 };
  });
}

/** V0.166: one host of the phone's load strip and sheet: its name, its load when it reports one, and since when it is
 *  offline. */
export interface StripHost {
  name: string;
  load?: HostLoad;
  /** offline since (RFC 3339, "" when unknown); absent while online */
  offline?: string;
}

/** V0.166: the hosts the phone shows load for, in the owner's host order (the front's `hosts`): each online host that
 *  reports a load, and each offline host (its load gone). A host that reports no load while online (older than V0.80)
 *  is left out. Without a host list (a front older than V0.54), the load's own order. */
export function stripHosts(load: HostLoad[], hosts: { name: string; online: boolean; since?: string }[]): StripHost[] {
  const by = new Map(load.map((h) => [h.host, h]));
  if (!hosts.length) return load.map((h) => ({ name: h.host, load: h }));
  const out: StripHost[] = [];
  for (const h of hosts) {
    if (!h.online) out.push({ name: h.name, offline: h.since ?? "" });
    else if (by.has(h.name)) out.push({ name: h.name, load: by.get(h.name) });
  }
  // a load from a host the list does not name (a front that names its own host otherwise) still shows
  for (const h of load) if (!hosts.some((x) => x.name === h.host)) out.push({ name: h.host, load: h });
  return out;
}

/** V0.166: the strip shows this many hosts; the rest are "+n" and in the sheet. */
export const STRIP_MAX = 2;
