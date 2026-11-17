// V0.163: the owner sees heavy jobs as percentages of the host (CPU and memory), not slots: the rail's bar, its
// tooltip, a job's row, the queue's reason and Settings → Heavy jobs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { budgetLine, heavyDetail, jobLines, normalizeLoad, railBudget, railLabel, threadJob, threadSlot, type HostLoad } from "../src/load.ts";

const host: HostLoad = {
  host: "desk",
  cores: 6,
  load: [2, 2, 2],
  cgroups: true,
  provider: "cgroups",
  capacity: { cpu: 6, memory: 13e9 },
  threads: [{ id: "t_a", memory: 2e9, job: "make test-go" }, { id: "t_b", memory: 6e9 }],
  slots: {
    n: 2,
    held: [
      { owner: "t_a", kind: "test", cpuPct: 50, memPct: 15, pct: 50 },
      { owner: "t_b", kind: "build", cpuPct: 10, memPct: 46, pct: 46 },
    ],
    waiting: [
      { owner: "t_c", kind: "test", cpuPct: 45, memPct: 15, pct: 45 },
      { owner: "t_d", kind: "e2e", cpuPct: 20, memPct: 10, pct: 20 },
    ],
    cpuPct: 60,
    memPct: 62,
    cpuBudgetPct: 100,
    memBudgetPct: 100,
    cpuFreePct: 40,
    memFreePct: 38,
    usedPct: 62,
    tight: "memory",
    binding: "cpu",
    needPct: 45,
    why: "needs ≈ 45 % CPU, 40 % free",
  },
};

test("the rail's bar is the tighter resource's use of its budget, in percent, with no count", () => {
  assert.deepEqual(railBudget(host), { text: "62 %", note: "2 wait", share: 0.62, hot: true, waiting: 2 });
  const counted: HostLoad = { ...host, provider: undefined, slots: { n: 4, held: [{ owner: "t_a", kind: "test", pct: 25 }], waiting: [], usedPct: 25 } };
  assert.deepEqual(railBudget(counted), { text: "25 %", note: "", share: 0.25, hot: false, waiting: 0 });
  assert.match(railLabel(host, false), /heavy jobs use 62 % of their memory budget, 2 waiting/);
});

test("the tooltip names CPU and memory, each job's share, the budget and why the next waits", () => {
  const d = heavyDetail(host);
  assert.equal(d[0], "Heavy jobs: CPU 60 % · memory 62 % of this host in use; the budget is CPU 100 % · memory 100 %");
  assert.equal(d[1], "Running: test ≈ 50 % CPU · 15 % memory; build ≈ 10 % CPU · 46 % memory");
  assert.equal(d[2], "2 waiting; the next needs ≈ 45 % CPU, 40 % free");
  assert.ok(!d.join(" ").includes("slot"));
});

test("a job's row and a queued row speak percent", () => {
  assert.equal(threadJob([host], "t_a"), "make test-go ≈ 50 %");
  assert.equal(threadSlot([host], "t_b"), "build job ≈ 46 %");
  assert.equal(threadSlot([host], "t_c"), "waiting: needs ≈ 45 % CPU, 40 % free");
  assert.equal(threadSlot([host], "t_d"), "waiting (1 ahead) · needs ≈ 20 %");
});

test("Settings: the budget, and each class as CPU and memory percent of this host", () => {
  assert.equal(budgetLine(host), "CPU 100 % · memory 100 % of what this host gives its agents (6 cores, 13.0 GB)");
  const h: HostLoad = { ...host, jobs: [{ class: "fyr: make test-go", runs: 5, cores: 3, demand: 3, peak: 2e9, wall: 60, at: "x", cpuPct: 50, memPct: 15, learned: 5 }] };
  assert.equal(jobLines(h)[0].cost, "≈ 50 % CPU · 15 % memory of this host · learned from 5 runs");
});

test("an older host's `slots` and a new host's `heavy` read the same", () => {
  const { slots, ...rest } = host;
  const fromNew = normalizeLoad([{ ...rest, heavy: slots }])[0];
  assert.deepEqual(fromNew.slots, slots);
  assert.deepEqual(normalizeLoad([host])[0].slots, slots);
});
