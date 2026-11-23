// The client survives the other DTO endpoints with any optional field missing (the host omits empty fields), as
// contract.test.ts does for /api/state. The shapes come from the mock server, which mirrors the host's JSON
// (the Go side has no fixture for these yet: the sandbox that wrote this had no Go toolchain, so
// TestStateContractFixture still writes only host-state.json).
import { test } from "node:test";
import assert from "node:assert/strict";
import { mockChanges } from "../src/mock-changes.ts";
import { mockLoad, mockQueueLoad, mockQuota } from "../src/mock-load.ts";
import { stampSlots } from "../src/hostfeed.ts";
import { sortFiles, statusMark, summaryLine } from "../src/changes.ts";
import { cardLines } from "../src/card.ts";
import { loadCard, normalizeLoad, railBudget, railLabel, threadCPU } from "../src/host-load.ts";
import { cardState, quotaCard, quotaSummary } from "../src/quota.ts";

const NOW = Date.parse("2026-01-01T00:00:00Z");

/** Every way to remove one non-required key, at any depth, from a list of DTOs. */
function variants<T>(list: T[], required: Set<string>): { label: string; list: T[] }[] {
  const out = [{ label: "as sent", list: structuredClone(list) }];
  const walk = (path: (string | number)[], v: unknown) => {
    if (!v || typeof v !== "object") return;
    for (const k of Object.keys(v)) {
      const p = [...path, Array.isArray(v) ? Number(k) : k];
      if (!Array.isArray(v) && !required.has(k)) {
        const s = structuredClone(list) as any;
        let o = s;
        for (const x of p.slice(0, -1)) o = o[x as any];
        delete o[p[p.length - 1] as any];
        out.push({ label: p.join("."), list: s });
      }
      walk(p, (v as any)[k]);
    }
  };
  walk([], list);
  return out;
}

function survives<T>(what: string, list: T[], required: string[], use: (l: T[]) => void) {
  const vs = variants(list, new Set(required));
  assert.ok(vs.length > 5, `${what}: the fixture has optional fields to remove`);
  for (const v of vs) {
    try {
      use(v.list);
    } catch (e) {
      assert.fail(`${what} without ${v.label}: ${(e as Error).message}`);
    }
  }
}

test("quota: every card function survives a profile with any optional field missing", () => {
  survives("quota", mockQuota(NOW), ["profile", "kind", "name"], (list) => {
    for (const q of list) {
      cardLines(quotaCard(q, q.profile, NOW));
      assert.equal(typeof quotaSummary(q, q.profile), "string");
      cardState(q);
    }
  });
});

test("load: every load function survives a host with any optional field missing", () => {
  for (const [name, load] of [["busy host", mockLoad()], ["queue", mockQueueLoad("wait")]] as const) {
    survives(name, load, ["host", "cores", "load", "id", "pid", "name", "n", "kind"], (list) => {
      const norm = normalizeLoad(list);
      for (const h of norm) {
        cardLines(loadCard(h));
        railBudget(h);
        railLabel(h, true);
        for (const t of h.threads ?? []) threadCPU(norm, t.id);
      }
    });
  }
});

test("changes: the summary and the file list survive a missing optional field", () => {
  const dto = mockChanges("t_lasso");
  survives("changes", [dto], ["git", "path", "status"], ([c]) => {
    assert.equal(typeof summaryLine(c), "string");
    for (const f of sortFiles(c.files ?? [])) statusMark(f.status);
  });
});

test("load: a pool whose lists arrive as null (a Go nil slice) reads as empty", () => {
  const [h] = mockLoad();
  const raw = { ...h, slots: { ...h.slots!, held: null, waiting: null } } as unknown as typeof h;
  const [n] = normalizeLoad([raw]);
  assert.deepEqual([n.slots!.held, n.slots!.waiting], [[], []]);
  assert.equal(stampSlots({ ...h.slots!, held: null, waiting: null } as never, "h_1" as never).held.length, 0);
  cardLines(loadCard(n));
});

// ---- the lists the host omits when empty (Go omitempty), through the decoders ----

import { decodeChanges, decodeFilePatch, defaultMode, sinceLabel, sinceLine } from "../src/changes.ts";
import { changeList, changeChip } from "../src/offers.ts";
import { decodeThread } from "../src/decode.ts";
import { guardChart, guardLine, guardLog, guardSummary } from "../src/guard.ts";
import { hostLeftovers, leftoverRows, tempRows } from "../src/leftovers.ts";
import { withGuard, withLeftovers } from "../src/mock-load.ts";
import { TH } from "./helpers.ts";

