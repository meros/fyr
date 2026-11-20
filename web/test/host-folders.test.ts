// SPEC V0.132: New project, a project's Change folder and New thread browse the folders of the chosen host, on the
// desktop and the phone. A thread follows its project's host; with no project, or a project being made, the owner
// picks the host. An offline host says so and nothing falls back to another.
import { test } from "node:test";
import assert from "node:assert/strict";
import { changeOptions, chooseHost, chooseProject, createArgs, formHost, formProfiles, initialForm, makingProject, settleForm, type FormCtx } from "../src/new-thread.ts";
import { hostChoices, hostOfflineNote } from "../src/hosts.ts";
import { dayTime } from "../src/format.ts";
import type { Profile, Project } from "../src/types.ts";
import { hid } from "./helpers.ts";

const now = Date.parse("2026-11-17T09:00:00Z");
const iso = (minAgo: number) => new Date(now - minAgo * 60_000).toISOString();
const claude: Profile = { id: "claude", name: "Claude", color: "#000", models: ["opus", "sonnet"], defaultModel: "opus" };
const zai: Profile = { id: "zai", name: "Z.ai", color: "#111", models: ["glm-5"] };
// Slice 2b: a host is its install id (here its name, for short), with its own profiles.
const hosts = [
  { name: "desk", id: hid("desk"), online: true, since: iso(180) },
  { name: "lab", id: hid("lab"), online: true, since: iso(40) },
  { name: "host-b", id: hid("host-b"), online: false, since: iso(25) },
];
const projects: Project[] = [
  { id: "p_d", name: "Desk project", folder: "/home/o/d", host: "desk", hostId: hid("desk") },
  { id: "p_l", name: "Lab notes", folder: "/home/o/lab-notes", host: "lab", hostId: hid("lab") },
];
const ctx: FormCtx = { projects, profiles: { desk: [claude], lab: [zai], "host-b": [claude] }, hosts };

test("V0.132: the hosts to pick from, in the owner's order, offline ones named; none with one host; decision 0031 2.6: no \"(this install)\"", () => {
  assert.deepEqual(hostChoices(hosts), [
    { id: "desk", name: "desk", label: "desk", online: true },
    { id: "lab", name: "lab", label: "lab", online: true },
    { id: "host-b", name: "host-b", label: "host-b (offline)", online: false },
  ]);
  assert.deepEqual(hostChoices([{ name: "desk", online: true }]), []);
  assert.deepEqual(hostChoices(undefined), []);
  // Slice 2b: a host with no install id cannot be named, so it is not offered.
  assert.deepEqual(hostChoices([...hosts.slice(0, 1), { name: "old-nuc", online: true }]).map((c) => c.name), ["desk"]);
});

test("V0.132: an offline host says so plainly; an online one says nothing; slice 2b: none, or one not listed, asks", () => {
  assert.equal(hostOfflineNote(hid("host-b"), hosts, now), `host-b is offline since ${dayTime(iso(25), now)}. Its folders cannot be read and nothing can be made on it until it is back; pick another host or wait.`);
  assert.equal(hostOfflineNote(hid("lab"), hosts, now), "");
  assert.equal(hostOfflineNote(undefined, hosts, now), "Pick the host it starts on.");
  assert.equal(hostOfflineNote(hid("gone"), hosts, now), "That host is no longer one of this Fyr's hosts; pick another.");
});

test("V0.132, V0.139: the host decides the project: picking a project of another host moves the host to it", () => {
  let f = settleForm(initialForm(), ctx, "p_l", {});
  assert.equal(formHost(f, ctx), "lab");
  assert.deepEqual(formProfiles(f, ctx).map((p) => p.id), ["zai"]);
  assert.equal(createArgs(f, ctx, { text: "go" }, "p_l").host, "lab");
  f = chooseProject(f, projects[0], ctx);
  assert.equal(formHost(f, ctx), "desk", "a desk project puts the thread on desk");
});

test("V0.132, V0.139: picking a host drops a folder of the old one; its providers follow", () => {
  let f = chooseProject(settleForm(initialForm(), ctx, null, {}), null, ctx);
  assert.equal(formHost(f, ctx), "desk", "the first host in the owner's order");
  f = { ...f, picked: "/home/o/on-desk" };
  f = chooseHost(f, hid("lab"), ctx);
  assert.equal(formHost(f, ctx), "lab");
  assert.equal(f.picked, "", "a folder of another host does not come along");
  assert.deepEqual([f.profile, f.model], ["zai", "glm-5"]);
  const a = createArgs({ ...f, picked: "/home/o/x" }, ctx, { text: "go" }, f.projectId ?? undefined);
  assert.equal(a.host, "lab");
  assert.equal(a.cwd, "/home/o/x");
  // An offline host can be picked; the form says why it cannot start (hostOfflineNote).
  assert.equal(formHost(chooseHost(f, hid("host-b"), ctx), ctx), "host-b");
});

test("V0.132: a project made in the New thread form is made on the form's host", () => {
  let f = chooseHost(chooseProject(settleForm(initialForm(), ctx, null, {}), null, ctx), hid("lab"), ctx);
  f = { ...f, creating: "New", draft: { name: "New", folder: "/home/o/new" } };
  assert.equal(formHost(f, ctx), "lab");
  assert.deepEqual(makingProject(f), { name: "New", folder: "/home/o/new" });
  assert.equal(createArgs(f, ctx, { text: "go" }, "p_new").host, "lab");
});

test("V0.132: Change folder browses the thread's host", () => {
  assert.equal(changeOptions(projects[1], "/home/o/lab-notes", hid("lab")).host, "lab");
  assert.equal(changeOptions(undefined, "", undefined).host, undefined);
});

test("V0.132: one host: slice 2b: the args name it too (no call has a default host)", () => {
  const one: FormCtx = { projects: [{ id: "p_a", name: "a", folder: "/a", hostId: hid("desk") }], profiles: { desk: [claude] }, hosts: [hosts[0]] };
  const f = settleForm(initialForm(), one, "p_a", {});
  assert.equal(createArgs(f, one, { text: "go" }, "p_a").host, "desk");
  // No project, one host: the form starts on it.
  const g = settleForm(initialForm(), { ...one, projects: [] }, null, {});
  assert.equal(createArgs(g, one, { text: "go" }, undefined).host, "desk");
  assert.deepEqual([g.profile, g.model], ["claude", "opus"]);
  // No host the front can name: nothing to start on, and the form says so instead of guessing.
  const none: FormCtx = { projects: [], profiles: {}, hosts: [{ name: "old-nuc", online: true }] };
  const h = settleForm(initialForm(), none, null, {});
  assert.equal(formHost(h, none), undefined);
  assert.equal(hostOfflineNote(formHost(h, none), none.hosts, now), "Pick the host it starts on.");
});
