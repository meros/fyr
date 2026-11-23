// V0.admission-tidy: a thread that holds a slot and waits to grow it for a heavier command is in both of its host's
// lists. It shows once: its row stays in its group and says its job, then "waiting to grow" and why; the Waiting to run
// section has no row for it, but it keeps its place for a reorder. Shared code, so desktop and phone match.
import { test } from "node:test";
import assert from "node:assert/strict";
import { threadJob, threadSlot, type HostLoad } from "../src/host-load.ts";
import { slotQueue, withQueue } from "../src/slot-queue.ts";
import { sections } from "../src/store.ts";
import { thread } from "./helpers.ts";

const host: HostLoad = {
  host: "desk",
  cores: 8,
  load: [1, 1, 1],
  cgroups: true,
  threads: [{ id: "t_a", memory: 0, job: "go test ./..." }],
  slots: {
    n: 2,
    why: "needs ≈ 8.6 GB memory, 2.0 GB free",
    held: [
      { owner: "t_a", kind: "test", pct: 20 },
      { owner: "t_b", kind: "test", pct: 30 },
    ],
    waiting: [
      { owner: "t_a", kind: "e2e", pct: 60 },
      { owner: "t_b", kind: "e2e", pct: 60 },
      { owner: "t_c", kind: "build", pct: 30 },
    ],
  },
};

test("V0.admission-tidy: a holder that waits to grow says so after its job, once", () => {
  assert.equal(threadJob([host], "t_a"), "go test ./... ≈ 20 % · waiting to grow: needs ≈ 8.6 GB memory, 2.0 GB free");
  assert.equal(threadSlot([host], "t_a"), "go test ./... ≈ 20 % · waiting to grow: needs ≈ 8.6 GB memory, 2.0 GB free");
  assert.equal(threadSlot([host], "t_b"), "test job ≈ 30 % · waiting to grow (1 ahead)");
  assert.equal(threadSlot([host], "t_c"), "waiting (2 ahead) · needs ≈ 30 %");
});

test("V0.admission-tidy: a grower has no row in Waiting to run and stays in its group, but keeps its place", () => {
  const q = slotQueue([host]);
  assert.deepEqual(
    q.map((e) => [e.owner, e.pos, !!e.grow]),
    [
      ["t_a", 1, true],
      ["t_b", 2, true],
      ["t_c", 3, false],
    ],
  );
  const threads = { t_a: thread("t_a", "working", "working"), t_b: thread("t_b", "working", "working"), t_c: thread("t_c", "working", "working") };
  assert.deepEqual(
    withQueue(q, threads).map((r) => r.entry.owner),
    ["t_c"],
  );
  const sec = sections(threads, q);
  assert.deepEqual(
    sec.slots.map((t) => t.id),
    ["t_c"],
  );
  assert.ok(sec.working.some((t) => t.id === "t_a"), "the grower left its group");
});

test("V0.grow-waits-moving: a finished thread that waits to grow is in Moving without you, not in Finished", () => {
  const threads = { t_a: thread("t_a", "seen", "seen"), t_b: thread("t_b", "working", "working"), t_c: thread("t_c", "working", "working") };
  const s = sections(threads, slotQueue([host]));
  assert.ok(s.working.some((t) => t.id === "t_a"));
  assert.ok(!s.seen.some((t) => t.id === "t_a"));
});
