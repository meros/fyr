// Decision 0031 slice 2b (#453): the store keeps one state per host, by install id; what the screens draw are selectors
// over the hosts. Nothing is merged: a thread's profiles and actions are its own host's, each host has its own deploy and
// stall, and the update banner is the update of the host the front runs in.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  actionsOn,
  applySnapshot,
  applyThreadPatch,
  bannerDeploy,
  hostOfThread,
  initialStore,
  profilesByHost,
  profilesOn,
  setDeploy,
  setStall,
  stateForCache,
  upsertProject,
  upsertThread,
  viewOf,
} from "../src/store.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { Profile, Project } from "../src/types.ts";
import { hid, snapshot, thread } from "./helpers.ts";

const A = hid("AAAA-1");
const B = hid("BBBB-2");
const hosts: HostInfo[] = [
  { name: "desk", id: A, online: true },
  { name: "lab", id: B, online: true },
  { name: "old-nuc", online: true },
];
const prof = (id: string, name: string): Profile => ({ id, name, color: "", models: ["m"] });
const T = (host: typeof A, rid: string, title = rid) => thread(`${host}/${rid}`, "seen", "seen", { hostId: host, rid, title });
const P = (host: typeof A, rid: string, name: string): Project => ({ id: `${host}/${rid}`, rid, name, hostId: host });

function twoHosts() {
  return applySnapshot(
    initialStore(),
    snapshot({
      hosts,
      front: { id: A, name: "desk" },
      threads: [T(A, "t_same", "on desk"), T(B, "t_same", "on lab"), T(B, "t_lab")],
      projects: [P(A, "p_1", "Fyr"), P(B, "p_2", "Lab notes")],
      per: {
        [A]: { profiles: [prof("claude", "Claude")], actions: [{ name: "thread.settle", label: "Settle" }], deploy: { phase: "build" } as never },
        [B]: { profiles: [prof("claude", "Claude (lab)"), prof("zai", "Z.ai")], actions: [], stall: { at: "2026-11-20T10:00:00Z", what: "x" } },
      },
    }),
    0,
  );
}

test("each host's records are kept in that host's own state; a host with no install id has none", () => {
  const s = twoHosts();
  assert.deepEqual(Object.keys(s.hosts), [A, B]);
  assert.deepEqual(Object.keys(s.hosts[A].threads), [`${A}/t_same`]);
  assert.deepEqual(Object.keys(s.hosts[B].threads), [`${B}/t_same`, `${B}/t_lab`]);
  assert.deepEqual(s.hosts[B].projects.map((p) => p.name), ["Lab notes"]);
  assert.deepEqual(s.hostList.map((h) => h.name), ["desk", "lab", "old-nuc"], "the front's list, in the owner's order");
});

test("the screens' lists are selectors over every host: two hosts with one thread id are two rows", () => {
  const v = viewOf(twoHosts());
  assert.deepEqual(Object.keys(v.threads).sort(), [`${A}/t_same`, `${B}/t_lab`, `${B}/t_same`]);
  assert.equal(v.threads[`${B}/t_same`].title, "on lab");
  assert.deepEqual(v.projects.map((p) => p.name), ["Fyr", "Lab notes"]);
  assert.deepEqual(v.stalls.map((x) => x.host), ["lab"], "a stall is its host's, named by it");
});

test("the view is rebuilt only when something it reads changed", () => {
  const s = twoHosts();
  const v = viewOf(s);
  assert.equal(viewOf(s), v, "the same store is the same view");
  const conn = { ...s, conn: "live" as const };
  assert.equal(viewOf(conn).threads, v.threads, "a change outside the hosts keeps the lists");
  const up = upsertThread(s, { ...T(A, "t_new") });
  assert.notEqual(viewOf(up).threads, v.threads);
  assert.ok(viewOf(up).threads[`${A}/t_new`]);
});

