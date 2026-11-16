// V0.109: the marked row in the thread list is the open thread, whatever path opened it (a row click, a link from
// another thread, a notification, ?open=, the keyboard, a new thread). The list keeps no cursor of its own.
import { SECTIONS, unpinned, type SectionKey, type Sections } from "./store.ts";

/** The row the list marks: the open thread; with nothing open, the last thread opened (where j/k starts). */
export function markedRow(openId: string | null, lastOpen: string | null): string | null {
  return openId ?? lastOpen;
}

/**
 * What the list must change to draw the row of `id`: open its section, and show all of it when the row is past the
 * section's limit. `limits` is what each section draws now (0 when closed); `full` is the limit of an open section.
 * null: the row is drawn already, or no section holds it (search, the project filter or a quiet project hides it:
 * the list then marks no row and the filter stays as the owner set it).
 */
export function revealRow(sec: Sections, id: string | null, limits: Partial<Record<SectionKey, number>>, full: Record<SectionKey, number>): { section: SectionKey; expand: boolean } | null {
  if (!id || sec.pinned.some((t) => t.id === id)) return null;
  for (const { key } of SECTIONS) {
    const i = unpinned(sec[key]).findIndex((t) => t.id === id);
    if (i < 0) continue;
    if (i < (limits[key] ?? Infinity)) return null;
    return { section: key, expand: i >= full[key] };
  }
  return null;
}

/** The row to open when the rows `ids` leave the list while `cur` is open: the next row, else the one before. */
export function rowAfter(order: string[], cur: string | null, ids: string[]): string | null {
  if (!cur || !ids.includes(cur)) return null;
  const i = order.indexOf(cur);
  return order.slice(i + 1).find((x) => !ids.includes(x)) ?? order.slice(0, Math.max(0, i)).reverse().find((x) => !ids.includes(x)) ?? null;
}
