import { test } from "node:test";
import assert from "node:assert/strict";
import { projectIconUrl, splitSharedPatch, expandOrder, findProject, linkCandidates, linkProjects, memberOn, projectOf, projectRecords, projectSubtitle, recordOf } from "../src/projects.ts";
import { applySnapshot, initialStore, setHosts, setProjectOrder, stateForCache, upsertProject } from "../src/store.ts";
import { activeFilters, applyFilters, liveProject } from "../src/filters.ts";
import { chooseHost, initialForm, settleForm } from "../src/new-thread.ts";
import { applyHidden, projectIsHidden, pushHiddenOf, setProjectShown } from "../src/hidden.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { Project } from "../src/types.ts";
import { thread } from "./helpers.ts";

const host = (id: string, name: string, online = true) => ({ id, name, online }) as HostInfo;
const A = host("A", "desk"), B = host("B", "mac");
const rec = (hostId: string, pid: string, name: string, extra: Partial<Project> = {}): Project => ({ id: `${hostId}/${pid}`, rid: pid, hostId, host: hostId === "A" ? "desk" : "mac", name, threads: 1, ...extra });
const on = (id: string, h: string, p: Project) => thread(id, "working", "working", { host: h, projectId: p.id });
const fyrA = rec("A", "p1", "Fyr", { link: "l_1", color: "rose", threads: 2 });
const fyrB = rec("B", "p9", "fyr-mac", { link: "l_1", color: "blue", threads: 3 });
const other = rec("B", "p2", "Stacks");

test("without links the list is the records themselves", () => {
  const rs = [rec("A", "p1", "One"), other];
  const out = linkProjects(rs, [A, B]);
  assert.deepEqual(out, rs);
  assert.equal(out[0], rs[0]);
});

test("a link with one member is an ordinary project", () => {
  assert.deepEqual(linkProjects([fyrA, other], [A, B]), [fyrA, other]);
});

test("a linked project is one entry at its earliest member's place, shown from the first host and counting all threads", () => {
  const out = linkProjects([other, fyrB, fyrA], [A, B]);
  assert.deepEqual(out.map((p) => p.name), ["Stacks", "Fyr"]);
  assert.equal(out[1].id, fyrA.id);
  assert.equal(out[1].threads, 5);
  assert.deepEqual(out[1].members!.map((m) => m.id), [fyrA.id, fyrB.id]);
  assert.equal(projectSubtitle(out[1]), "On 2 hosts");
});

test("an offline host's member is not the one shown", () => {
  const [p] = linkProjects([fyrA, fyrB], [host("A", "desk", false), B]);
  assert.equal(p.name, "fyr-mac");
});

test("every member's id finds the project, and its own record", () => {
  const ps = linkProjects([fyrA, fyrB, other], [A, B]);
  assert.equal(findProject(ps, fyrB.id)?.id, fyrA.id);
  assert.equal(recordOf(ps, fyrB.id)?.name, "fyr-mac");
  assert.equal(memberOn(ps[0], "B")?.id, fyrB.id);
  assert.equal(projectRecords(ps).length, 3);
  assert.equal(projectOf({ projectId: fyrB.id, project: "" }, ps)?.id, fyrA.id);
});

test("the filter and the chips treat both hosts' threads as one project", () => {
  const ps = linkProjects([fyrA, fyrB, other], [A, B]);
  const threads = { a: on("a", "desk", fyrA), b: on("b", "mac", fyrB), c: on("c", "mac", other) };
  // A filter saved with the member that is not shown still holds.
  const sel = { hostsOff: [], project: fyrB.id! };
  assert.equal(liveProject(sel, ps), fyrA.id);
  assert.deepEqual(Object.keys(applyFilters(threads, sel, ps, "desk")).sort(), ["a", "b"]);
  const chips = activeFilters({ threads, projects: ps, hosts: [A, B], self: "desk", sel: { hostsOff: [], project: null }, open: {}, now: 0, showUntil: 0 });
  assert.deepEqual(chips.projects.map((c) => [c.project.name, c.n]), [["Fyr", 2], ["Stacks", 1]]);
});

