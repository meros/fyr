// SPEC V0.97: Merge all green on the Changes board.
import { test } from "node:test";
import assert from "node:assert/strict";
import { greenMerges, mergeAllGreen, type GreenMerge } from "../src/board.ts";
import type { BoardItem } from "../src/board.ts";
import type { Change, Offer, Thread } from "../src/types.ts";
import { thread } from "./helpers.ts";
import { headLabel } from "../src/pr.ts";

const merge: Offer = { id: "fyr.merge", label: "Merge", run: "fyr", effect: "pr.merge", by: "observed", digest: "d1" };
const ch = (id: string, url: string, head: string, ready?: Change["ready"]): Change => ({
  id, key: id, kind: "code", title: id, phase: "review", phaseBy: "observed", createdAt: "2026-11-16T09:00:00Z", updatedAt: "2026-11-16T09:00:00Z",
  pr: { url, number: Number(url.split("/").pop()), head, state: "OPEN" }, ready, offers: [merge],
});
const item = (t: Thread, c: Change): BoardItem => ({ change: c, thread: t });

test("SPEC V0.97: Merge all green takes the changes ready to merge, one per PR, each with the head the board showed", () => {
  const t1 = thread("t1", "seen", "seen");
  const t2 = thread("t2", "seen", "seen");
  const items = [
    item(t1, ch("c1", "https://github.com/a/b/pull/1", "aaaaaaa1111", "Merge")),
    item(t2, ch("c2", "https://github.com/a/b/pull/1", "aaaaaaa1111", "Merge")), // the same PR in another thread
    item(t1, ch("c3", "https://github.com/a/b/pull/2", "bbbbbbb2222", "Review and merge")),
    item(t2, ch("c4", "https://github.com/a/b/pull/3", "ccccccc3333", "Merge")),
    item(t2, ch("c5", "https://github.com/a/b/pull/4", "ddddddd4444")),
  ];
  const got = greenMerges(items);
  assert.deepEqual(got.map((m) => [m.thread, m.args]), [
    ["t1", { on: "c1", action: "fyr.merge", digest: "d1", head: "aaaaaaa1111" }],
    ["t2", { on: "c4", action: "fyr.merge", digest: "d1", head: "ccccccc3333" }],
  ]);
});

test("SPEC V0.97: Merge all green sends one merge after another; a refusal stays on its row and the rest go on", async () => {
  const t1 = thread("t1", "seen", "seen");
  const list = greenMerges([
    item(t1, ch("c1", "https://github.com/a/b/pull/1", "a1", "Merge")),
    item(t1, ch("c2", "https://github.com/a/b/pull/2", "b2", "Merge")),
    item(t1, ch("c3", "https://github.com/a/b/pull/3", "c3", "Merge")),
  ]);
  const log: string[] = [];
  let inFlight = 0;
  const send = async (m: GreenMerge) => {
    inFlight++;
    assert.equal(inFlight, 1, "one merge at a time");
    log.push("start " + m.change);
    await new Promise((r) => setTimeout(r, 5));
    log.push("end " + m.change);
    inFlight--;
    return m.change === "c2" ? { ok: false, error: "The head moved. Look again." } : { ok: true };
  };
  const errs = await mergeAllGreen(list, send);
  assert.deepEqual(log, ["start c1", "end c1", "start c2", "end c2", "start c3", "end c3"]);
  assert.deepEqual(errs, { "t1/c2": "The head moved. Look again." });
});

test("SPEC V0.97: a Merge button names the head it merges", () => {
  assert.equal(headLabel("Merge", "a1b2c3d4e5f60718"), "Merge a1b2c3d");
  assert.equal(headLabel("Merge", undefined), "Merge");
});
