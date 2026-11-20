// V0.client-refs (decision 0031 slice 3, test 6 and test 7's migration): what the device keeps moves once from the
// hosts' own ids and names to refs and install ids, and from a host's old id to its new one when the id rotates.
import { test } from "node:test";
import assert from "node:assert/strict";
import { hostsOffToIds, migrateStorage, moveHostKeys, moveRef, refFor, rotations, STORAGE_VERSION_KEY, type KVList } from "../src/storage-migrate.ts";
import { draftStore } from "../src/drafts.ts";
import { moveHostInStore, initialStore, resolveRef, emptyHost, allThreads } from "../src/store.ts";
import { hid } from "./helpers.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { Thread } from "../src/types.ts";

function kv(init: Record<string, string> = {}): KVList & { m: Map<string, string> } {
  const m = new Map(Object.entries(init));
  return {
    m,
    getItem: (k) => m.get(k) ?? null,
    setItem: (k, v) => void m.set(k, v),
    removeItem: (k) => void m.delete(k),
    get length() {
      return m.size;
    },
    key: (i) => [...m.keys()][i] ?? null,
  };
}

const A = hid("AAAA-1111-AAAA-1111");
const B = hid("BBBB-2222-BBBB-2222");
const hosts: HostInfo[] = [{ name: "desk", online: true, id: A }, { name: "lab", online: true, id: B }, { name: "old-nuc", online: true }];
const th = (host: string, rid: string) => ({ id: host ? `${host}/${rid}` : rid, rid, hostId: hid(host) });
// t_same is on both hosts; t_one only on A; p_web only on B; t_nuc on the host with no id
const threads = [th(A, "t_same"), th(B, "t_same"), th(A, "t_one"), th("", "t_nuc")];
const projects = [th(B, "p_web"), th(A, "p_same"), th(B, "p_same")];

test("refFor: the one record with the id; a ref stays; two or none is null", () => {
  assert.equal(refFor(threads, "t_one"), `${A}/t_one`);
  assert.equal(refFor(threads, `${B}/t_same`), `${B}/t_same`);
  assert.equal(refFor(threads, "t_same"), null);
  assert.equal(refFor(threads, "t_gone"), null);
  assert.equal(refFor(threads, "t_nuc"), "t_nuc");
});

test("4.2: drafts, the filters, New thread's project and the folded views move to refs and ids, once", () => {
  const s = kv({
    "fyr.draft.t_one": "half a sentence",
    "fyr.draft.t_same": "which host?",
    [`fyr.draft.${A}/t_new`]: "already a ref",
    "fyr.projectFilter": "p_web",
    "fyr.lastProject": "p_web",
    "fyr.newDefaults": JSON.stringify({ profile: "claude", model: "opus" }),
    "fyr.view.collapsed": JSON.stringify(["t_one", "t_same"]),
    "fyr.hostsOff": JSON.stringify(["lab", "old-nuc", "gone", A]),
  });
  assert.equal(migrateStorage(s, { hosts, threads, projects }), true);
  assert.equal(s.getItem(`fyr.draft.${A}/t_one`), "half a sentence");
  assert.equal(s.getItem("fyr.draft.t_one"), null, "the old key goes once the draft has its ref");
  assert.equal(s.getItem("fyr.draft.t_same"), "which host?", "two hosts have it: it stays under the old key");
  assert.equal(s.getItem(`fyr.draft.${A}/t_new`), "already a ref");
  assert.equal(s.getItem("fyr.projectFilter"), `${B}/p_web`);
  assert.deepEqual(JSON.parse(s.getItem("fyr.newThread")!), { projectId: `${B}/p_web`, profile: "claude", model: "opus" });
  assert.equal(s.getItem("fyr.lastProject"), null);
  assert.equal(s.getItem("fyr.newDefaults"), null);
  assert.deepEqual(JSON.parse(s.getItem("fyr.view.collapsed")!), [`${A}/t_one`, "t_same"]);
  assert.deepEqual(JSON.parse(s.getItem("fyr.hostsOff")!), [B, "old-nuc", A], "a name no host has is dropped; a host with no id keeps its name");
  assert.equal(s.getItem(STORAGE_VERSION_KEY), "2");

  // Once: a later draft under a bare id (an old tab) is not moved again.
  s.setItem("fyr.draft.t_one", "from an old tab");
  assert.equal(migrateStorage(s, { hosts, threads, projects }), false);
  assert.equal(s.getItem("fyr.draft.t_one"), "from an old tab");
});

