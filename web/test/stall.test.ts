// SPEC 0.5 V0.147: a host's stall reaches the banner, from its state and from the stream.
import { test } from "node:test";
import assert from "node:assert/strict";
import { decodeStall, shownStall, stallKey, stallLine } from "../src/stall.ts";
import { applySnapshot, initialStore, setStall, viewOf } from "../src/store.ts";
import { TH, hid, snapshot } from "./helpers.ts";

const a = { at: "2026-11-17T11:07:10Z", what: "lock engine.mu held 12s by goroutine 81 at engine.(*Engine).Tick", file: "/d/stalls/x.txt", exiting: false };

test("a host's own stall is named by the host", () => {
  assert.deepEqual(decodeStall(a, "desk"), { host: "desk", ...a });
  assert.equal(decodeStall({ nope: 1 }, "desk"), null);
  assert.equal(decodeStall("x"), null);
});

test("the stream replaces a host's stall, and the newest undismissed shows", () => {
  let s = applySnapshot(initialStore(), snapshot({ hosts: [{ name: "desk", id: TH, online: true }, { name: "lab", id: hid("L"), online: true }] }), 0);
  s = setStall(s, TH, a);
  assert.equal(viewOf(s).stalls[0].host, "desk", "slice 2b: a stall is its host's, named by it");
  const later = { ...a, at: "2026-11-17T12:10:00Z", exiting: true };
  s = setStall(s, TH, later);
  assert.equal(viewOf(s).stalls.length, 1);
  const other = { host: "lab", ...a, at: "2026-11-17T12:00:00Z" };
  assert.equal(viewOf(setStall(s, hid("L"), other)).stalls.length, 2, "each host keeps its own");
  const list = viewOf(setStall(s, hid("L"), other)).stalls;
  assert.equal(shownStall(list, null)?.at, later.at);
  assert.equal(shownStall(list, stallKey(list[0]))?.host, "lab");
  assert.equal(shownStall([], null), null);
});

test("the line says what happened in words, the details where the dump is", () => {
  const l = stallLine({ host: "desk", ...a }, false);
  assert.match(l.text, /^Fyr stalled at \d\d:\d\d: a step took over 10 s$/);
  assert.match(l.more, /Held longest: lock engine\.mu held 12s/);
  assert.match(l.more, /Goroutine dump: \/d\/stalls\/x\.txt/);
  assert.match(stallLine({ host: "lab", ...a, exiting: true }, true).text, /^Fyr was stuck and restarted itself at \d\d:\d\d on lab; no turn was lost$/);
});
