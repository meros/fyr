// Decision 0029 S3 (#187), V0.163: Settings shows each class's learned need on this host and how many runs it comes from.
import { test } from "node:test";
import assert from "node:assert/strict";
import { jobLines, type HostLoad } from "../src/host-load.ts";

const h: HostLoad = {
  host: "desk",
  cores: 4,
  load: [1, 1, 1],
  cgroups: true,
  threads: [],
  provider: "cgroups",
  jobs: [
    { class: "fyr: make test", runs: 14, cores: 2.3, demand: 3.4, peak: 2.1e9, wall: 95, at: "x", learned: 14, cpuPct: 58, memPct: 15 },
    { class: "fyr: go test ./...", runs: 2, cores: 0.4, demand: 0.4, peak: 0.5e9, wall: 30, at: "x", learned: 2, cpuPct: 10, memPct: 4 },
    { class: "fyr: e2e", runs: 1, cores: 0.9, demand: 0.9, peak: 1e9, wall: 60, at: "x" },
  ],
};

test("a learned class shows its need; under three runs it says how many runs are still to learn", () => {
  const l = jobLines(h);
  assert.equal(l[0].cost, "≈ 58 % CPU · 15 % memory of this host · learned from 14 runs");
  assert.equal(l[1].cost, "≈ 10 % CPU · 4 % memory of this host · 2 of 3 runs to learn");
  assert.equal(l[2].cost, "");
  assert.equal(l[0].learned, true);
  assert.equal(l[2].learned, false);
});

// V0.load-cleanup: the one-number cost of decision 0029 S2 is no longer read; a class without percentages shows no need.
test("a class with only the old one-number cost shows no need", () => {
  const old = { ...h, jobs: [{ ...h.jobs![2], learned: 3, cost: 1.7 } as NonNullable<HostLoad["jobs"]>[number]] };
  assert.equal(jobLines(old)[0].cost, "");
});
