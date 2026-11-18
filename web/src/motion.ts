// V0.native-feel: the numbers behind the app's motion. Pure: no DOM, so the tests run them directly.
// motion-ui.tsx plays them on the compositor.

export interface Spring {
  stiffness: number;
  damping: number;
}

/** A sheet settling: just under critical damping, so it lands softly and does not wobble. */
export const SHEET_SPRING: Spring = { stiffness: 380, damping: 36 };
/** Small things returning to place: a row after a swipe, a sheet that changes height. */
export const SNAP_SPRING: Spring = { stiffness: 560, damping: 38 };
/** A sheet or a row leaving: it accelerates away from the finger. */
export const EXIT_SPRING: Spring = { stiffness: 300, damping: 34 };

const FRAME_S = 1 / 60;
const SUBSTEPS = 4;
const MAX_FRAMES = 90;
const REST_PX = 0.3;
const REST_V = 8;

/** The position at every 60 Hz frame of a spring from `from` to `to`, starting at velocity `v` (px/s). The first value
 *  is `from` and the last is exactly `to`. */
export function springPath(from: number, to: number, v: number, s: Spring): number[] {
  const out = [from];
  let x = from;
  let vel = v;
  const dt = FRAME_S / SUBSTEPS;
  for (let f = 0; f < MAX_FRAMES; f++) {
    for (let i = 0; i < SUBSTEPS; i++) {
      vel += (-s.stiffness * (x - to) - s.damping * vel) * dt;
      x += vel * dt;
    }
    out.push(x);
    if (Math.abs(x - to) < REST_PX && Math.abs(vel) < REST_V) break;
  }
  out[out.length - 1] = to;
  return out;
}

/** The path up to the frame that first reaches `edge`: something leaving the screen stops when it is out of sight. */
export function pathUntil(path: number[], edge: number): number[] {
  const rising = path[path.length - 1] >= path[0];
  const i = path.findIndex((x) => (rising ? x >= edge : x <= edge));
  return i > 0 ? path.slice(0, i + 1) : path;
}

export const frameMs = (path: number[]) => Math.max(1, (path.length - 1) * FRAME_S * 1000);

/** How far something moves when pulled `over` px past its limit: the resistance of a native scroll edge. */
export function rubberBand(over: number, range: number): number {
  const c = 0.55;
  const a = Math.abs(over);
  return Math.sign(over) * ((a * range * c) / (range + c * a));
}

export interface Sample {
  t: number;
  p: number;
}

const VELOCITY_WINDOW_MS = 100;

/** Adds a sample and drops what is older than the window a release is measured over. */
export function addSample(list: Sample[], t: number, p: number): Sample[] {
  const kept = list.filter((s) => t - s.t <= VELOCITY_WINDOW_MS);
  kept.push({ t, p });
  return kept;
}

/** The finger's speed at release in px/s; 0 when it had stopped. */
export function releaseVelocity(list: Sample[], now: number): number {
  const recent = list.filter((s) => now - s.t <= VELOCITY_WINDOW_MS);
  if (recent.length < 2) return 0;
  const a = recent[0];
  const b = recent[recent.length - 1];
  return b.t > a.t ? ((b.p - a.p) / (b.t - a.t)) * 1000 : 0;
}

export const DRAG_START_PX = 6;

/** A released sheet closes when it would coast past half its height, and never while the finger flicks it up. */
export function sheetCloses(y: number, v: number, height: number): boolean {
  if (height <= 0 || v < -60) return false;
  return y + v * 0.18 > height * 0.5;
}
