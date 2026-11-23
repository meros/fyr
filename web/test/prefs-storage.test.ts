// storedGet/storedSet: one tolerant way into localStorage. Blocked storage reads as nothing and a write says it failed.
import { test } from "node:test";
import assert from "node:assert/strict";
import { browserStorage, storedGet, storedJson, storedSet } from "../src/prefs.ts";
import { asObject, flags, parseJson, strings } from "../src/decode.ts";
import { memoryStorage, withStorage } from "./helpers.ts";

test("a working storage round-trips and forgets", () => {
  withStorage(memoryStorage(), () => {
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

// V0.web-helpers: stored JSON goes through one decoder; nothing a device kept can throw or come back the wrong shape.
test("storedJson: the decoded value, else the fallback for every kind of bad value", () => {
  const corrupt = { missing: undefined, empty: "", text: "not json", cut: '["a",', nul: "null", num: "42", str: '"x"', obj: '{"a":1}' };
  withStorage(memoryStorage({ ...Object.fromEntries(Object.entries(corrupt).filter((e): e is [string, string] => e[1] !== undefined)), good: '["a","b","a","",3]' }), () => {
    for (const k of Object.keys(corrupt)) assert.deepEqual(storedJson(k, (v) => (Array.isArray(v) ? strings(v) : undefined), ["fb"]), ["fb"], k);
    assert.deepEqual(storedJson("good", strings, []), ["a", "b"]);
  });
});

test("storedJson: a storage that throws, or none at all, reads as the fallback", () => {
  const boom = () => {
    throw new Error("SecurityError");
  };
  withStorage({ getItem: boom, setItem: boom, removeItem: boom }, () => assert.deepEqual(storedJson("k", flags, { x: true }), { x: true }));
  withStorage(undefined, () => assert.deepEqual(storedJson("k", flags, {}), {}));
  const was = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  Object.defineProperty(globalThis, "localStorage", { get: boom, configurable: true });
  try {
    assert.deepEqual(storedJson("k", strings, ["fb"]), ["fb"], "the accessor itself throws");
  } finally {
    if (was) Object.defineProperty(globalThis, "localStorage", was);
    else delete (globalThis as { localStorage?: unknown }).localStorage;
  }
});

test("parseJson: a decoder that refuses the shape or throws gives the fallback", () => {
  assert.deepEqual(parseJson("[1]", asObject, { fb: 1 }), { fb: 1 });
  assert.deepEqual(parseJson('{"a":1}', asObject, {}), { a: 1 });
  assert.equal(parseJson("{}", () => { throw new Error("bad"); }, "fb"), "fb");
});

test("the decoders: strings and flags keep what fits and drop the rest", () => {
  assert.deepEqual(strings(["a", "", 1, null, "a", "b"]), ["a", "b"]);
  assert.deepEqual(strings({ a: "x" }), []);
  assert.deepEqual(flags({ a: true, b: false, c: 1, d: "true" }), { a: true, b: false });
  assert.deepEqual(flags([true]), {});
  assert.equal(asObject(null), undefined);
  assert.equal(asObject([]), undefined);
});
