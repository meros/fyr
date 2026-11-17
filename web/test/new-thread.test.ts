// #41: one New thread model for desktop and phone, with one remembered-defaults record.
import { test } from "node:test";
import assert from "node:assert/strict";
import { chooseHost, chooseProject, createArgs, initialForm, loadRemembered, NEW_THREAD_KEY, pickProfile, saveRemembered, settleForm, type FormCtx } from "../src/new-thread.ts";
import type { Profile, Project } from "../src/types.ts";

function memory(init: Record<string, string> = {}) {
  const m = new Map(Object.entries(init));
  return {
    m,
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  };
}

const profiles: Profile[] = [
  { id: "claude", name: "Claude", color: "#000", models: ["opus", "sonnet"], defaultModel: "opus" },
  { id: "work", name: "Work", color: "#111", models: ["sonnet", "haiku"] },
  { id: "off", name: "Off", color: "#222", models: ["x"], unavailable: "needs a token" },
];
const projects: Project[] = [
  { id: "p_a", name: "a", folder: "/a", lastUsed: "2026-11-01" },
  { id: "p_b", name: "b", folder: "/b", lastUsed: "2026-11-10", profile: "work", model: "haiku" },
];
const ctx: FormCtx = { projects, profiles };

test("#41: the desktop's and the phone's remembered defaults migrate into one record", () => {
  const kv = memory({ "fyr.lastProject": "p_b", "fyr.newDefaults": '{"cwd":"/a","profile":"work","model":"sonnet"}' });
  assert.deepEqual(loadRemembered(kv), { projectId: "p_b", profile: "work", model: "sonnet" });
  assert.equal(kv.m.has("fyr.lastProject"), false, "the old desktop key is gone");
  assert.equal(kv.m.has("fyr.newDefaults"), false, "the old phone key is gone");
  assert.deepEqual(JSON.parse(kv.m.get(NEW_THREAD_KEY)!), { projectId: "p_b", profile: "work", model: "sonnet" });
  assert.deepEqual(loadRemembered(kv), { projectId: "p_b", profile: "work", model: "sonnet" }, "read back from the one record");
});

test("#41: an old key written after the record wins; broken JSON and blocked storage read as nothing", () => {
  const kv = memory({ [NEW_THREAD_KEY]: '{"projectId":"p_a","profile":"claude","model":"opus"}', "fyr.lastProject": "p_b" });
  assert.deepEqual(loadRemembered(kv), { projectId: "p_b", profile: "claude", model: "opus" });
  assert.deepEqual(loadRemembered(memory({ [NEW_THREAD_KEY]: "not json", "fyr.newDefaults": "{" })), {});
  assert.deepEqual(loadRemembered(null), {});
  const out = memory();
  saveRemembered(out, { projectId: "", profile: "work", model: "sonnet" });
  assert.deepEqual(loadRemembered(out), { projectId: "", profile: "work", model: "sonnet" });
});

test("#41, V0.121: the start project is the filter's, else the remembered one, else the first in the owner's order; none while projects load", () => {
  assert.equal(settleForm(initialForm(), { projects: [], profiles }, null, {}).projectId, null, "projects still loading");
  assert.equal(settleForm(initialForm(), ctx, "p_a", { projectId: "p_b" }).projectId, "p_a");
  assert.equal(settleForm(initialForm(), ctx, null, { projectId: "p_b" }).projectId, "p_b");
  assert.equal(settleForm(initialForm(), ctx, "gone", { projectId: "gone" }).projectId, "p_a", "V0.121: the first in the owner's order");
});

test("#41: the profile is the project's default, else the remembered one, else the first that can run", () => {
  const withDefaults = settleForm(initialForm(), ctx, "p_b", { profile: "claude", model: "sonnet" });
  assert.deepEqual([withDefaults.profile, withDefaults.model], ["work", "haiku"], "V0.23: the project's default wins");
  const remembered = settleForm(initialForm(), ctx, "p_a", { profile: "work", model: "sonnet" });
  assert.deepEqual([remembered.profile, remembered.model], ["work", "sonnet"]);
  const badModel = settleForm(initialForm(), ctx, "p_a", { profile: "work", model: "gone" });
  assert.deepEqual([badModel.profile, badModel.model], ["work", "sonnet"], "a model the profile lost: its default");
  const off = settleForm(initialForm(), { projects, profiles: [profiles[2], ...profiles.slice(0, 2)] }, "p_a", { profile: "off" });
  assert.deepEqual([off.profile, off.model], ["claude", "opus"], "V0.50: a profile that cannot run is not picked");
  const none = settleForm(initialForm(), { projects: [], profiles: [] }, null, {});
  assert.equal(none.profile, "", "no profiles yet");
  assert.equal(settleForm(none, { projects: [], profiles }, null, {}).profile, "claude", "picked once they arrive");
});

test("#41: choosing a project takes its defaults; a host without the current profile picks one it has", () => {
  const hosts = [
    { name: "local", online: true, profiles },
    { name: "srv", online: true, profiles: [profiles[1]] },
  ];
  const c: FormCtx = { projects: [...projects, { id: "p_s", name: "s", host: "srv" }], profiles, hosts };
  let f = settleForm(initialForm(), c, "p_a", {});
  assert.deepEqual([f.profile, f.model], ["claude", "opus"]);
  f = chooseProject(f, c.projects[2], c);
  assert.deepEqual([f.projectId, f.profile, f.model], ["p_s", "work", "sonnet"]);
  f = chooseProject(f, null, c);
  assert.deepEqual([f.projectId, f.profile], [undefined, "work"], "No project keeps a profile the host has");
  // V0.139: No project stays on the host picked (srv); the owner picks local to get its providers back.
  f = chooseHost(f, "local", c);
  f = pickProfile(f, "claude", c);
  assert.deepEqual([f.profile, f.model], ["claude", "opus"]);
});

test("#41: the command's arguments: a picked folder wins, a project being made starts in its folder", () => {
  const f = { ...settleForm(initialForm(), ctx, "p_a", {}), text: "go", title: "  ", liveView: true };
  assert.deepEqual(createArgs(f, ctx, { text: "go" }, "p_a"), { cwd: "/a", text: "go", attachments: undefined, profile: "claude", model: "opus", effort: undefined, title: undefined, projectId: "p_a", view: true });
  assert.equal(createArgs({ ...f, picked: "/x" }, ctx, { text: "go" }, "p_a").cwd, "/x");
  const making = { ...f, creating: "new", draft: { name: "new", folder: "/n" } };
  assert.equal(createArgs(making, ctx, { text: "go" }, "p_new").cwd, "/n");
  assert.equal(createArgs({ ...making, draft: { name: "new", folder: "" } }, ctx, { text: "go" }, "p_new").cwd, undefined, "no folder: a scratch folder");
});
