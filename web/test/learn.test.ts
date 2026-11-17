// Decision 0029 S3 (#187): Settings shows each class's learned cost on this host and how many runs it comes from.
import { test } from "node:test";
import assert from "node:assert/strict";
import { jobLines, type HostLoad } from "../src/load.ts";

const h: HostLoad = {
  host: "desk",
  cores: 4,
  load: [1, 1, 1],
  cgroups: true,
  threads: [],
  provider: "cgroups",
  jobs: [
    { class: "fyr: make test", runs: 14, cores: 2.3, demand: 3.4, peak: 2.1e9, wall: 95, at: "x", cost: 1.7, learned: 14 },
    { class: "fyr: go test ./...", runs: 2, cores: 0.4, demand: 0.4, peak: 0.5e9, wall: 30, at: "x", cost: 1, learned: 2 },
    { class: "fyr: e2e", runs: 1, cores: 0.9, demand: 0.9, peak: 1e9, wall: 60, at: "x" },
  ],
};

test("a learned class shows its cost; under three runs it says it still counts as 1.0", () => {
  const l = jobLines(h);
  assert.equal(l[0].cost, "cost 1.7 · learned from 14 runs");
  assert.equal(l[1].cost, "cost 1.0 · 2 of 3 runs to learn");
  assert.equal(l[2].cost, "");
  assert.equal(l[0].learned, true);
  assert.equal(l[2].learned, false);
});
