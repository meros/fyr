// SPEC V0.project-archive: an archived project leaves the lists; its shelved threads leave the inbox until new work
// brings one back.
import { test } from "node:test";
import assert from "node:assert/strict";
import { applyArchived, archiveBlock, archivedProjects, archiveTargets, inboxThreads, isArchived, isShelved, projectThreads } from "../src/archived.ts";
import { linkProjects } from "../src/projects.ts";
import type { HostInfo } from "../src/hosts.ts";
import { initialStore, type View } from "../src/store.ts";
import type { Project, Thread } from "../src/types.ts";
import { hid, thread } from "./helpers.ts";

const AT = "2026-11-20T10:00:00Z";
const P = (id: string, name: string, extra: Partial<Project> = {}): Project => ({ id, rid: id.split("/")[1], name, host: "desk", hostId: hid(id.split("/")[0]), threads: 1, lastUsed: "", ...extra }) as Project;
const T = (id: string, projectId: string, extra: Partial<Thread> = {}): Thread => thread(id, "done", "seen", { projectId, title: id, ...extra });

const done = P("D/p1", "old work", { archivedAt: AT });
const live = P("D/p2", "fyr");
const projects = [done, live];
const threads = Object.fromEntries(
  [T("D/a", "D/p1", { shelvedAt: AT }), T("D/b", "D/p1" /* back: new work cleared its mark */), T("D/c", "D/p2"), T("D/d", "D/p2", { shelvedAt: AT } /* a stale mark: its project is not archived */)].map((t) => [t.id, t]),
);

test("an archived project leaves the store's projects, so the filter and every picker lose it; the threads stay", () => {
  const s: View = { ...initialStore(), projects, threads, stalls: [] };
  const out = applyArchived(s);
  assert.deepEqual(out.projects.map((p) => p.name), ["fyr"]);
  assert.equal(out.threads, threads);
  assert.equal(applyArchived({ ...s, projects: [live] }).projects.length, 1);
  const none = { ...s, projects: [live] };
  assert.equal(applyArchived(none), none, "nothing archived gives the same store back");
});

test("a shelved thread of an archived project leaves the inbox; one new work brought back, and any other, stays", () => {
  assert.equal(isShelved(threads["D/a"], projects), true);
  assert.equal(isShelved(threads["D/b"], projects), false);
  assert.equal(isShelved(threads["D/d"], projects), false, "a mark whose project is active hides nothing");
  assert.deepEqual(Object.keys(inboxThreads(threads, projects)), ["D/b", "D/c", "D/d"]);
  const active = { "D/c": threads["D/c"] };
  assert.equal(inboxThreads(active, projects), active);
});

test("the Archived projects list and the project's own threads", () => {
  assert.deepEqual(archivedProjects(projects).map((p) => p.name), ["old work"]);
  assert.deepEqual(projectThreads(done, threads).map((t) => t.id).sort(), ["D/a", "D/b"]);
});

test("archiving is off while a thread works or asks, with the reason the host gives", () => {
  assert.equal(archiveBlock(live, threads), null);
  const busy = { ...threads, "D/e": T("D/e", "D/p2", { title: "Deploy", running: true }) };
  assert.equal(archiveBlock(live, busy), "“Deploy” is working: stop its run or wait for it first");
  const queued = { ...threads, "D/e": T("D/e", "D/p2", { title: "Deploy", outbox: [{ id: "m1", state: "queued" }] }) };
  assert.match(archiveBlock(live, queued) ?? "", /is working/);
  const asking = { ...threads, "D/e": T("D/e", "D/p2", { title: "Deploy", decisions: [{ id: "d1" } as Thread["decisions"][number]] }) };
  assert.equal(archiveBlock(live, asking), "“Deploy” has an open question: answer it first");
  const archivedOne = { ...threads, "D/e": T("D/e", "D/p2", { running: true, archivedAt: AT }) };
  assert.equal(archiveBlock(live, archivedOne), null, "an archived thread has no say");
});

test("decision 0034: a linked project is archived when every member is, and Archive goes to the members not yet archived", () => {
  const hosts: HostInfo[] = [{ name: "desk", online: true, id: hid("D") }, { name: "lab", online: true, id: hid("L") }];
  const m1 = P("D/p9", "shared", { link: "l_0123456789abcdef", archivedAt: AT });
  const m2 = { ...P("L/p9", "shared", { link: "l_0123456789abcdef" }), host: "lab", hostId: hid("L") };
  const [half] = linkProjects([m1, m2], hosts);
  assert.equal(isArchived(half), false);
  assert.deepEqual(archiveTargets(half, true).map((m) => m.id), ["L/p9"]);
  assert.deepEqual(archiveTargets(half, false).map((m) => m.id), ["D/p9"]);
  const [all] = linkProjects([m1, { ...m2, archivedAt: AT }], hosts);
  assert.equal(isArchived(all), true);
  assert.deepEqual(applyArchived({ ...initialStore(), projects: [all, live], threads: {}, stalls: [] }).projects.map((p) => p.name), ["fyr"]);
  // A thread on the archived member is put away; one on a member still active is not.
  const ts = [T("D/x", "D/p9", { shelvedAt: AT }), T("L/y", "L/p9", { shelvedAt: AT })];
  assert.deepEqual(ts.map((t) => isShelved(t, [half])), [true, false]);
});
