// SPEC V0.164 (design/rogue-agents/SPEC.md 8, 12.7): undeclared heavy work on the row, the rail, the strip, in the
// queue's wait reason, in Settings and in the conversation. Pure texts; the components only print them.
import { test } from "node:test";
import assert from "node:assert/strict";
import { buildConversation } from "../src/conversation.ts";
import { mockLoad, mockUndeclaredCommand, withUndeclared } from "../src/mock.ts";
import { cardLines, loadCard, railBudget, threadSlot, type HostLoad, type UndeclaredUse } from "../src/load.ts";
import {
  addedRows,
  findUndeclared,
  parseThreshold,
  railUndeclared,
  rowUndeclared,
  stripView,
  thresholdRows,
  undeclaredRows,
  undeclaredTip,
} from "../src/undeclared.ts";

const NOW = Date.parse("2026-11-17T14:32:00");
const ago = (sec: number) => new Date(NOW - sec * 1000).toISOString();

const map: UndeclaredUse = {
  thread: "t_map",
  cpu: 2.1,
  mem: 2.8e9,
  cpuPct: 52,
  memPct: 20,
  since: ago(180),
  command: "node test/layout.e2e.mjs",
  paused: false,
  queued: false,
  overCpu: true,
  overMem: true,
};
const babysit: UndeclaredUse = { ...map, thread: "t_baby", cpu: 1.2, mem: 1.6e9, cpuPct: 30, memPct: 11, since: ago(50), command: "yarn pre-push:check" };
const nix: UndeclaredUse = { ...map, thread: "t_nix", cpu: 0.2, mem: 1.9e9, cpuPct: 5, memPct: 14, since: ago(22 * 60), command: "nix flake check", overCpu: false };

function host(und: UndeclaredUse[], extra: Partial<HostLoad> = {}): HostLoad {
  return {
    host: "desk",
    cores: 8,
    load: [6, 5, 4],
    cgroups: true,
    provider: "cgroups",
    capacity: { cpu: 4, memory: 13.96e9 },
    headroom: 0.06,
    agents: { cpu: 3, cpuMax: 4, memory: 9e9, memoryMax: 14e9 },
    threads: [],
    slots: {
      n: 2,
      held: [{ owner: "t_w2", kind: "test", cpuPct: 20, memPct: 16, pct: 20 }],
      waiting: [{ owner: "t_wait", kind: "test", cpuPct: 45, memPct: 20, pct: 45 }],
      why: "needs ≈ 45 % CPU, 20 % free (Map perf uses 52 % undeclared)",
      cpuPct: 95,
      memPct: 41,
      cpuBudgetPct: 100,
      memBudgetPct: 100,
      usedPct: 95,
      tight: "cpu",
      undeclared: und,
      undeclaredCpuPct: 52,
      undeclaredMemPct: 20,
    },
    ...extra,
  };
}
const lookup = (id: string) => ({ t_map: { title: "Map perf", project: "acme/web" }, t_baby: { title: "Babysit PR #4170", project: "acme/web" }, t_nix: { title: "Nix flake update", project: "dotfiles" } })[id] ?? { title: id };

test("the row: the word and the use, only the measures that are over; paused says so", () => {
  const h = host([map, { ...babysit, overCpu: false }, { ...nix, thread: "t_paused", paused: true }]);
  assert.equal(rowUndeclared([h], "t_map", NOW)?.text, "undeclared · 2.8 GB · 2.1 cores");
  assert.equal(rowUndeclared([h], "t_baby", NOW)?.text, "undeclared · 1.6 GB");
  const p = rowUndeclared([h], "t_paused", NOW);
  assert.equal(p?.text, "undeclared · paused: memory");
  assert.equal(p?.paused, true);
  assert.equal(rowUndeclared([h], "t_none", NOW), null);
  assert.equal(rowUndeclared([], "t_map", NOW), null);
  assert.equal(rowUndeclared([h], "t_map", NOW)!.title, "Heavy work Fyr didn't schedule: 2.8 GB and 2.1 cores more than at rest, for 3 min.");
  // CPU only
  assert.equal(rowUndeclared([host([{ ...map, overMem: false }])], "t_map", NOW)?.text, "undeclared · 2.1 cores");
});

