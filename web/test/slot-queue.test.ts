// SPEC V0.137: the threads that wait for a slot have their own section, numbered by their real place in the host's
// queue, and the owner reorders it.
import { test } from "node:test";
import assert from "node:assert/strict";
import type { HostLoad } from "../src/host-load.ts";
import { flatOrder, sections, SECTIONS } from "../src/store.ts";
import { queueParts, reorderOwners, slotQueue, withQueue } from "../src/slot-queue.ts";
import { thread } from "./helpers.ts";

const host = (name: string, waiting: { owner: string; kind: string; priority?: boolean }[], held = 2): HostLoad => ({
  host: name,
  cores: 8,
  load: [1, 1, 1],
  cgroups: false,
  threads: [],
  slots: { n: 2, held: Array.from({ length: held }, (_, i) => ({ owner: `h${name}${i}`, kind: "test" })), waiting },
});

test("V0.137: the queue is numbered per host by the real place, priority waiters first and not movable", () => {
  const q = slotQueue([
    host("desk", [
      { owner: "self-deploy", kind: "test", priority: true },
      { owner: "t_a", kind: "build" },
      { owner: "t_b", kind: "e2e" },
    ]),
    host("lab", [{ owner: "t_c", kind: "test" }]),
    host("idle", []),
  ]);
  assert.deepEqual(
    q.map((e) => [e.host, e.owner, e.pos, e.kind, e.priority]),
    [
      ["desk", "self-deploy", 1, "test", true],
      ["desk", "t_a", 2, "build", false],
      ["desk", "t_b", 3, "e2e", false],
      ["lab", "t_c", 1, "test", false],
    ],
  );
  assert.deepEqual(slotQueue([host("desk", [])]), []);
  assert.deepEqual(slotQueue([{ ...host("desk", []), slots: undefined }]), []);
});

test("V0.137: a queued thread moves to Waiting for a slot from its section; Needs you keeps its own", () => {
  const threads = {
    t_a: thread("t_a", "done", "seen"),
    t_b: thread("t_b", "working", "working"),
    t_n: thread("t_n", "asking", "needs"),
    t_x: thread("t_x", "done", "seen"),
  };
  const q = slotQueue([host("desk", [{ owner: "t_b", kind: "test" }, { owner: "t_n", kind: "test" }, { owner: "t_a", kind: "build" }])]);
  const sec = sections(threads, q);
  assert.deepEqual(
    sec.slots.map((t) => t.id),
    ["t_b", "t_a"],
    "in the queue's order",
  );
  assert.deepEqual(
    sec.unread.map((t) => t.id),
    ["t_x"],
  );
  assert.deepEqual(sec.working, []);
  assert.deepEqual(
    sec.needs.map((t) => t.id),
    ["t_n"],
  );
  // the section sits right after Working, and j/k walk it there
  assert.deepEqual(
    SECTIONS.map((s) => s.key),
    ["needs", "unread", "seen", "working", "slots", "snoozed", "settled"],
  );
  assert.deepEqual(flatOrder(sec), ["t_n", "t_x", "t_b", "t_a"]);
  // without a queue nothing moves
  assert.deepEqual(sections(threads).slots, []);
});

test("V0.137: a pinned queued thread stays in the queue's section, not in the pinned block", () => {
  const threads = { t_a: thread("t_a", "done", "seen", { pinnedAt: "2026-11-17T09:00:00Z" }) };
  const sec = sections(threads, slotQueue([host("desk", [{ owner: "t_a", kind: "test" }])]));
  assert.deepEqual(
    sec.slots.map((t) => t.id),
    ["t_a"],
  );
  assert.deepEqual(sec.pinned, []);
  assert.deepEqual(flatOrder(sec), ["t_a"]);
});

test("V0.137: the section's parts are one per host with a queue; the host shows only when more than one has one", () => {
  const one = queueParts(slotQueue([host("desk", [{ owner: "t_a", kind: "test" }]), host("lab", [])]));
  assert.equal(one.length, 1);
  assert.equal(one[0].label, null);
  const two = queueParts(slotQueue([host("desk", [{ owner: "t_a", kind: "test" }]), host("lab", [{ owner: "t_b", kind: "e2e" }])]));
  assert.deepEqual(
    two.map((p) => [p.host, p.label, p.entries.map((e) => e.owner)]),
    [
      ["desk", "desk", ["t_a"]],
      ["lab", "lab", ["t_b"]],
    ],
  );
});

test("V0.137: withQueue matches entries to the threads shown; a hidden thread keeps its number", () => {
  const q = slotQueue([host("desk", [{ owner: "self-deploy", kind: "test", priority: true }, { owner: "t_a", kind: "test" }, { owner: "t_hidden", kind: "test" }, { owner: "t_b", kind: "e2e" }])]);
  const rows = withQueue(q, { t_a: thread("t_a", "done", "seen"), t_b: thread("t_b", "done", "seen") });
  assert.deepEqual(
    rows.map((r) => [r.entry.owner, r.entry.pos, r.thread?.id ?? null]),
    [
      ["self-deploy", 1, null],
      ["t_a", 2, "t_a"],
      ["t_b", 4, "t_b"],
    ],
  );
});

test("V0.137: a move among the rows shown keeps hidden waiters in their place; priority waiters are never sent", () => {
  const all = ["t_a", "t_hidden", "t_b", "t_c"];
  const shown = ["t_a", "t_b", "t_c"];
  // t_c to the top of what is shown
  assert.deepEqual(reorderOwners(all, shown, 2, 0), ["t_c", "t_hidden", "t_a", "t_b"]);
  // t_a one down
  assert.deepEqual(reorderOwners(all, shown, 0, 1), ["t_b", "t_hidden", "t_a", "t_c"]);
  // nothing moves
  assert.deepEqual(reorderOwners(all, shown, 1, 1), all);
});
