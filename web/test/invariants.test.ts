// V0.load-invariants: a broken load invariant is a card in Needs you on both fronts (invariants-ui.tsx, drawn by the
// desktop list and the phone inbox) with Fix it and Dismiss; Settings → Automation lists each invariant.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { HostLoad } from "../src/host-load.ts";
import type { Invariants } from "../src/guard.ts";
import { cardHead, cardWhen, fixState, invariantCards, invariantRows, linesLabel } from "../src/invariants.ts";

const NOW = Date.parse("2026-11-19T14:32:00Z");
const iso = (minAgo: number) => new Date(NOW - minAgo * 60_000).toISOString();

const card = { id: "thaw-into-critical", text: "The guard thawed work while the agents' memory stalled critically.", at: iso(12), evidence: "thread=t_a work=undeclared reason=ended agents_stall=critical", lines: ["14:19:50 guard: froze undeclared work thread=t_a", "14:20:00 guard: thawed undeclared work thread=t_a"] };

function host(name: string, inv?: Invariants): HostLoad {
  return { host: name, cores: 8, load: [1, 1, 1], cgroups: true, threads: [], slots: { n: 2, held: [], waiting: [], guard: { short: false, max: 8, refreezeSec: 60, events: [], invariants: inv } } } as unknown as HostLoad;
}

const stats = [
  { id: "thaw-into-critical", text: "The guard thawed work while the agents' memory stalled critically.", count: 2, last: iso(12) },
  { id: "probe-silent", text: "The probe is silent.", count: 0 },
];

test("the cards of every host, the newest first, with the host named when there are several", () => {
  const older = { ...card, id: "probe-silent", at: iso(60) };
  const cs = invariantCards([host("desk", { cards: [older], stats, fixDir: "/home/o/git/fyr" }), host("lab", { cards: [card], stats })]);
  assert.deepEqual(cs.map((c) => [c.host, c.card.id, c.fixDir]), [["lab", "thaw-into-critical", null], ["desk", "probe-silent", "/home/o/git/fyr"]]);
  assert.equal(cardHead(cs[0], 2), "Load invariant broken: thaw-into-critical on lab");
  assert.equal(cardHead(cs[0], 1), "Load invariant broken: thaw-into-critical");
  assert.match(cardWhen(card, NOW), / · 12 min ago$/);
  assert.equal(linesLabel(card), "2 guard lines from the 10 minutes before");
  assert.deepEqual(invariantCards([host("desk"), host("old")]), []);
});

test("Fix it starts a thread in the Fyr checkout; without one the card says so and the button is off", () => {
  assert.deepEqual(fixState({ host: "desk", card, fixDir: "/home/o/git/fyr" }), { can: true, note: "Fix it starts a thread in /home/o/git/fyr with this evidence." });
  const none = fixState({ host: "desk", card, fixDir: null });
  assert.equal(none.can, false);
  assert.match(none.note, /no project with a Fyr checkout/);
});

test("Settings lists each invariant with its last violation and count", () => {
  const rows = invariantRows(host("desk", { cards: [card], stats }), NOW);
  assert.deepEqual(rows.map((r) => [r.id, r.open]), [["thaw-into-critical", true], ["probe-silent", false]]);
  assert.match(rows[0].desc, /^2 times · last .+, 12 min ago · in Needs you$/);
  assert.equal(rows[1].desc, "never broken");
  assert.deepEqual(invariantRows(host("old"), NOW), []);
});

test("both fronts draw the card under Needs you, with both buttons (one shared component)", () => {
  const src = (f: string) => readFileSync(join(import.meta.dirname, "..", "src", f), "utf8");
  assert.match(src("inbox.tsx"), /s\.key === "needs" && <InvariantCards phone=\{false\}/);
  assert.match(src("phone.tsx"), /s\.key === "needs" && <InvariantCards phone /);
  const ui = src("invariants-ui.tsx");
  assert.match(ui, /"host\.invariantFix"/);
  assert.match(ui, /"host\.invariantDismiss"/);
  assert.match(ui, />\s*Fix it\s*</);
  assert.match(ui, />\s*Dismiss\s*</);
  const settings = src("settings-ui.tsx") + src("settings-automation.tsx");
  assert.match(settings, /label="Load invariants"/);
  assert.match(settings, /label="Thread invariants"/);
});

test("V0.thread-invariants: thread cards name Thread, show transitions, split in Settings, and work without slots", () => {
  const tCard = {
    id: "thread-phantom-running",
    kind: "thread" as const,
    text: "A thread is marked Running, but the agent manager has no live turn for it.",
    at: iso(5),
    evidence: "thread=t_x engine.running=true agent.live=false",
    lines: ["14:26:00 run.started (observer:process) seen→working running=true"],
  };
  const mixedStats = [
    ...stats.map((s) => ({ ...s, kind: "load" as const })),
    { id: "thread-phantom-running", kind: "thread" as const, text: tCard.text, count: 1, last: iso(5) },
    { id: "thread-rule-invalid", kind: "thread" as const, text: "Rule invalid.", count: 0 },
  ];
  const h = host("desk", { cards: [card, tCard], stats: mixedStats, fixDir: "/home/o/git/fyr" });
  const cs = invariantCards([h]);
  assert.equal(cs[0].card.id, "thread-phantom-running");
  assert.equal(cardHead(cs[0], 1), "Thread invariant broken: thread-phantom-running");
  assert.equal(cardHead(cs[0], 2), "Thread invariant broken: thread-phantom-running on desk");
  assert.equal(linesLabel(tCard), "1 recent transition");
  assert.equal(linesLabel({ ...tCard, lines: ["a", "b"] }), "2 recent transitions");

  assert.deepEqual(invariantRows(h, NOW, "load").map((r) => r.id), ["thaw-into-critical", "probe-silent"]);
  assert.deepEqual(invariantRows(h, NOW, "thread").map((r) => [r.id, r.open]), [["thread-phantom-running", true], ["thread-rule-invalid", false]]);

  // Fallback when slots are off: invariants live directly on HostLoad.
  const noSlots = { host: "mac", cores: 8, load: [1, 1, 1], cgroups: false, threads: [], invariants: { cards: [tCard], stats: mixedStats } } as unknown as HostLoad;
  assert.equal(invariantCards([noSlots]).length, 1);
  assert.equal(invariantRows(noSlots, NOW, "thread").length, 2);
});
