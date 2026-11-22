// The shared span words: ago, span and minuteOf round down, so a span never reads longer than it was.
import { test } from "node:test";
import assert from "node:assert/strict";
import { ago, minuteOf, relTime, span } from "../src/format.ts";

test("ago rounds down and never goes negative", () => {
  assert.equal(ago(-5), "0 s");
  assert.equal(ago(59_999), "59 s");
  assert.equal(ago(119_000), "1 min");
  assert.equal(ago(3600_000 * 5 + 1), "5 h");
  assert.equal(ago(86400_000 * 3), "3 d");
});

test("span says hours with their minutes", () => {
  assert.equal(span(40_900), "40 s");
  assert.equal(span(14 * 60_000), "14 min");
  assert.equal(span(65 * 60_000), "1 h 5 min");
  assert.equal(span(2 * 3600_000), "2 h");
});

test("relTime and minuteOf", () => {
  const now = Date.parse("2026-01-01T00:10:00Z");
  assert.equal(relTime("2026-01-01T00:00:00Z", now), "10 min");
  assert.equal(relTime("nope", now), "");
  assert.equal(minuteOf(now), minuteOf(now + 59_000));
  assert.notEqual(minuteOf(now), minuteOf(now + 60_000));
});
