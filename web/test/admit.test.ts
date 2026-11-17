// Decision 0029 S2 (#186), V0.163: with admission by a budget, the rail shows the tighter resource's use as a percentage
// bar, the tooltip lists each job's share of CPU and memory and why the next job waits, and the first waiting thread's
// row says why.
import { test } from "node:test";
import assert from "node:assert/strict";
import { loadDetail, railBudget, railLabel, slotTip, threadJob, threadSlot, type HostLoad } from "../src/load.ts";

const host: HostLoad = {
  host: "desk",
  cores: 4,
  load: [3, 3, 3],
  cgroups: true,
  threads: [],
  provider: "cgroups",
  slowdown: 2.1,
  slots: {
    n: 2,
    why: "CPU: new jobs run 2.1× slower",
    held: [
      { owner: "t_a", kind: "test", cpuPct: 50, memPct: 10, pct: 50 },
      { owner: "t_b", kind: "e2e", cpuPct: 5, memPct: 6, pct: 6 },
    ],
    waiting: [
      { owner: "t_c", kind: "test", cpuPct: 50, memPct: 10, pct: 50 },
      { owner: "t_d", kind: "build", cpuPct: 30, memPct: 5, pct: 30 },
    ],
    cpuPct: 55,
    memPct: 16,
    cpuBudgetPct: 100,
    memBudgetPct: 100,
    cpuFreePct: 45,
    memFreePct: 84,
    usedPct: 55,
    tight: "cpu",
    binding: "cpu",
  },
};

test("the rail shows the tighter resource's use in percent, hot while anyone waits", () => {
  assert.deepEqual(railBudget(host), { text: "55 %", note: "2 wait", share: 0.55, hot: true, waiting: 2 });
  assert.deepEqual(railBudget({ ...host, slots: { ...host.slots!, usedPct: 100, waiting: [] } }), { text: "100 %", note: "full", share: 1, hot: false, waiting: 0 });
  assert.equal(railBudget({ ...host, slots: { ...host.slots!, waiting: [] } })!.note, "");
  assert.equal(railBudget({ ...host, slots: { ...host.slots!, waiting: [{ owner: "t_c", kind: "test" }] } })!.note, "1 waits");
});

test("the tooltip lists each job's CPU and memory and why the next job waits", () => {
  const d = loadDetail(host);
  assert.ok(d.includes("Running: test ≈ 50 % CPU · 10 % memory; e2e ≈ 5 % CPU · 6 % memory"), d.join("\n"));
  assert.ok(d.includes("2 waiting; the next CPU: new jobs run 2.1× slower"), d.join("\n"));
  assert.match(railLabel(host, false), /heavy jobs use 55 % of their CPU budget, 2 waiting/);
  assert.ok(slotTip(host).startsWith("Heavy jobs: CPU 55 % · memory 16 % of this host in use"));
});

test("the first waiting thread's row says why it waits; the others their place and need", () => {
  assert.equal(threadSlot([host], "t_c"), "waiting: CPU: new jobs run 2.1× slower");
  assert.equal(threadSlot([host], "t_d"), "waiting (1 ahead) · needs ≈ 30 %");
});

test("S5: a frozen job's row says so first, with or without its command", () => {
  const h: HostLoad = { ...host, threads: [{ id: "t_a", memory: 3e9, job: "make test", paused: true }, { id: "t_b", memory: 1e9, paused: true }] };
  assert.equal(threadJob([h], "t_a"), "make test · paused: memory");
  assert.equal(threadJob([h], "t_b"), "e2e job · paused: memory");
});

test("S4: the tooltip shows the adapted budget per resource and why", () => {
  const h: HostLoad = { ...host, slots: { ...host.slots!, cpuBudgetPct: 125, budgetWhy: "the host is calm and a job waits for CPU: the CPU budget grows" } };
  assert.ok(loadDetail(h).some((l) => l.includes("the budget is CPU 125 % · memory 100 % (the host is calm and a job waits for CPU: the CPU budget grows)")), loadDetail(h).join("\n"));
});
