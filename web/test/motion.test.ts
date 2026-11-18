// V0.native-feel: the numbers behind the motion.
import assert from "node:assert/strict";
import test from "node:test";
import { addSample, frameMs, pathUntil, releaseVelocity, rubberBand, SHEET_SPRING, sheetCloses, SNAP_SPRING, springPath } from "../src/motion.ts";

test("a spring starts where it is, ends exactly at its target and rests within a second and a half", () => {
  const p = springPath(600, 0, 0, SHEET_SPRING);
  assert.equal(p[0], 600);
  assert.equal(p[p.length - 1], 0);
  assert.ok(frameMs(p) <= 1500);
});

test("a sheet settling does not visibly overshoot", () => {
  assert.ok(Math.min(...springPath(600, 0, 0, SHEET_SPRING)) > -6);
});

test("the finger's speed carries into the spring", () => {
  const still = springPath(100, 0, 0, SNAP_SPRING);
  const thrown = springPath(100, 0, 2000, SNAP_SPRING);
  assert.ok(thrown[1] > still[1], "moving away, it keeps going away first");
  assert.ok(Math.max(...thrown) > 100);
});

test("what leaves stops at the edge", () => {
  const p = pathUntil(springPath(0, 500, 0, SHEET_SPRING), 400);
  assert.ok(p[p.length - 1] >= 400);
  assert.ok(p[p.length - 2] < 400);
});

test("rubber band: resists more the further it goes and never reaches the range", () => {
  assert.ok(rubberBand(100, 600) < 55);
  assert.ok(rubberBand(200, 600) - rubberBand(100, 600) < rubberBand(100, 600));
  assert.ok(rubberBand(100000, 600) < 600);
  assert.equal(rubberBand(-100, 600), -rubberBand(100, 600));
});

test("the release speed comes from the last 100 ms; a finger that stopped has none", () => {
  let s = addSample([], 0, 0);
  s = addSample(s, 50, 50);
  s = addSample(s, 100, 100);
  assert.equal(releaseVelocity(s, 100), 1000);
  assert.equal(releaseVelocity(s, 400), 0);
  s = addSample(s, 300, 100);
  assert.equal(s.length, 1, "old samples are dropped");
});

test("a sheet closes past half its height or on a flick down, never on a flick up", () => {
  assert.equal(sheetCloses(320, 0, 600), true);
  assert.equal(sheetCloses(280, 0, 600), false);
  assert.equal(sheetCloses(80, 1500, 600), true);
  assert.equal(sheetCloses(500, -500, 600), false);
});