test("the meter has two parts, admitted and undeclared, and its tooltip names both and the threads", () => {
  const h = host([map, babysit]);
  assert.deepEqual(railBudget(h), { text: "95 %", note: "1 waits", share: 0.43, und: 0.52, hot: true, waiting: 1 });
  // without undeclared work the meter is as before (one part, no `und`)
  const none = host([], { slots: { ...h.slots!, undeclared: undefined, undeclaredCpuPct: undefined, undeclaredMemPct: undefined } });
  assert.deepEqual(railBudget(none), { text: "95 %", note: "1 waits", share: 0.95, hot: true, waiting: 1 });
  // memory tight: the memory share of undeclared is the hatched part
  const mem = host([map], { slots: { ...h.slots!, tight: "memory", usedPct: 60, memPct: 60 } });
  assert.equal(railBudget(mem)?.und, 0.2);
  assert.equal(railBudget(mem)?.share, 0.4);
  const tip = cardLines(loadCard(h, { lookup, now: NOW }));
  assert.ok(tip.includes("CPU: 95 % of the host · budget 100 % · 52 % undeclared"), tip.join("\n"));
  assert.ok(tip.some((l) => /^Memory: 41 % of the host · budget \d+ % · 20 % undeclared$/.test(l)), tip.join("\n"));
  assert.ok(tip.includes("Undeclared: Map perf (acme/web) · 2.8 GB · 2.1 cores for 3 min"), tip.join("\n"));
  assert.ok(tip.includes("…: Babysit PR #4170 (acme/web) · 1.6 GB · 1.2 cores for 50 s"), tip.join("\n"));
  assert.equal(undeclaredTip(none, lookup, NOW), "");
  // without names the card still says the shares
  assert.ok(cardLines(loadCard(h)).some((l) => l.includes("52 % undeclared")));
});

test("the rail button counts the flagged threads; one opens the thread, several a menu", () => {
  const two = railUndeclared(host([map, babysit]), lookup, NOW)!;
  assert.equal(two.count, 2);
  assert.equal(two.only, null);
  assert.deepEqual(two.entries, [
    { id: "t_map", label: "Map perf · 2.8 GB · 2.1 cores" },
    { id: "t_baby", label: "Babysit PR #4170 · 1.6 GB · 1.2 cores" },
  ]);
  // V0.hover-cards: the button's hover card names each thread, its project, its use and for how long
  assert.deepEqual(cardLines(two.card), [
    "These 2 threads run heavy jobs without asking first. Fyr counts them against desk's heavy-job budget.",
    "Map perf · acme/web · 2.8 GB · 2.1 cores · for 3 min",
    "Babysit PR #4170 · acme/web · 1.6 GB · 1.2 cores · for 50 s",
  ]);
  assert.equal(two.card.source, "Click for the list.");
  assert.equal(two.aria, "2 threads run undeclared heavy work");
  const one = railUndeclared(host([map]), lookup, NOW)!;
  assert.equal(one.count, 1);
  assert.equal(one.only, "t_map");
  assert.equal(one.card.source, "Click to open it.");
  assert.equal(one.aria, "1 thread runs undeclared heavy work");
  assert.equal(railUndeclared(host([]), lookup, NOW), null);
});

test("V0.209: the strip shows only paused work, in the harbour words, with nothing to press", () => {
  assert.equal(stripView(host([map]), map), null);
  const p = { ...map, paused: true };
  const s = stripView(host([p]), p)!;
  assert.equal(s.head, "Paused by Fyr: memory is short on desk (6 % free).");
  assert.equal(s.body, "Frozen, not killed; it resumes when memory has recovered for 30 s, or after 10 min unless memory stalls.");
  assert.equal(s.command, "node test/layout.e2e.mjs");
  assert.equal(s.note, "2.8 GB · undeclared");
  // without a headroom reading the words do not invent a number
  assert.equal(stripView(host([p], { headroom: undefined }), p)!.head, "Paused by Fyr: memory is short on desk.");
});

