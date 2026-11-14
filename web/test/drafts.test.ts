// SPEC 0.5 V0.29: a composer draft survives a remount and a reload; sending clears it.
import { test } from "node:test";
import assert from "node:assert/strict";
import { draftStore, type KV } from "../src/drafts.ts";

function fakeStorage(): KV & { m: Map<string, string> } {
  const m = new Map<string, string>();
  return { m, getItem: (k) => m.get(k) ?? null, setItem: (k, v) => void m.set(k, v), removeItem: (k) => void m.delete(k) };
}

test("a draft is kept per thread and survives a new store (a reload)", () => {
  const st = fakeStorage();
  const a = draftStore(st);
  a.set("t1", "half a sentence");
  a.set("t2", "other thread");
  assert.equal(a.get("t1"), "half a sentence");
  const afterReload = draftStore(st);
  assert.equal(afterReload.get("t1"), "half a sentence");
  assert.equal(afterReload.get("t2"), "other thread");
  afterReload.set("t1", "");
  assert.equal(draftStore(st).get("t1"), "", "sending clears it");
});

test("without storage (private mode) the draft lives in memory", () => {
  const broken: KV = { getItem: () => { throw new Error("denied"); }, setItem: () => { throw new Error("denied"); }, removeItem: () => {} };
  const d = draftStore(broken);
  d.set("t1", "still here");
  assert.equal(d.get("t1"), "still here");
});
