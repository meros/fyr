// SPEC 0.5 V0.80: the machine load (rail) and each working thread's CPU (rows), as words. Pure.

export interface ThreadLoad {
  /** a thread id, or "deploy" */
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

/** The rail's tooltip lines. */
export function loadDetail(h: HostLoad): string[] {
  const out = [`${h.host}: load ${h.load.map(one).join(" · ")} (1, 5, 15 min) on ${h.cores} cores`];
  if (h.cpuPressure !== undefined) out.push(`CPU pressure ${Math.round(h.cpuPressure)} % (10 s)`);
  const a = h.agents;
  if (!h.cgroups || !a) {
    out.push("Agents share the daemon's cgroup (no per-thread figures)");
    return out;
  }
  const cpu = a.cpu === undefined ? "" : a.cpuMax ? `${one(a.cpu)} of ${a.cpuMax} cores` : `${one(a.cpu)} cores, no cap`;
  const mem = a.memoryMax ? `${gb(a.memory)} of ${gb(a.memoryMax)} GB` : `${gb(a.memory)} GB`;
  out.push(`Agents ${[cpu, mem].filter(Boolean).join(" · ")}`);
  return out;
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