test("profiles and actions are each host's own: there is no merged list", () => {
  const s = twoHosts();
  assert.deepEqual(profilesOn(s, A).map((p) => p.name), ["Claude"]);
  assert.deepEqual(profilesOn(s, B).map((p) => p.name), ["Claude (lab)", "Z.ai"], "one id on two hosts is two providers");
  assert.deepEqual(profilesOn(s, undefined), [], "no host, no profiles: never the first host's");
  assert.deepEqual(profilesOn(s, hid("gone")), []);
  assert.deepEqual(profilesByHost(s), { [A]: profilesOn(s, A), [B]: profilesOn(s, B) });
  assert.equal(profilesByHost(s), profilesByHost(s), "the same hosts give the same object (a context value)");
  assert.deepEqual(actionsOn(s, A).map((a) => a.name), ["thread.settle"]);
  assert.deepEqual(actionsOn(s, B), []);
});

test("each host has its own deploy; the banner is the one of the host the front runs in", () => {
  const s = twoHosts();
  assert.equal(bannerDeploy(s)?.phase, "build");
  assert.equal(s.hosts[B].deploy, null);
  const lab = setDeploy(s, { phase: "test", host: B } as never);
  assert.equal(lab.hosts[B].deploy?.phase, "test", "another host's update goes to that host");
  assert.equal(bannerDeploy(lab)?.phase, "build", "and is not the banner's");
  assert.equal(setDeploy(s, { phase: "test" } as never), s, "an update that names no host goes nowhere");
});

test("a frame goes into the state of the host it names; one that names none is not taken for any host's", () => {
  const s = twoHosts();
  const up = upsertThread(s, { ...T(B, "t_same", "renamed on lab"), rev: 9 });
  assert.equal(up.hosts[B].threads[`${B}/t_same`].title, "renamed on lab");
  assert.equal(up.hosts[A], s.hosts[A], "the other host's state is untouched");
  const { hostId: _, ...noHost } = T(A, "t_x");
  assert.equal(upsertThread(s, noHost), s);
  assert.equal(upsertProject(s, { id: "p_x", name: "x" }), s);
  assert.equal(hostOfThread(s, `${B}/t_lab`), B);
  assert.equal(hostOfThread(s, "t_lab"), undefined, "a thread the store does not have has no host to ask");
});

test("a patch applies to the copy in the state of the host whose stream sent it", () => {
  let s = twoHosts();
  s = upsertThread(s, { ...T(A, "t_same"), rev: 5 });
  s = upsertThread(s, { ...T(B, "t_same"), rev: 5 });
  const r = applyThreadPatch(s, { id: `${B}/t_same`, host: B, rev: 6, base: 5, patch: { title: "patched" } });
  assert.equal(r.needFull, false);
  assert.equal(r.store.hosts[B].threads[`${B}/t_same`].title, "patched");
  assert.equal(r.store.hosts[A].threads[`${A}/t_same`].title, "t_same");
  assert.equal(applyThreadPatch(s, { id: `${B}/t_same`, host: A, rev: 6, base: 5, patch: {} }).needFull, true, "another host's copy is not this one");
});

test("a stall from the stream is its host's newest", () => {
  const s = setStall(twoHosts(), A, { at: "2026-11-20T11:00:00Z", what: "y" });
  assert.deepEqual(viewOf(s).stalls.map((x) => [x.host, x.what]), [["desk", "y"], ["lab", "x"]]);
});

test("the device's copy keeps one state per host and reads back the same", () => {
  const s = twoHosts();
  const stored = stateForCache(s, "2026-11-20T12:00:00Z");
  assert.deepEqual(Object.keys(stored.states), [A, B]);
  assert.deepEqual(stored.front, { id: A, name: "desk" });
  const back = applySnapshot(initialStore(), stored, 0);
  assert.deepEqual(Object.keys(viewOf(back).threads).sort(), Object.keys(viewOf(s).threads).sort());
  assert.deepEqual(profilesOn(back, B).map((p) => p.id), ["claude", "zai"]);
  assert.equal(bannerDeploy(back)?.phase, "build");
});
