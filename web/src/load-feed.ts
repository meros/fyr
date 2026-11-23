// The host load the whole client shares (load-ui.tsx polls it): the latest list, who listens, and the one rule that a
// read which started before a local patch (a reorder's receipt, V0.137) is stale and must not overwrite it.

import type { HostLoad, Slots } from "./host-load.ts";

export interface LoadFeed {
  current(): HostLoad[];
  subscribe(f: (l: HostLoad[]) => void): () => void;
  size(): number;
  /** Show `slots` as the host's pool at once. */
  patchSlots(host: string, slots: Slots | ((s: Slots) => Slots)): void;
  /** Read the load; the answer replaces the list unless a patch came while it was on its way. A failed read keeps the list. */
  poll(read: () => Promise<HostLoad[]>): Promise<void>;
}

export function createLoadFeed(normalize: (l: HostLoad[]) => HostLoad[]): LoadFeed {
  let current: HostLoad[] = [];
  let patches = 0;
  const subs = new Set<(l: HostLoad[]) => void>();
  const emit = () => {
    for (const s of subs) s(current);
  };
  return {
    current: () => current,
    size: () => subs.size,
    subscribe(f) {
      subs.add(f);
      return () => void subs.delete(f);
    },
    patchSlots(host, slots) {
      patches++;
      current = current.map((h) => (h.host === host && h.slots ? { ...h, slots: typeof slots === "function" ? slots(h.slots) : slots } : h));
      emit();
    },
    async poll(read) {
      const started = patches;
      const l = await read().then(normalize).catch(() => null);
      // The next poll confirms the patch; one that started before it carries the pool as it was.
      if (!l || started !== patches) return;
      current = l;
      emit();
    },
  };
}
