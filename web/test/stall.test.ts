// SPEC 0.5 V0.147: a host's stall reaches the banner, from the state (a host's or a front's) and from the stream.
import { test } from "node:test";
import assert from "node:assert/strict";
import { decodeStall, shownStall, stallKey, stallLine, stallsOf, upsertStall } from "../src/stall.ts";
import { initialStore, setStall } from "../src/store.ts";

const a = { at: "2026-11-17T11:07:10Z", what: "lock engine.mu held 12s by goroutine 81 at engine.(*Engine).Tick", file: "/d/stalls/x.txt", exiting: false };

test("a host's own stall is named by the host; a front's stalls carry theirs", () => {
  assert.deepEqual(stallsOf({ host: "desk", stall: a }), [{ host: "desk", ...a }]);
  assert.deepEqual(stallsOf({ stalls: [{ ...a, host: "lab" }, { nope: 1 }] }), [{ host: "lab", ...a }]);
  assert.deepEqual(stallsOf({ host: "desk" }), []);
  assert.equal(decodeStall("x"), null);
});

test("the stream replaces a host's stall, and the newest undismissed shows", () => {
  let s = setStall({ ...initialStore(), host: "desk" }, a);
  const later = { ...a, at: "2026-11-17T12:10:00Z", exiting: true };
  s = setStall(s, later);
  assert.equal(s.stalls.length, 1);
  const other = { host: "lab", ...a, at: "2026-11-17T12:00:00Z" };
  const list = upsertStall(s.stalls, other);
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
