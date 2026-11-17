// SPEC V0.132: New project, a project's Change folder and New thread browse the folders of the chosen host, on the
// desktop and the phone. A thread follows its project's host; with no project, or a project being made, the owner
// picks the host. An offline host says so and nothing falls back to another.
import { test } from "node:test";
import assert from "node:assert/strict";
import { changeOptions, chooseHost, chooseProject, createArgs, formHost, formProfiles, hostLock, initialForm, makingProject, settleForm, type FormCtx } from "../src/new-thread.ts";
import { hostChoices, hostOfflineNote } from "../src/hosts.ts";
import { fsQuery } from "../src/folders.ts";
import { dayTime } from "../src/format.ts";
import type { Profile, Project } from "../src/types.ts";

const now = Date.parse("2026-11-17T09:00:00Z");
const iso = (minAgo: number) => new Date(now - minAgo * 60_000).toISOString();
const claude: Profile = { id: "claude", name: "Claude", color: "#000", models: ["opus", "sonnet"], defaultModel: "opus" };
const zai: Profile = { id: "zai", name: "Z.ai", color: "#111", models: ["glm-5"] };
const hosts = [
  { name: "desk", online: true, since: iso(180), profiles: [claude] },
  { name: "lab", online: true, since: iso(40), profiles: [zai] },
  { name: "meros-server", online: false, since: iso(25), profiles: [claude] },
];
const projects: Project[] = [
  { id: "p_d", name: "Desk project", folder: "/home/o/d", host: "desk" },
  { id: "p_l", name: "Lab notes", folder: "/home/o/lab-notes", host: "lab" },
];
const ctx: FormCtx = { projects, profiles: [claude, zai], hosts };

test("V0.132: the hosts to pick from, in the owner's order, this install and offline ones named; none with one host", () => {
  assert.deepEqual(hostChoices(hosts, "lab"), [
    { name: "desk", label: "desk", online: true },
    { name: "lab", label: "lab (this install)", online: true },
    { name: "meros-server", label: "meros-server (offline)", online: false },
  ]);
  assert.deepEqual(hostChoices([{ name: "desk", online: true }], "desk"), []);
  assert.deepEqual(hostChoices(undefined, "desk"), []);
});

test("V0.132: an offline host says so plainly; an online or unknown one says nothing", () => {
  assert.equal(hostOfflineNote("meros-server", hosts, now), `meros-server is offline since ${dayTime(iso(25), now)}. Its folders cannot be read and nothing can be made on it until it is back; pick another host or wait.`);
  assert.equal(hostOfflineNote("lab", hosts, now), "");
  assert.equal(hostOfflineNote("", hosts, now), "");
});

test("V0.132: the picker's reads name the host", () => {
  assert.equal(fsQuery("/api/fs/list?path=%2Fhome", "lab"), "/api/fs/list?path=%2Fhome&host=lab");
  assert.equal(fsQuery("/api/fs/recent", "meros server"), "/api/fs/recent?host=meros%20server");
  assert.equal(fsQuery("/api/fs/recent", undefined), "/api/fs/recent");
  assert.equal(fsQuery("/api/fs/recent", ""), "/api/fs/recent");
});

test("V0.132: a thread follows its project's host, and the host is locked while a project is chosen", () => {
  let f = chooseProject(settleForm(initialForm(), ctx, "p_d", {}), projects[1], ctx);
  assert.equal(formHost(f, ctx), "lab");
  assert.deepEqual(formProfiles(f, ctx).map((p) => p.id), ["zai"]);
  assert.equal(hostLock(f, ctx), "Lab notes");
  assert.equal(createArgs(f, ctx, { text: "go" }, "p_l").host, "lab");
  // Picking a host does nothing while the project holds it.
  assert.equal(formHost(chooseHost(f, "desk", ctx), ctx), "lab");
});

test("V0.132: with no project the owner picks the host; its providers follow and the picked folder is dropped", () => {
  let f = chooseProject(settleForm(initialForm(), ctx, "p_d", {}), null, ctx);
  assert.equal(hostLock(f, ctx), "");
  assert.equal(formHost(f, ctx), "desk", "the front's first host until one is picked");
  f = { ...f, picked: "/home/o/on-desk" };
  f = chooseHost(f, "lab", ctx);
  assert.equal(formHost(f, ctx), "lab");
  assert.equal(f.picked, "", "a folder of another host does not come along");
  assert.deepEqual([f.profile, f.model], ["zai", "glm-5"]);
  const a = createArgs({ ...f, picked: "/home/o/x" }, ctx, { text: "go" }, undefined);
  assert.equal(a.host, "lab");
  assert.equal(a.cwd, "/home/o/x");
  // An offline host can be shown but is the owner's pick; the form says why it cannot start (hostOfflineNote).
  assert.equal(formHost(chooseHost(f, "meros-server", ctx), ctx), "meros-server");
});

test("V0.132: a project made in the New thread form is made on the form's host", () => {
  let f = chooseProject(settleForm(initialForm(), ctx, "p_d", {}), null, ctx);
  f = chooseHost(f, "lab", ctx);
  f = { ...f, creating: "New", draft: { name: "New", folder: "/home/o/new" } };
  assert.equal(hostLock(f, ctx), "", "the host stays the owner's pick while the project is made");
  assert.equal(formHost(f, ctx), "lab");
  assert.deepEqual(makingProject(f), { name: "New", folder: "/home/o/new" });
  assert.equal(createArgs(f, ctx, { text: "go" }, "p_new").host, "lab");
});

test("V0.132: Change folder browses the thread's host", () => {
  assert.equal(changeOptions(projects[1], "/home/o/lab-notes", "lab").host, "lab");
  assert.equal(changeOptions(undefined, "", undefined).host, undefined);
});

test("V0.132: one host: no host in the args, as before", () => {
  const one: FormCtx = { projects: [{ id: "p_a", name: "a", folder: "/a" }], profiles: [claude] };
  const f = settleForm(initialForm(), one, "p_a", {});
  assert.equal("host" in createArgs(f, one, { text: "go" }, "p_a"), false);
});
