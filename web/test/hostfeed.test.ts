// Decision 0031 slice 2a, 2b: the front's state and each host's own state become the snapshot the store reads, one
// state per host; a record's host is the host whose state or frame it came in.
import { test } from "node:test";
import assert from "node:assert/strict";
import { addressable, decodeFront, hostOfFrame, reachabilityChanged, snapshotOf, stampThread } from "../src/hostfeed.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { HostId } from "../src/hostid.ts";
import type { StateDTO, ThreadDTO } from "../src/types.ts";
import { hid } from "./helpers.ts";

const desk: HostInfo & { id: HostId } = { id: hid("AAAA-1"), name: "desk", online: true, since: "2026-11-17T08:00:00Z", machine: "laptop" };
const lab: HostInfo & { id: HostId } = { id: hid("BBBB-2"), name: "lab", online: false, since: "2026-11-17T09:30:00Z" };
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
  const out = snapshotOf(front(), [
    { host: desk, state: state("on desk") },
    { host: lab, state: state("on lab") },
  ]);
  const threads = Object.values(out.states).flatMap((st) => st.threads);
  const projects = Object.values(out.states).flatMap((st) => st.projects);
  assert.deepEqual(threads.map((t) => [t.title, t.host, t.hostId, t.id]), [["on desk", "desk", "AAAA-1", "AAAA-1/t_same"], ["on lab", "lab", "BBBB-2", "BBBB-2/t_same"]], "one id on two hosts is two records");
  assert.deepEqual(projects.map((p) => [p.name, p.host, p.hostId]), [["on desk", "desk", "AAAA-1"], ["on lab", "lab", "BBBB-2"]]);
});

test("an offline host's threads carry the time it went away; an online host's carry none", () => {
  const out = snapshotOf(front(), [
    { host: desk, state: state("a") },
    { host: lab, state: state("b") },
  ]);
  assert.equal(out.states["AAAA-1"].threads[0].hostOffline, undefined);
  assert.equal(out.states["BBBB-2"].threads[0].hostOffline, "2026-11-17T09:30:00Z");
  assert.equal(stampThread({ ...th("x"), hostOffline: "stale" } as ThreadDTO, desk).hostOffline, undefined, "a stamp from an earlier state does not stay");
});

test("slice 2b: nothing is merged; each host keeps its deploy, actions, profiles and stall, and the front is only named", () => {
  const out = snapshotOf(front(), [
    { host: desk, state: state("a", { deploy: { phase: "waiting" } as never, actions: [{ id: "x" }] as never, profiles: [{ id: "claude" }, { id: "own" }] as never, stall: { at: "t" } as never }) },
    { host: lab, state: state("b", { deploy: { phase: "installing" } as never, actions: [{ id: "y" }] as never, profiles: [{ id: "claude" }, { id: "zai" }] as never }) },
  ]);
  const [d, l] = [out.states["AAAA-1"], out.states["BBBB-2"]];
  assert.deepEqual([(d.deploy as { phase: string; host: string }).phase, (d.deploy as { host: string }).host], ["waiting", "AAAA-1"], "a deploy names its host by id");
  assert.equal((l.deploy as { phase: string }).phase, "installing");
  assert.deepEqual([d.actions, l.actions], [[{ id: "x" }], [{ id: "y" }]]);
  assert.deepEqual([d.profiles.map((p) => p.id), l.profiles.map((p) => p.id)], [["claude", "own"], ["claude", "zai"]]);
  assert.deepEqual(d.stall, { at: "t", host: "desk" }, "a stall names its host");
  assert.equal(l.stall, undefined);
  assert.deepEqual(out.front, { id: "AAAA-1", name: "desk" });
  assert.deepEqual(Object.keys(out.states), ["AAAA-1", "BBBB-2"], "a host with no install id has no state");
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
