// V0.filmstrip-fold (#61): the filmstrip folds on both fronts, hides per device and thread, and the header chip brings it back.
import { test } from "node:test";
import assert from "node:assert/strict";
import { bringBackFilmstrip, filmstripHidden, filmstripKey, filmstripOpen, filmstripView, previewsLabel, setFilmstripHidden, setFilmstripOpen, showsPreviewsChip, takeReveal } from "../src/filmstrip.ts";
import { moveHostKeys } from "../src/host-rotation.ts";
import { ref } from "../src/hostfeed.ts";

const m = new Map<string, string>();
const store = { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k), key: (i: number) => [...m.keys()][i] ?? null, get length() { return m.size; } };
(globalThis as { localStorage?: unknown }).localStorage = store;

test("the key is the thread's ref, so two hosts' threads of one id never share it", () => {
  assert.equal(filmstripKey(ref("HOST-A", "t1")), "fyr.filmstrip.HOST-A/t1");
  assert.notEqual(filmstripKey(ref("HOST-A", "t1")), filmstripKey(ref("HOST-B", "t1")));
});

test("folded by default, open on a press, nothing without previews", () => {
  assert.equal(filmstripView(3, false, false), "folded");
  assert.equal(filmstripView(3, false, true), "open");
  assert.equal(filmstripView(3, true, true), "hidden");
  assert.equal(filmstripView(0, false, true), "none");
  assert.equal(filmstripView(0, true, false), "none");
  assert.equal(previewsLabel(1), "1 preview");
  assert.equal(previewsLabel(4), "4 previews");
});

test("the chip shows only while the strip is hidden and has previews", () => {
  assert.equal(showsPreviewsChip(2, true), true);
  assert.equal(showsPreviewsChip(2, false), false);
  assert.equal(showsPreviewsChip(0, true), false);
});

test("a hide is kept per thread on this device; showing it again forgets the key", () => {
  const a = ref("HOST-A", "t1");
  const b = ref("HOST-A", "t2");
  assert.equal(filmstripHidden(a), false);
  setFilmstripOpen(a, true);
  setFilmstripHidden(a, true);
  assert.equal(m.get(filmstripKey(a)), "hidden");
  assert.equal(filmstripHidden(a), true);
  assert.equal(filmstripOpen(a), false, "a hide folds the strip");
  assert.equal(filmstripHidden(b), false, "another thread keeps its strip");
  setFilmstripHidden(a, false);
  assert.equal(m.has(filmstripKey(a)), false);
  assert.equal(filmstripHidden(a), false);
});

test("a hide stored before this page loaded is read back", () => {
  const c = ref("HOST-A", "t3");
  m.set(filmstripKey(c), "hidden");
  assert.equal(filmstripHidden(c), true);
});

test("a host's rotation moves the hide with its other refs", () => {
  m.clear();
  m.set(filmstripKey(ref("OLD1-OLD2-OLD3-OLD4", "t9")), "hidden");
  assert.equal(moveHostKeys(store, "OLD1-OLD2-OLD3-OLD4", "NEW1-NEW2-NEW3-NEW4"), 1);
  assert.equal(filmstripHidden(ref("NEW1-NEW2-NEW3-NEW4", "t9")), true);
});

test("the chip brings the strip back open and asks it once to scroll into view", () => {
  const d = ref("HOST-A", "t4");
  setFilmstripHidden(d, true);
  bringBackFilmstrip(d);
  assert.equal(filmstripHidden(d), false);
  assert.equal(filmstripOpen(d), true);
  assert.equal(takeReveal(ref("HOST-A", "t5")), false, "another thread's strip does not scroll");
  assert.equal(takeReveal(d), true);
  assert.equal(takeReveal(d), false);
});

test("with storage blocked, a hide lasts this page", () => {
  const e = ref("HOST-A", "t6");
  const saved = (globalThis as { localStorage?: unknown }).localStorage;
  const deny = () => {
    throw new Error("denied");
  };
  (globalThis as { localStorage?: unknown }).localStorage = { getItem: deny, setItem: deny, removeItem: deny };
  try {
    setFilmstripHidden(e, true);
    assert.equal(filmstripHidden(e), true);
  } finally {
    (globalThis as { localStorage?: unknown }).localStorage = saved;
  }
});