test("findUndeclared reads a thread's use from any host; the wait reason is the host's text", () => {
  const h = host([map]);
  assert.equal(findUndeclared([h], "t_map")?.u.command, "node test/layout.e2e.mjs");
  assert.equal(findUndeclared([h], "t_x"), null);
  assert.equal(threadSlot([h], "t_wait"), "waiting: needs ≈ 45 % CPU, 20 % free (Map perf uses 52 % undeclared)");
});

test("Settings: Undeclared now lists every flagged thread", () => {
  const rows = undeclaredRows(host([map, nix]), NOW, lookup);
  assert.deepEqual(rows[0], { thread: "t_map", title: "Map perf", project: "acme/web", command: "node test/layout.e2e.mjs", usage: "2.8 GB (20 %) · 2.1 cores (52 %) · 3 min" });
  assert.deepEqual(rows[1], { thread: "t_nix", title: "Nix flake update", project: "dotfiles", command: "nix flake check", usage: "1.9 GB (14 %) · 22 min" });
  assert.deepEqual(undeclaredRows(host([]), NOW, lookup), []);
});

test("Settings: learned commands, added patterns and the thresholds", () => {
  const h = host([], {
    heavyAdded: [{ pattern: "gradle assemble", kind: "build", at: "2026-11-16T10:00:00", refused: 0, learned: true }, { pattern: "python3? build_admin_ny\\.py", kind: "build", at: "2026-11-16T10:00:00", refused: 4 }],
    undeclaredSettings: { on: true, cpu: 0.25, memory: 0.08, after: 45 },
  });
  assert.deepEqual(addedRows(h), [
    { pattern: "gradle assemble", line: "build · learned 16 Nov" },
    { pattern: "python3? build_admin_ny\\.py", line: "build · added 16 Nov · refused 4 times since" },
  ]);
  assert.equal(addedRows(host([], { heavyAdded: [{ pattern: "x", kind: "test", at: "2026-11-16T10:00:00", refused: 0 }] }))[0].line, "test · added 16 Nov · not refused since");
  assert.equal(addedRows(host([], { heavyAdded: [{ pattern: "x", kind: "test", at: "2026-11-16T10:00:00", refused: 1 }] }))[0].line, "test · added 16 Nov · refused once since");
  const t = thresholdRows(h)!;
  assert.deepEqual(
    t.map((r) => [r.key, r.title, r.now, r.value, r.unit]),
    [
      ["cpu", "CPU above its idle level", "Now 1.0 of desk's 4 cores", "25", "% of this host"],
      ["memory", "Memory above its idle level", "Now 1.1 GB of desk's 14 GB", "8", "% of this host"],
      ["after", "For at least", "Shorter bursts (a typecheck, one test file) never count", "45", "seconds"],
    ],
  );
  assert.equal(thresholdRows(host([])), null); // a host that sends no settings has no group
  // saving: whole percent to a share, seconds within the host's 30 to 120
  assert.deepEqual(parseThreshold("cpu", "30"), { cpu: 0.3 });
  assert.deepEqual(parseThreshold("memory", "8"), { memory: 0.08 });
  assert.deepEqual(parseThreshold("after", "60"), { after: 60 });
  assert.ok("error" in parseThreshold("after", "10"));
  assert.ok("error" in parseThreshold("cpu", "0"));
  assert.ok("error" in parseThreshold("cpu", "abc"));
  assert.ok("error" in parseThreshold("memory", "101"));
});

