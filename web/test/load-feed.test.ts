// V0.137: a reorder's receipt shows at once; a poll that started before it must not put the old order back.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createLoadFeed } from "../src/load-feed.ts";
import type { HostLoad, Slots } from "../src/host-load.ts";

const slots = (...owners: string[]): Slots => ({ held: [], waiting: owners.map((owner) => ({ owner, kind: "" })) }) as unknown as Slots;
const host = (s: Slots): HostLoad => ({ host: "h", cores: 4, load: [0, 0, 0], threads: [], slots: s }) as unknown as HostLoad;
const order = (l: HostLoad[]) => l[0].slots!.waiting.map((w) => w.owner).join(",");

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
}

test("a slow poll that resolves after a patch does not overwrite it", async () => {
  const feed = createLoadFeed((l) => l);
  await feed.poll(async () => [host(slots("a", "b"))]);
  const slow = deferred<HostLoad[]>();
  const p = feed.poll(() => slow.promise);
  feed.patchSlots("h", slots("b", "a"));
  slow.resolve([host(slots("a", "b"))]); // read before the reorder
  await p;
  assert.equal(order(feed.current()), "b,a");
});

test("a poll that starts after the patch replaces it with what the host says", async () => {
  const feed = createLoadFeed((l) => l);
  await feed.poll(async () => [host(slots("a", "b"))]);
  feed.patchSlots("h", (s) => ({ ...s, waiting: [...s.waiting].reverse() }));
  assert.equal(order(feed.current()), "b,a");
  await feed.poll(async () => [host(slots("b", "a", "c"))]);
  assert.equal(order(feed.current()), "b,a,c");
});

test("a failed poll keeps the list and tells nobody", async () => {
  const feed = createLoadFeed((l) => l);
  await feed.poll(async () => [host(slots("a"))]);
  let told = 0;
  feed.subscribe(() => told++);
  await feed.poll(() => Promise.reject(new Error("down")));
  assert.equal(order(feed.current()), "a");
  assert.equal(told, 0);
});

test("subscribers hear a patch and stop hearing after they leave", async () => {
  const feed = createLoadFeed((l) => l);
  await feed.poll(async () => [host(slots("a"))]);
  const heard: string[] = [];
  const off = feed.subscribe((l) => heard.push(order(l)));
  feed.patchSlots("h", slots("z"));
  off();
  feed.patchSlots("h", slots("y"));
  assert.deepEqual(heard, ["z"]);
  assert.equal(feed.size(), 0);
});
