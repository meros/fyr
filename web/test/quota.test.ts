// SPEC 0.5 V0.44: provider quota words: the summary line, colour from 80 %, the busiest part, details.
import { test } from "node:test";
import assert from "node:assert/strict";
import { cardLines } from "../src/card.ts";
import { CARD_WORD, cardState, money, quotaCard, windowName, quotaParts, quotaSummary, quotaWakeOptions, busiestFigure, type Quota } from "../src/quota.ts";

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
  // V0.quota-in-thread: the phone's header has room for one figure: the busiest window's share
  assert.deepEqual(busiestFigure(q), { text: "80 %", hot: true });
  assert.deepEqual(busiestFigure(claude), { text: "61 %", hot: false });
  assert.deepEqual(busiestFigure({ ...claude, windows: [{ name: "5 h", usedPct: 86 }, { name: "7 d", usedPct: 40 }] }), { text: "86 %", hot: true });
});

test("money and the balance", () => {
  assert.equal(money(12.4, "USD"), "$12.40");
  assert.equal(money(5, "SEK"), "5.00 SEK");
  assert.deepEqual(busiestFigure({ profile: "d", kind: "deepseek", balance: { amount: 12.9, currency: "CNY" } }), { text: "¥12.90 left", hot: false });
  assert.equal(busiestFigure({ profile: "d", kind: "anthropic-oauth", error: "Login expired; run this provider once" }), null);
});

// V0.hover-cards: the quota's hover card (and the phone's Details): a verdict, each window with its reset time, the
// problem and its next try, how old the reading is.
test("the card: windows by their names, resets in local time, the problem once, the age", () => {
  const c = quotaCard({ ...claude, stale: true, error: "Rate limited by the provider; trying again later", retryAt: "2026-11-14T10:20:00Z" }, "Claude", now);
  const d = cardLines(c);
  assert.equal(c.head, "Claude");
  assert.equal(d[0], "Plenty left");
  assert.match(d[1], /^5 hours: 34 % used · resets \d\d:\d\d$/);
  assert.equal(d[2], "7 days: 61 % used");
  assert.match(d[3], /^Problem: Rate limited by the provider; trying again later · next try \d\d:\d\d$/);
  assert.equal(c.source, "Read 2 min ago · may be out of date");
  assert.equal(windowName("5 h"), "5 hours");
  assert.equal(windowName("1 d"), "1 day");
  assert.equal(windowName("Opus week"), "Opus week");
});

test("the card's verdict: running low from 80 %, used up at 100 %, the error's word without data", () => {
  const low = quotaCard({ ...claude, windows: [{ name: "5 h", usedPct: 86, resetsAt: "2026-11-14T12:00:00Z" }] }, "Claude", now);
  assert.equal(low.verdict, "Running low");
  assert.equal(low.level, "warm");
  assert.match(low.summary!, /^The 5 hours window is 86 % used; it resets \d\d:\d\d\.$/);
  const out = quotaCard({ ...claude, windows: [{ name: "7 d", usedPct: 100 }] }, "Claude", now);
  assert.deepEqual([out.verdict, out.level, out.summary], ["Used up", "hot", "New runs on this account wait until the window resets."]);
  const bare = quotaCard({ profile: "x", kind: "anthropic-oauth", error: "Login expired; run this provider once" }, "Work", now);
  assert.deepEqual(cardLines(bare), ["Login expired", "Problem: Login expired; run this provider once"]);
  const session = quotaCard({ profile: "x", kind: "anthropic-oauth", source: "session" }, "Work", now);
  assert.deepEqual([session.verdict, session.summary], ["After a run", "Usage shows after the next run on this account."]);
  assert.equal(quotaCard({ profile: "d", kind: "deepseek", balance: { amount: 12.9, currency: "CNY" } }, "DeepSeek", now).sections[0].rows[0].value, "¥12.90 left");
});

// SPEC 0.5 V0.77 (1), #65: the card says each thing once. Today it repeated the same sentence three times (the line,
// its own detail, and the profile's line above), all while the badge said Ready.
test("the card says one error and its next try, once", () => {
  const q: Quota = { profile: "claudeAcme", kind: "anthropic-oauth", windows: [{ name: "5 h", usedPct: 34 }], stale: true, error: "Rate limited by the provider; trying again later", retryAt: "2026-11-14T10:31:00Z" };
  assert.equal(quotaSummary(q, "Claude (acme)"), "Claude (acme) 5 h 34 %");
  const d = cardLines(quotaCard(q, "Claude (acme)", now));
  assert.deepEqual(d.filter((l) => /Rate limited/.test(l)).length, 1, `one mention: ${d.join(" | ")}`);
  assert.equal(d.filter((l) => /next try/i.test(l)).length, 1);
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
  assert.equal(quotaCard(q, "Claude (acme)", now).source, "From the last run, 12 min ago");
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

test("V0.quota-reset-wake: wake options enable only fresh windows with a future reset time", () => {
  const opts = quotaWakeOptions(claude, undefined, now);
  assert.equal(opts.length, 2);
  assert.equal(opts[0].window, "5 h");
  assert.equal(opts[0].disabledReason, undefined);
  assert.equal(opts[1].window, "7 d");
  assert.match(opts[1].disabledReason ?? "", /No reset time known/);

  // Armed window carries its rule id so the control cancels it.
  const armed = quotaWakeOptions(claude, [{ rule: "w_1", at: "2026-11-14T13:00:00Z", provider: "claudeAlt", window: "5 h" }], now);
  assert.equal(armed[0].armedRule, "w_1");

  // Stale reading disables every window.
  const stale = quotaWakeOptions({ ...claude, stale: true }, undefined, now);
  assert.match(stale[0].disabledReason ?? "", /stale/i);

  // Token account without a reset time names sessions.
  const tok = quotaWakeOptions({ profile: "tok", kind: "anthropic-oauth", source: "session", windows: [{ name: "5 h", usedPct: 90 }] }, undefined, now);
  assert.match(tok[0].disabledReason ?? "", /token account/i);
});
