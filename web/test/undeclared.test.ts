// SPEC V0.164 (design/rogue-agents/SPEC.md 8, 12.7): undeclared heavy work on the row, the rail, the strip, in the
// queue's wait reason, in Settings and in the conversation. Pure texts; the components only print them.
import { test } from "node:test";
import assert from "node:assert/strict";
import { buildConversation } from "../src/conversation.ts";
import { railBudget, slotTip, threadSlot, type HostLoad, type UndeclaredUse } from "../src/load.ts";
import {
  addedRows,
  findUndeclared,
  parseThreshold,
  railUndeclared,
  rowUndeclared,
  stripView,
  suggestionRows,
  thresholdRows,
  undeclaredRows,
  undeclaredTip,
} from "../src/undeclared.ts";

const NOW = Date.parse("2026-11-17T14:32:00");
const at = (hm: string) => `2026-11-17T${hm}:00`; // no zone: local, so the clock text does not depend on TZ
const ago = (sec: number) => new Date(NOW - sec * 1000).toISOString();

const map: UndeclaredUse = {
  thread: "t_map",
  cpu: 2.1,
  mem: 2.8e9,
  cpuPct: 52,
  memPct: 20,
  since: ago(180),
  command: "node test/layout.e2e.mjs",
  told: at("14:29"),
  dismissed: false,
  paused: false,
  queued: false,
  overCpu: true,
  overMem: true,
  matched: false,
};
const babysit: UndeclaredUse = { ...map, thread: "t_baby", cpu: 1.2, mem: 1.6e9, cpuPct: 30, memPct: 11, since: ago(50), command: "yarn pre-push:check", told: at("14:31") };
const nix: UndeclaredUse = { ...map, thread: "t_nix", cpu: 0.2, mem: 1.9e9, cpuPct: 5, memPct: 14, since: ago(22 * 60), command: "nix flake check", told: undefined, dismissed: true, overCpu: false };

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

