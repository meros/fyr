// Decision 0031 slice 2a: the front's state and each host's own state become the state the store reads; a record's host
// is the host whose state or frame it came in.
import { test } from "node:test";
import assert from "node:assert/strict";
import { addressable, decodeFront, flatten, hostOfFrame, reachabilityChanged, stampThread } from "../src/hostfeed.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { StateDTO, ThreadDTO } from "../src/types.ts";

const desk: HostInfo = { id: "AAAA-1", name: "desk", online: true, since: "2026-11-17T08:00:00Z", machine: "laptop" };
const lab: HostInfo = { id: "BBBB-2", name: "lab", online: false, since: "2026-11-17T09:30:00Z" };
const old: HostInfo = { name: "old-nuc", online: true };
const front = () =>
  decodeFront({ now: "2026-11-17T10:00:00Z", user: "me", buildId: "b1", front: { id: "AAAA-1", name: "desk" }, hosts: [desk, lab, old], projectOrder: ["p_same"], hostMarks: { "BBBB-2": { color: "teal" } } });
const th = (title: string): ThreadDTO => ({ id: "t_same", title, rev: 5 }) as ThreadDTO;
const state = (title: string, extra: Partial<StateDTO> = {}): StateDTO =>
  ({ now: "", user: "", threads: [th(title)], projects: [{ id: "p_same", name: title, parent: "", dirs: [] }], actions: [], profiles: [], ...extra }) as StateDTO;

test("the front's state is the hosts and what is the front's own, nothing of a host's", () => {
  const f = front();
  assert.equal(f.front.id, "AAAA-1");
  assert.deepEqual(f.hosts.map((h) => h.id), ["AAAA-1", "BBBB-2", undefined]);
  assert.deepEqual(addressable(f.hosts).map((h) => h.name), ["desk", "lab"], "a host with no install id is listed and cannot be addressed");
});

test("a thread and a project are stamped with the host they came in: name to show, id to send", () => {
  const out = flatten(front(), [
    { host: desk, state: state("on desk") },
    { host: lab, state: state("on lab") },
  ]);
  assert.deepEqual(out.threads.map((t) => [t.title, t.host, t.hostId]), [["on desk", "desk", "AAAA-1"], ["on lab", "lab", "BBBB-2"]], "one id on two hosts is two records");
  assert.deepEqual(out.projects.map((p) => [p.name, p.host, p.hostId]), [["on desk", "desk", "AAAA-1"], ["on lab", "lab", "BBBB-2"]]);
});

test("an offline host's threads carry the time it went away; an online host's carry none", () => {
  const out = flatten(front(), [
    { host: desk, state: state("a") },
    { host: lab, state: state("b") },
  ]);
  assert.equal(out.threads[0].hostOffline, undefined);
  assert.equal(out.threads[1].hostOffline, "2026-11-17T09:30:00Z");
  assert.equal(stampThread({ ...th("x"), hostOffline: "stale" } as ThreadDTO, desk).hostOffline, undefined, "a stamp from an earlier state does not stay");
});

test("the rest of the state: the host the front runs in gives the deploy, actions are the first host's, profiles merge by id", () => {
  const out = flatten(front(), [
    { host: desk, state: state("a", { deploy: { phase: "waiting" } as never, actions: [{ id: "x" }] as never, profiles: [{ id: "claude" }, { id: "own" }] as never, stall: { at: "t" } as never }) },
    { host: lab, state: state("b", { deploy: { phase: "installing" } as never, actions: [{ id: "y" }] as never, profiles: [{ id: "claude" }, { id: "zai" }] as never }) },
  ]);
  assert.equal((out.deploy as { phase: string }).phase, "waiting", "the banner is the front's own host's update");
  assert.deepEqual(out.actions, [{ id: "x" }]);
  assert.deepEqual(out.profiles.map((p) => p.id), ["claude", "own", "zai"]);
  assert.deepEqual(out.stalls, [{ at: "t", host: "desk" }], "a stall names its host");
  assert.equal(out.host, "desk");
  assert.equal(out.hostId, "AAAA-1");
  assert.equal(out.machine, "laptop");
  assert.deepEqual(out.projectOrder, ["p_same"]);
  assert.deepEqual(out.hostMarks, { "BBBB-2": { color: "teal" } });
});

test("a frame names its host by install id", () => {
  const hs = front().hosts;
  assert.equal(hostOfFrame(hs, { host: "BBBB-2" })?.name, "lab");
  assert.equal(hostOfFrame(hs, { host: "ZZZZ" }), undefined);
  assert.equal(hostOfFrame(hs, {}), undefined, "a frame with no host is not a host's");
});

test("reachability: only a host coming or going asks for a new read", () => {
  const hs = front().hosts;
  assert.equal(reachabilityChanged(hs, hs.map((h) => ({ ...h, since: "later" }))), false);
  assert.equal(reachabilityChanged(hs, hs.map((h) => (h.id === "BBBB-2" ? { ...h, online: true } : h))), true);
});
