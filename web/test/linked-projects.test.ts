import { test } from "node:test";
import assert from "node:assert/strict";
import { addHosts, sameRepo, projectIconUrl, splitSharedPatch, expandOrder, findProject, linkCandidates, linkProjects, memberOn, projectOf, projectRecords, projectSubtitle, recordOf } from "../src/projects.ts";
import { applySnapshot, initialStore, setHosts, setProjectOrder, stateForCache, upsertProject, viewOf, type View } from "../src/store.ts";
import { activeFilters, applyFilters, liveProject } from "../src/filters.ts";
import { chooseHost, initialForm, settleForm } from "../src/new-thread.ts";
import { applyHidden, projectIsHidden, pushHiddenOf, setProjectShown } from "../src/hidden.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { Project } from "../src/types.ts";
import { thread, hid, snapshot } from "./helpers.ts";

const host = (id: string, name: string, online = true) => ({ id: hid(id), name, online }) as HostInfo;
const A = host("A", "desk"), B = host("B", "mac");
const rec = (hostId: string, pid: string, name: string, extra: Partial<Project> = {}): Project => ({ id: `${hostId}/${pid}`, rid: pid, hostId: hid(hostId), host: hostId === "A" ? "desk" : "mac", name, threads: 1, ...extra });
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
  assert.deepEqual(Object.keys(applyFilters(threads, sel, ps)).sort(), ["a", "b"]);
  const chips = activeFilters({ threads, projects: ps, hosts: [A, B], sel: { hostsOff: [], project: null }, open: {}, now: 0, showUntil: 0 });
  assert.deepEqual(chips.projects.map((c) => [c.project.name, c.n]), [["Fyr", 2], ["Stacks", 1]]);
});

test("the store keeps records: a frame for a member, a new host order and the device cache", () => {
  let s = applySnapshot(initialStore(), snapshot({ hosts: [A, B], projects: [fyrA, fyrB, other] }), 0);
  assert.equal(viewOf(s).projects.length, 2);
  s = upsertProject(s, { ...fyrB, name: "Renamed", rev: 9 });
  assert.equal(recordOf(viewOf(s).projects, fyrB.id)?.name, "Renamed");
  assert.equal(viewOf(s).projects.length, 2);
  assert.equal(viewOf(setHosts(s, [B, A])).projects.find((p) => p.members)?.name, "Renamed");
  assert.equal(Object.values(stateForCache(s, "x").states).flatMap((st) => st.projects).length, 3, "each host's records, one state per host");
  s = upsertProject(s, { id: fyrB.id, hostId: fyrB.hostId, deleted: true } as Project);
  assert.ok(!viewOf(s).projects.some((p) => p.members), "one member left: an ordinary project");
});

test("reordering moves the members together", () => {
  const ps = linkProjects([fyrA, fyrB, other], [A, B]);
  assert.deepEqual(expandOrder([other.id!, fyrA.id!], ps), [other.id, fyrA.id, fyrB.id]);
  const base = applySnapshot(initialStore(), snapshot({ hosts: [A, B], projects: [fyrA, fyrB, other] }), 0);
  const s = setProjectOrder(base, expandOrder([other.id!, fyrA.id!], ps));
  assert.deepEqual(viewOf(s).projects.map((p) => p.name), ["Stacks", "Fyr"]);
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
  const ctx = { projects: ps, profiles: {}, hosts: [A, B] };
  const f = settleForm(initialForm(), ctx, fyrB.id!, {});
  assert.equal(f.host, "A");
  assert.equal(f.projectId, fyrA.id);
  const moved = chooseHost(f, hid("B"), ctx);
  assert.equal(moved.projectId, fyrB.id);
});

