// SPEC V0.176: leftover processes and temporary folders on the open thread, the rail, the phone's load sheet and in
// Settings. Pure texts; the components only print them.
import { test } from "node:test";
import assert from "node:assert/strict";
import type { HostLoad } from "../src/host-load.ts";
import { buildConversation } from "../src/conversation.ts";
import { cardLines } from "../src/card.ts";
import { findLeftovers, hostLeftovers, leftoverRows, stripText, tempRows, type Leftover } from "../src/leftovers.ts";
import { mockLeftoverCommand, withLeftovers, mockLoad } from "../src/mock-load.ts";

const NOW = Date.parse("2026-11-17T14:32:00Z");
const iso = (minAgo: number) => new Date(NOW - minAgo * 60_000).toISOString();
const lookup = (id: string) => ({ title: id === "t_map" ? "Map perf" : id, project: "fyr" });

const chrome: Leftover = { id: "11@1", thread: "t_map", pid: 11, name: "chromium", command: "chromium --headless", procs: 9, rss: 1.9e9, started: iso(65), seen: iso(60), endsAt: new Date(NOW + 25 * 60_000).toISOString() };
const http: Leftover = { id: "12@1", thread: "t_map", pid: 12, name: "http.server", command: "python3 -m http.server 8000", procs: 1, rss: 20e6, started: iso(60), seen: iso(58) };
const fwd: Leftover = { id: "13@1", thread: "t_nix", pid: 13, name: "kubectl port-forward", command: "timeout 12h kubectl port-forward svc/x", procs: 2, rss: 60e6, started: iso(90), seen: iso(88), until: new Date(NOW + 10.5 * 3600_000).toISOString() };
const kept: Leftover = { ...http, id: "14@1", pid: 14, name: "vite", command: "node vite --port 5173", kept: { thread: "t_map", pid: 14, until: new Date(NOW + 3 * 3600_000).toISOString(), why: "dev server", by: "agent" } };
const orphan: Leftover = { ...http, id: "15@1", pid: 15, thread: "t_gone", orphan: true, name: "engine.test", rss: 0, endsAt: iso(1) };

function host(ls: Leftover[], extra: Partial<HostLoad> = {}): HostLoad {
  return { host: "desk", cores: 8, load: [1, 1, 1], cgroups: true, threads: [], leftovers: ls, ...extra };
}

test("the thread's strip names its leftovers with their memory, the heavy ones first", () => {
  const f = findLeftovers([host([http, chrome, fwd])], "t_map")!;
  assert.equal(f.host, "desk");
  assert.deepEqual(f.items.map((x) => x.pid), [11, 12]);
  assert.equal(stripText(f.items, NOW), "2 leftover processes: chromium (1.9 GB), http.server · Fyr ends them in 25 min");
  assert.equal(stripText([fwd], NOW), "1 leftover process: kubectl port-forward · its timeout ends it in 10 h");
  assert.equal(stripText([kept], NOW), "1 leftover process: vite · kept for 3 h: dev server");
  assert.equal(findLeftovers([host([fwd])], "t_map"), null);
});

test("V0.kept-not-leftover: a process the agent kept stays off its thread's strip; one the owner kept stays on it", () => {
  assert.equal(findLeftovers([host([kept])], "t_map"), null);
  assert.deepEqual(findLeftovers([host([kept, http])], "t_map")!.items.map((x) => x.pid), [12]);
  const owners: Leftover = { ...kept, kept: { ...kept.kept!, by: "owner" } };
  assert.deepEqual(findLeftovers([host([owners])], "t_map")!.items.map((x) => x.pid), [14]);
});

test("the rail and the phone's sheet count a host's leftovers and their memory", () => {
  assert.equal(hostLeftovers(host([])), null);
  const r = hostLeftovers(host([chrome, http, fwd]))!;
  assert.equal(r.count, 3);
  assert.equal(r.text, "3 leftover processes · 2.0 GB");
  // V0.hover-cards: the rail's hover card and the phone sheet's text, the heaviest first
  assert.equal(r.card.head, "Still running after their thread");
  assert.deepEqual(cardLines(r.card), ["3 processes that threads started and left behind, 2.0 GB in all.", ...cardLines({ sections: r.card.sections })]);
  assert.match(cardLines({ sections: r.card.sections })[0], /^chromium · (\d+ processes · )?1\.9 GB$/);
  assert.equal(r.card.source, "End or keep them in Settings → Hosts → desk.", "V0.settings-ia: on the host's page");
});

