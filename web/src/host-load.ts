// SPEC 0.5 V0.80: the machine load (rail) and each working thread's CPU (rows), as words. Pure.
import type { HostId } from "./hostid.ts";
import type { Guard, Invariants } from "./guard.ts";
import type { Leftover, Temp } from "./leftovers.ts";
import type { Card, CardRow } from "./card.ts";

interface ThreadLoad {
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
  /** V0.services-not-jobs: its running services (a dev stack, a dev server, a compose project's containers) */
  services?: ThreadService[];
}
/** V0.services-not-jobs: one running service of a thread. memory is what it holds in the thread's cgroup, containers
 *  what its compose project's containers hold outside it, reserve what admission counts for it. */
export interface ThreadService {
  name: string;
  memory: number;
  project?: string;
  containers?: number;
  reserve: number;
}
/** V0.services-not-jobs: a service pattern of the host; a built-in one can be removed and added back. */
interface ServicePattern {
  pattern: string;
  builtIn?: boolean;
}
/** Decision 0029 S1: one measured job class (its newest run's measures, the median wall time of its last ten). */
interface JobClass {
  class: string;
  runs: number;
  cores: number;
  demand: number;
  peak: number;
  /** seconds */
  wall: number;
  at: string;
  /** decision 0029 S3: how many runs the class is learned from */
  learned?: number;
  /** V0.163: the class's need as percentages of this host */
  cpuPct?: number;
  memPct?: number;
}
export interface HostLoad {
  host: string;
  /** Slice 2b: the install id of the host it was read from (hostfeed.ts `stampLoad`): a command about it names this */
  hostId?: HostId;
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
  /** Decision 0029 S1 (#185), absent without a provider. */
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
  /** V0.164, V0.209: the thresholds of undeclared heavy work and the heavy commands Fyr learned (or the owner added
   *  before V0.209); absent on a host without V0.164 */
  undeclaredSettings?: UndeclaredSettings;
  heavyAdded?: HeavyAdded[];
  /** V0.rerun-classes: the classes the owner marked rerunnable; absent on an older host */
  rerun?: { classes: string[] };
  /** V0.oomd-guard: the unit's own memory stall (full avg10, %) and oomd's pressure limit on it (%) */
  unitMemoryStall?: number;
  oomdLimit?: number;
  /** V0.176: the processes threads left running, past their grace, and the settled threads' worktrees */
  leftovers?: Leftover[];
  temps?: Temp[];
  /** V0.services-not-jobs: the commands this host treats as services */
  servicePatterns?: ServicePattern[];
  /** V0.thread-invariants: invariants view fallback when slots are off */
  invariants?: Invariants;
}
/** V0.164: when a thread counts as heavy: shares of what the host gives its agents, and for how many seconds. */
interface UndeclaredSettings {
  on: boolean;
  cpu: number;
  memory: number;
  after: number;
}
interface HeavyAdded {
  pattern: string;
  kind: string;
  at: string;
  /** how often the gate refused it since; a learned command is never refused */
  refused: number;
  /** V0.209: Fyr learned it from the work it saw */
  learned?: boolean;
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
  paused: boolean;
  queued: boolean;
  overCpu: boolean;
  overMem: boolean;
}
/** One owner of a slot or a place in the queue: a thread id, or "self-deploy". */
interface SlotHolder {
  owner: string;
  /** "test", "build" or "e2e" */
  kind: string;
  since?: string;
  priority?: boolean;
  /** V0.163: the job's need as percentages of the host, CPU and memory, and pct the larger */
  cpuPct?: number;
  memPct?: number;
  pct?: number;
}
export interface Slots {
  n: number;
  held: SlotHolder[];
  waiting: SlotHolder[];
  /** why the head waits */
  why?: string;
  /** decision 0029 S4: why the budget moved (absent while it is full) */
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
  /** decision 0032: what the memory guard did lately */
  guard?: Guard;
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
  return list.map((h) => {
    const slots = h.slots ?? h.heavy;
    return slots ? { ...h, slots: withLists(slots) } : h;
  });
}

