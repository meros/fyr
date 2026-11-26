// SPEC 0.5 V0.114: every way the owner can get into a quiet project is shut. V0.quiet-lift: the one way in is to lift
// the quiet on the project's page; the host then sends the project and its threads without quietUntil, so every path,
// every device and every host of a linked project open at once. One test per path; the phone and the desktop share
// each function.
import { test } from "node:test";
import assert from "node:assert/strict";
import { admitThread, createProjects, hideQuiet, isQuiet, reachable } from "../src/quiet.ts";
import { flatOrder, sections } from "../src/store.ts";
import type { Project, Thread } from "../src/types.ts";
import { filterByProject, thread } from "./helpers.ts";

const now = Date.parse("2026-11-16T20:00:00Z");
const until = "2026-11-17T05:00:00Z";
const projects = [
  { id: "p_acme", name: "Acme", quietUntil: until },
  { id: "p_fyr", name: "Fyr" },
] as Project[];
const threads = {
  q1: thread("q1", "asking", "needs", { projectId: "p_acme", quietUntil: until }),
  q2: thread("q2", "seen", "seen", { projectId: "p_acme", quietUntil: until }),
  f1: thread("f1", "done", "needs", { projectId: "p_fyr" }),
};
/** What the host sends while Acme's quiet is lifted: no quietUntil on the project or its threads. */
const lifted = Object.fromEntries(Object.entries(threads).map(([k, t]) => [k, { ...t, quietUntil: undefined }])) as Record<string, Thread>;
const liftedProjects = projects.map((p) => ({ ...p, quietUntil: undefined, quietLift: "2026-11-16T20:30:00Z" })) as Project[];

/** The inbox the app draws: the filter's threads, what the quiet lets through, then V0.46's hiding. */
function inbox(filter: string | null, query: string, ts: Record<string, Thread> = threads, ps = projects) {
  const reach = reachable(filterByProject(ts, filter, ps), now);
  return hideQuiet(reach, !!filter || !!query, now);
}

test("project filter (desktop chip, phone chips): a quiet project picked shows none of its threads until lifted", () => {
  assert.deepEqual(Object.keys(inbox("p_acme", "")), [], "V0.46 (4) showed them on one tap");
  assert.deepEqual(Object.keys(inbox("p_acme", "", lifted, liftedProjects)).sort(), ["q1", "q2"], "lifted, they show");
  assert.deepEqual(Object.keys(inbox("p_fyr", "")), ["f1"], "a project that is not quiet is not touched");
});

test("search (desktop ⌘K, phone): quiet threads are not found until lifted", () => {
  assert.deepEqual(Object.keys(reachable(threads, now)).sort(), ["f1"]);
  assert.deepEqual(Object.keys(reachable(lifted, now)).sort(), ["f1", "q1", "q2"]);
  assert.deepEqual(Object.keys(inbox(null, "q")), ["f1"]);
});

test("New thread: a quiet project is not offered; lifted it is", () => {
  assert.deepEqual(createProjects(projects, now).map((p) => p.id), ["p_fyr"]);
  assert.deepEqual(createProjects(liftedProjects, now).map((p) => p.id), ["p_acme", "p_fyr"]);
  assert.equal(isQuiet(liftedProjects[0], now), false);
});

test("a link (From thread, Started by, a [Fyr] notice, a board card, Needs you) to a quiet thread shows the gate", () => {
  assert.deepEqual(admitThread("f1", "q1", threads.q1, now), { admitted: null, gated: true });
  assert.deepEqual(admitThread("f1", "q1", lifted.q1, now), { admitted: "q1", gated: false });
  assert.deepEqual(admitThread("q1", "f1", threads.f1, now), { admitted: "f1", gated: false });
});

test("?open= and a push tap: the thread is not admitted before it loads, and gated when it arrives", () => {
  assert.deepEqual(admitThread(null, "q1", undefined, now), { admitted: null, gated: false });
  assert.deepEqual(admitThread(null, "q1", threads.q1, now), { admitted: null, gated: true });
});

test("back and forward: leaving a quiet thread and coming back goes through the gate again", () => {
  let st = admitThread(null, "q1", lifted.q1, now);
  assert.equal(st.gated, false);
  st = admitThread(st.admitted, null, undefined, now); // the inbox (phone) or another screen
  st = admitThread(st.admitted, "q1", threads.q1, now); // back, once the lift has ended
  assert.equal(st.gated, true);
  st = admitThread("f1", "q1", threads.q1, now); // desktop: another thread, then back
  assert.equal(st.gated, true);
});

test("an open thread keeps working when its window starts, or its lift ends (V0.46 (6))", () => {
  const before = admitThread(null, "f1", threads.f1, now);
  const quietNow = thread("f1", "done", "needs", { projectId: "p_fyr", quietUntil: until });
  assert.deepEqual(admitThread(before.admitted, "f1", quietNow, now), { admitted: "f1", gated: false });
});

test("j/k and g1–g5 walk the inbox order, which holds no quiet thread", () => {
  const all = { needs: Infinity, working: Infinity, seen: Infinity, settled: Infinity, snoozed: Infinity };
  assert.deepEqual(flatOrder(sections(inbox("p_acme", "")), all), []);
  assert.deepEqual(flatOrder(sections(inbox(null, "")), all), ["f1"]);
});
