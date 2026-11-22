// V0.207, V0.open-row-glides: the list keeps its place. A stable order (`trackArrivals` and the order in store.ts) keeps
// a row from moving under the owner; a row that changes state moves at once, the open one too, and glides there; j/k
// walks from where the open row was (`stillRows`). One implementation for both fronts.
import { SECTIONS, isPinned, naturalAt, sectionOf, type Arrival, type SectionKey, type Sections } from "./store.ts";
import { needsGroupRank } from "./rows.ts";
import type { Thread } from "./types.ts";

/** The group whose order is arrival, or null (Seen, Snoozed and Settled keep their own order, the queue its own). */
function arrivalKey(t: Thread): string | null {
  const k = sectionOf(t);
  if (k === "needs") return `needs:${needsGroupRank(t)}`;
  return k === "working" || k === "unread" ? k : null;
}

/** V0.stable-groups (V0.207 kept the Unread time the same way): when each thread came into its group. A thread that
 *  stays in its group keeps its time, so nothing it does moves it; one that comes in later gets `now`, so it goes last.
 *  On the first sight (an empty map: a reload) the time is the thread's own time. */
export function trackArrivals(prev: Readonly<Record<string, Arrival>>, threads: Record<string, Thread>, now: number): Record<string, Arrival> {
  const first = Object.keys(prev).length === 0;
  const out: Record<string, Arrival> = {};
  for (const t of Object.values(threads)) {
    const key = arrivalKey(t);
    if (!key) continue;
    const p = prev[t.id];
    out[t.id] = p && p.key === key ? p : { key, at: first ? naturalAt(t) : Math.max(naturalAt(t), now) };
  }
  return out;
}

/** The open row's place when it was opened. */
export interface RowPlace {
  id: string;
  section: SectionKey;
  index: number;
}

const HELD: SectionKey[] = ["needs", "unread", "seen", "working"];

/** The row's place among the sections, or null. A queued row has no place of its own to keep: the queue orders it. */
export function placeOf(sec: Sections, id: string | null): RowPlace | null {
  if (!id) return null;
  for (const section of HELD) {
    const index = sec[section].findIndex((t) => t.id === id);
    if (index >= 0) return { id, section, index };
  }
  return null;
}

function withPinned(out: Sections): Sections {
  out.pinned = SECTIONS.filter((s) => s.key !== "slots").flatMap((s) => out[s.key].filter(isPinned));
  return out;
}

/**
 * What the list draws. `live` is the truth, and every other row is where `live` has it. `open` puts the open row back
 * in the place it had when it was opened, unless it left for Snoozed, Settled or the queue, or is gone.
 */
export function stillRows(live: Sections, open: RowPlace | null): Sections {
  if (!open) return live;
  const row = HELD.flatMap((k) => live[k]).find((t) => t.id === open.id);
  if (!row) return live;
  const out: Sections = { ...live };
  for (const k of HELD) out[k] = live[k].filter((t) => t.id !== open.id);
  out[open.section] = [...out[open.section].slice(0, open.index), row, ...out[open.section].slice(open.index)];
  return withPinned(out);
}

/** The row to hold still on the screen: the one under the pointer, else the open row if it is in view, else the first
 *  row in view. At the very top with no pointer in the list there is none: a new row may then push the list down, so the
 *  owner sees it. V0.open-row-glides: the open row may move to another place, and the list does not chase it: the row
 *  above it stands in (else the one below), so the rows under it close the gap it leaves. */
export function anchorRow(o: { hovered: string | null; open: string | null; firstVisible: string | null; visible(id: string): boolean; scrollTop: number; besideOpen?: string | null }): string | null {
  const id = o.hovered ?? (o.scrollTop <= 0 ? null : o.open && o.visible(o.open) ? o.open : o.firstVisible);
  return id && id === o.open ? (o.besideOpen ?? null) : id;
}

/** V0.finish-debounce: how long a thread that stopped working stays in Moving before it shows as finished. A run that
 *  ends and starts again within it (a queued message, a Fyr line that wakes the agent) never blinks into Finished. */
export const FINISH_HOLD_MS = 5_000;

/** A thread held in Moving: when it left, and the attention it had there. */
export interface FinishHold {
  at: number;
  attention: Thread["attention"];
}

/**
 * V0.finish-debounce: the threads as the list shows them. A thread whose live section went from working to finished
 * (seen) within FINISH_HOLD_MS keeps its working attention. Any other move is at once: back to working, to Your move,
 * Snoozed or Settled. `last` is the previous live map; `next` is when the earliest hold ends, or null.
 */
export function steadyThreads(
  prev: Readonly<Record<string, FinishHold>>,
  last: Readonly<Record<string, Thread>> | null,
  threads: Record<string, Thread>,
  now: number,
): { threads: Record<string, Thread>; holds: Record<string, FinishHold>; next: number | null } {
  const holds: Record<string, FinishHold> = {};
  let out: Record<string, Thread> | null = null;
  let next: number | null = null;
  for (const t of Object.values(threads)) {
    if (t.attention?.section !== "seen") continue;
    const was = last?.[t.id]?.attention;
    const h = prev[t.id] ?? (was?.section === "working" ? { at: now, attention: was } : undefined);
    if (!h || now - h.at >= FINISH_HOLD_MS) continue;
    holds[t.id] = h;
    (out ??= { ...threads })[t.id] = { ...t, attention: h.attention };
    const end = h.at + FINISH_HOLD_MS;
    if (next === null || end < next) next = end;
  }
  return { threads: out ?? threads, holds, next };
}
