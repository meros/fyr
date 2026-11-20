// Decision 0031 2.1, 2.5 (V0.client-refs, test 6): what this device keeps in localStorage names a record by its ref
// (`<hostId>/<id>`) and a host by its install id. When a host's id rotates (hosts[].idWas), what the device keeps under
// the old id moves to the new one. Pure but for the storage passed in. (The one-time move from names and bare ids to
// refs, V0.client-refs' migration, ended with the old routes: V0.old-routes-end.)
import type { HostId } from "./hostid.ts";
import type { HostInfo } from "./hosts.ts";

/** localStorage as far as the move uses it. */
export interface KVList {
  getItem(k: string): string | null;
  setItem(k: string, v: string): void;
  removeItem(k: string): void;
  readonly length: number;
  key(i: number): string | null;
}

function keys(kv: KVList): string[] {
  const out: string[] = [];
  for (let i = 0; i < kv.length; i++) {
    const k = kv.key(i);
    if (k !== null) out.push(k);
  }
  return out;
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
export function rotations(hosts: readonly Pick<HostInfo, "id" | "idWas">[]): { was: HostId; now: HostId }[] {
  return hosts.flatMap((h) => (h.id && h.idWas && h.id !== h.idWas ? [{ was: h.idWas, now: h.id }] : []));
}
