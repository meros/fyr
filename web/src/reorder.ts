// SPEC V0.119 (4), V0.reorder-one: the one reorder control's pure part. The hook and its faces (a grip on the desktop, a
// Reorder mode with ↑, ↓ and a handle on the phone) are in reorder-ui.tsx.

/** The list with the item at `from` moved to `to`; out of range or the same place: an unchanged copy. */
export function move<T>(ids: readonly T[], from: number, to: number): T[] {
  const out = [...ids];
  if (from < 0 || to < 0 || from >= ids.length || to >= ids.length || from === to) return out;
  const [x] = out.splice(from, 1);
  out.splice(to, 0, x);
  return out;
}

/** One step up (-1) or down (1); it stops at the ends. */
export function moveBy<T>(ids: readonly T[], id: T, d: -1 | 1): T[] {
  const i = ids.indexOf(id);
  return i < 0 ? [...ids] : move(ids, i, i + d);
}

/** The grip's accessible name: "Move Opus 5.5, 1 of 4". */
export function moveLabel(name: string, i: number, n: number): string {
  return `Move ${name}, ${i + 1} of ${n}`;
}

// ---- the drag's geometry (V0.reorder-one), measured once when the row is lifted ----

/** Where the held row lands: the number of the other rows whose middle is above the held row's middle. */
export function dropIndex(mids: readonly number[], from: number, center: number): number {
  let to = 0;
  for (let j = 0; j < mids.length; j++) if (j !== from && mids[j] < center) to++;
  return to;
}

/** How far row `j` slides while row `from` is held over place `to`: one step out of the way, or not at all. */
export function shiftOf(j: number, from: number, to: number, step: number): number {
  if (from < j && j <= to) return -step;
  if (to <= j && j < from) return step;
  return 0;
}

/** The held row's height and the gap after it: how far the others slide to make room. */
export function stepOf(tops: readonly number[], bottoms: readonly number[], from: number): number {
  const h = bottoms[from] - tops[from];
  if (from + 1 < tops.length) return h + Math.max(0, tops[from + 1] - bottoms[from]);
  if (from > 0) return h + Math.max(0, tops[from] - bottoms[from - 1]);
  return h;
}

/** How far the held row travels to its new place when it is let go. */
export function slotOffset(tops: readonly number[], bottoms: readonly number[], from: number, to: number): number {
  if (to > from) return bottoms[to] - bottoms[from];
  if (to < from) return tops[to] - tops[from];
  return 0;
}

/** Near an edge of the list's scroller the list scrolls by itself, faster the closer the pointer is (px per frame). */
export function edgeSpeed(y: number, top: number, bottom: number, edge = 48, max = 16): number {
  const room = Math.min(edge, (bottom - top) / 4);
  if (room <= 0) return 0;
  if (y < top + room) return -Math.ceil(max * Math.min(1, (top + room - y) / room));
  if (y > bottom - room) return Math.ceil(max * Math.min(1, (y - (bottom - room)) / room));
  return 0;
}