/** The host encodes an empty list as null (a Go nil slice), so the two lists of a pool may arrive as null: read them as empty. */
export function withLists(s: Slots): Slots {
  return { ...s, held: s.held ?? [], waiting: s.waiting ?? [] };
}

/** A thread at or above this many cores is coloured. */
const HOT_CORES = 2;
/** CPU pressure at or above this share colours the rail figure. */
const HOT_PRESSURE = 40;

interface LoadPart {
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
type LoadLevel = "calm" | "warm" | "hot";
const WARM_SHARE = 0.7;

interface RailMeter {
  text: string;
  /** the 1-minute load over the cores, 0 to 1; a full ring past the cores */
  share: number;
  level: LoadLevel;
}

/** Decision 0029 S1: the slowdown that colours the ring warm, and hot. */
const WARM_SLOWDOWN = 1.5;
const HOT_SLOWDOWN = 3;

/** Decision 0029 S1: the ring's level from the slowdown and the memory stall level; null when the host measures
 *  neither (no provider), so the V0.99 rule holds. */
function signalLevel(h: HostLoad): LoadLevel | null {
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

interface RailBudget {
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

/** Decision 0029 S2, V0.163: the rail's heavy jobs in use as a bar ("55 %" of their budget), hot while anyone waits;
 *  null on a host without heavy jobs. */
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

/** V0.163: Settings → Heavy jobs: the budget as percentages of what the host gives its agents. */
export function budgetLine(h: HostLoad): string {
  const s = h.slots;
  if (!s || s.cpuBudgetPct === undefined) return "";
  const cap = h.capacity ? ` (${h.capacity.cpu} cores, ${gb(h.capacity.memory)} GB)` : "";
  return `CPU ${s.cpuBudgetPct} % · memory ${s.memBudgetPct ?? 100} % of what this host gives its agents${cap}`;
}

/** V0.152: a short distinct name per host for the rail: the tokens all names share at the start or the end go
 *  ("home-laptop", "home-server" → "laptop", "server"); the full name if that leaves one empty or two alike. */
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


/** V0.admission-tidy: a holder that also waits, to grow its slot for a heavier command, is in both of the host's lists;
 *  its one row says so after its job: " · waiting to grow: needs ≈ 8.6 GB memory, 2.0 GB free" at the queue's head,
 *  " · waiting to grow (1 ahead)" behind it; "" when it does not wait. */
function growNote(s: Slots, id: string): string {
  const i = s.waiting.findIndex((x) => x.owner === id);
  if (i < 0) return "";
  return i === 0 && s.why ? ` · waiting to grow: ${s.why}` : ` · waiting to grow (${i} ahead)`;
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
    return t?.job ? `${t.job}${share}${growNote(h.slots!, id)}` : null;
  }
  return null;
}

/** V0.services-not-jobs: a thread's running services on its row, "service · 3.2 GB" or "2 services · 5.1 GB" (what
 *  they hold now, containers included); null when it runs none. */
export function threadService(list: HostLoad[], id: string): string | null {
  for (const h of list) {
    const s = h.threads?.find((x) => x.id === id)?.services;
    if (!s?.length) continue;
    const mem = s.reduce((n, x) => n + x.memory + (x.containers ?? 0), 0);
    return `${s.length === 1 ? "service" : `${s.length} services`} · ${gbText(mem)}`;
  }
  return null;
}

/** A thread's heavy job (V0.163, in percent of the host): "build job ≈ 46 %" while admitted; the queue's head says why it
 *  waits ("waiting: needs ≈ 45 % CPU, 40 % free"), the others their place and need; a holder that also waits says
 *  "waiting to grow" after its job (V0.admission-tidy); null when it has none. */
export function threadSlot(list: HostLoad[], id: string): string | null {
  for (const h of list) {
    const s = h.slots;
    if (!s) continue;
    const held = s.held.find((x) => x.owner === id);
    const i = s.waiting.findIndex((x) => x.owner === id);
    if (held) {
      const t = h.threads?.find((x) => x.id === id);
      const share = held.pct !== undefined ? ` ≈ ${held.pct} %` : "";
      return (t?.job ? `${t.job}${share}` : `${held.kind} job${share}`) + growNote(s, id);
    }
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
    const need = j.cpuPct !== undefined ? `≈ ${j.cpuPct} % CPU · ${j.memPct ?? 0} % memory of this host` : "";
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

// ---- V0.load-card: the host's load as a card (the rail's hover card, the phone's sheet) ----

export type { CardRow, CardSection } from "./card.ts";
export { cardLines } from "./card.ts";
export interface LoadCard extends Card {
  host: string;
  level: LoadLevel;
  /** "Calm", "Busy" or "Overloaded" */
  verdict: string;
  /** one sentence: what the level means for new work */
  summary: string;
}

/** V0.oomd-guard: the guard warns when the stall comes within this many points of oomd's limit (OomdNearMargin). */
const OOMD_NEAR = 10;
const VERDICT: Record<LoadLevel, string> = { calm: "Calm", warm: "Busy", hot: "Overloaded" };
const pct = (n: number) => `${Math.round(n)} %`;

/** V0.load-card: one host's load in plain words, grouped: a verdict and what it means, the machine, the heavy jobs.
 *  Every figure of the old tooltip lines is here; the measure names (PSI, load average) give way to what they mean. */
export function loadCard(h: HostLoad, names?: { lookup: Lookup; now: number }): LoadCard {
  const m = railMeter(h);
  const out: LoadCard = { host: h.host, head: h.host, headMono: true, level: m.level, verdict: VERDICT[m.level], summary: summary(h, m), sections: [] };
  if (h.provider) out.source = `Measured by ${h.provider}`;

  const mach: CardRow[] = [];
  mach.push({
    label: "Load",
    value: h.cores > 0 ? `${m.text.replace("%", " %")} of ${h.cores} cores` : `${one(h.load[0])} on ${h.cores} cores`,
    note: `${loadNum(h.load[0])} now · ${loadNum(h.load[1])} over 5 min · ${loadNum(h.load[2])} over 15 min`,
    hot: railLoad(h).hot,
  });
  if (h.slowdown !== undefined) mach.push({ label: "Speed", value: h.slowdown < 1.05 ? "full speed" : `${one(h.slowdown)}× slower than idle`, note: "for a new job", hot: h.slowdown >= HOT_SLOWDOWN });
  if (h.cpuPressure !== undefined || h.agentsCpuPressure !== undefined) {
    const p = h.cpuPressure ?? h.agentsCpuPressure!;
    mach.push({
      label: "CPU waits",
      value: `${pct(p)} of the time`,
      note: h.cpuPressure !== undefined && h.agentsCpuPressure !== undefined ? `agents ${pct(h.agentsCpuPressure)}` : h.cpuPressure === undefined ? "agents" : undefined,
      hot: p >= HOT_PRESSURE,
    });
  }
  if (h.headroom !== undefined && h.capacity) {
    const st = h.memoryStall;
    const stall = st === undefined ? undefined : st === "ok" ? "no stalls" : `${st === "critical" ? "stalling hard" : "stalling"}${h.memoryPressure !== undefined ? ` ${one(h.memoryPressure)} %` : ""}`;
    mach.push({ label: "Memory", value: `${pct(h.headroom * 100)} free of ${gb(h.capacity.memory)} GB`, note: stall, hot: st === "critical" });
  }
  // V0.load-cleanup: oomd's rule on the agents (V0.oomd-guard) and the stall it compares; hot within OOMD_NEAR of it,
  // where the guard warns. Absent without a rule.
  if (h.oomdLimit) {
    const st = h.unitMemoryStall;
    mach.push({ label: "oomd", value: `kills at ${pct(h.oomdLimit)} stall`, note: st === undefined ? undefined : `now ${pct(st)}`, hot: st !== undefined && st >= h.oomdLimit - OOMD_NEAR });
  }
  const a = h.agents;
  if (!a) mach.push({ label: "Agents", value: "share the daemon's cgroup", note: "no per-thread figures" });
  else {
    const cpu = a.cpu === undefined ? "" : a.cpuMax ? `${one(a.cpu)} of ${a.cpuMax} cores` : `${one(a.cpu)} cores`;
    const mem = a.memoryMax ? `${gb(a.memory)} of ${gb(a.memoryMax)} GB` : `${gb(a.memory)} GB`;
    // V0.process-tree-hosts: without cgroups the figures are each thread's process tree.
    const note = [a.cpu !== undefined && !a.cpuMax && "no cap", !h.cgroups && "process trees"].filter(Boolean).join(" · ");
    mach.push({ label: "Agents use", value: [cpu, mem].filter(Boolean).join(" · "), note: note || undefined });
  }
  out.sections.push({ title: "Machine", rows: mach });

  const heavy = heavyRows(h, names);
  if (heavy.length) out.sections.push({ title: "Heavy jobs", rows: heavy });
  return out;
}

const loadNum = (l: number) => (l >= 10 ? String(Math.round(l)) : one(l));

function summary(h: HostLoad, m: RailMeter): string {
  const mem = h.memoryStall === "critical" ? " Memory is short." : h.memoryStall === "elevated" ? " Memory is getting tight." : "";
  if (h.slowdown !== undefined) return (h.slowdown < 1.05 ? "New jobs run at full speed." : `New jobs run ${one(h.slowdown)}× slower than on an idle machine.`) + mem;
  return (h.cores > 0 ? `The load is ${m.text.replace("%", " %")} of the cores.` : "") + mem;
}

const cap = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

/** The heavy jobs: CPU and memory in use against their budget (V0.163; V0.164's undeclared share), why the budget moved,
 *  each running job, who waits and why, and the threads that run heavy work undeclared. A host that counts jobs says that. */
function heavyRows(h: HostLoad, names?: { lookup: Lookup; now: number }): CardRow[] {
  const s = h.slots;
  if (!s) return [];
  const rows: CardRow[] = [];
  if (s.cpuPct === undefined || s.memPct === undefined) {
    rows.push({ label: "In use", value: `${s.held.length} of ${s.n} jobs`, note: "counted, not measured" });
  } else {
    const res = (label: string, used: number, budget: number | undefined, und: number | undefined): CardRow => ({
      label,
      value: `${used} % of the host`,
      note: [`budget ${budget ?? 100} %`, und ? `${und} % undeclared` : ""].filter(Boolean).join(" · "),
      hot: used > (budget ?? 100),
    });
    rows.push(res("CPU", s.cpuPct, s.cpuBudgetPct, s.undeclaredCpuPct));
    rows.push(res("Memory", s.memPct, s.memBudgetPct, s.undeclaredMemPct));
    if (s.budgetWhy) rows.push({ label: "Budget", value: cap(s.budgetWhy) });
  }
  s.held.forEach((x, i) =>
    rows.push({ label: i ? "" : "Running", value: x.kind, note: x.cpuPct !== undefined || x.memPct !== undefined ? `≈ ${x.cpuPct ?? x.pct ?? 0} % CPU · ${x.memPct ?? 0} % memory` : x.pct !== undefined ? `≈ ${x.pct} %` : undefined }),
  );
  if (s.waiting.length) rows.push({ label: "Waiting", value: `${s.waiting.length} ${s.waiting.length === 1 ? "job" : "jobs"}`, note: s.why ? `the next ${s.why}` : undefined, hot: true });
  if (names)
    (s.undeclared ?? []).forEach((u, i) => {
      const mm = usedMeasures(u);
      const l = names.lookup(u.thread);
      const what = [mm.mem && gbText(u.mem), mm.cpu && cpuText(u.cpu)].filter(Boolean).join(" · ");
      rows.push({ label: i ? "" : "Undeclared", value: `${l.title}${l.project ? ` (${l.project})` : ""}`, note: `${what} for ${spanText((names.now - Date.parse(u.since)) / 1000)}` });
    });
  return rows;
}
