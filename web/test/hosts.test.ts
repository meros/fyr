// SPEC 0.5 V0.51: Settings → Hosts lists the front's hosts; an offline host's threads say so and are read-only.
import { test } from "node:test";
import assert from "node:assert/strict";
import { decodeHosts, hostStatus, offlineLine } from "../src/hosts.ts";
import { applySnapshot, initialStore } from "../src/store.ts";
import { clockTime } from "../src/format.ts";

test("an offline thread names its host and the time; an online one says nothing", () => {
  const since = "2026-11-14T18:40:00Z";
  assert.equal(offlineLine({ host: "host-a", hostOffline: since }), `host-a offline since ${clockTime(since)} · read-only`);
  assert.equal(offlineLine({ host: "host-a" }), "");
});

test("hosts decode leniently and read as words", () => {
  const hs = decodeHosts([{ name: "a", online: true, since: "2026-11-14T18:40:00Z" }, { name: "b", online: false }, { online: true }, null]);
  assert.equal(hs.length, 2);
  assert.equal(hostStatus(hs[0], "a"), "this machine · online");
  assert.equal(hostStatus(hs[1], "a"), "offline");
  assert.deepEqual(decodeHosts(undefined), []);
});

test("the store keeps the state's hosts, and none from an older host", () => {
  const base = { now: "2026-11-14T18:40:00Z", user: "owner", threads: [], actions: [], profiles: [], projects: [] };
  const s = applySnapshot(initialStore(), { ...base, hosts: [{ name: "a", online: true }] }, Date.now());
  assert.deepEqual(s.hosts.map((h) => h.name), ["a"]);
  assert.deepEqual(applySnapshot(initialStore(), base, Date.now()).hosts, []);
});
