// SPEC V0.137: the threads that wait for a slot for a heavy job (V0.86) have their own section in the thread list,
// numbered by their real place in their host's queue, and the owner reorders it. Pure.

import type { HostLoad } from "./host-load.ts";
import { move } from "./reorder.ts";
import type { Thread } from "./types.ts";

/** One place in a host's queue. */
export interface QueueEntry {
  host: string;
  /** a thread id, or "self-deploy" (the deploy's or the updater's tests) */
  owner: string;
  /** "test", "build" or "e2e" */
  kind: string;
  /** 1-based: the place in the host's queue, the priority waiters counted */
  pos: number;
  /** the deploy's or the updater's wait: ahead of every thread, not movable */
  priority: boolean;
  /** V0.admission-tidy: the thread holds a slot and waits to grow it for a heavier command. It runs, so it has no row in
   *  this section (its row says "waiting to grow", threadSlot) but keeps its place for a reorder. */
  grow?: boolean;
}

/** Every host's queue, host by host in the load's order (the front's host order). Hosts without slots or without a
 *  queue give nothing. One order per host, not one per kind: every kind takes from the host's one pool (V0.86 (1)). */
export function slotQueue(load: HostLoad[]): QueueEntry[] {
  const out: QueueEntry[] = [];
  for (const h of load) {
    const held = new Set((h.slots?.held ?? []).map((x) => x.owner));
    (h.slots?.waiting ?? []).forEach((w, i) =>
      out.push({ host: h.host, owner: w.owner, kind: w.kind, pos: i + 1, priority: !!w.priority, ...(held.has(w.owner) ? { grow: true } : {}) }),
    );
  }
  return out;
}

/** The section's parts: one per host with a queue. The host's name shows only when more than one host has one. */
export interface QueuePart {
  host: string;
  label: string | null;
  entries: QueueEntry[];
}

export function queueParts(q: QueueEntry[]): QueuePart[] {
  const parts: QueuePart[] = [];
  for (const e of q) {
    let p = parts.find((x) => x.host === e.host);
    if (!p) parts.push((p = { host: e.host, label: null, entries: [] }));
    p.entries.push(e);
  }
  if (parts.length > 1) for (const p of parts) p.label = p.host;
  return parts;
}

/** A row of the section: a priority wait (no thread) or a queued thread the list shows. A thread the list hides (a
 *  filter, a search, a quiet project) has no row, and the others keep their real numbers. */
interface QueueRow {
  entry: QueueEntry;
  thread: Thread | null;
}

export function withQueue(entries: QueueEntry[], threads: Record<string, Thread>): QueueRow[] {
  const out: QueueRow[] = [];
  for (const e of entries) {
    if (e.grow) continue; // V0.admission-tidy: it runs; its row is where it is
    if (e.priority) out.push({ entry: e, thread: null });
    else if (threads[e.owner]) out.push({ entry: e, thread: threads[e.owner] });
  }
  return out;
}

/** The host's new order of its threads' places after a move among the rows shown (from, to are indexes into `shown`).
 *  A waiter not shown keeps its place; the shown ones fill their places in their new order. Priority waiters are
 *  never in `all`: the host keeps them ahead. */
export function reorderOwners(all: readonly string[], shown: readonly string[], from: number, to: number): string[] {
  const next = move(shown, from, to);
  const set = new Set(shown);
  let k = 0;
  return all.map((o) => (set.has(o) ? next[k++] : o));
}

/** The words of a priority wait's row. */
export function priorityLabel(e: QueueEntry): string {
  return e.owner === "self-deploy" ? `Fyr's own update · ${e.kind}` : `${e.owner} · ${e.kind}`;
}
