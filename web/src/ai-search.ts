// V0.ai-search: what AI search asks each host and how the answers become one list. No UI here (ai-search-ui.tsx).

import type { HostId } from "./hostid.ts";
import type { SearchHit } from "./api.ts";
import { bare, ref } from "./hostfeed.ts";
import type { Thread } from "./types.ts";

/** The most threads asked about on one host; the host reads no more than this either. */
export const AI_SEARCH_THREADS = 120;

export interface AiHit {
  id: string;
  why: string;
}

/** The threads to ask each host about, in list order: host id → the host's own thread ids. A thread with no host id
 *  (an offline host's cached copy can be read, but its host cannot answer) is left out. */
export function askPerHost(threads: readonly Thread[]): Map<HostId, string[]> {
  const out = new Map<HostId, string[]>();
  for (const t of threads) {
    if (t.hostOffline) continue;
    const ids = out.get(t.hostId) ?? [];
    if (ids.length < AI_SEARCH_THREADS) ids.push(t.rid ?? bare(t.hostId, t.id));
    out.set(t.hostId, ids);
  }
  return out;
}

/** One list from the hosts' answers: each host's best first, then each host's second, and so on. */
export function mergeHits(answers: readonly { host: string; hits: readonly SearchHit[] }[]): AiHit[] {
  const out: AiHit[] = [];
  const seen = new Set<string>();
  const rounds = Math.max(0, ...answers.map((a) => a.hits.length));
  for (let i = 0; i < rounds; i++) {
    for (const a of answers) {
      const h = a.hits[i];
      if (!h) continue;
      const id = ref(a.host, h.id);
      if (seen.has(id)) continue;
      seen.add(id);
      out.push({ id, why: h.why ?? "" });
    }
  }
  return out;
}