test("the row: the word and the use, only the measures that are over; paused says so; a dismissed thread has no mark", () => {
  const h = host([map, { ...babysit, overCpu: false }, { ...nix, thread: "t_paused", dismissed: false, paused: true }, nix]);
  assert.equal(rowUndeclared([h], "t_map", NOW)?.text, "undeclared · 2.8 GB · 2.1 cores");
  assert.equal(rowUndeclared([h], "t_baby", NOW)?.text, "undeclared · 1.6 GB");
  const p = rowUndeclared([h], "t_paused", NOW);
  assert.equal(p?.text, "undeclared · paused: memory");
  assert.equal(p?.paused, true);
  assert.equal(rowUndeclared([h], "t_nix", NOW), null); // dismissed
  assert.equal(rowUndeclared([h], "t_none", NOW), null);
  assert.equal(rowUndeclared([], "t_map", NOW), null);
  assert.match(rowUndeclared([h], "t_map", NOW)!.title, /^Heavy work Fyr did not admit: this thread uses 2\.8 GB and 2\.1 cores above its idle level, for 3 min\. It counts against the host's heavy jobs\.$/);
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
  const tip = slotTip(h, { lookup, now: NOW }).split("\n");
  assert.match(tip[0], /CPU 95 % \(undeclared 52 %\) · memory 41 % \(undeclared 20 %\)/);
  assert.equal(tip[tip.length - 1], "Undeclared: Map perf (acme/web), 2.8 GB and 2.1 cores for 3 min; Babysit PR #4170 (acme/web), 1.6 GB and 1.2 cores for 50 s");
  assert.equal(undeclaredTip(none, lookup, NOW), "");
  // the plain tooltip (no names) still says the shares
  assert.match(slotTip(h), /undeclared 52 %/);
});

test("the rail button counts the threads that are not dismissed; one opens the thread, several a menu", () => {
  const two = railUndeclared(host([map, babysit, nix]), lookup)!;
  assert.equal(two.count, 2);
  assert.equal(two.only, null);
  assert.deepEqual(two.entries, [
    { id: "t_map", label: "Map perf · 2.8 GB · 2.1 cores" },
    { id: "t_baby", label: "Babysit PR #4170 · 1.6 GB · 1.2 cores" },
  ]);
  assert.equal(two.title, "2 threads run undeclared heavy work: Map perf, Babysit PR #4170. Click for the list");
  assert.equal(two.aria, "2 threads run undeclared heavy work");
  const one = railUndeclared(host([map, nix]), lookup)!;
  assert.equal(one.count, 1);
  assert.equal(one.only, "t_map");
  assert.equal(one.title, "Map perf runs undeclared heavy work (2.8 GB · 2.1 cores). Click to open it");
  assert.equal(one.aria, "1 thread runs undeclared heavy work");
  assert.equal(railUndeclared(host([nix]), lookup), null); // the hatched part stays, the button does not
  assert.equal(railUndeclared(host([]), lookup), null);
});

test("the strip: the use, the command, what it counts against, when the agent was told; Add and Dismiss", () => {
  const s = stripView(host([map]), map, NOW);
  assert.equal(s.paused, false);
  assert.equal(s.head, "Heavy work Fyr did not admit");
  assert.equal(s.usage, "2.8 GB (20 % of desk) · 2.1 cores (52 %) · 3 min");
  assert.equal(s.command, "node test/layout.e2e.mjs");
  assert.equal(s.note, "counted against desk's heavy jobs · agent told 14:29");
  assert.equal(s.add, true);
  assert.equal(s.dismiss, true);
  // the command already matches a pattern: no Add
  assert.equal(stripView(host([map]), { ...map, matched: true }, NOW).add, false);
  assert.equal(stripView(host([map]), { ...map, matched: true }, NOW).dismiss, true);
  // memory only; not told yet
  const m = stripView(host([map]), { ...map, overCpu: false, told: undefined }, NOW);
  assert.equal(m.usage, "2.8 GB (20 % of desk) · 3 min");
  assert.equal(m.note, "counted against desk's heavy jobs");
  // an hour
  assert.equal(stripView(host([map]), { ...map, since: ago(65 * 60) }, NOW).usage, "2.8 GB (20 % of desk) · 2.1 cores (52 %) · 1 h 5 min");
});

test("the paused strip: the harbour words, no buttons", () => {
  const p = { ...map, paused: true };
  const s = stripView(host([p]), p, NOW);
  assert.equal(s.paused, true);
  assert.equal(s.head, "Paused by Fyr: memory is short on desk (6 % free).");
  assert.equal(s.body, "Frozen, not killed; it resumes when memory has recovered for 30 s, within 10 min at most.");
  assert.equal(s.note, "2.8 GB · undeclared");
  assert.equal(s.add, false);
  assert.equal(s.dismiss, false);
  // without a headroom reading the words do not invent a number
  assert.equal(stripView(host([p], { headroom: undefined }), p, NOW).head, "Paused by Fyr: memory is short on desk.");
});

test("findUndeclared reads a thread's use from any host; the wait reason is the host's text", () => {
  const h = host([map]);
  assert.equal(findUndeclared([h], "t_map")?.u.command, "node test/layout.e2e.mjs");
  assert.equal(findUndeclared([h], "t_x"), null);
  assert.equal(threadSlot([h], "t_wait"), "waiting: needs ≈ 45 % CPU, 20 % free (Map perf uses 52 % undeclared)");
});

test("Settings: Undeclared now lists every flagged thread; a dismissed one stays, dimmed", () => {
  const rows = undeclaredRows(host([map, nix]), NOW, lookup);
  assert.deepEqual(rows[0], { thread: "t_map", title: "Map perf", project: "acme/web", command: "node test/layout.e2e.mjs", usage: "2.8 GB (20 %) · 2.1 cores (52 %) · 3 min", tail: "agent told 14:29", dismissed: false });
  assert.deepEqual(rows[1], { thread: "t_nix", title: "Nix flake update", project: "dotfiles", command: "nix flake check", usage: "1.9 GB (14 %) · 22 min", tail: "dismissed, still counted", dismissed: true });
  assert.deepEqual(undeclaredRows(host([]), NOW, lookup), []);
});

test("Settings: suggestions, added patterns and the thresholds", () => {
  const h = host([], {
    suggestions: [
      { id: "s1", command: "node test/layout.e2e.mjs", pattern: "node (\\S*/)?layout\\.e2e\\.mjs", kind: "e2e", seen: 3, threads: 2, lastThread: "t_map", lastAt: at("14:29"), memory: 2.8e9, cpu: 2.1, seconds: 240 },
      { id: "s2", command: "yarn pre-push:check", pattern: "yarn pre-push:check", kind: "test", seen: 1, threads: 1, lastThread: "t_baby", lastAt: at("14:31"), memory: 1.6e9, cpu: 1.2, seconds: 100 },
    ],
    heavyAdded: [{ pattern: "python3? build_admin_ny\\.py", kind: "build", at: "2026-11-16T10:00:00", refused: 4 }],
    undeclaredSettings: { on: true, cpu: 0.25, memory: 0.08, after: 45 },
  });
  const s = suggestionRows(h, lookup);
  assert.equal(s[0].line, "Seen 3 times in 2 threads, last 14:29 in Map perf · up to 2.8 GB, 2.1 cores, 4 min");
  assert.equal(s[0].pattern, "node (\\S*/)?layout\\.e2e\\.mjs");
  assert.equal(s[0].kind, "e2e");
  assert.equal(s[1].line, "Seen once, 14:31 in Babysit PR #4170 · up to 1.6 GB, 1.2 cores, 2 min");
  const a = addedRows(h);
  assert.deepEqual(a, [{ pattern: "python3? build_admin_ny\\.py", line: "build · added 16 Nov from a suggestion · refused 4 times since" }]);
  assert.equal(addedRows(host([], { heavyAdded: [{ pattern: "x", kind: "test", at: "2026-11-16T10:00:00", refused: 0 }] }))[0].line, "test · added 16 Nov from a suggestion · not refused since");
  assert.equal(addedRows(host([], { heavyAdded: [{ pattern: "x", kind: "test", at: "2026-11-16T10:00:00", refused: 1 }] }))[0].line, "test · added 16 Nov from a suggestion · refused once since");
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
  const h = host([map, nix], { suggestions: [], heavyAdded: [], undeclaredSettings: { on: true, cpu: 0.25, memory: 0.08, after: 45 } });
  const text = [JSON.stringify(undeclaredRows(h, NOW, lookup)), JSON.stringify(thresholdRows(h)), slotTip(h, { lookup, now: NOW }), railUndeclared(h, lookup)?.title, stripView(h, map, NOW).note].join(" ");
  assert.ok(!/slot|reservation/i.test(text), text);
});

test("N9: the event the host writes for the agent's notice is one machine line that unfolds to the whole text", () => {
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
