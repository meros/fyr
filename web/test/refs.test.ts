// Decision 0031 slice 2b: a record is named by (host id, record id); the same id on two hosts is two records.
import { test } from "node:test";
import assert from "node:assert/strict";
import { bare, flatten, decodeFront, ref, stampEvent, stampLoad, unrefDeep } from "../src/hostfeed.ts";
import { applySnapshot, initialStore, resolveRef, upsertThread } from "../src/store.ts";
import { orderProjects } from "../src/projects.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { StateDTO, ThreadDTO } from "../src/types.ts";

const A: HostInfo = { id: "AAAA-1", name: "desk", online: true };
const B: HostInfo = { id: "BBBB-2", name: "lab", online: true };
const front = decodeFront({ now: "2026-11-17T10:00:00Z", user: "me", front: { id: "AAAA-1", name: "desk" }, hosts: [A, B] });
const st = (title: string): StateDTO =>
  ({ now: "", user: "", threads: [{ id: "t_same", title, rev: 5, projectId: "p_same", project: "P" }], projects: [{ id: "p_same", name: title, parent: "", dirs: [] }], actions: [], profiles: [] }) as unknown as StateDTO;

test("one thread id and one project id on two hosts stay two records in the store", () => {
  const flat = flatten(front, [{ host: A, state: st("on desk") }, { host: B, state: st("on lab") }]);
  const s = applySnapshot(initialStore(), flat, 0);
  assert.deepEqual(Object.keys(s.threads).sort(), ["AAAA-1/t_same", "BBBB-2/t_same"]);
  assert.equal(s.threads["BBBB-2/t_same"].title, "on lab");
  assert.equal(s.threads["BBBB-2/t_same"].rid, "t_same", "the host's own id is kept for its calls");
  assert.equal(s.threads["BBBB-2/t_same"].projectId, "BBBB-2/p_same", "a project id inside a record is the same host's");
  assert.deepEqual(s.projects.map((p) => p.id).sort(), ["AAAA-1/p_same", "BBBB-2/p_same"]);
  const up = upsertThread(s, { ...(s.threads["AAAA-1/t_same"] as ThreadDTO), title: "changed", rev: 9 });
  assert.equal(up.threads["AAAA-1/t_same"].title, "changed");
  assert.equal(up.threads["BBBB-2/t_same"].title, "on lab", "a frame of one host does not touch the other's");
});

test("a ref goes to its host bare; only that host's refs are taken off", () => {
  assert.equal(ref("AAAA-1", "t_1"), "AAAA-1/t_1");
  assert.equal(bare("AAAA-1", "AAAA-1/t_1"), "t_1");
  assert.equal(bare("BBBB-2", "AAAA-1/t_1"), "AAAA-1/t_1");
  assert.equal(ref("", "t_1"), "t_1", "a host with no id has no ref");
  assert.deepEqual(unrefDeep("AAAA-1", { id: "AAAA-1/p_1", text: "hello", list: ["AAAA-1/t_1", "x"] }), { id: "p_1", text: "hello", list: ["t_1", "x"] });
});

test("events, load and slots name threads by ref", () => {
  assert.equal(stampEvent({ seq: 1, at: "", subject: "t_1", type: "msg", data: {} }, "AAAA-1").subject, "AAAA-1/t_1");
  const l = stampLoad({ host: "desk", cores: 1, load: [0, 0, 0], cgroups: true, threads: [{ id: "t_1" }, { id: "self-deploy" }], slots: { held: [{ owner: "t_2", kind: "test" }], waiting: [{ owner: "self-deploy", kind: "" }], undeclared: [{ thread: "t_3" }] } } as never, "AAAA-1");
  assert.deepEqual(l.threads.map((t) => t.id), ["AAAA-1/t_1", "self-deploy"]);
  assert.equal(l.slots!.held[0].owner, "AAAA-1/t_2");
  assert.equal(l.slots!.waiting[0].owner, "self-deploy");
  assert.equal(l.slots!.undeclared![0].thread, "AAAA-1/t_3");
});

test("a link names the host's own id: it finds the one record that has it, and none when two do", () => {
  const flat = flatten(front, [{ host: A, state: st("a") }, { host: B, state: { ...st("b"), threads: [{ id: "t_only", title: "x", rev: 1 }, { id: "t_same", title: "y", rev: 1 }] as never } }]);
  assert.equal(resolveRef(flat.threads, "t_only"), "BBBB-2/t_only");
  assert.equal(resolveRef(flat.threads, "t_same"), "t_same", "two hosts have it: refused");
  assert.equal(resolveRef(flat.threads, "AAAA-1/t_same"), "AAAA-1/t_same");
});

test("a project order saved before refs still places the projects", () => {
  const flat = flatten(front, [{ host: A, state: st("a") }, { host: B, state: st("b") }]);
  assert.deepEqual(orderProjects(flat.projects, ["BBBB-2/p_same"]).map((p) => p.id), ["BBBB-2/p_same", "AAAA-1/p_same"]);
  assert.equal(orderProjects(flat.projects, ["p_same"]).length, 2);
});
