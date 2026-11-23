// V0.usage-ledger: the token chip's words, on both fronts.
import { test } from "node:test";
import assert from "node:assert/strict";
import { tokens, usageLabel, usageTitle } from "../src/usage.ts";

test("tokens rounds to a short count", () => {
  assert.equal(tokens(undefined), "0");
  assert.equal(tokens(950), "950");
  assert.equal(tokens(1000), "1k");
  assert.equal(tokens(1250), "1.3k");
  assert.equal(tokens(12_345), "12k");
  assert.equal(tokens(1_234_567), "1.2M");
  assert.equal(tokens(25_400_000), "25M");
  assert.equal(tokens(3_000_000_000), "3B");
});

test("no chip before a thread counted anything", () => {
  assert.equal(usageLabel(undefined), "");
  assert.equal(usageLabel({}), "");
  assert.equal(usageLabel({ total: 0 }), "");
  assert.equal(usageTitle(undefined), "");
});

test("the chip says the total and its title each kind", () => {
  const u = { input: 40, output: 5_200, thinking: 800, cacheRead: 1_200_000, cacheWrite1h: 30_000, total: 1_235_240 };
  assert.equal(usageLabel(u), "1.2M tok");
  const t = usageTitle(u);
  assert.match(t, /Input 40 · output 5\.2k \(thinking 800\)/);
  assert.match(t, /Cache read 1\.2M · cache write 30k/);
});