test("Settings words: undeclared, never slot or reservation", () => {
  const h = host([map, nix], { heavyAdded: [], undeclaredSettings: { on: true, cpu: 0.25, memory: 0.08, after: 45 } });
  const text = [JSON.stringify(undeclaredRows(h, NOW, lookup)), JSON.stringify(thresholdRows(h)), cardLines(loadCard(h, { lookup, now: NOW })).join(" "), cardLines(railUndeclared(h, lookup, NOW)?.card ?? { sections: [] }).join(" "), stripView(h, { ...map, paused: true })?.note].join(" ");
  assert.ok(!/slot|reservation/i.test(text), text);
});

test("N9: the event an older host wrote for the agent's notice is still one machine line that unfolds to the whole text", () => {
  const full = "This thread runs heavy work Fyr did not admit: about 2.8 GB memory (20 % of desk) and 2.1 cores (52 %) for 45 s, from `node test/layout.e2e.mjs`. Before a heavy job call mcp__fyr__slot_acquire ...";
  const items = buildConversation([
    { seq: 1, at: "2026-11-17T14:29:00Z", subject: "t", type: "slot", data: { undeclared: full, mem: 2.8e9, cpu: 2.1 } },
    { seq: 2, at: "2026-11-17T14:30:00Z", subject: "t", type: "slot", data: { granted: true } }, // other slot events are not shown
  ]);
  assert.equal(items.length, 1);
  const it = items[0] as { kind: string; line: string; text: string };
  assert.equal(it.kind, "undeclared");
  assert.equal(it.line, "Fyr told the agent: heavy work it did not ask for (2.8 GB, 2.1 cores); ask before a heavy job");
  assert.equal(it.text, full);
  // one measure only
  const m = buildConversation([{ seq: 1, at: "2026-11-17T14:29:00Z", subject: "t", type: "slot", data: { undeclared: full, mem: 2.8e9, cpu: 0 } }])[0] as { line: string };
  assert.equal(m.line, "Fyr told the agent: heavy work it did not ask for (2.8 GB); ask before a heavy job");
});

// The mock state ?mock=rail&undeclared=1 (=paused, =2) feeds all of the above, and answers the owner's commands.
test("the mock load: one thread, paused, and two flagged; the commands update it", () => {
  const same = (id: string) => ({ title: id });
  assert.equal(mockLoad()[0].slots?.undeclared, undefined, "without ?undeclared the mock host sends none");
  const one = withUndeclared(mockLoad(), "1", NOW)[0];
  assert.deepEqual(one.slots!.undeclared!.map((u) => [u.thread, u.paused]), [["t_mapperf", false]]);
  assert.equal(one.slots!.undeclaredCpuPct, 52);
  assert.match(one.slots!.why!, /\(Map perf uses 52 % undeclared\)$/);
  assert.equal(rowUndeclared([one], "t_mapperf", NOW)?.text, "undeclared · 2.8 GB · 2.1 cores");
  const paused = withUndeclared(mockLoad(), "paused", NOW)[0];
  assert.equal(rowUndeclared([paused], "t_mapperf", NOW)?.text, "undeclared · paused: memory");
  assert.ok(stripView(paused, paused.slots!.undeclared![0]));
  const two = withUndeclared(mockLoad(), "2", NOW)[0];
  assert.equal(railUndeclared(two, same, NOW)!.count, 2);
  assert.equal(two.heavyAdded![0].learned, true);
  assert.equal(mockUndeclaredCommand(two, "host.heavyRemove", { pattern: "x" }).ok, false);
  assert.equal(mockUndeclaredCommand(two, "host.heavyRemove", { pattern: two.heavyAdded![0].pattern }).ok, true);
  assert.equal(two.heavyAdded!.length, 0);
  assert.equal(mockUndeclaredCommand(two, "host.undeclaredSettings", { cpu: 0.5 }).ok, true);
  assert.equal(two.undeclaredSettings!.cpu, 0.5);
  assert.equal(mockUndeclaredCommand(two, "host.undeclaredSettings", { after: 5 }).ok, false);
});
