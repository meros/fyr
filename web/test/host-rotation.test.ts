// V0.client-refs (decision 0031 test 6): when a host's install id rotates, what the device keeps moves from the old id
// to the new one. V0.old-routes-end: the one-time move from bare ids and names is gone; a draft is its ref's only.
import { test } from "node:test";
import assert from "node:assert/strict";
import { moveHostKeys, moveRef, rotations, type KVList } from "../src/host-rotation.ts";
import { draftStore } from "../src/drafts.ts";
import { moveHostInStore, initialStore, resolveRef, emptyHost, allThreads } from "../src/store.ts";
import { hid } from "./helpers.ts";
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
const th = (host: string, rid: string) => ({ id: `${host}/${rid}`, rid });
// t_same is on both hosts; t_one only on A
const threads = [th(A, "t_same"), th(B, "t_same"), th(A, "t_one")];

test("a draft is its ref's: a bare id written by an old client is not read for either host", () => {
  const s = kv({ "fyr.draft.t_same": "which host?" });
  assert.equal(draftStore(s).get(`${A}/t_same`), "");
  draftStore(s).set(`${A}/t_same`, "on A");
  assert.equal(draftStore(s).get(`${A}/t_same`), "on A");
  assert.equal(draftStore(s).get(`${B}/t_same`), "", "B's thread with the same id has its own draft");
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