test("changes: a reply with no files, no since.files, null or garbage reads as empty lists", () => {
  for (const raw of [{ git: true }, { git: true, since: {} }, { git: true, files: null, since: { files: null, mode: "x" } }, null, [], "x", 3]) {
    const c = decodeChanges(raw);
    assert.deepEqual(c.files, [], JSON.stringify(raw));
    if (c.since) assert.deepEqual(c.since.files, []);
    assert.equal(defaultMode(c), "whole");
    assert.equal(summaryLine(c).length > 0, true);
  }
  const c = decodeChanges({ git: true, since: { head: "abc", at: "2026-01-01T00:00:00Z", commits: 1 } });
  assert.equal(c.since?.mode, "ancestor");
  assert.equal(sinceLabel(c.since!), "New since you looked · 0");
  assert.equal(typeof sinceLine(c.since!, NOW), "string");
});

test("changes: a file keeps its path and gets counts and a status when the host left them out", () => {
  const c = decodeChanges({ git: true, files: [{ path: "a.go" }, { nope: 1 }, null, { path: "b.go", additions: 2, status: "added" }] });
  assert.deepEqual(c.files!.map((f) => [f.path, f.status, f.additions, f.deletions, f.binary]), [["a.go", "modified", 0, 0, false], ["b.go", "added", 2, 0, false]]);
});

test("changes: with the mock reply's optional fields removed one at a time the decoded reply holds", () => {
  survives("changes decoded", [mockChanges("t_lasso")], ["git"], ([c]) => {
    const d = decodeChanges(c);
    assert.ok(Array.isArray(d.files));
    for (const f of sortFiles(d.files!)) statusMark(f.status);
    if (d.since) assert.ok(Array.isArray(d.since.files));
    summaryLine(d);
    defaultMode(d);
  });
});

test("file patch: an empty or null body is an empty patch of the asked path", () => {
  for (const raw of [null, {}, []]) assert.deepEqual(decodeFilePatch(raw, "a.go"), { path: "a.go", patch: "", binary: false, truncated: false });
});

test("thread: changes, offers and runs missing fields or items decode to what the lists read", () => {
  const t = decodeThread({ id: "t1", changes: [{ id: "c1" }, null, 4, { id: "c2", updatedAt: "2026-01-02T00:00:00Z", offers: [{ id: "o" }, null], runs: [{}] }] }, TH);
  assert.deepEqual(t.changes.map((c) => [c.id, c.phase, c.updatedAt]), [["c1", "working", ""], ["c2", "working", "2026-01-02T00:00:00Z"]]);
  assert.deepEqual(t.changes[1].offers!.map((o) => o.label), [""]);
  assert.equal(t.changes[1].runs![0].ok, false);
  assert.equal(changeList(t)[0].id, "c2"); // newest first, with no updatedAt to compare on c1
});

test("thread: the change lists survive any optional field removed from a change", () => {
  const raw = { id: "t1", changes: [{ id: "c1", key: "k", kind: "code", title: "T", phase: "review", phaseBy: "agent", createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-02T00:00:00Z", offers: [{ id: "o", label: "L", run: "fyr", by: "agent", digest: "d" }], runs: [{ action: "a", label: "L", by: "agent", at: "2026-01-02T00:00:00Z", ok: true }] }] };
  survives("change", [raw], ["id"], ([r]) => {
    const t = decodeThread(r, TH);
    changeList(t);
    changeChip(t);
  });
});

test("guard and leftovers: the functions survive any optional field of the mock host removed", () => {
  const withBoth = () => withLeftovers(withGuard(mockLoad(), "1", NOW), "1", NOW);
  survives("guard", withBoth(), ["host", "cores", "load", "id", "pid", "name", "n", "kind"], (list) => {
    const norm = normalizeLoad(list);
    const g = norm.find((h) => h.slots?.guard)?.slots?.guard;
    if (g) {
      guardChart(g, NOW, (id) => id);
      guardSummary(g, NOW);
      guardLine(g, "t_mapperf", NOW);
      guardLog(g, (id) => id, (iso) => iso);
    }
    for (const h of norm) {
      hostLeftovers(h);
      leftoverRows(h, (id) => ({ title: id }), NOW);
      tempRows(h, (id) => ({ title: id }), NOW);
    }
  });
});