test("the store keeps records: a frame for a member, a new host order and the device cache", () => {
  let s = applySnapshot(initialStore(), { now: new Date(0).toISOString(), user: "o", hosts: [A, B], threads: [], actions: [], profiles: [], projects: [fyrA, fyrB, other] } as never, 0);
  assert.equal(s.projects.length, 2);
  s = upsertProject(s, { ...fyrB, name: "Renamed", rev: 9 });
  assert.equal(recordOf(s.projects, fyrB.id)?.name, "Renamed");
  assert.equal(s.projects.length, 2);
  assert.equal(setHosts(s, [B, A]).projects.find((p) => p.members)?.name, "Renamed");
  assert.equal((stateForCache(s, "x").projects ?? []).length, 3);
  s = upsertProject(s, { id: fyrB.id, deleted: true } as Project);
  assert.ok(!s.projects.some((p) => p.members), "one member left: an ordinary project");
});

test("reordering moves the members together", () => {
  const ps = linkProjects([fyrA, fyrB, other], [A, B]);
  assert.deepEqual(expandOrder([other.id!, fyrA.id!], ps), [other.id, fyrA.id, fyrB.id]);
  const s = setProjectOrder({ ...initialStore(), hosts: [A, B], projects: ps }, expandOrder([other.id!, fyrA.id!], ps));
  assert.deepEqual(s.projects.map((p) => p.name), ["Stacks", "Fyr"]);
});

test("what can join: another host's project outside any other link", () => {
  const third = rec("C", "p3", "Fyr", {});
  const taken = rec("C", "p4", "X", { link: "l_2" });
  const ps = linkProjects([fyrA, fyrB, other, third, taken], [A, B]);
  assert.deepEqual(linkCandidates(ps[0], ps).map((p) => p.id), [third.id]);
  assert.deepEqual(linkCandidates(other, ps).map((p) => p.id), [third.id]);
});

test("New thread stays host first: the filter picks the host's own member, and a new host keeps the project", () => {
  const ps = linkProjects([fyrA, fyrB, other], [A, B]);
  const ctx = { projects: ps, profiles: [], hosts: [A, B] };
  const f = settleForm(initialForm(), ctx, fyrB.id!, {});
  assert.equal(f.host, "desk");
  assert.equal(f.projectId, fyrA.id);
  const moved = chooseHost(f, "mac", ctx);
  assert.equal(moved.projectId, fyrB.id);
});

test("the shared values are the newest member's; the image is the first online member's that has one", () => {
  const a = { ...fyrA, sharedAt: "2026-11-18T10:00:00Z", image: "v1" };
  const b = { ...fyrB, name: "Newer", color: "teal", sharedAt: "2026-11-18T11:00:00Z" };
  const [p] = linkProjects([a, b], [A, B]);
  assert.equal(p.id, a.id, "the first host is still the one shown");
  assert.deepEqual([p.name, p.color], ["Newer", "teal"]);
  assert.match(projectIconUrl(p, 16)!, /\/h\/A\/projects\/A%2Fp1\/icon/);
  const [q] = linkProjects([{ ...a, image: undefined }, { ...b, image: "v2" }], [A, B]);
  assert.match(projectIconUrl(q, 16)!, /\/h\/B\//);
});

test("an edit splits into what the link shares and what stays this host's", () => {
  assert.deepEqual(splitSharedPatch({ name: "x", quiet: null, profile: "p", model: "m" }), { shared: { name: "x", quiet: null }, own: { profile: "p", model: "m" } });

test("hiding: any member's id hides the linked project, and a hidden host takes only its own record", () => {
  const projects = linkProjects([fyrA, fyrB, other], [A, B]);
  const s = { ...initialStore(), hosts: [A, B], projects };
  const hidden = { hosts: [], projects: [fyrB.id!] };
  assert.ok(projectIsHidden(projects[0], hidden));
  assert.deepEqual(applyHidden(s, hidden).projects.map((p) => p.name), ["Stacks"]);
  assert.deepEqual(pushHiddenOf([A, B], projects, hidden).projects.map((p) => p.id).sort(), ["p1", "p9"]);
  assert.deepEqual(setProjectShown(hidden, projects[0], true).projects, [], "showing it clears whichever member was stored");
  const noB = applyHidden(s, { hosts: ["B"], projects: [] }).projects;
  assert.deepEqual(noB.map((p) => [p.id, !!p.members]), [[fyrA.id, false]]);
});
