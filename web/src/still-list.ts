// V0.207, V0.open-row-only: the list keeps its place. Two things keep a row from moving under the owner: a stable order
// (`trackUnread` and the order in store.ts), and the open row staying where it was until the owner opens another. Every
// other row moves the moment its state changes: nothing waits. One implementation for both fronts.
import { SECTIONS, isPinned, sectionOf, type SectionKey, type Sections } from "./store.ts";
import type { Thread } from "./types.ts";

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
 *  owner sees it. */
export function anchorRow(o: { hovered: string | null; open: string | null; firstVisible: string | null; visible(id: string): boolean; scrollTop: number }): string | null {
  if (o.hovered) return o.hovered;
  if (o.scrollTop <= 0) return null;
  return o.open && o.visible(o.open) ? o.open : o.firstVisible;
}
