// storedGet/storedSet: one tolerant way into localStorage. Blocked storage reads as nothing and a write says it failed.
import { test } from "node:test";
import assert from "node:assert/strict";
import { browserStorage, storedGet, storedSet } from "../src/prefs.ts";

const g = globalThis as { localStorage?: unknown };

function withStorage(ls: unknown, f: () => void) {
  const was = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  Object.defineProperty(globalThis, "localStorage", { value: ls, configurable: true, writable: true });
  try {
    f();
  } finally {
    if (was) Object.defineProperty(globalThis, "localStorage", was);
    else delete g.localStorage;
  }
}

test("a working storage round-trips and forgets", () => {
  const m = new Map<string, string>();
  const ls = { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k) };
  withStorage(ls, () => {
    assert.equal(storedSet("k", "v"), true);
    assert.equal(storedGet("k"), "v");
    assert.equal(storedSet("k", null), true);
    assert.equal(storedGet("k"), null);
    assert.ok(browserStorage());
  });
});

test("a blocked storage reads as nothing and a write reports failure", () => {
  const boom = () => {
    throw new Error("SecurityError");
  };
  withStorage({ getItem: boom, setItem: boom, removeItem: boom }, () => {
    assert.equal(storedGet("k"), null);
    assert.equal(storedSet("k", "v"), false);
    assert.equal(storedSet("k", null), false);
  });
});

test("no storage at all", () => {
  withStorage(undefined, () => {
    assert.equal(browserStorage(), null);
    assert.equal(storedGet("k"), null);
    assert.equal(storedSet("k", "v"), false);
  });
});
