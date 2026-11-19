// SPEC 0.5 V0.80: the machine load in the rail and each working thread's CPU, as words.
import { test } from "node:test";
import assert from "node:assert/strict";
import { cardLines, cpuText, loadCard, railBudget, railLoad, railMeter, shortHostNames, stripHosts, threadCPU, threadService, threadSlot, type HostLoad } from "../src/load.ts";

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

test("the card names the load, how often the CPU makes work wait, and the agents' use", () => {
  assert.deepEqual(cardLines(loadCard(busy)), [
    "Overloaded: The load is 131 % of the cores.",
    "Load: 131 % of 8 cores · 11 now · 6.0 over 5 min · 4.3 over 15 min",
    "CPU waits: 43 % of the time",
    "Agents use: 3.9 of 4 cores · 9.1 of 15.0 GB",
  ]);
  const plain: HostLoad = { host: "dev", cores: 4, load: [1, 2, 3], cgroups: false, threads: [] };
  assert.deepEqual(cardLines(loadCard(plain)), [
    "Calm: The load is 25 % of the cores.",
    "Load: 25 % of 4 cores · 1.0 now · 2.0 over 5 min · 3.0 over 15 min",
    "Agents: share the daemon's cgroup · no per-thread figures",
  ]);
  // V0.process-tree-hosts: a host without cgroups reads its agents from the process trees.
  const mac: HostLoad = { ...plain, agents: { cpu: 1.5, cpuMax: 0, memory: 2e9, memoryMax: 0 } };
  assert.equal(cardLines(loadCard(mac))[2], "Agents use: 1.5 cores · 2.0 GB · no cap · process trees");
});

