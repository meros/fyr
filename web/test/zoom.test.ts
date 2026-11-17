import { test } from "node:test";
import assert from "node:assert/strict";
import { FRAME_ZOOMS, MAX_SCALE, clampView, doubleTap, stepFrameZoom, toggleView, wheelFactor, zoomAt, type View } from "../src/zoom.ts";

const stage = { width: 400, height: 800 };
// an image fitted to 400 × 300 in the stage's centre
const fitted = { width: 400, height: 300 };

test("V0.165 zoom about a point keeps that point under the finger or the pointer", () => {
  const v0: View = { scale: 1, x: 0, y: 0 };
  // the point 100 px right of the stage's centre
  const v = zoomAt(v0, 2, { x: 300, y: 400 }, stage, fitted);
  assert.equal(v.scale, 2);
  // the image point under it was +100 from the centre at scale 1; at scale 2 it sits at 2 × that + pan
  assert.equal(200 + v.x, 100);
  // vertically the zoomed image (600 px) still fits the stage (800 px), so it stays centred
  assert.equal(v.y, 0);
});

test("V0.165 the scale stays between fit (1) and MAX_SCALE", () => {
  const v0: View = { scale: 1, x: 0, y: 0 };
  assert.equal(zoomAt(v0, 0.5, { x: 200, y: 400 }, stage, fitted).scale, 1, "never smaller than fit");
  assert.equal(zoomAt(v0, 100, { x: 200, y: 400 }, stage, fitted).scale, MAX_SCALE);
});

test("V0.165 pan is clamped: the image never leaves an empty edge inside the stage", () => {
  // at scale 2 the image is 800 × 600: 200 px spare each side horizontally; vertically it fits (600 < 800) so it centres
  const v = clampView({ scale: 2, x: 500, y: 300 }, stage, fitted);
  assert.equal(v.x, 200);
  assert.equal(v.y, 0);
  assert.deepEqual(clampView({ scale: 1, x: 40, y: -10 }, stage, fitted), { scale: 1, x: 0, y: 0 }, "fit is centred");
});

test("V0.165 double tap or double click toggles fit and 100 %", () => {
  // natural 1600 px wide, fitted 400: 100 % is scale 4, centred on the point
  const z = toggleView({ scale: 1, x: 0, y: 0 }, { x: 200, y: 400 }, stage, fitted, 1600);
  assert.equal(z.scale, 4);
  assert.deepEqual(toggleView(z, { x: 200, y: 400 }, stage, fitted, 1600), { scale: 1, x: 0, y: 0 }, "back to fit");
  // an image no bigger than the stage zooms 2.5×
  assert.equal(toggleView({ scale: 1, x: 0, y: 0 }, { x: 200, y: 400 }, stage, fitted, 300).scale, 2.5);
});

test("V0.165 a double tap: two short taps close in time and place", () => {
  assert.equal(doubleTap({ t: 0, x: 10, y: 10 }, { t: 250, x: 18, y: 14 }), true);
  assert.equal(doubleTap({ t: 0, x: 10, y: 10 }, { t: 400, x: 10, y: 10 }), false, "too slow");
  assert.equal(doubleTap({ t: 0, x: 10, y: 10 }, { t: 100, x: 80, y: 10 }), false, "too far");
  assert.equal(doubleTap(null, { t: 100, x: 80, y: 10 }), false);
});

test("V0.165 wheel: up zooms in, down out, a trackpad pinch (ctrl) is finer; lines count as 16 px", () => {
  assert.ok(wheelFactor(-100, 0, false) > 1);
  assert.ok(wheelFactor(100, 0, false) < 1);
  assert.ok(Math.abs(Math.log(wheelFactor(-10, 0, true))) > Math.abs(Math.log(wheelFactor(-10, 0, false))), "ctrl+wheel (pinch) moves more per pixel");
  assert.equal(wheelFactor(-3, 1, false), wheelFactor(-48, 0, false));
});

test("V0.165 an HTML page's zoom in fullscreen steps through fixed levels", () => {
  assert.equal(stepFrameZoom(1, 1), 1.25);
  assert.equal(stepFrameZoom(1, -1), 0.8);
  assert.equal(stepFrameZoom(FRAME_ZOOMS[FRAME_ZOOMS.length - 1], 1), FRAME_ZOOMS[FRAME_ZOOMS.length - 1]);
  assert.equal(stepFrameZoom(FRAME_ZOOMS[0], -1), FRAME_ZOOMS[0]);
  assert.equal(stepFrameZoom(1.1, 1), 1.25, "between levels: the next one up");
});