test("Settings lists each leftover with its thread, what holds it and when it ends; orphans name no thread", () => {
  const rows = leftoverRows(host([chrome, fwd, kept, orphan]), lookup, NOW);
  assert.deepEqual(
    rows.map((r) => [r.id, r.title, r.desc, r.thread]),
    [
      ["11@1", "chromium · 9 processes · 1.9 GB", "Map perf · fyr · ends in 25 min", "t_map"],
      ["13@1", "kubectl port-forward · 2 processes · 60 MB", "t_nix · fyr · its timeout ends it in 10 h", "t_nix"],
      ["14@1", "vite · 20 MB", "Map perf · fyr · kept for 3 h by the agent: dev server", "t_map"],
      ["15@1", "engine.test", "of no thread · ends now", null],
    ],
  );
  assert.equal(rows[2].canKeep, false);
  assert.equal(rows[0].canKeep, true);
});

test("Settings lists the worktrees nobody works in; only a safe one can be removed, and says when Fyr removes it", () => {
  const now = Date.parse("2026-11-19T12:00:00Z");
  const h = host([], {
    temps: [
      { path: "/r/.claude/worktrees/a", kind: "worktree", thread: "t_map", bytes: 120e6, safe: true },
      { path: "/r/.claude/worktrees/b", kind: "worktree", thread: "t_nix", bytes: 3e6, safe: false, why: "2 commits not pushed" },
      // V0.worktree-sweep: one with no thread, one Fyr removes in 5 h, one in use.
      { path: "/r/.claude/worktrees/c", kind: "worktree", bytes: 9e6, safe: true, removeAt: "2026-11-19T17:00:00Z" },
      { path: "/r/.claude/worktrees/d", kind: "worktree", bytes: 9e6, safe: true, why: "in use" },
    ],
  });
  assert.deepEqual(
    tempRows(h, lookup, now).map((r) => [r.title, r.desc, r.canRemove]),
    [
      ["/r/.claude/worktrees/a", "Map perf · 120 MB · pushed and clean", true],
      ["/r/.claude/worktrees/b", "t_nix · 3 MB · kept: 2 commits not pushed", false],
      ["/r/.claude/worktrees/c", "9 MB · pushed and clean · Fyr removes it in 5 h", true],
      ["/r/.claude/worktrees/d", "9 MB · pushed and clean · in use", true],
    ],
  );
});

test("the line Fyr leaves on the thread is a note in the conversation", () => {
  const items = buildConversation([{ seq: 1, at: iso(1), subject: "t", type: "slot", data: { leftovers: "Fyr ended 2 leftover processes (the thread settled): chromium (1.9 GB), http.server" } }]);
  const note = items.find((x) => x.kind === "note") as { line: string } | undefined;
  assert.equal(note?.line, "ended 2 leftover processes (the thread settled): chromium (1.9 GB), http.server");
});

test("the mock host ends and keeps a leftover and removes a safe worktree", () => {
  const [h] = withLeftovers(mockLoad(), "1");
  const n = h.leftovers!.length;
  const id = h.leftovers![0].id;
  assert.deepEqual(mockLeftoverCommand(h, "host.leftoverEnd", { id }), { ok: true, result: { id } });
  assert.equal(h.leftovers!.length, n - 1);
  const k = h.leftovers![0].id;
  assert.equal(mockLeftoverCommand(h, "host.leftoverKeep", { id: k }).ok, true);
  assert.equal(h.leftovers![0].kept?.by, "owner");
  assert.equal(mockLeftoverCommand(h, "host.leftoverEnd", { id: "nope" }).ok, false);
  const safe = h.temps!.find((t) => t.safe)!;
  assert.equal(mockLeftoverCommand(h, "host.tempRemove", { path: safe.path }).ok, true);
  const unsafe = h.temps!.find((t) => !t.safe)!;
  assert.equal(mockLeftoverCommand(h, "host.tempRemove", { path: unsafe.path }).ok, false);
});

// V0.settings-ia: a leftover of a thread this device cannot look up showed the thread's raw id, with an Open that led nowhere.
test("V0.settings-ia: a leftover whose thread is gone says so and offers no Open", () => {
  const gone = (id: string) => (id === "t_map" ? { title: "Map perf", project: "fyr" } : { title: id, gone: true });
  const rows = leftoverRows(host([chrome, fwd]), gone, NOW);
  assert.deepEqual(rows.map((r) => [r.desc, r.thread]), [
    ["Map perf · fyr · ends in 25 min", "t_map"],
    ["of a thread that is gone · its timeout ends it in 10 h", null],
  ]);
});
