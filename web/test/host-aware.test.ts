// SPEC V0.139: every screen that acts on a host says which, lets the owner choose it where it is a choice, takes its
// lists (projects, folders, providers, models) from that host, and is honest when that host is offline. New thread is
// host first: the host in the owner's order (V0.132), then that host's projects.
import { test } from "node:test";
import assert from "node:assert/strict";
import { chooseHost, chooseProject, formHost, formProjects, initialForm, settleForm, type FormCtx } from "../src/new-thread.ts";
import { hostChip, profilesOf } from "../src/hosts.ts";
import { dayTime } from "../src/format.ts";
import type { Profile, Project } from "../src/types.ts";
import { hid } from "./helpers.ts";

const now = Date.parse("2026-11-17T09:00:00Z");
const iso = (minAgo: number) => new Date(now - minAgo * 60_000).toISOString();
const claude: Profile = { id: "claude", name: "Claude", color: "#000", models: ["opus", "sonnet"], defaultModel: "opus" };
const labClaude: Profile = { id: "claude", name: "Claude", color: "#000", models: ["sonnet"], defaultModel: "sonnet" };
const zai: Profile = { id: "zai", name: "Z.ai", color: "#111", models: ["glm-5"] };
// The owner's order puts lab first, though this install is desk (V0.132). Slice 2b: a host is its install id (here its
// name, for short), and each host has its own profiles.
const hosts = [
  { name: "lab", id: hid("lab"), online: true, since: iso(40) },
  { name: "desk", id: hid("desk"), online: true, since: iso(180) },
  { name: "host-b", id: hid("host-b"), online: false, since: iso(25) },
];
const projects: Project[] = [
  { id: "p_d", name: "Desk project", folder: "/home/o/d", host: "desk", hostId: hid("desk") },
  { id: "p_l", name: "Lab notes", folder: "/home/o/lab-notes", host: "lab", hostId: hid("lab") },
  { id: "p_l2", name: "Robot arm", folder: "/home/o/arm", host: "lab", hostId: hid("lab") },
];
const byHost = { lab: [labClaude, zai], desk: [claude], "host-b": [claude] };
const ctx: FormCtx = { projects, profiles: byHost, hosts };

test("V0.139: New thread starts on the first host in the owner's order, not the connected one, with that host's projects", () => {
  const f = settleForm(initialForm(), ctx, null, { projectId: "p_d" });
  assert.equal(formHost(f, ctx), "lab", "a remembered project on another host does not move the start");
  assert.equal(f.projectId, "p_l");
  assert.deepEqual(formProjects(f, ctx).map((p) => p.id), ["p_l", "p_l2"]);
  assert.deepEqual([f.profile, f.model], ["claude", "sonnet"], "lab's Claude, with lab's models");
});

test("V0.139: a project filter starts on that project's host", () => {
  const f = settleForm(initialForm(), ctx, "p_d", {});
  assert.equal(formHost(f, ctx), "desk");
  assert.equal(f.projectId, "p_d");
  assert.deepEqual(formProjects(f, ctx).map((p) => p.id), ["p_d"]);
});

test("V0.139: picking a host lists its projects and starts on its remembered or first one", () => {
  let f = settleForm(initialForm(), ctx, null, {});
  f = chooseHost(f, hid("desk"), ctx, { projectId: "p_d" });
  assert.equal(f.projectId, "p_d");
  f = chooseHost(f, hid("lab"), ctx, { projectId: "p_l2" });
  assert.equal(f.projectId, "p_l2");
  f = chooseHost(f, hid("host-b"), ctx);
  assert.equal(f.projectId, undefined, "a host with no projects: No project");
  assert.deepEqual(formProjects(f, ctx), []);
  // No project stays No project on a host change.
  f = chooseHost(chooseProject(chooseHost(f, hid("lab"), ctx), null, ctx), hid("desk"), ctx);
  assert.equal(f.projectId, undefined);
});

test("V0.139: one host: every project, as before", () => {
  const one: FormCtx = { projects: projects.map(({ host: _, ...p }) => ({ ...p, hostId: hid("desk") })), profiles: { desk: [claude] }, hosts: [hosts[1]] };
  const f = settleForm(initialForm(), one, null, {});
  assert.equal(formProjects(f, one).length, 3);
});

test("V0.139: the read-only host chip of a project or thread: its name, and offline said with the time", () => {
  assert.deepEqual(hostChip(hid("lab"), hosts, now), { name: "lab", line: "", offline: false });
  assert.deepEqual(hostChip(hid("host-b"), hosts, now), { name: "host-b", line: `offline since ${dayTime(iso(25), now)} · read-only`, offline: true });
  assert.equal(hostChip(hid("desk"), [hosts[1]], now), null, "one host: no chip");
  assert.deepEqual(hostChip(undefined, hosts, now), { name: "Unknown host", line: "not one of this Fyr's hosts · read-only", offline: true }, "slice 2b: a record with no host is not the first host's");
});

test("V0.139: a project's or thread's providers are its host's (an id on two hosts is two providers)", () => {
  assert.deepEqual(profilesOf({ hostId: hid("lab") }, byHost).map((p) => p.models.join()), ["sonnet", "glm-5"]);
  assert.deepEqual(profilesOf({ hostId: hid("desk") }, byHost).map((p) => p.id), ["claude"]);
  assert.deepEqual(profilesOf({}, byHost), [], "slice 2b: a record with no host has no providers, not a merged list");
});
