// V0.load-invariants: a broken load invariant is a card in Needs you on both fronts (invariants-ui.tsx, drawn by the
// desktop list and the phone inbox) with Fix it and Dismiss; Settings → Automation lists each invariant.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { HostLoad } from "../src/load.ts";
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
});
