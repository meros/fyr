// SPEC 0.5 V0.38: the last state and each opened thread's events stay on the
// device (IndexedDB). At start the app draws the stored state at once and asks
// the host only for threads changed since; opening a thread shows the stored
// events and fetches only newer ones. Without IndexedDB everything still
// works from the network.
import type { FyrEvent, StateDTO } from "./types.ts";
import type { Snapshot } from "./hostfeed.ts";

type Partial = StateDTO & { partial?: boolean };

/** Threads in a partial state replace stored ones by id; a full state replaces everything. */
export function mergeStateDelta(stored: StateDTO, next: Partial): StateDTO {
  if (!next.partial) return next;
  const byId = new Map(stored.threads.map((t) => [t.id, t]));
  for (const t of next.threads) byId.set(t.id, t);
  return { ...next, partial: undefined, threads: [...byId.values()] } as StateDTO;
}

/** V0.host-ids (decision 0031 2.5, C7): the device keeps each host's own state, by install id, as that host last sent
 *  it. A host's "since" is its own newest rev, and its partial answer merges into its own state, and the hosts are
 *  kept apart (slice 2b), so one host's clock or records never decide what is asked of another. */
export function hostSince(stored: StateDTO | null): number {
  return stored ? sinceRev(stored) : 0;
}

/** A host's answer on top of its stored state: a partial answer is the threads that changed, a full one is all. */
export function nextHostState(stored: StateDTO | null, answer: Partial): StateDTO {
  return stored ? mergeStateDelta(stored, answer) : ({ ...answer, partial: undefined } as StateDTO);
}

/** Revs are the host's clock in ns. Frames reach a device out of rev order (the stream holds a thread up to
 *  100 ms, a front relays several hosts), so a frame lost when the stream drops can be older than the newest rev the
 *  device holds. Asking a minute early fetches it again; the cost is the threads that changed in that minute. */
const SINCE_SLACK_NS = 60_000_000_000;

/** The rev to ask "since": a minute before the newest the device holds (also covers that revs above 2^53 round). */
export function sinceRev(s: StateDTO): number {
  let max = 0;
  for (const t of s.threads) if (typeof t.rev === "number" && t.rev > max) max = t.rev;
  return max > SINCE_SLACK_NS ? max - SINCE_SLACK_NS : 0;
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

async function del(key: string): Promise<void> {
  const d = await db();
  if (!d) return;
  await new Promise<void>((resolve) => {
    try {
      const tx = d.transaction(STORE, "readwrite");
      tx.objectStore(STORE).delete(key);
      tx.oncomplete = () => resolve();
      tx.onerror = () => resolve();
      tx.onabort = () => resolve();
    } catch {
      resolve();
    }
  });
}

/** V0.client-refs (decision 0031 2.5): a host's id rotated: its stored timelines move to the new id; the stored state
 *  is a cache and is read again (the host's records come stamped with the new id). */
async function moveHost(was: string, now: string): Promise<void> {
  const d = await db();
  if (!d || !was || !now || was === now) return;
  await new Promise<void>((resolve) => {
    try {
      const tx = d.transaction(STORE, "readwrite");
      const st = tx.objectStore(STORE);
      const from = `events:${was}/`;
      const r = st.openCursor();
      r.onsuccess = () => {
        const c = r.result;
        if (!c) return;
        const k = String(c.key);
        if (k.startsWith(from)) {
          st.put(c.value, `events:${now}/${k.slice(from.length)}`);
          c.delete();
        } else if (k === `host:${was}`) {
          st.put(c.value, `host:${now}`); // the host's own state: its records are its own, not stamped
          c.delete();
        }
        c.continue();
      };
      tx.oncomplete = () => resolve();
      tx.onerror = () => resolve();
      tx.onabort = () => resolve();
    } catch {
      resolve();
    }
  });
}

export const cache = {
  moveHost,
  /** the device's copy of the whole state, drawn at once on start (V0.38): slice 2b's snapshot, one state per host.
   *  V0.old-routes-end: the older flat copy ("state2", the old /api/state's shape) goes when the new one is written. */
  state: () => get<Snapshot>("state3").then((s) => (s && typeof s.states === "object" && s.states && Array.isArray(s.hosts) ? s : null)),
  saveState: (s: Snapshot) => put("state3", s).then(() => del("state2")),
  /** one host's own state as it last sent it, by install id: its "since" (V0.host-ids) */
  hostState: (hostId: string) => get<StateDTO>(`host:${hostId}`),
  saveHostState: (hostId: string, s: StateDTO) => put(`host:${hostId}`, s),
  events: (threadId: string) => get<FyrEvent[]>(`events:${threadId}`),
  saveEvents: (threadId: string, evs: FyrEvent[]) => put(`events:${threadId}`, evs),
};
