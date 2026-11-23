// V0.state-index: how a device brings one host's stored threads up to date. The host lists every thread (id, rev,
// attention state); the device compares that with what it holds. The stored threads are the progress: each carries its
// own rev, so a sync that is cut off is simply resumed by the next diff, and no clock, "since" or cursor can drift.

import type { StateDTO } from "./types.ts";

export interface IndexEntry {
  id: string;
  rev: number;
  state?: string;
}

type Stored = StateDTO["threads"];

/** The threads asked first: what waits for the owner, then what is moving, then the rest by how recently it changed. */
const RANK: Record<string, number> = { asking: 0, working: 1, done: 2, waiting: 2, snoozed: 4, seen: 4, settled: 5 };
const rankOf = (state: string | undefined): number => (state !== undefined && state in RANK ? RANK[state] : 3);

export interface SyncPlan {
  /** The stored threads the host still has: what the list shows at once. A thread the host dropped is not here. */
  kept: Stored;
  /** Ids to fetch, most urgent first: the host's thread is missing here or has another rev than the stored copy. */
  want: string[];
  /** How many stored threads the host no longer has. */
  gone: number;
}

export function planSync(stored: StateDTO | null, index: IndexEntry[]): SyncPlan {
  const have = new Map<string, Stored[number]>();
  for (const t of stored?.threads ?? []) have.set(t.id, t);
  const onHost = new Set(index.map((e) => e.id));
  const kept = [...have.values()].filter((t) => onHost.has(t.id));
  // `!==`, not `<`: the host is the truth, also when its clock went back (a restored database).
  const stale = index.filter((e) => have.get(e.id)?.rev !== e.rev);
  stale.sort((a, b) => rankOf(a.state) - rankOf(b.state) || b.rev - a.rev);
  return { kept, want: stale.map((e) => e.id), gone: have.size - kept.length };
}

export function chunks<T>(xs: T[], n: number): T[][] {
  const out: T[][] = [];
  for (let i = 0; i < xs.length; i += n) out.push(xs.slice(i, i + n));
  return out;
}
