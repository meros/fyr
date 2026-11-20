// V0.arrived-from: after an answer moves the owner on to the next thread that needs them, the new thread says so
// and offers the way back. Pure; arrived-bar.tsx draws it on both fronts.

export interface AnswerMove {
  /** the thread just answered */
  from: string;
  fromTitle: string;
  /** the thread the answer moved on to */
  to: string;
  /** threads that still need the owner after this one */
  left: number;
  /** the owner has been on `to` (the bar shows only there, and only until they leave it) */
  reached?: boolean;
}

/** The arrival for a move from `from` to `to` among the threads that need the owner (`needs`, ids in order). */
export function arrive(from: { id: string; title: string }, to: string, needs: string[]): AnswerMove {
  return { from: from.id, fromTitle: from.title || "Untitled", to, left: needs.filter((id) => id !== from.id && id !== to).length };
}

/** The arrival after `open` is on screen: kept on `to`; kept on `from` only before `to` was reached (a front may
 *  still show `from` for a frame while it opens `to`); dropped anywhere else, so it never shows twice. */
export function arrivalOnOpen(a: AnswerMove | null, open: string | null): AnswerMove | null {
  if (!a) return null;
  if (open === a.to) return a.reached ? a : { ...a, reached: true };
  if (open === a.from && !a.reached) return a;
  return null;
}

/** The line the bar shows. */
export function arrivalText(a: AnswerMove): { done: string; more: string } {
  return { done: `Answered “${a.fromTitle}”`, more: a.left === 0 ? "moved on to the last one that needs you" : `moved on to this one · ${a.left} more ${a.left === 1 ? "needs" : "need"} you` };
}
