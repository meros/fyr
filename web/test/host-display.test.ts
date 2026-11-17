// SPEC V0.140: the display surfaces say which host: each ring of the rail's load names its host, a change card on the
// board names its thread's host, with several hosts; with one, nothing changes.
import { test } from "node:test";
import assert from "node:assert/strict";
import { railLabel, type HostLoad } from "../src/load.ts";
import { cardHost } from "../src/board.ts";

const desk: HostLoad = { host: "desk", cores: 8, load: [2.6, 2, 1], threads: [], slots: { n: 2, held: [{ thread: "t1", kind: "test" }] as never, waiting: [] as never } } as unknown as HostLoad;

test("V0.140: a load ring's label names its host when the rail has several", () => {
  assert.equal(railLabel(desk, true), "desk: load 2.6 on 8 cores, 1 of 2 slots in use");
  assert.equal(railLabel(desk, false), "Load 2.6 on 8 cores, 1 of 2 slots in use");
});

test("V0.140: a change card names its thread's host with several hosts", () => {
  const hosts = [{ name: "desk", online: true }, { name: "lab", online: true }];
  assert.equal(cardHost({ thread: { host: "lab" } }, hosts), "lab");
  assert.equal(cardHost({ thread: { host: "lab" } }, [hosts[0]]), "");
  assert.equal(cardHost({ thread: {} }, hosts), "");
});
