// SPEC 0.5 V0.40: the thread list can be hidden; in a narrow desktop window it starts hidden.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  CONVERSATION_MIN_WIDTH,
  LIST_MIN_WIDTH,
  LIST_WIDTH_DEFAULT,
  LIST_WIDTH_MAX,
  LIST_WIDTH_MIN,
  clampListWidth,
  listShown,
  listWidthForKey,
  listWidthMax,
  parseListWidth,
} from "../src/prefs.ts";

test("the list shows by the owner's choice, else by the window width", () => {
  assert.equal(listShown(null, 1440, true), true, "wide: shown");
  assert.equal(listShown(null, LIST_MIN_WIDTH - 1, true), false, "narrow: hidden by default");
  assert.equal(listShown(true, 800, true), true, "the owner's choice wins");
  assert.equal(listShown(false, 1800, true), false);
  assert.equal(listShown(false, 1800, false), true, "no thread open: the list is all there is");
});

// SPEC V0.184: the owner sets the list's width; the window limits what is applied, never what is remembered.
test("the list's width stays between its minimum and what leaves the conversation its room", () => {
  assert.equal(listWidthMax(1920), LIST_WIDTH_MAX, "a wide window: the list's own maximum");
  assert.equal(listWidthMax(1000), 1000 - 76 - CONVERSATION_MIN_WIDTH, "a narrower one: the conversation keeps its minimum");
  assert.equal(listWidthMax(800), LIST_WIDTH_MIN, "too narrow for both: the list keeps its minimum");
  assert.equal(clampListWidth(500, 1920), 500);
  assert.equal(clampListWidth(10, 1920), LIST_WIDTH_MIN);
  assert.equal(clampListWidth(5000, 1920), LIST_WIDTH_MAX);
  assert.equal(clampListWidth(600, 1000), 444, "a stored width is clamped to the window");
  assert.equal(clampListWidth(LIST_WIDTH_DEFAULT, 800), LIST_WIDTH_MIN, "so is the default");
  assert.equal(clampListWidth(400.6, 1920), 401, "whole pixels");
});

test("a stored width is read only when it is a number", () => {
  assert.equal(parseListWidth("450"), 450);
  for (const bad of [null, "", "abc", "12px", "NaN", "Infinity", "-5", "0"]) assert.equal(parseListWidth(bad), null, String(bad));
});

test("the handle's keys", () => {
  const at = (key: string, shift = false, w = 400, win = 1920) => listWidthForKey(key, shift, w, win);
  assert.equal(at("ArrowRight"), 416);
  assert.equal(at("ArrowLeft"), 384);
  assert.equal(at("ArrowRight", true), 464);
  assert.equal(at("ArrowLeft", true), 336);
  assert.equal(at("Home"), LIST_WIDTH_MIN);
  assert.equal(at("End"), LIST_WIDTH_MAX);
  assert.equal(at("End", false, 400, 1000), 444);
  assert.equal(at("ArrowRight", false, 440, 1000), 444, "arrows stop at the limit");
  assert.equal(at("j"), null, "other keys stay the app's");
  assert.equal(at("ArrowUp"), null, "the handle moves sideways only");
});