test("the shared values are the newest member's; the image is the first online member's that has one", () => {
  const a = { ...fyrA, sharedAt: "2026-11-18T10:00:00Z", image: "v1" };
  const b = { ...fyrB, name: "Newer", color: "teal", sharedAt: "2026-11-18T11:00:00Z" };
  const [p] = linkProjects([a, b], [A, B]);
  assert.equal(p.id, a.id, "the first host is still the one shown");
  assert.deepEqual([p.name, p.color], ["Newer", "teal"]);
  assert.match(projectIconUrl(p, 16)!, /\/h\/A\/projects\/p1\/icon/);
  const [q] = linkProjects([{ ...a, image: undefined }, { ...b, image: "v2" }], [A, B]);
  assert.match(projectIconUrl(q, 16)!, /\/h\/B\//);
  const older = { ...a, imageAt: "2026-11-18T10:00:00Z" };
  const newer = { ...b, image: "v2", imageAt: "2026-11-18T11:00:00Z" };
  assert.match(projectIconUrl(linkProjects([older, newer], [A, B])[0], 16)!, /\/h\/B\/.*v=v2/, "the newest image of the online members");
  assert.match(projectIconUrl(linkProjects([older, newer], [A, { ...B, online: false }])[0], 16)!, /\/h\/A\/.*v=v1/, "an offline host's image cannot be read");
  assert.match(projectIconUrl(linkProjects([{ ...a }, { ...b, image: "v2" }], [A, B])[0], 16)!, /\/h\/A\//, "without stamps, the first in host order");

  // V0.linked-values-sync: a removal set last wins over an older image on another host.
  const removed = { ...a, image: undefined, imageAt: "2026-11-19T10:00:00Z" };
  assert.equal(projectIconUrl(linkProjects([removed, { ...b, image: "v2", imageAt: "2026-11-18T11:00:00Z" }], [A, B])[0], 16), null, "the newest is no image");
  assert.match(projectIconUrl(linkProjects([removed, { ...b, image: "v2", imageAt: "2026-11-20T10:00:00Z" }], [A, B])[0], 16)!, /\/h\/B\//, "an image set after the removal shows");
});

test("an edit splits into what the link shares and what stays this host's", () => {
  assert.deepEqual(splitSharedPatch({ name: "x", quiet: null, profile: "p", model: "m" }), { shared: { name: "x", quiet: null }, own: { profile: "p", model: "m" } });
});

test("hiding: any member's id hides the linked project, and a hidden host takes only its own record", () => {
  const projects = linkProjects([fyrA, fyrB, other], [A, B]);
  const s: View = { ...initialStore(), hostList: [A, B], projects, threads: {}, stalls: [] };
  const hidden = { hosts: [], projects: [fyrB.id!] };
  assert.ok(projectIsHidden(projects[0], hidden));
  assert.deepEqual(applyHidden(s, hidden).projects.map((p) => p.name), ["Stacks"]);
  assert.deepEqual(pushHiddenOf([A, B], projects, hidden).projects.map((p) => p.id).sort(), ["p1", "p9"]);
  assert.deepEqual(setProjectShown(hidden, projects[0], true).projects, [], "showing it clears whichever member was stored");
  const noB = applyHidden(s, { hosts: ["B"], projects: [] }).projects;
  assert.deepEqual(noB.map((p) => [p.id, !!p.members]), [[fyrA.id, false]]);
});

test("the same repository comes first among what can join, and only a host without the project can get it", () => {
  const mine = rec("A", "p1", "Fyr", { repo: "github.com/x/fyr" });
  const twin = rec("B", "p7", "Other name", { repo: "github.com/x/fyr" });
  const ps = linkProjects([mine, other, twin], [A, B]);
  assert.deepEqual(linkCandidates(mine, ps).map((p) => p.id), [twin.id, other.id]);
  assert.ok(sameRepo(mine, twin));
  assert.ok(!sameRepo(rec("A", "x", "x"), rec("B", "y", "y")), "no repository is no match");
  assert.deepEqual(addHosts(mine, [A, B, host("C", "off", false)]).map((h) => h.id), ["B"]);
  assert.deepEqual(addHosts(linkProjects([fyrA, fyrB], [A, B])[0], [A, B]), []);
});
