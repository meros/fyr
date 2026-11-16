// SPEC 0.5 V0.80: the machine load (rail) and each working thread's CPU (rows), as words. Pure.

export interface ThreadLoad {
  /** a thread id, or "self-deploy" */
  id: string;
  /** cores over the last read; absent on a host's first read */
  cpu?: number;
  memory: number;
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
}
/** One owner of a slot or a place in the queue: a thread id, or "self-deploy". */
export interface SlotHolder {
  owner: string;
  /** "test", "build" or "e2e" */
  kind: string;
  since?: string;
  priority?: boolean;
}
export interface Slots {
  n: number;
  held: SlotHolder[];
  waiting: SlotHolder[];
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

/** The rail's ring: the figure of railLoad, the share of the cores it fills and its colour level. */
export function railMeter(h: HostLoad): RailMeter {
  const f = railLoad(h);
  const share = h.cores > 0 ? Math.min(1, Math.max(0, h.load[0] / h.cores)) : 1;
  return { text: f.text, share, level: f.hot ? "hot" : share >= WARM_SHARE ? "warm" : "calm" };
}

/** The rail's tooltip lines. */
export function loadDetail(h: HostLoad): string[] {
  const out = [`${h.host}: load ${h.load.map(one).join(" · ")} (1, 5, 15 min) on ${h.cores} cores`];
  if (h.cpuPressure !== undefined) out.push(`CPU pressure ${Math.round(h.cpuPressure)} % (10 s)`);
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
  return s ? [`Slots: ${s.held.length} of ${s.n} in use, ${s.waiting.length} waiting`] : [];
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
  if (!s) return null;
  return { n: s.n, held: Math.min(s.held.length, s.n), waiting: s.waiting.length };
}

const withArticle = (kind: string) => (/^[aeiou]/.test(kind) ? `an ${kind}` : `a ${kind}`);

/** A thread's slot: "waiting for a test slot (2 ahead)" or "test slot"; null when it has none. */
export function threadSlot(list: HostLoad[], id: string): string | null {
  for (const h of list) {
    const s = h.slots;
    if (!s) continue;
    const held = s.held.find((x) => x.owner === id);
    if (held) return `${held.kind} slot`;
    const i = s.waiting.findIndex((x) => x.owner === id);
    if (i >= 0) return `waiting for ${withArticle(s.waiting[i].kind)} slot (${i} ahead)`;
  }
  return null;
}
