// SPEC 0.5 V0.44: provider quota words: the summary line, colour from 80 %, the rail figure, details.
import { test } from "node:test";
import assert from "node:assert/strict";
import { money, quotaDetail, quotaParts, quotaSummary, railFigure, type Quota } from "../src/quota.ts";

const now = Date.parse("2026-11-14T10:00:00Z");
const claude: Quota = {
  profile: "claudeAgent",
  kind: "anthropic-oauth",
  windows: [
    { name: "5 h", usedPct: 34.2, resetsAt: "2026-11-14T13:00:00Z" },
    { name: "7 d", usedPct: 61 },
  ],
  updatedAt: "2026-11-14T09:58:00Z",
};

test("the summary line reads like the spec", () => {
  assert.equal(quotaSummary(claude, "Claude"), "Claude 5 h 34 % · 7 d 61 %");
  const ds: Quota = { profile: "ds", kind: "deepseek", balance: { amount: 12.4, currency: "CNY" } };
  assert.equal(quotaSummary(ds, "DeepSeek"), "DeepSeek ¥12.40 left");
  const or: Quota = { profile: "or", kind: "openrouter", balance: { spent: 3.25, currency: "USD" } };
  assert.equal(quotaSummary(or, "Router"), "Router $3.25 used");
  assert.equal(quotaSummary({ profile: "x", kind: "anthropic-oauth", error: "Login expired; run this profile once" }, "Work"), "Work: Login expired; run this profile once");
  assert.equal(quotaSummary({ profile: "x", kind: "deepseek" }, "D"), "D: not read yet");
});

test("only a window at 80 % or more is hot", () => {
  const q: Quota = { ...claude, windows: [{ name: "5 h", usedPct: 79.4 }, { name: "7 d", usedPct: 80 }] };
  assert.deepEqual(quotaParts(q).map((p) => p.hot), [false, true]);
  assert.deepEqual(railFigure(q), { text: "80%", hot: true });
  assert.deepEqual(railFigure(claude), { text: "61%", hot: false });
});

test("money and the rail balance", () => {
  assert.equal(money(12.4, "USD"), "$12.40");
  assert.equal(money(5, "SEK"), "5.00 SEK");
  assert.deepEqual(railFigure({ profile: "d", kind: "deepseek", balance: { amount: 12.9, currency: "CNY" } }), { text: "¥12", hot: false });
  assert.equal(railFigure({ profile: "d", kind: "openrouter", balance: { spent: 1, currency: "USD" } }), null);
});

test("details: resets in local time, freshness, the stale note and the next try", () => {
  const d = quotaDetail({ ...claude, stale: true, error: "Rate limited by the provider; trying again later", retryAt: "2026-11-14T10:20:00Z" }, "Claude", now);
  assert.match(d[0], /^Claude 5 h: 34 % used, resets \d\d:\d\d$/);
  assert.equal(d[1], "Claude 7 d: 61 % used");
  assert.equal(d[2], "Updated 2 min ago");
  assert.equal(d[3], "Stale: showing the last data read");
  assert.equal(d[4], "Rate limited by the provider; trying again later");
  assert.match(d[5], /^Next try \d\d:\d\d$/);
});
