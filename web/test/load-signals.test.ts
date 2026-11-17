// Decision 0029 S1 (#185): the ring's colour follows the slowdown and the memory stall level on any host with a
// provider; the tooltip names them; a slot holder's row names its job; Settings lists the measured job classes.
import { test } from "node:test";
import assert from "node:assert/strict";
import { jobLines, loadDetail, railMeter, threadJob, threadSlot, type HostLoad } from "../src/load.ts";

const host: HostLoad = {
  host: "desk",
  cores: 4,
  load: [2, 2, 2],
  cpuPressure: 30,
  cgroups: true,
  agents: { cpu: 1, cpuMax: 4, memory: 4e9, memoryMax: 15e9 },
  threads: [{ id: "t_a", cpu: 2.1, memory: 3.2e9, job: "make test" }, { id: "t_b", cpu: 0.9, memory: 1e9 }],
  slots: { n: 2, held: [{ owner: "t_a", kind: "test" }, { owner: "t_b", kind: "e2e" }], waiting: [] },
  provider: "cgroups",
  capacity: { cpu: 4, memory: 13 * 2 ** 30 },
  slowdown: 1.2,
  headroom: 0.46,
  memoryPressure: 0.4,
  memoryStall: "ok",
  agentsCpuPressure: 35,
};

test("the ring is calm under a 1.5× slowdown, warm from 1.5×, hot from 3× (the load figure stays)", () => {
  assert.equal(railMeter(host).level, "calm");
  assert.equal(railMeter({ ...host, slowdown: 1.5 }).level, "warm");
  assert.equal(railMeter({ ...host, slowdown: 3 }).level, "hot");
  // A high load average alone no longer colours the ring when the host measures its slowdown.
  assert.equal(railMeter({ ...host, load: [9, 9, 9] }).level, "calm");
  assert.equal(railMeter({ ...host, load: [9, 9, 9] }).text, `${Math.round(900 / host.cores)}%`);
});

test("the memory stall level colours the ring too", () => {
  assert.equal(railMeter({ ...host, memoryStall: "elevated" }).level, "warm");
  assert.equal(railMeter({ ...host, memoryStall: "critical" }).level, "hot");
});

test("without a provider (macOS for now) the ring follows the load as before", () => {
  const plain: HostLoad = { host: "mac", cores: 8, load: [9, 1, 1], cgroups: false, threads: [] };
  assert.equal(railMeter(plain).level, "hot");
  assert.equal(railMeter({ ...plain, load: [1, 1, 1] }).level, "calm");
});

test("the tooltip names the slowdown, the memory and the provider", () => {
  const d = loadDetail(host);
  assert.ok(d.includes("Slowdown 1.2× (a new job runs 1.2× slower than on an idle host)"), d.join("\n"));
  assert.ok(d.includes("Memory 46 % free of 14.0 GB for agents · stall ok (0.4 %)"), d.join("\n"));
  assert.ok(d.includes("Agents' CPU pressure 35 % (10 s)"), d.join("\n"));
  assert.ok(d.includes("Signals: cgroups"), d.join("\n"));
  assert.ok(!loadDetail({ ...host, slowdown: undefined }).some((l) => l.startsWith("Slowdown")));
});

test("a slot holder's row names its job and its memory; without a matched command its kind", () => {
  assert.equal(threadSlot([host], "t_a"), "make test · 3.2 GB");
  assert.equal(threadSlot([host], "t_b"), "e2e slot");
  assert.equal(threadJob([host], "t_a"), "make test · 3.2 GB");
  assert.equal(threadJob([host], "t_b"), null);
  assert.equal(threadJob([{ ...host, slots: { n: 2, held: [], waiting: [] } }], "t_a"), null);
});

test("Settings lists each measured job class in words", () => {
  const h: HostLoad = {
    ...host,
    jobs: [
      { class: "fyr: make test", runs: 12, cores: 2.34, demand: 3.1, peak: 2.1e9, wall: 95, at: "2026-11-17T09:00:00Z" },
      { class: "Acme: yarn screenshot:diff", runs: 1, cores: 1.0, demand: 1.0, peak: 6.5e9, wall: 437, at: "2026-11-17T08:00:00Z" },
    ],
  };
  assert.deepEqual(jobLines(h), [
    { class: "fyr: make test", text: "12 runs · 2.3 cores, would use 3.1 · peak 2.1 GB · 1 min 35 s" },
    { class: "Acme: yarn screenshot:diff", text: "1 run · 1.0 core · peak 6.5 GB · 7 min 17 s" },
  ]);
  assert.deepEqual(jobLines(host), []);
});
