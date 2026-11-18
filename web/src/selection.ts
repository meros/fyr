// V0.109: the marked row in the thread list is the open thread, whatever path opened it (a row click, a link from
// another thread, a notification, ?open=, the keyboard, a new thread). The list keeps no cursor of its own.
// V0.186: a folded section stays folded; its header carries the mark instead (foldedRow).
import { SECTIONS, isOpen, unpinned, type SectionKey, type Sections } from "./store.ts";
import type { Thread } from "./types.ts";

type Open = Partial<Record<SectionKey, boolean>>;

/** V0.186: the folds, remembered per device. */
export const SECTION_OPEN_KEY = "fyr.sectionOpen";

/** V0.186: what the device stored; anything else counts as nothing. An open Settled is left out: it closes 30 s after
 *  the owner leaves it (V0.48), and that time does not survive a reload. */
export function parseSectionOpen(raw: string | null): Open {
  if (!raw) return {};
  try {
    const v = JSON.parse(raw) as unknown;
    if (!v || typeof v !== "object" || Array.isArray(v)) return {};
    const out: Open = {};
    for (const { key } of SECTIONS) {
      const x = (v as Record<string, unknown>)[key];
      if (typeof x === "boolean" && !(key === "settled" && x)) out[key] = x;
    }
    return out;
  } catch {
    return {};
  }
}

/** The row the list marks: the open thread; with nothing open, the last thread opened (where j/k starts). */
export function markedRow(openId: string | null, lastOpen: string | null): string | null {
  return openId ?? lastOpen;
}

/**
 * V0.186: the open section that must show all of itself to draw the row of `id`, because the row is past its limit.
 * `limits` is what each section draws now (0 when folded). null: the row is drawn, it is in a folded section (which
 * stays folded, see foldedRow), or no section holds it (search hides it, or a quiet project).
 */
export function cutSection(sec: Sections, id: string | null, limits: Partial<Record<SectionKey, number>>): SectionKey | null {
  if (!id || sec.pinned.some((t) => t.id === id)) return null;
  for (const { key } of SECTIONS) {
    const i = unpinned(sec[key], key).findIndex((t) => t.id === id);
    if (i < 0) continue;
    const limit = limits[key] ?? Infinity;
    return limit > 0 && i >= limit ? key : null;
  }
  return null;
}

/** V0.186: the folded section that holds the current thread, whose header then carries the mark. null when the row
 *  itself can be marked: its section is open, it is pinned or queued (neither folds), or no section holds it. */
export function foldedRow(sec: Sections, id: string | null, open: Open): { section: SectionKey; thread: Thread } | null {
  if (!id || sec.pinned.some((t) => t.id === id)) return null;
  for (const { key } of SECTIONS) {
    const thread = unpinned(sec[key], key).find((t) => t.id === id);
    if (!thread) continue;
    return key !== "slots" && !isOpen(key, open) ? { section: key, thread } : null;
  }
  return null;
}

/** V0.186: what a screen reader says for a folded header that holds the current thread. */
export function heldLabel(label: string, n: number, title: string): string {
  return `${label}, ${n.toLocaleString()} thread${n === 1 ? "" : "s"}, folded, holds the current thread: ${title}`;
}

/** V0.186: j/k. The next drawn row in `dir`; folded sections are skipped and stay folded. From a thread inside one the
 *  step starts at its place in the list (`all` is every row in order, `shown` the drawn ones). null: nowhere to go. */
export function stepRow(all: string[], shown: string[], cur: string | null, dir: 1 | -1): string | null {
  const i = cur ? all.indexOf(cur) : -1;
  if (i < 0) return shown[0] ?? null;
  const drawn = new Set(shown);
  const way = dir > 0 ? all.slice(i + 1) : all.slice(0, i).reverse();
  return way.find((x) => drawn.has(x)) ?? (drawn.has(cur!) ? cur : null);
}

/** The row to open when the rows `ids` leave the list while `cur` is open: the next drawn row, else the one before.
 *  V0.186: `all` is every row in order, so a thread in a folded section steps from its place there. */
export function rowAfter(order: string[], cur: string | null, ids: string[], all: string[] = order): string | null {
  if (!cur || !ids.includes(cur)) return null;
  const i = all.indexOf(cur);
  const drawn = new Set(order);
  const ok = (x: string) => drawn.has(x) && !ids.includes(x);
  return all.slice(i + 1).find(ok) ?? all.slice(0, Math.max(0, i)).reverse().find(ok) ?? null;
}
