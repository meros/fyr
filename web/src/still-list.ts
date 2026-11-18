// V0.207: the list keeps its place. Three things keep a row from moving under the owner: a stable order (`trackUnread`
// and the order in store.ts), the open row staying where it was until the owner leaves it, and a hold while the owner's
// hand is on the list. Row content and every count stay live; only places wait. One implementation for both fronts.
import { SECTIONS, isPinned, sectionOf, type SectionKey, type Sections } from "./store.ts";
import { needsGroupRank } from "./rows.ts";
import type { Thread } from "./types.ts";

/** After the last j/k: reading the next row takes a moment, and no pointer says the owner is still there. */
export const KEY_HOLD_MS = 5000;
/** After a touch or a scroll. Differs from the keys because a flick goes on moving after the finger lifts. */
export const TOUCH_HOLD_MS = 3000;

/** Where the owner's hand is. */
export interface Hand {
  /** the mouse pointer is in the list */
  inside: boolean;
  /** a finger is on the list */
  touching: boolean;
  /** the time (ms) until which the last key, touch or scroll holds the list */
  until: number;
}

export const NO_HAND: Hand = { inside: false, touching: false, until: 0 };

export function holding(h: Hand, now: number): boolean {
  return h.inside || h.touching || now < h.until;
}

/** The time of each unread thread's becoming unread. A thread that stays unread keeps its time, so later activity does
 *  not move it. On the first sight (a reload) the time is its last activity. */
export function trackUnread(prev: Readonly<Record<string, number>>, threads: Record<string, Thread>): Record<string, number> {
  const out: Record<string, number> = {};
  for (const t of Object.values(threads)) {
    if (sectionOf(t) !== "unread") continue;
    out[t.id] = prev[t.id] ?? (Date.parse(t.activityAt ?? t.updatedAt) || 0);
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
 * What the list draws. `live` is the truth. With `held` (the rows as they stood when the hold began) rows keep their
 * section and order. A new Your-move row comes in at once, at the top of its group (V0.your-move); any other new row waits. Snoozed,
 * Settled and the queue are always live, so the owner's settle, snooze and drag act at once. `open` then puts the open
 * row back in its place, hold or no hold, unless it left for Snoozed, Settled or the queue, or is gone. `freed` is the
 * row the owner just marked seen or unseen: their own act, so it goes to its live place at once.
 */
export function stillRows(live: Sections, held: Sections | null, open: RowPlace | null, freed: string | null = null): Sections {
  if (!held && !open) return live;
  const out: Sections = { ...live, needs: [...live.needs], unread: [...live.unread], seen: [...live.seen], working: [...live.working] };
  const now = new Map<string, Thread>();
  for (const k of HELD) for (const t of live[k]) now.set(t.id, t);
  if (held) {
    for (const k of HELD) out[k] = held[k].flatMap((t) => now.get(t.id) ?? []);
    // A row that turned into Needs you goes there at once, wherever the hold had it.
    const arrived = live.needs.filter((t) => !held.needs.some((x) => x.id === t.id));
    for (const k of HELD) if (k !== "needs") out[k] = out[k].filter((t) => !arrived.some((x) => x.id === t.id));
    for (const t of [...arrived].reverse()) {
      const rank = needsGroupRank(t);
      const at = out.needs.findIndex((x) => x.attention.section === "needs" && needsGroupRank(x) >= rank);
      out.needs.splice(at < 0 ? out.needs.length : at, 0, t);
    }
    // Rows that come back from the live sections have no held place, and must not vanish until the hold ends.
    const back = [held.slots, held.snoozed, held.settled].flat().map((t) => t.id);
    const places = [...back, ...(freed ? [freed] : [])].flatMap((id) => placeOf(live, id) ?? []);
    for (const to of places.sort((x, y) => x.index - y.index)) {
      for (const k of HELD) out[k] = out[k].filter((t) => t.id !== to.id);
      out[to.section] = [...out[to.section].slice(0, to.index), now.get(to.id)!, ...out[to.section].slice(to.index)];
    }
  }
  const row = open && now.get(open.id);
  if (open && row) {
    for (const k of HELD) out[k] = out[k].filter((t) => t.id !== open.id);
    out[open.section] = [...out[open.section].slice(0, open.index), row, ...out[open.section].slice(open.index)];
  }
  return withPinned(out);
}

/** The length of the longest rising run in `xs`: the rows of a section that can stay put while the others move. */
function longestRising(xs: number[]): number {
  const tails: number[] = [];
  for (const x of xs) {
    let lo = 0;
    let hi = tails.length;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (tails[mid] < x) lo = mid + 1;
      else hi = mid;
    }
    tails[lo] = x;
  }
  return tails.length;
}

/** V0.your-move: how many rows the hold keeps from moving: those the live list has in another section or not yet drawn,
 *  and those that change place inside their section. One row moving counts once, not every row it shifts. The open row
 *  stays where it is after the hold too, so it does not count. */
export function waitingMoves(drawn: Sections, live: Sections, open: string | null): number {
  let n = 0;
  for (const k of HELD) {
    const at = new Map(drawn[k].map((t, i) => [t.id, i]));
    const rows = live[k].filter((t) => t.id !== open);
    const kept = rows.flatMap((t) => at.get(t.id) ?? []);
    n += rows.length - longestRising(kept);
  }
  return n;
}

/** The row to hold still on the screen: the one under the pointer, else the open row if it is in view, else the first
 *  row in view. At the very top with no pointer in the list there is none: a new row may then push the list down, so the
 *  owner sees it. */
export function anchorRow(o: { hovered: string | null; open: string | null; firstVisible: string | null; visible(id: string): boolean; scrollTop: number }): string | null {
  if (o.hovered) return o.hovered;
  if (o.scrollTop <= 0) return null;
  return o.open && o.visible(o.open) ? o.open : o.firstVisible;
}
