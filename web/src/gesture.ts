// Row swipes on the phone (design/mobile/README.md, section 2). Pure: the view feeds pointer
// positions in and acts on the result.
//
// - A swipe starts only after 12 px of horizontal movement at an angle under 30 degrees, so a
//   vertical scroll wins. The left 24 px stay free for the iOS Back edge gesture.
// - Right settles (commits at 50 % of the row width), left opens the snooze sheet (at 30 %).
// - An unavailable direction shows its reason and snaps back.

import type { Thread } from "./types.ts";

export const SWIPE_START_PX = 12;
export const SWIPE_MAX_ANGLE = 30;
export const EDGE_PX = 24;
export const SETTLE_AT = 0.5;
export const SNOOZE_AT = 0.3;
export const LONG_PRESS_MS = 400;
export const LONG_PRESS_SLOP = 8;
/** V0.native-feel: a quick flick commits short of the distance, once the row has moved this far. */
export const FLICK_PX_S = 700;
export const FLICK_MIN_PX = 40;

export type Phase = "idle" | "pending" | "swipe" | "scroll" | "edge";

export interface Swipe {
  phase: Phase;
  x0: number;
  y0: number;
  dx: number;
}

export const idle: Swipe = { phase: "idle", x0: 0, y0: 0, dx: 0 };

/** `x` is relative to the screen's left edge. */
export function swipeStart(x: number, y: number): Swipe {
  if (x < EDGE_PX) return { phase: "edge", x0: x, y0: y, dx: 0 };
  return { phase: "pending", x0: x, y0: y, dx: 0 };
}

export function swipeMove(s: Swipe, x: number, y: number): Swipe {
  if (s.phase === "edge" || s.phase === "scroll" || s.phase === "idle") return s;
  const dx = x - s.x0;
  const dy = y - s.y0;
  if (s.phase === "swipe") return { ...s, dx };
  const ax = Math.abs(dx);
  const ay = Math.abs(dy);
  if (Math.hypot(ax, ay) < SWIPE_START_PX) return s;
  const angle = (Math.atan2(ay, ax) * 180) / Math.PI;
  if (ax >= SWIPE_START_PX && angle < SWIPE_MAX_ANGLE) return { ...s, phase: "swipe", dx };
  return { ...s, phase: "scroll", dx: 0 };
}

export interface Side {
  ok: boolean;
  /** the exact Action name ("Settle", "Settle when done") or the reason it is unavailable */
  label: string;
}

export interface SwipeAvail {
  right: Side;
  left: Side;
}

/** What a row's swipes do, from the Action registry's availability (U3). */
export function swipeAvail(t: Thread): SwipeAvail {
  const un = t.unavailable ?? {};
  const settleReason = un["thread.settle"];
  const snoozeReason = un["thread.snooze"];
  return {
    right: settleReason ? { ok: false, label: shortReason(settleReason, t) } : { ok: true, label: settleLabel(t) },
    left: snoozeReason ? { ok: false, label: snoozeReason } : { ok: true, label: "Snooze…" },
  };
}

/** V0.203: Settle settles now, on every thread; on a running one that stops the run, and the label says so. */
export function settleLabel(t: Pick<Thread, "running">): string {
  return t.running ? "Settle and stop run" : "Settle";
}

function shortReason(reason: string, t: Thread): string {
  if (t.attention.state === "asking") return "Answer first";
  return reason;
}

export type SwipeResult = { kind: "settle" } | { kind: "snooze" } | { kind: "none"; reason?: string };

/** Past the distance that commits on release; the row says so while the finger is still down. */
export function swipeArmed(s: Swipe, width: number, avail: SwipeAvail): boolean {
  if (s.phase !== "swipe" || width <= 0) return false;
  const f = s.dx / width;
  return f > 0 ? avail.right.ok && f >= SETTLE_AT : avail.left.ok && -f >= SNOOZE_AT;
}

/** `vx`: the finger's speed at release (px/s). A flick along the swipe commits; a flick back cancels. */
export function swipeEnd(s: Swipe, width: number, avail: SwipeAvail, vx = 0): SwipeResult {
  if (s.phase !== "swipe" || width <= 0) return { kind: "none" };
  const right = s.dx > 0;
  const side = right ? avail.right : avail.left;
  if (!side.ok) return { kind: "none", reason: side.label };
  const along = right ? vx : -vx;
  if (along <= -FLICK_PX_S) return { kind: "none" };
  const flick = along >= FLICK_PX_S && Math.abs(s.dx) >= FLICK_MIN_PX;
  if (!flick && !swipeArmed(s, width, avail)) return { kind: "none" };
  return { kind: right ? "settle" : "snooze" };
}

/** How far the row moves: unavailable directions resist (a short rubber band). */
export function swipeOffset(s: Swipe, avail: SwipeAvail): number {
  if (s.phase !== "swipe") return 0;
  const side = s.dx > 0 ? avail.right : avail.left;
  return side.ok ? s.dx : Math.sign(s.dx) * Math.min(Math.abs(s.dx) * 0.35, 96);
}

/** Long press: fires after 400 ms unless the finger moved more than 8 px. */
export function longPressCancelled(x0: number, y0: number, x: number, y: number): boolean {
  return Math.hypot(x - x0, y - y0) > LONG_PRESS_SLOP;
}
