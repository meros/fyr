// SPEC 0.5 V0.80: the machine load in the rail and each working thread's CPU, as words.
import { test } from "node:test";
import assert from "node:assert/strict";
import { cpuText, loadDetail, railLoad, threadCPU, type HostLoad } from "../src/load.ts";

const busy: HostLoad = {
  host: "laptop",
  cores: 8,
  load: [10.5, 6, 4.25],
  cpuPressure: 42.5,
  cgroups: true,
  agents: { cpu: 3.94, cpuMax: 4, memory: 9.1e9, memoryMax: 14 * 2 ** 30 },
  threads: [
    { id: "t_busy", cpu: 2.46, memory: 3e9 },
    { id: "t_calm", cpu: 0.04, memory: 2e8 },
    { id: "t_new", memory: 1e8 },
  ],
};

test("the rail figure is the 1-minute load; hot past the cores or at 40 % CPU pressure", () => {
  assert.deepEqual(railLoad(busy), { text: "11", hot: true });
  assert.deepEqual(railLoad({ ...busy, load: [6.04, 1, 1], cpuPressure: 12 }), { text: "6.0", hot: false });
  assert.deepEqual(railLoad({ ...busy, load: [6, 1, 1], cpuPressure: 40 }), { text: "6.0", hot: true });
  assert.deepEqual(railLoad({ ...busy, load: [8, 1, 1], cpuPressure: undefined }), { text: "8.0", hot: false });
  assert.deepEqual(railLoad({ ...busy, load: [8.1, 1, 1], cpuPressure: undefined }), { text: "8.1", hot: true });
});

test("the detail names the averages, the pressure and the agents' budget", () => {
  assert.deepEqual(loadDetail(busy), [
    "laptop: load 10.5 · 6.0 · 4.3 (1, 5, 15 min) on 8 cores",
    "CPU pressure 43 % (10 s)",
    "Agents 3.9 of 4 cores · 9.1 of 15.0 GB",
  ]);
  const plain: HostLoad = { host: "dev", cores: 4, load: [1, 2, 3], cgroups: false, threads: [] };
  assert.deepEqual(loadDetail(plain), ["dev: load 1.0 · 2.0 · 3.0 (1, 5, 15 min) on 4 cores", "Agents share the daemon's cgroup (no per-thread figures)"]);
});

test("a thread's CPU is in cores, hot from 2, and absent without a second read", () => {
  assert.deepEqual(threadCPU([busy], "t_busy"), { text: "2.5 cores", hot: true });
  assert.deepEqual(threadCPU([busy], "t_calm"), { text: "0.0 cores", hot: false });
  assert.equal(threadCPU([busy], "t_new"), null);
  assert.equal(threadCPU([busy], "t_none"), null);
  assert.equal(threadCPU([], "t_busy"), null);
  assert.equal(cpuText(1), "1.0 core");
  assert.equal(cpuText(12.34), "12 cores");
});