test("4.2: a draft the migration could not place is read as a fallback, and goes once written under the ref", () => {
  const s = kv({ "fyr.draft.t_same": "which host?" });
  const d = draftStore(s);
  assert.equal(d.get(`${A}/t_same`), "which host?");
  d.set(`${A}/t_same`, "on A");
  assert.equal(s.getItem("fyr.draft.t_same"), null);
  assert.equal(draftStore(s).get(`${B}/t_same`), "", "B's thread with the same id has its own draft");
  assert.equal(draftStore(s).get(`${A}/t_same`), "on A");
});

test("hostsOffToIds: names to ids, ids stay, unknown names go", () => {
  assert.deepEqual(hostsOffToIds(["desk", "desk", B, "x"], hosts), [A, B]);
});

test("test 6: a rotated host's keys and refs move from the old id to the new one", () => {
  const N = hid("NNNN-3333-NNNN-3333");
  const s = kv({
    [`fyr.draft.${A}/t_one`]: "draft on A",
    [`fyr.draft.${B}/t_same`]: "draft on B",
    "fyr.hostsOff": JSON.stringify([A]),
    "fyr.projectFilter": `${A}/p_same`,
    "fyr.hidden": JSON.stringify({ hosts: [A], projects: [] }),
    "other.key": A,
  });
  assert.equal(moveHostKeys(s, A, N), 4);
  assert.equal(s.getItem(`fyr.draft.${N}/t_one`), "draft on A");
  assert.equal(s.getItem(`fyr.draft.${A}/t_one`), null);
  assert.equal(s.getItem(`fyr.draft.${B}/t_same`), "draft on B");
  assert.equal(s.getItem("fyr.hostsOff"), JSON.stringify([N]));
  assert.equal(s.getItem("fyr.projectFilter"), `${N}/p_same`);
  assert.equal(s.getItem("fyr.hidden"), JSON.stringify({ hosts: [N], projects: [] }));
  assert.equal(s.getItem("other.key"), A, "not Fyr's");
  assert.equal(moveHostKeys(s, A, N), 0, "a second pass changes nothing");

  assert.deepEqual(rotations([{ id: N, idWas: A }, { id: B }, { idWas: A }]), [{ was: A, now: N }]);
  assert.equal(moveRef(`${A}/t_one`, A, N), `${N}/t_one`);
  assert.equal(moveRef(`${B}/t_one`, A, N), `${B}/t_one`);

  const t = { id: `${A}/t_one`, rid: "t_one", hostId: A } as Thread;
  const bt = { id: `${B}/t_same`, hostId: B } as Thread;
  const st = { ...initialStore(), hosts: { [A]: { ...emptyHost(), threads: { [t.id]: t } }, [B]: { ...emptyHost(), threads: { [bt.id]: bt } } }, events: { [t.id]: [{ seq: 1, subject: t.id } as never] }, resumeFrom: { [t.id]: 1 } };
  const moved = moveHostInStore(st, A, N);
  assert.deepEqual(Object.keys(moved.hosts).sort(), [B, N].sort(), "slice 2b: the host's state moves to the new id");
  assert.deepEqual(Object.keys(allThreads(moved)).sort(), [`${B}/t_same`, `${N}/t_one`]);
  assert.equal(moved.hosts[N].threads[`${N}/t_one`].hostId, N);
  assert.equal((moved.events[`${N}/t_one`][0] as { subject: string }).subject, `${N}/t_one`);
  assert.deepEqual(moved.resumeFrom, { [`${N}/t_one`]: 1 });
  assert.equal(moveHostInStore(moved, A, N), moved, "nothing left to move");
});

test("C12: a push tap with the host opens that host's thread; an old tap with the id alone only when one host has it", () => {
  const recs = threads.map((t) => ({ id: t.id, rid: t.rid }));
  assert.equal(resolveRef(recs, `${B}/t_same`), `${B}/t_same`);
  assert.equal(resolveRef(recs, "t_one"), `${A}/t_one`);
  assert.equal(resolveRef(recs, "t_same"), "t_same", "two hosts have it: not resolved, so nothing opens");
});
