// SPEC 0.5 V0.134: the inbox filters (host and project) show only active values; a selected value stays.
import { test } from "node:test";
import assert from "node:assert/strict";
import { activeFilters, applyFilters, filterEmpty, inView, threadHost } from "../src/filters.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { Project, Thread } from "../src/types.ts";
import { thread } from "./helpers.ts";

const P = (id: string, name: string, extra: Partial<Project> = {}): Project => ({ id, name, threads: 1, lastUsed: "", ...extra }) as Project;
const projects = [P("p_web", "web"), P("p_fyr", "fyr"), P("p_home", "home")];
const H = (name: string, online = true): HostInfo => ({ name, online });
const hosts = [H("lab"), H("desk"), H("server", false)]; // the owner's order (V0.132): lab first
const NOW = Date.parse("2026-11-17T12:00:00Z");
const byId = (...ts: Thread[]) => Object.fromEntries(ts.map((t) => [t.id, t]));
const T = (id: string, section: "needs" | "working" | "seen" | "snoozed" | "settled", projectId: string, host: string, extra: Record<string, unknown> = {}) =>
  thread(id, "done", section, { projectId, project: projects.find((p) => p.id === projectId)?.name ?? "", host, ...extra });
const base = { projects, hosts, self: "desk", now: NOW, showUntil: 0, open: {} };
const names = (f: ReturnType<typeof activeFilters>) => ({ hosts: f.hosts.map((h) => h.name), projects: f.projects.map((p) => p.project.name) });

test("a thread's host: its own, else the front's own host", () => {
  assert.equal(threadHost(T("a", "needs", "p_web", "lab"), "desk"), "lab");
  assert.equal(threadHost(thread("b", "done", "needs"), "desk"), "desk");
});

test("in view: an unfolded section or pinned; Snoozed and Settled are folded by default; archived never", () => {
  assert.equal(inView(T("a", "needs", "p_web", "lab"), {}), true);
  assert.equal(inView(T("a", "settled", "p_web", "lab"), {}), false);
  assert.equal(inView(T("a", "snoozed", "p_web", "lab"), {}), false);
  assert.equal(inView(T("a", "settled", "p_web", "lab"), { settled: true }), true);
  assert.equal(inView(T("a", "needs", "p_web", "lab"), { needs: false }), false, "a section the owner folded");
  assert.equal(inView(T("a", "needs", "p_web", "lab", { pinnedAt: "2026-11-17T10:00:00Z" }), { needs: false }), true, "pinned threads are always shown");
  assert.equal(inView(thread("x", "done", "archived" as never), {}), false);
});

test("one chip per host and project with threads in view, in the owner's order", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "working", "p_web", "lab"), T("c", "seen", "p_web", "server"));
  const f = activeFilters({ ...base, threads, sel: { host: null, project: null } });
  assert.deepEqual(names(f), { hosts: ["lab", "desk", "server"], projects: ["web", "fyr"] });
  assert.deepEqual(f.hosts.map((h) => h.n), [1, 1, 1]);
  assert.equal(f.hosts[2].info?.online, false, "an offline host still has threads, so it counts");
});

test("a group with one active value shows no chips", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "working", "p_web", "desk"));
  assert.deepEqual(names(activeFilters({ ...base, threads, sel: { host: null, project: null } })), { hosts: [], projects: ["web", "fyr"] });
  const one = byId(T("a", "needs", "p_fyr", "desk"), T("b", "working", "p_fyr", "lab"));
  assert.deepEqual(names(activeFilters({ ...base, threads: one, sel: { host: null, project: null } })), { hosts: ["lab", "desk"], projects: [] });
});

test("folded sections are not considered", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "settled", "p_web", "lab"), T("c", "snoozed", "p_home", "server"));
  assert.deepEqual(names(activeFilters({ ...base, threads, sel: { host: null, project: null } })), { hosts: [], projects: [] });
  assert.deepEqual(names(activeFilters({ ...base, threads, open: { settled: true }, sel: { host: null, project: null } })), { hosts: ["lab", "desk"], projects: ["web", "fyr"] });
});

