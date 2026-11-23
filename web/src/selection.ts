// V0.109: the marked row in the thread list is the open thread, whatever path opened it (a row click, a link from
// another thread, a notification, ?open=, the keyboard, a new thread). The list keeps no cursor of its own.
// V0.186: a folded section stays folded. V0.192: the current thread's row still shows, alone, in its place (Guest).
import { asObject } from "./decode.ts";
import { AUTO_HIDE_SECTIONS } from "./inbox-autohide.ts";
import { SECTIONS, isOpen, matchesQuery, sectionOf, unpinned, type SectionKey, type Sections } from "./store.ts";
import type { Thread } from "./types.ts";

type Open = Partial<Record<SectionKey, boolean>>;

/** V0.186: the folds, remembered per device. */
export const SECTION_OPEN_KEY = "fyr.sectionOpen";

/** V0.186: what the device stored; anything else counts as nothing. An open Snoozed or Settled is left out: it closes
 *  10 s after the owner leaves it (V0.196), and that time does not survive a reload. */
export function decodeSectionOpen(v: unknown): Open | undefined {
  const o = asObject(v);
  if (!o) return undefined;
  const out: Open = {};
  for (const { key } of SECTIONS) {
    const x = o[key];
    if (typeof x === "boolean" && !(AUTO_HIDE_SECTIONS.includes(key) && x)) out[key] = x;
  }
  return out;
}

/** The row the list marks: the open thread; with nothing open, the last thread opened (where j/k starts). */
export function markedRow(openId: string | null, lastOpen: string | null): string | null {
  return openId ?? lastOpen;
}

/**
 * V0.186: the open section that must show all of itself to draw the row of `id`, because the row is past its limit.
 * `limits` is what each section draws now (0 when folded). null: the row is drawn, it is in a folded section (which
 * stays folded and draws it as the guest), or no section holds it (search hides it, or a locked quiet project).
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

/** V0.186: the folded section that holds the current thread. null when its section is open, it is pinned (the pinned
 *  block does not fold), or no section holds it. V0.waiting-folds: Waiting to run folds too. */
export function foldedRow(sec: Sections, id: string | null, open: Open): { section: SectionKey; thread: Thread } | null {
  if (!id || sec.pinned.some((t) => t.id === id)) return null;
  for (const { key } of SECTIONS) {
    const thread = unpinned(sec[key], key).find((t) => t.id === id);
    if (!thread) continue;
    return !isOpen(key, open) ? { section: key, thread } : null;
  }
  return null;
}

/** V0.192: why the current thread's row is in the list although the list would not draw it. */
export type GuestWhy = "filter" | "quiet" | "folded";

/** V0.192: the one row that punches through: the current thread, hidden by a filter, a quiet project or a fold. */
export interface Guest {
  id: string;
  why: GuestWhy;
}

/** V0.192: the row's tag, and the longer line on hover. */
export const GUEST_TEXT: Record<GuestWhy, { tag: string; hint: string }> = {
  filter: { tag: "Outside filter", hint: "The filters hide this thread. It stays here while it is the current thread." },
  quiet: { tag: "Quiet project", hint: "Its project is in quiet hours. It stays here while it is the current thread." },
  folded: { tag: "In folded section", hint: "This section is folded. The row stays here while it is the current thread." },
};

/** V0.192: why the current thread must be added to the list's threads; null when it is there already or stays out.
 *  `allowed`: a quiet project's thread only once admitted (V0.114). Search still decides: a typed query that the
 *  thread does not match hides it. */
export function hiddenWhy(t: Thread | undefined, o: { shown: Record<string, Thread>; filtered: Record<string, Thread>; query: string; allowed: boolean }): "filter" | "quiet" | null {
  if (!t || o.shown[t.id] || !sectionOf(t) || !o.allowed) return null;
  if (o.query.trim() && !matchesQuery(t, o.query)) return null;
  return o.filtered[t.id] ? "quiet" : "filter";
}

/** V0.192: the guest row, given the sections that hold it (`rows`) and `hidden` from hiddenWhy. V0.phone-no-guest: the
 *  phone marks no row at all (V0.109 (5)), so it never has a current thread to punch through with: `phone` leaves the
 *  filters, quiet hours and the folds alone there. */
export function guestOf(rows: Sections, id: string | null, hidden: "filter" | "quiet" | null, open: Open, phone = false): Guest | null {
  if (phone || !id) return null;
  if (hidden) return { id, why: hidden };
  return foldedRow(rows, id, open) ? { id, why: "folded" } : null;
}

/** V0.192: what a section draws: its rows up to the limit when open, and the guest in its own place, folded or cut. */
export function drawnRows(list: Thread[], open: boolean, limit: number, guest: Guest | null): Thread[] {
  return list.filter((t, i) => (open && i < limit) || t.id === guest?.id);
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

/** V0.settle-leaves: where the desktop goes when the owner settles `ids` while `cur` is the marked (open) thread. From
 *  the list and from the thread pane alike the settled thread is let go: the next drawn row (else the one before), or
 *  "home" (the status screen, V0.status-home) when no row is left. "stay": the marked thread is not among `ids`. */
export function afterSettle(order: string[], cur: string | null, ids: string[], all: string[] = order): string | "home" | "stay" {
  if (!cur || !ids.includes(cur)) return "stay";
  return rowAfter(order, cur, ids, all) ?? "home";
}
