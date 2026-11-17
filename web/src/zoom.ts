// Zoom and pan of an image in the preview viewer, and the zoom steps of an HTML page in full screen (SPEC 0.5 V0.165).
// Pure. The image is drawn fitted and centred in its stage; a view moves it by (x, y) and scales it about its centre.

export interface View {
  scale: number;
  x: number;
  y: number;
}
export interface Size {
  width: number;
  height: number;
}
export interface Point {
  x: number;
  y: number;
}

/** 1 is fit; the image zooms up to 8 × its fitted size. */
export const MAX_SCALE = 8;
export const FIT: View = { scale: 1, x: 0, y: 0 };

/** Keep the image on the stage: a side smaller than the stage is centred, a larger one leaves no empty edge. */
export function clampView(v: View, stage: Size, fitted: Size): View {
  const scale = Math.min(MAX_SCALE, Math.max(1, v.scale));
  const lim = (img: number, box: number) => Math.max(0, (img * scale - box) / 2);
  const lx = lim(fitted.width, stage.width);
  const ly = lim(fitted.height, stage.height);
  const c = (n: number, l: number) => (l === 0 ? 0 : Math.min(l, Math.max(-l, n)));
  return { scale, x: c(v.x, lx), y: c(v.y, ly) };
}

/** Zoom by `factor` about `pt` (stage coordinates): the image point under it stays under it, as far as the edges allow. */
export function zoomAt(v: View, factor: number, pt: Point, stage: Size, fitted: Size): View {
  const s2 = Math.min(MAX_SCALE, Math.max(1, v.scale * factor));
  const qx = pt.x - stage.width / 2;
  const qy = pt.y - stage.height / 2;
  const px = (qx - v.x) / v.scale;
  const py = (qy - v.y) / v.scale;
  return clampView({ scale: s2, x: qx - s2 * px, y: qy - s2 * py }, stage, fitted);
}

/** Double tap or double click: a zoomed image goes back to fit; at fit it goes to 100 % (its own pixels), or 2.5 × when it is not bigger than fitted. */
export function toggleView(v: View, pt: Point, stage: Size, fitted: Size, naturalWidth: number): View {
  if (v.scale > 1.01) return { ...FIT };
  const full = fitted.width > 0 ? naturalWidth / fitted.width : 0;
  const target = full >= 1.2 ? Math.min(MAX_SCALE, full) : 2.5;
  return zoomAt(v, target / v.scale, pt, stage, fitted);
}

export interface Tap {
  t: number;
  x: number;
  y: number;
}
/** Two taps make a double tap within 300 ms and 30 px. */
export function doubleTap(prev: Tap | null, cur: Tap): boolean {
  return !!prev && cur.t - prev.t < 300 && Math.hypot(cur.x - prev.x, cur.y - prev.y) < 30;
}

/** The zoom factor of one wheel event: up zooms in. A trackpad pinch arrives as ctrl+wheel with small deltas, so it moves more per pixel. */
export function wheelFactor(deltaY: number, deltaMode: number, ctrl: boolean): number {
  const px = deltaMode === 1 ? deltaY * 16 : deltaMode === 2 ? deltaY * 400 : deltaY;
  return Math.exp(-px * (ctrl ? 0.01 : 0.002));
}

/** An HTML page's zoom levels in full screen (the page lays out at width / zoom and is scaled up). */
export const FRAME_ZOOMS = [0.5, 0.67, 0.8, 1, 1.25, 1.5, 2, 3];
export function stepFrameZoom(z: number, dir: 1 | -1): number {
  if (dir > 0) return FRAME_ZOOMS.find((l) => l > z + 1e-6) ?? FRAME_ZOOMS[FRAME_ZOOMS.length - 1];
  return [...FRAME_ZOOMS].reverse().find((l) => l < z - 1e-6) ?? FRAME_ZOOMS[0];
}
