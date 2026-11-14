import { test } from "node:test";
import assert from "node:assert/strict";
import { KEYBOARD_MIN_PX, VIEWPORT_VV_EVENTS, VIEWPORT_WINDOW_EVENTS, rereadLater, viewportVars } from "../src/viewport.ts";

test("the app height is the visual viewport's, never a stored one", () => {
  assert.deepEqual(viewportVars({ innerHeight: 844, vvHeight: 844, vvOffsetTop: 0 }), { height: "844px", top: "0px", keyboard: false });
  // Resume after the size changed: the next read gives the new size.
  assert.deepEqual(viewportVars({ innerHeight: 717, vvHeight: 717, vvOffsetTop: 0 }), { height: "717px", top: "0px", keyboard: false });
});

test("keyboard: a shorter visual viewport, scrolled down on iOS", () => {
  assert.deepEqual(viewportVars({ innerHeight: 844, vvHeight: 506.4, vvOffsetTop: 127 }), { height: "506px", top: "127px", keyboard: true });
  assert.equal(viewportVars({ innerHeight: 844, vvHeight: 844 - KEYBOARD_MIN_PX, vvOffsetTop: 0 }).keyboard, false, "a toolbar is not a keyboard");
  assert.equal(viewportVars({ innerHeight: 844, vvHeight: 844, vvOffsetTop: -5 }).top, "0px");
});

test("without visualViewport: 100dvh", () => {
  assert.deepEqual(viewportVars({ innerHeight: 844 }), { height: "100dvh", top: "0px", keyboard: false });
  assert.deepEqual(viewportVars({ innerHeight: 844, vvHeight: 0 }), { height: "100dvh", top: "0px", keyboard: false });
});

test("every event that can change the size re-reads it; a resume re-reads again on later frames", () => {
  for (const e of ["resize", "pageshow", "orientationchange", "focusin", "focusout"]) assert.ok((VIEWPORT_WINDOW_EVENTS as readonly string[]).includes(e), e);
  assert.deepEqual([...VIEWPORT_VV_EVENTS], ["resize", "scroll"]);
  assert.equal(rereadLater("visibilitychange", "visible"), true);
  assert.equal(rereadLater("visibilitychange", "hidden"), false);
  assert.equal(rereadLater("pageshow"), true);
  assert.equal(rereadLater("orientationchange"), true);
  assert.equal(rereadLater("scroll"), false);
});
