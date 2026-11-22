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
