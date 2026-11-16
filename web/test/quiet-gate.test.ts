// SPEC 0.5 V0.114: every way the owner can get into a quiet project goes through the same 30 s hold (V0.74), with one
// unlock per device for all of them. One test per path; the phone and the desktop share each function.
import { test } from "node:test";
import assert from "node:assert/strict";
import { QUIET_SHOW_MS, admitThread, createProjects, hideQuiet, loadQuietShow, quietLocked, reachable, setQuietShow, subscribeQuietShow } from "../src/quiet.ts";
import { flatOrder, sections } from "../src/store.ts";
import { filterByProject } from "../src/projects.ts";
import type { Project } from "../src/types.ts";
import { thread } from "./helpers.ts";

const now = Date.parse("2026-11-16T20:00:00Z");
const until = "2026-11-17T05:00:00Z";
const projects = [
  { id: "p_norban", name: "Norban", quietUntil: until },
  { id: "p_fyr", name: "Fyr" },
] as Project[];
const threads = {
  q1: thread("q1", "asking", "needs", { projectId: "p_norban", quietUntil: until }),
  q2: thread("q2", "seen", "seen", { projectId: "p_norban", quietUntil: until }),
  f1: thread("f1", "done", "needs", { projectId: "p_fyr" }),
};
const unlocked = now + QUIET_SHOW_MS;

/** A Storage stand-in. */
function kv() {
  const m = new Map<string, string>();
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k) };
}

/** The inbox the app draws: the filter's threads, what the lock lets through, then V0.46's hiding. */
function inbox(filter: string | null, query: string, showUntil: number) {
  const reach = reachable(filterByProject(threads, filter, projects), now, showUntil);
  return hideQuiet(reach, !!filter || !!query, now);
}

test("project filter (desktop chip, phone chips): a quiet project picked shows none of its threads without the hold", () => {
  assert.deepEqual(Object.keys(inbox("p_norban", "", 0)), [], "V0.46 (4) showed them on one tap");
  assert.deepEqual(Object.keys(inbox("p_norban", "", unlocked)).sort(), ["q1", "q2"], "after the hold they show");
  assert.deepEqual(Object.keys(inbox("p_fyr", "", 0)), ["f1"], "a project that is not quiet is not touched");
});

test("search (desktop ⌘K, phone): quiet threads are not found without the hold", () => {
  const found = (showUntil: number) => Object.keys(reachable(filterByProject(threads, null, projects), now, showUntil)).sort();
  assert.deepEqual(found(0), ["f1"]);
  assert.deepEqual(found(unlocked), ["f1", "q1", "q2"]);
  assert.deepEqual(Object.keys(inbox(null, "q", 0)), ["f1"]);
});

test("New thread: the same unlock as every other path", () => {
  const s = kv();
  assert.deepEqual(createProjects(projects, now, loadQuietShow(s)).map((p) => p.id), ["p_fyr"]);
  setQuietShow(s, now);
  assert.deepEqual(createProjects(projects, now, loadQuietShow(s)).map((p) => p.id), ["p_norban", "p_fyr"]);
});

test("one hold opens every path; Hide shuts every path, and listeners hear both", () => {
  const s = kv();
  const heard: number[] = [];
  const off = subscribeQuietShow((e) => heard.push(e));
  const end = setQuietShow(s, now);
  assert.equal(end, unlocked);
  assert.equal(quietLocked(projects[0], now, loadQuietShow(s)), false, "filter, search and links read the same value");
  setQuietShow(s, null);
  assert.equal(quietLocked(projects[0], now, loadQuietShow(s)), true);
  off();
  setQuietShow(s, now);
  assert.deepEqual(heard, [unlocked, 0], "an unsubscribed listener hears nothing more");
  assert.equal(quietLocked(projects[0], unlocked, unlocked), true, "the unlock lasts 30 minutes");
});

test("a link (From thread, Started by, a [Fyr] notice, a board card, Needs you) to a quiet thread shows the gate", () => {
  assert.deepEqual(admitThread("f1", "q1", threads.q1, now, 0), { admitted: null, gated: true });
  assert.deepEqual(admitThread("f1", "q1", threads.q1, now, unlocked), { admitted: "q1", gated: false });
  assert.deepEqual(admitThread("q1", "f1", threads.f1, now, 0), { admitted: "f1", gated: false });
});

test("?open= and a push tap: the thread is not admitted before it loads, and gated when it arrives", () => {
  assert.deepEqual(admitThread(null, "q1", undefined, now, 0), { admitted: null, gated: false });
  assert.deepEqual(admitThread(null, "q1", threads.q1, now, 0), { admitted: null, gated: true });
});

test("back and forward: leaving a quiet thread and coming back goes through the gate again", () => {
  let st = admitThread(null, "q1", threads.q1, now, unlocked);
  assert.equal(st.gated, false);
  st = admitThread(st.admitted, null, undefined, now, 0); // the inbox (phone) or another screen
  st = admitThread(st.admitted, "q1", threads.q1, now, 0); // back, once the unlock has ended
  assert.equal(st.gated, true);
  st = admitThread("f1", "q1", threads.q1, now, 0); // desktop: another thread, then back
  assert.equal(st.gated, true);
});

test("an open thread keeps working when its window starts (V0.46 (6))", () => {
  const before = admitThread(null, "f1", threads.f1, now, 0);
  const quietNow = thread("f1", "done", "needs", { projectId: "p_fyr", quietUntil: until });
  assert.deepEqual(admitThread(before.admitted, "f1", quietNow, now, 0), { admitted: "f1", gated: false });
});

test("j/k and g1–g5 walk the inbox order, which holds no quiet thread without the hold", () => {
  const order = flatOrder(sections(inbox("p_norban", "", 0)), { needs: Infinity, working: Infinity, seen: Infinity, settled: Infinity, snoozed: Infinity });
  assert.deepEqual(order, []);
  const all = flatOrder(sections(inbox(null, "", 0)), { needs: Infinity, working: Infinity, seen: Infinity, settled: Infinity, snoozed: Infinity });
  assert.deepEqual(all, ["f1"]);
});
