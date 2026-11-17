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
  /** the host's slots for heavy jobs (V0.86) */
  slots?: Slots;
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
}
/** One owner of a slot or a place in the queue: a thread id, or "self-deploy". */
export interface SlotHolder {
  owner: string;
  /** "test", "build" or "e2e" */
  kind: string;
  since?: string;
  priority?: boolean;
  /** decision 0029 S2: the holder's share of the budget (1.0 a job, 0.1 an idle hold) */
  cost?: number;
}
export interface Slots {
  n: number;
  held: SlotHolder[];
  waiting: SlotHolder[];
  /** decision 0029 S2: the holders' costs summed (absent without admission by a budget), and why the head waits */
  used?: number;
  why?: string;
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

function slotDetail(h: HostLoad): string[] {
  const s = h.slots;
  if (!s) return [];
  if (s.used === undefined) return [`Slots: ${s.held.length} of ${s.n} in use, ${s.waiting.length} waiting`];
  // Decision 0029 S2: the budget in use, each holder's cost, and why the next job waits.
  const costs = s.held.map((x) => `${x.kind} ${one(x.cost ?? 1)}${(x.cost ?? 1) < 0.2 ? " idle" : ""}`).join(", ");
  const why = s.waiting.length && s.why ? `; the next waits: ${s.why}` : "";
  return [`Slots: ${one(s.used)} of ${s.n} in use${costs ? ` (${costs})` : ""}, ${s.waiting.length} waiting${why}`];
}

export interface RailBudget {
  text: string;
  /** V0.152: "full", "1 waits" or "" */
  note: string;
  /** used over the budget, 0 to 1 */
  share: number;
  hot: boolean;
  waiting: number;
}

/** Decision 0029 S2: the rail's slot budget in use as a bar ("1.1/2"), hot while anyone waits; null without
 *  admission by a budget (then the pips show). */
export function railBudget(h: HostLoad): RailBudget | null {
  const s = h.slots;
  if (!s || s.used === undefined) return null;
  const share = s.n > 0 ? Math.min(1, Math.max(0, s.used / s.n)) : 1;
  const waiting = s.waiting.length;
  // V0.152: a word under the bar only when it matters; red (hot) only while a job is held back.
  const note = waiting ? `${waiting} ${waiting === 1 ? "waits" : "wait"}` : s.used >= s.n - 0.05 ? "full" : "";
  return { text: `${one(s.used)}/${s.n}`, note, share: Math.round(share * 100) / 100, hot: waiting > 0, waiting };
}

/** V0.152: the slot meter's own tooltip: what it is, the budget in use, each holder's cost, who waits and why. */
export function slotTip(h: HostLoad): string {
  const d = slotDetail(h)[0];
  return d ? d.replace(/^Slots: /, "Slots for heavy jobs (tests, builds, e2e runs): ") : "";
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

/** The rail's slot figure, "1/2 slots"; hot while anyone waits. Null without slots. */
export function railSlots(h: HostLoad): LoadPart | null {
  const s = h.slots;
  if (!s) return null;
  return { text: `${s.held.length}/${s.n} slots`, hot: s.waiting.length > 0 };
}

/** Above this many slots the rail writes "3/8" in place of the pips. */
export const MAX_PIPS = 6;

export interface RailPips {
  n: number;
  held: number;
  waiting: number;
}

/** The rail's slot pips (V0.99): n pips, the first `held` filled; null without slots. */
export function railPips(h: HostLoad): RailPips | null {
  const s = h.slots;
  if (!s || s.used !== undefined) return null;
  return { n: s.n, held: Math.min(s.held.length, s.n), waiting: s.waiting.length };
}

const withArticle = (kind: string) => (/^[aeiou]/.test(kind) ? `an ${kind}` : `a ${kind}`);

/** Decision 0029 S1: the job a thread's slot runs, "make test · 3.2 GB", once the gate hook saw its command; null
 *  otherwise. The row shows it first on its line, since the line is cut at its end. */
export function threadJob(list: HostLoad[], id: string): string | null {
  for (const h of list) {
    if (!h.slots?.held.some((x) => x.owner === id)) continue;
    const t = h.threads?.find((x) => x.id === id);
    return t?.job ? `${t.job} · ${gb(t.memory)} GB` : null;
  }
  return null;
}

/** A thread's slot: "waiting for a test slot (2 ahead)" or "test slot"; null when it has none. */
export function threadSlot(list: HostLoad[], id: string): string | null {
  for (const h of list) {
    const s = h.slots;
    if (!s) continue;
    const held = s.held.find((x) => x.owner === id);
    if (held) {
      // Decision 0029 S1: the job the slot runs, by the heavy command the thread ran, and its memory now.
      const t = h.threads?.find((x) => x.id === id);
      return t?.job ? `${t.job} · ${gb(t.memory)} GB` : `${held.kind} slot`;
    }
    const i = s.waiting.findIndex((x) => x.owner === id);
    if (i >= 0) return `waiting for ${withArticle(s.waiting[i].kind)} slot (${i} ahead)${i === 0 && s.why ? `: ${s.why}` : ""}`;
  }
  return null;
}

/** V0.99, V0.140: a ring's label (its title and aria-label): the load, the cores and the slots; with several hosts it
 *  starts with the host's name. */
export function railLabel(h: HostLoad, several: boolean): string {
  const m = railMeter(h);
  const p = railPips(h);
  const b = railBudget(h);
  const slots = b ? `, ${one(h.slots!.used!)} of ${h.slots!.n} slot budget in use${b.waiting ? `, ${b.waiting} waiting` : ""}` : p ? `, ${p.held} of ${p.n} slots in use${p.waiting ? `, ${p.waiting} waiting` : ""}` : "";
  const s = `oad ${railLoad(h).text} on ${h.cores} cores${h.cores > 0 ? ` (${m.text} of the cores)` : ""}${slots}`;
  return several ? `${h.host}: l${s}` : `L${s}`;
}

const duration = (sec: number) => {
  const m = Math.floor(sec / 60);
  const r = Math.round(sec % 60);
  return m ? `${m} min ${r} s` : `${r} s`;
};

/** Decision 0029 S1: Settings → Automation's line per measured job class: its runs, the cores its newest run used
 *  (and would have used when starved), its memory peak, and its typical wall time. */
export function jobLines(h: HostLoad): { class: string; text: string }[] {
  return (h.jobs ?? []).map((j) => {
    const cores = cpuText(j.cores) + (j.demand >= j.cores + 0.05 ? `, would use ${one(j.demand)}` : "");
    return { class: j.class, text: `${j.runs} ${j.runs === 1 ? "run" : "runs"} · ${cores} · peak ${gb(j.peak)} GB · ${duration(j.wall)}` };
  });
}
