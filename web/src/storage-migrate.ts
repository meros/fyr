// V0.client-refs (decision 0031 slice 3, 2.5 and 4.2): what this device keeps in localStorage names a record by its ref
// (`<hostId>/<id>`) and a host by its install id, so two hosts' records with one id stay apart and a renamed host keeps
// its place. Keys written before name a record by the host's own id and a host by its name; they move once, after the
// first state that lists the hosts and their records, and `fyr.storage = 2` marks that done. When a host's id rotates
// (hosts[].idWas), what the device keeps under the old id moves to the new one. Pure but for the storage passed in.
import type { HostInfo } from "./hosts.ts";
import { HOSTS_OFF_KEY, parseHostsOff } from "./filters.ts";
import { loadRemembered, NEW_THREAD_KEY } from "./new-thread.ts";

export const STORAGE_VERSION_KEY = "fyr.storage";
export const STORAGE_VERSION = "2";
export const DRAFT_PREFIX = "fyr.draft.";
export const PROJECT_FILTER_KEY = "fyr.projectFilter";
export const COLLAPSED_KEY = "fyr.view.collapsed";

/** localStorage as far as the migration uses it. */
export interface KVList {
  getItem(k: string): string | null;
  setItem(k: string, v: string): void;
  removeItem(k: string): void;
  readonly length: number;
  key(i: number): string | null;
}

/** A record as the client keeps it: `id` is its ref, `rid` the host's own id. */
export interface RefRecord {
  id?: string;
  rid?: string;
}

/** The ref of the one record whose own id is `id`; an id that is a ref already stays; none or two is null. */
export function refFor(records: readonly RefRecord[], id: string): string | null {
  if (!id) return null;
  if (records.some((r) => r.id === id)) return id;
  const hit = records.filter((r) => r.rid === id);
  return hit.length === 1 && hit[0].id ? hit[0].id : null;
}

function keys(kv: KVList): string[] {
  const out: string[] = [];
  for (let i = 0; i < kv.length; i++) {
    const k = kv.key(i);
    if (k !== null) out.push(k);
  }
  return out;
}

/** V0.169's hosts turned off: a name becomes its host's install id (the name stays for a host that reports none);
 *  a name no host has is dropped; an id stays. */
export function hostsOffToIds(off: readonly string[], hosts: readonly Pick<HostInfo, "id" | "name">[]): string[] {
  const out: string[] = [];
  for (const k of off) {
    if (hosts.some((h) => h.id === k)) out.push(k);
    else {
      const h = hosts.find((x) => x.name === k);
      if (h) out.push(h.id || h.name);
    }
  }
  return [...new Set(out)];
}

/** Move what this device keeps to refs and ids, once. A draft or a value whose record no single host has stays under
 *  its old key (drafts.ts reads it as a fallback for one release). Returns whether it ran. */
export function migrateStorage(kv: KVList | null, s: { hosts: readonly HostInfo[]; threads: readonly RefRecord[]; projects: readonly RefRecord[] }): boolean {
  if (!kv) return false;
  try {
    if (kv.getItem(STORAGE_VERSION_KEY) === STORAGE_VERSION) return false;
    // Drafts: fyr.draft.<id> → fyr.draft.<hostId>/<id>. A draft already under the ref wins (the newer client wrote it).
    for (const k of keys(kv)) {
      if (!k.startsWith(DRAFT_PREFIX)) continue;
      const id = k.slice(DRAFT_PREFIX.length);
      if (id.includes("/")) continue;
      const r = refFor(s.threads, id);
      if (!r) continue;
      const text = kv.getItem(k);
      if (text && kv.getItem(DRAFT_PREFIX + r) === null) kv.setItem(DRAFT_PREFIX + r, text);
      kv.removeItem(k);
    }
    // The project filter.
    const pf = kv.getItem(PROJECT_FILTER_KEY);
    const pr = pf && refFor(s.projects, pf);
    if (pr && pr !== pf) kv.setItem(PROJECT_FILTER_KEY, pr);
    // New thread's defaults: fyr.lastProject and fyr.newDefaults fold into fyr.newThread (#41), whose project is a ref.
    const rem = loadRemembered(kv);
    const rp = rem.projectId && refFor(s.projects, rem.projectId);
    if (rp && rp !== rem.projectId) kv.setItem(NEW_THREAD_KEY, JSON.stringify({ ...rem, projectId: rp }));
    // The live views folded away.
    const raw = kv.getItem(COLLAPSED_KEY);
    if (raw) {
      try {
        const ids = JSON.parse(raw) as unknown;
        if (Array.isArray(ids)) kv.setItem(COLLAPSED_KEY, JSON.stringify([...new Set(ids.filter((x): x is string => typeof x === "string").map((x) => refFor(s.threads, x) ?? x))]));
      } catch {
        kv.removeItem(COLLAPSED_KEY);
      }
    }
    // The hosts turned off: names → ids.
    const off = kv.getItem(HOSTS_OFF_KEY);
    if (off !== null) {
      const ids = hostsOffToIds(parseHostsOff(off), s.hosts);
      if (ids.length) kv.setItem(HOSTS_OFF_KEY, JSON.stringify(ids));
      else kv.removeItem(HOSTS_OFF_KEY);
    }
    kv.setItem(STORAGE_VERSION_KEY, STORAGE_VERSION);
    return true;
  } catch {
    return false; // storage denied: the next load tries again
  }
}

/** A ref of host `was` as a ref of host `now`; anything else as it is. */
export function moveRef(id: string, was: string, now: string): string {
  return was && now && id.startsWith(`${was}/`) ? `${now}/${id.slice(was.length + 1)}` : id;
}

/** Decision 0031 2.1, 2.5: a host's install id rotated from `was` to `now`. Every key and value of Fyr's in this
 *  storage that names `was` names `now` instead: drafts, the filters, New thread's defaults, the hidden hosts. An install
 *  id is 19 characters of its own alphabet (`F7QK-2M9X-AB34-CD56`), so it occurs in nothing else. A key the new id
 *  already has keeps its value. Returns how many keys changed. */
export function moveHostKeys(kv: KVList | null, was: string, now: string): number {
  if (!kv || !was || !now || was === now) return 0;
  let n = 0;
  try {
    for (const k of keys(kv)) {
      if (!k.startsWith("fyr.")) continue;
      const v = kv.getItem(k);
      const k2 = k.split(was).join(now);
      const v2 = v === null ? null : v.split(was).join(now);
      if (k2 === k && v2 === v) continue;
      if (k2 !== k) {
        if (kv.getItem(k2) === null && v2 !== null) kv.setItem(k2, v2);
        kv.removeItem(k);
      } else if (v2 !== null) kv.setItem(k, v2);
      n++;
    }
  } catch {
    /* storage denied */
  }
  return n;
}

/** The rotations in a host list: each host listed with a new id and the id it had. */
export function rotations(hosts: readonly Pick<HostInfo, "id" | "idWas">[]): { was: string; now: string }[] {
  return hosts.filter((h) => h.id && h.idWas && h.id !== h.idWas).map((h) => ({ was: h.idWas!, now: h.id! }));
}