test("each group counts the threads the other group lets through (host AND project)", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "needs", "p_web", "lab"), T("c", "needs", "p_web", "desk"));
  // With desk picked, web and fyr both have threads there.
  assert.deepEqual(names(activeFilters({ ...base, threads, sel: { host: "desk", project: null } })).projects, ["web", "fyr"]);
  // With fyr picked, only desk has fyr threads: the host group has one value and nothing selected, so it hides.
  assert.deepEqual(names(activeFilters({ ...base, threads, sel: { host: null, project: "p_fyr" } })).hosts, []);
  // With web picked, lab and desk both have web threads.
  assert.deepEqual(names(activeFilters({ ...base, threads, sel: { host: null, project: "p_web" } })).hosts, ["lab", "desk"]);
});

test("a selected value stays, selected, after its last thread leaves or is folded away; its group stays too", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "settled", "p_web", "lab"));
  const f = activeFilters({ ...base, threads, sel: { host: "lab", project: "p_web" } });
  assert.deepEqual(names(f), { hosts: ["lab"], projects: ["web"] });
  assert.equal(f.hosts[0].selected && f.projects[0].selected, true);
  assert.equal(f.hosts[0].n + f.projects[0].n, 0);
  // A host that is no longer on the front's list keeps its chip by name.
  const g = activeFilters({ ...base, threads, sel: { host: "gone", project: null } });
  assert.deepEqual(g.hosts.map((h) => [h.name, h.selected, h.info]), [["desk", false, hosts[1]], ["gone", true, undefined]], "desk still has a thread in view; gone goes after the listed hosts");
});

test("a selected value sits in the owner's order among the active ones", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "needs", "p_home", "desk"));
  assert.deepEqual(names(activeFilters({ ...base, threads, sel: { host: null, project: "p_web" } })).projects, ["web", "fyr", "home"]);
});

test("V0.114: a quiet project is not shown or counted while locked; unlocked, it is; selected, it stays", () => {
  const quietUntil = "2026-11-17T18:00:00Z";
  const qp = [P("p_web", "web"), P("p_fyr", "fyr"), P("p_home", "home", { quietUntil })];
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "needs", "p_web", "desk"), T("c", "needs", "p_home", "lab", { quietUntil }));
  const locked = activeFilters({ ...base, projects: qp, threads, sel: { host: null, project: null } });
  assert.deepEqual(names(locked), { hosts: [], projects: ["web", "fyr"] }, "lab has only quiet threads: not counted");
  const unlocked = activeFilters({ ...base, projects: qp, threads, showUntil: NOW + 60_000, sel: { host: null, project: null } });
  assert.deepEqual(names(unlocked).projects, ["web", "fyr", "home"]);
  assert.deepEqual(names(unlocked).hosts, [], "unlocked quiet threads stay out of the inbox (V0.46) unless their project is picked");
  const picked = activeFilters({ ...base, projects: qp, threads, sel: { host: null, project: "p_home" } });
  assert.deepEqual(names(picked).projects, ["web", "fyr", "home"]);
  assert.equal(picked.projects[2].selected, true);
});

test("applyFilters: host AND project; a project that no longer exists filters nothing", () => {
  const threads = byId(T("a", "needs", "p_fyr", "desk"), T("b", "needs", "p_web", "lab"), T("c", "needs", "p_web", "desk"));
  assert.deepEqual(Object.keys(applyFilters(threads, { host: "desk", project: "p_web" }, projects, "desk")), ["c"]);
  assert.deepEqual(Object.keys(applyFilters(threads, { host: "desk", project: null }, projects, "desk")), ["a", "c"]);
  assert.deepEqual(Object.keys(applyFilters(threads, { host: null, project: "p_gone" }, projects, "desk")), ["a", "b", "c"]);
});

test("the empty state: a filter is on and nothing it lets through is in view", () => {
  const threads = byId(T("a", "settled", "p_web", "lab"), T("b", "settled", "p_web", "lab"));
  assert.equal(filterEmpty(threads, { host: null, project: null }, {}), null, "no filter: the plain list");
  assert.equal(filterEmpty(threads, { host: "lab", project: null }, {}), "2 more in folded sections.");
  assert.equal(filterEmpty({}, { host: "lab", project: null }, {}), "");
  assert.equal(filterEmpty(threads, { host: "lab", project: null }, { settled: true }), null, "a thread in view: no empty state");
});
