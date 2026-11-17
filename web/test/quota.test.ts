// SPEC 0.5 V0.44: provider quota words: the summary line, colour from 80 %, the rail figure, details.
import { test } from "node:test";
import assert from "node:assert/strict";
import { CARD_WORD, cardState, money, quotaDetail, quotaParts, quotaSummary, railFigure, railInitials, type Quota } from "../src/quota.ts";

const now = Date.parse("2026-11-14T10:00:00Z");
const claude: Quota = {
  profile: "claudeAlt",
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
  assert.equal(quotaSummary({ profile: "x", kind: "anthropic-oauth", error: "Login expired; run this provider once" }, "Work"), "Work: Login expired; run this provider once");
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

// SPEC 0.5 V0.77 (1), #65: the card says each thing once. Today it repeated the same sentence three times (the line,
// its own detail, and the profile's line above), all while the badge said Ready.
test("the card says one error and its next try, once", () => {
  const q: Quota = { profile: "claudeAcme", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 34 }], stale: true, error: "Rate limited by the provider; trying again later", retryAt: "2026-11-14T10:31:00Z" };
  assert.equal(quotaSummary(q, "Claude (acme)"), "Claude (acme) 5 h 34 %");
  const d = quotaDetail(q, "Claude (acme)", now);
  assert.deepEqual(d.filter((l) => /Rate limited/.test(l)).length, 1, `one mention: ${d.join(" | ")}`);
  assert.equal(d.filter((l) => /Next try/.test(l)).length, 1);
  // the same, with no windows: the error is the line, not the name twice plus the error
  const bare: Quota = { profile: "x", kind: "anthropic-oauth", error: "Login expired; run this provider once", retryAt: "2026-11-14T10:31:00Z" };
  assert.equal(quotaSummary(bare, "Work"), "Work: Login expired; run this provider once");
});

test("the card's badge says Ready only when nothing is wrong", () => {
  assert.equal(cardState({ profile: "x", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 34 }] }), "ready");
  assert.equal(cardState({ profile: "x", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 34 }], error: "Rate limited by the provider; trying again later", retryAt: "2026-11-14T10:31:00Z" }), "limited");
  assert.equal(cardState({ profile: "x", kind: "anthropic-oauth", error: "Login expired; run this provider once" }), "expired");
  assert.equal(cardState({ profile: "x", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 34 }], stale: true }), "stale");
  assert.equal(cardState({ profile: "x", kind: "anthropic-oauth" }), "unknown");
});

// SPEC 0.5 V0.122: a token account's windows come from its last session's rate_limit_event, never from the usage
// endpoint (a setup-token token cannot read it). The card says where the numbers come from and how old they are; with
// none yet it says when they will show, and its badge never says "Rate limited".
test("a token account shows its last session's windows with their age, or when they will show", () => {
  const q: Quota = { profile: "claudeAcme", kind: "anthropic-oauth", source: "session", windows: [{ name: "7 d", usedPct: 67 }], updatedAt: "2026-11-14T09:48:00Z" };
  assert.equal(quotaSummary(q, "Claude (acme)"), "Claude (acme) 7 d 67 %");
  const d = quotaDetail(q, "Claude (acme)", now);
  assert.ok(d.includes("From the last session, 12 min ago"), d.join(" | "));
  assert.ok(!d.some((l) => l.startsWith("Updated")), d.join(" | "));
  assert.equal(cardState(q), "ready");
  const none: Quota = { profile: "claudeAcme", kind: "anthropic-oauth", source: "session" };
  assert.equal(quotaSummary(none, "Claude (acme)"), "Claude (acme): usage shows after the next run");
  assert.equal(cardState(none), "waiting");
  assert.equal(CARD_WORD[cardState(none)], "After a run");
});

// SPEC 0.5 V0.122: the badge names the error it has; only a rate limit says "Rate limited".
test("the badge says Rate limited only for a rate limit", () => {
  assert.equal(cardState({ profile: "x", kind: "anthropic-oauth", source: "session", error: "No token yet; paste it in Settings → Providers" }), "notoken");
  assert.equal(CARD_WORD.notoken, "No token");
  assert.equal(cardState({ profile: "x", kind: "deepseek", error: "The provider failed (HTTP 502)" }), "error");
  assert.equal(cardState({ profile: "x", kind: "anthropic-oauth", error: "Rate limited by the provider; trying again later" }), "limited");
});

test("V0.152: each provider's initial on the rail tells different providers apart", () => {
  assert.deepEqual(railInitials(["Claude", "Claude (work account)", "DeepSeek", "Z.ai"]), ["C", "W", "D", "Z"]);
  assert.deepEqual(railInitials(["Claude", "Claude"]), ["C", "C"]); // one provider on two hosts
  assert.deepEqual(railInitials(["Codex", "Claude"]), ["Co", "Cl"]);
  assert.deepEqual(railInitials(["åsa", "zai"]), ["Å", "Z"]);
});
