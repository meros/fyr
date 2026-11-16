// SPEC V0.119 (4): the one reorder control's pure part. The hook and the two faces (a grip on the desktop, a Reorder
// mode with ↑ and ↓ on the phone) are in settings-rows.tsx.

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
