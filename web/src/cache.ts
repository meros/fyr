// SPEC 0.5 V0.38: the last state and each opened thread's events stay on the
// device (IndexedDB). At start the app draws the stored state at once and asks
// the host only for threads changed since; opening a thread shows the stored
// events and fetches only newer ones. Without IndexedDB everything still
// works from the network.
import type { FyrEvent, StateDTO } from "./types.ts";

type Partial = StateDTO & { partial?: boolean };

/** Threads in a partial state replace stored ones by id; a full state replaces everything. */
export function mergeStateDelta(stored: StateDTO, next: Partial): StateDTO {
  if (!next.partial) return next;
  const byId = new Map(stored.threads.map((t) => [t.id, t]));
  for (const t of next.threads) byId.set(t.id, t);
  return { ...next, partial: undefined, threads: [...byId.values()] } as StateDTO;
}

/** The rev to ask "since": a microsecond before the newest (revs above 2^53 round in JS). */
export function sinceRev(s: StateDTO): number {
  let max = 0;
  for (const t of s.threads) if (typeof t.rev === "number" && t.rev > max) max = t.rev;
  return max > 0 ? max - 1000 : 0;
}

/** Stored and new events by seq, the newest `limit`. A full page of new events means a gap: drop the stored ones. */
export function mergeEventLists(cached: FyrEvent[], fresh: FyrEvent[], limit: number, fullPage: boolean): FyrEvent[] {
  if (fullPage) return fresh.slice(-limit);
  const bySeq = new Map<number, FyrEvent>();
  for (const e of cached) bySeq.set(e.seq, e);
  for (const e of fresh) bySeq.set(e.seq, e);
  return [...bySeq.values()].sort((a, b) => a.seq - b.seq).slice(-limit);
}

// ---- IndexedDB (best effort: every call resolves, never throws) ----

const DB = "fyr";
const STORE = "kv";
let dbp: Promise<IDBDatabase | null> | null = null;

function db(): Promise<IDBDatabase | null> {
  if (dbp) return dbp;
  dbp = new Promise((resolve) => {
    try {
      if (typeof indexedDB === "undefined") return resolve(null);
      const r = indexedDB.open(DB, 1);
      r.onupgradeneeded = () => r.result.createObjectStore(STORE);
      r.onsuccess = () => resolve(r.result);
      r.onerror = () => resolve(null);
      r.onblocked = () => resolve(null);
    } catch {
      resolve(null);
    }
  });
  return dbp;
}

async function get<T>(key: string): Promise<T | null> {
  const d = await db();
  if (!d) return null;
  return new Promise((resolve) => {
    try {
      const r = d.transaction(STORE, "readonly").objectStore(STORE).get(key);
      r.onsuccess = () => resolve((r.result as T) ?? null);
      r.onerror = () => resolve(null);
    } catch {
      resolve(null);
    }
  });
}

async function put(key: string, value: unknown): Promise<void> {
  const d = await db();
  if (!d) return;
  await new Promise<void>((resolve) => {
    try {
      const tx = d.transaction(STORE, "readwrite");
      tx.objectStore(STORE).put(value, key);
      tx.oncomplete = () => resolve();
      tx.onerror = () => resolve();
      tx.onabort = () => resolve();
    } catch {
      resolve();
    }
  });
}

export const cache = {
  state: () => get<StateDTO>("state"),
  saveState: (s: StateDTO) => put("state", s),
  events: (threadId: string) => get<FyrEvent[]>(`events:${threadId}`),
  saveEvents: (threadId: string, evs: FyrEvent[]) => put(`events:${threadId}`, evs),
};