// V0.load-cleanup: oomd's rule (V0.oomd-guard) on the card: where it kills and the stall now, hot within 10 points.
test("the card shows oomd's limit and the stall it compares, and nothing without a rule", () => {
  const rule = (o: Partial<HostLoad>) => cardLines(loadCard({ ...busy, ...o })).find((l) => l.startsWith("oomd:"));
  assert.equal(rule({ oomdLimit: 85, unitMemoryStall: 3.2 }), "oomd: kills at 85 % stall · now 3 %");
  assert.equal(rule({ oomdLimit: 80 }), "oomd: kills at 80 % stall");
  assert.equal(rule({ unitMemoryStall: 3.2 }), undefined);
  const row = (o: Partial<HostLoad>) => loadCard({ ...busy, ...o }).sections[0].rows.find((r) => r.label === "oomd");
  assert.equal(row({ oomdLimit: 85, unitMemoryStall: 74.9 })?.hot, false);
  assert.equal(row({ oomdLimit: 85, unitMemoryStall: 75 })?.hot, true);
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

// SPEC 0.5 V0.86, V0.163: heavy jobs on the rail and on a thread's row; a host that counts jobs (no measures) shows
// its share of the count in percent, not a count.
test("a counted host's heavy jobs read as a share of the count, hot while anyone waits", () => {
  const h: HostLoad = { ...busy, slots: { n: 2, held: [{ owner: "t_busy", kind: "test" }], waiting: [] } };
  assert.deepEqual(railBudget(h), { text: "50 %", note: "", share: 0.5, hot: false, waiting: 0 });
  const full: HostLoad = {
    ...busy,
    slots: {
      n: 2,
      held: [
        { owner: "t_busy", kind: "test" },
        { owner: "t_calm", kind: "build" },
      ],
      waiting: [
        { owner: "self-deploy", kind: "test", priority: true },
        { owner: "t_new", kind: "e2e" },
      ],
    },
  };
  assert.deepEqual(railBudget(full), { text: "100 %", note: "2 wait", share: 1, hot: true, waiting: 2 });
  assert.equal(railBudget({ ...busy, slots: undefined }), null);
  const d = cardLines(loadCard(full));
  assert.ok(d.includes("In use: 2 of 2 jobs · counted, not measured"), d.join("\n"));
  assert.ok(d.includes("Waiting: 2 jobs"), d.join("\n"));
  assert.equal(threadSlot([full], "t_new"), "waiting (1 ahead)");
  assert.equal(threadSlot([full], "self-deploy"), "waiting (0 ahead)");
  assert.equal(threadSlot([full], "t_calm"), "build job");
  assert.equal(threadSlot([full], "t_other"), null);
  assert.equal(threadSlot([], "t_new"), null);
});

test("V0.99: the ring fills with the load over the cores; calm, warm from 70 %, hot by the V0.80 rule", () => {
  const h = (l: number, p?: number): HostLoad => ({ ...busy, load: [l, 1, 1], cpuPressure: p });
  assert.deepEqual(railMeter(h(2)), { text: "25%", share: 0.25, level: "calm" });
  assert.deepEqual(railMeter(h(5.6)), { text: "70%", share: 0.7, level: "warm" });
  assert.deepEqual(railMeter(h(8)), { text: "100%", share: 1, level: "warm" });
  assert.deepEqual(railMeter(h(10.4)), { text: "130%", share: 1, level: "hot" });
  assert.deepEqual(railMeter(h(1, 45)), { text: "13%", share: 0.125, level: "hot" });
  assert.equal(railMeter({ ...h(1), cores: 0 }).share, 1);
  assert.equal(railMeter({ ...h(1), cores: 0 }).text, "1.0"); // no cores known: the load itself
});

test("V0.152: the rail names hosts by the part that differs, the full name when that is not enough", () => {
  const m = (ns: string[]) => [...shortHostNames(ns).values()];
  assert.deepEqual(m(["meros-laptop", "meros-server"]), ["laptop", "server"]);
  assert.deepEqual(m(["desk", "lab"]), ["desk", "lab"]);
  assert.deepEqual(m(["a.lan", "b.lan"]), ["a", "b"]);
  assert.deepEqual(m(["build-1.lan", "build-2.lan", "desk"]), ["build-1.lan", "build-2.lan", "desk"]);
  assert.deepEqual(m(["meros-pc", "meros"]), ["meros-pc", "meros"]);
  assert.deepEqual(m(["solo-host"]), ["solo-host"]);
});

test("V0.166: the phone's strip lists hosts in the owner's order: those with a load, and the offline ones", () => {
  const l = (host: string): HostLoad => ({ host, cores: 4, load: [1, 1, 1], cgroups: false, threads: [] });
  const hosts = [
    { name: "desk", online: true },
    { name: "server", online: false, since: "2026-11-17T10:00:00Z" },
    { name: "nuc", online: true }, // reports no load: left out
    { name: "lab", online: true },
  ];
  const r = stripHosts([l("lab"), l("desk")], hosts);
  assert.deepEqual(r.map((h) => [h.name, !!h.load, h.offline ?? null]), [["desk", true, null], ["server", false, "2026-11-17T10:00:00Z"], ["lab", true, null]]);
  assert.deepEqual(stripHosts([l("a")], []).map((h) => h.name), ["a"]); // no host list: the load's own
  assert.deepEqual(stripHosts([l("other")], [{ name: "desk", online: true }]).map((h) => h.name), ["other"]);
});

// V0.services-not-jobs: the row names a thread's running services and what they hold, containers included.
test("a thread's services on its row", () => {
  const h: HostLoad = {
    ...busy,
    threads: [
      { id: "t_one", memory: 4e9, services: [{ name: "process-compose up", memory: 3.2e9, reserve: 4e9 }] },
      { id: "t_two", memory: 1e9, services: [{ name: "npm run dev", memory: 1e9, reserve: 1e9 }, { name: "compose shop", memory: 0, project: "shop", containers: 2.1e9, reserve: 2.5e9 }] },
    ],
  };
  assert.equal(threadService([h], "t_one"), "service · 3.2 GB");
  assert.equal(threadService([h], "t_two"), "2 services · 3.1 GB");
  assert.equal(threadService([h], "t_busy"), null);
});
