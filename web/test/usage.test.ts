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

// ---- V0.usage-page ----
import { decodeUsage, mergeUsage, modelLabel, overheadRows, ownCallsFoot, perDay, rangeDays, usageSearch, type HostUsage } from "../src/usage.ts";

const desk: HostUsage = {
  hostId: "h1",
  host: "desk",
  groups: [
    { key: { project: "p_1" }, input: 10, output: 5, cacheRead: 100, total: 0 },
    { key: { project: "p_2" }, input: 1, output: 1, cacheRead: 1 },
    { key: { project: "" }, input: 2 },
  ],
  names: { project: { p_1: { name: "acme/web", link: "l_web" }, p_2: { name: "acme/ops" } } },
};
const lab: HostUsage = {
  hostId: "h2",
  host: "lab",
  groups: [{ key: { project: "p_9" }, input: 20, output: 5, cacheWrite1h: 7 }],
  names: { project: { p_9: { name: "acme/web", link: "l_web" } } },
};

test("a linked project is one row across hosts; others stay their host's", () => {
  const rows = mergeUsage([desk, lab], "project");
  assert.deepEqual(
    rows.map((r) => [r.id, r.label, r.total]),
    [
      ["link:l_web", "acme/web", 147],
      ["h1/p_2", "acme/ops", 3],
      ["h1/", "No project", 2],
    ],
  );
});

test("a provider is its host's, named with the host when there are several", () => {
  const a: HostUsage = { hostId: "h1", host: "desk", groups: [{ key: { provider: "claude" }, input: 1 }], names: { provider: { claude: "Claude" } } };
  const b: HostUsage = { hostId: "h2", host: "lab", groups: [{ key: { provider: "claude" }, input: 2 }], names: { provider: { claude: "Claude" } } };
  const rows = mergeUsage([a, b], "provider");
  assert.equal(rows.length, 2);
  assert.deepEqual(rows.map((r) => r.sub), ["lab", "desk"]);
  assert.equal(mergeUsage([a], "provider")[0].sub, undefined);
});

test("a model is one row everywhere, with a short name", () => {
  const g = (input: number): HostUsage => ({ hostId: "h" + input, host: "x", groups: [{ key: { model: "claude-opus-5-5" }, input }], names: {} });
  const rows = mergeUsage([g(1), g(2)], "model");
  assert.equal(rows.length, 1);
  assert.equal(rows[0].label, "Opus 5.5");
  assert.equal(rows[0].input, 3);
  assert.equal(modelLabel("gpt-x"), "gpt-x");
  assert.equal(modelLabel(""), "Unknown");
});

test("Fyr's own calls are every source but turns", () => {
  const h: HostUsage = { hostId: "h", host: "x", groups: [{ key: { source: "turn" }, input: 9 }, { key: { source: "job:title" }, input: 1 }], names: {} };
  assert.deepEqual(overheadRows([h]).map((r) => r.label), ["Titles"]);
});

test("rangeDays ends today and perDay fills the empty days", () => {
  const now = new Date(2026, 10, 23, 1, 0).getTime();
  assert.deepEqual(rangeDays("today", now), ["2026-11-23"]);
  const week = rangeDays("7d", now);
  assert.equal(week.length, 7);
  assert.equal(week[0], "2026-11-17");
  const h: HostUsage = { hostId: "h", host: "x", groups: [{ key: { day: "2026-11-20" }, output: 4 }], names: {} };
  assert.deepEqual(perDay([h], week).filter((d) => d.total).map((d) => [d.day, d.total]), [["2026-11-20", 4]]);
});

test("the query string and a host's answer read defensively", () => {
  assert.equal(usageSearch({ group: ["model", "part"], from: "2026-11-01", where: { project: "p_1" } }), "?group=model%2Cpart&from=2026-11-01&project=p_1");
  assert.equal(usageSearch({ group: [] }), "");
  const d = decodeUsage({ groups: [{ input: 1 }] }, "h", "x");
  assert.deepEqual(d.groups[0].key, {});
  assert.deepEqual(decodeUsage(null, "h", "x").groups, []);
});

import { todayHours, todayRange } from "../src/usage.ts";

test("today's hours run from local midnight to the hour now, from the hosts' UTC hours", () => {
  const now = new Date(2026, 10, 23, 14, 32).getTime();
  const utcHour = (h: number) => new Date(2026, 10, 23, h, 0).toISOString().slice(0, 13);
  const yesterday = new Date(2026, 10, 22, 23, 0).toISOString().slice(0, 13);
  const h: HostUsage = { hostId: "h", host: "x", groups: [{ key: { hour: utcHour(9) }, output: 5 }, { key: { hour: utcHour(14) }, input: 2 }, { key: { hour: yesterday }, input: 99 }], names: {} };
  const hrs = todayHours([h], now);
  assert.equal(hrs.length, 15);
  assert.equal(hrs[0].at, "00:00");
  assert.equal(hrs[9].total, 5);
  assert.deepEqual([hrs[14].total, hrs[14].now], [2, true]);
  assert.equal(hrs.reduce((n, x) => n + x.total, 0), 7); // yesterday's hour is left out
  assert.deepEqual(todayRange(now), { from: "2026-11-22", to: "2026-11-23" });
});

import { decodeOverhead, mergeOverhead, overheadKinds } from "../src/usage.ts";

test("V0.usage-overhead: the hosts' overhead adds up by part, and says what waits for calibration", () => {
  const a = decodeOverhead({ parts: { static: { cacheRead: 100, cacheWrite: 10 }, forced: { output: 5 } }, uncalibrated: 3 }, "h1", "desk");
  const b = decodeOverhead({ parts: { static: { cacheRead: 50 }, resident: { cacheRead: 7 } } }, "h2", "lab");
  const { lines, uncalibrated } = mergeOverhead([a, b]);
  assert.deepEqual(lines.map((l) => [l.id, l.total]), [["static", 160], ["resident", 7], ["forced", 5]]);
  assert.equal(uncalibrated, 3);
  assert.equal(overheadKinds(lines[0]), "cache read 150 · cache write 10");
  assert.equal(overheadKinds({ ...lines[0], kinds: {} }), "none");
  assert.deepEqual(decodeOverhead(null, "h", "x"), { hostId: "h", host: "x", parts: {}, uncalibrated: 0 });
});

test("V0.usage-no-turns-note: a range of Fyr's own calls only says it has no agent turns", () => {
  assert.match(ownCallsFoot(268, 268), /100 % of the range's tokens\. No agent turns in this range\.$/);
  assert.match(ownCallsFoot(268, 536), /50 % of the range's tokens\.$/);
  assert.doesNotMatch(ownCallsFoot(0, 0), /No agent turns/);
});
