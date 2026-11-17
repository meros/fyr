// Decision 0029 S2 (#186): with admission by a budget, the rail shows the budget in use as a bar, the tooltip lists the
// holders' costs and why the next job waits, and the first waiting thread's row says why. Without a budget (no
// provider), the pips stay.
import { test } from "node:test";
import assert from "node:assert/strict";
import { loadDetail, railBudget, railLabel, railPips, slotTip, threadSlot, type HostLoad } from "../src/load.ts";

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
    used: 1.1,
    why: "CPU: new jobs run 2.1× slower",
    held: [
      { owner: "t_a", kind: "test", cost: 1 },
      { owner: "t_b", kind: "e2e", cost: 0.1 },
    ],
    waiting: [
      { owner: "t_c", kind: "test" },
      { owner: "t_d", kind: "build" },
    ],
  },
};

test("the rail shows the budget in use, hot while anyone waits; pips without a budget", () => {
  assert.deepEqual(railBudget(host), { text: "1.1/2", note: "2 wait", share: 0.55, hot: true, waiting: 2 });
  assert.equal(railPips(host), null);
  const plain: HostLoad = { ...host, slots: { n: 2, held: [{ owner: "t_a", kind: "test" }], waiting: [] } };
  assert.equal(railBudget(plain), null);
  assert.deepEqual(railPips(plain), { n: 2, held: 1, waiting: 0 });
  assert.deepEqual(railBudget({ ...host, slots: { ...host.slots!, used: 2.4, waiting: [] } }), { text: "2.4/2", note: "full", share: 1, hot: false, waiting: 0 });
  // V0.152: a word only when it matters: none while there is room, "1 waits" (hot) while one job is held back.
  assert.equal(railBudget({ ...host, slots: { ...host.slots!, used: 1.1, waiting: [] } })!.note, "");
  assert.deepEqual(railBudget({ ...host, slots: { ...host.slots!, waiting: [{ owner: "t_c", kind: "test" }] } })!.note, "1 waits");
});

test("the tooltip lists the holders' costs and why the next job waits", () => {
  assert.ok(loadDetail(host).includes("Slots: 1.1 of 2 in use (test 1.0, e2e 0.1 idle), 2 waiting; the next waits: CPU: new jobs run 2.1× slower"), loadDetail(host).join("\n"));
  assert.match(railLabel(host, false), /1\.1 of 2 slot budget in use, 2 waiting/);
  // V0.152: the slot meter's own tooltip says what it is.
  assert.equal(slotTip(host), "Slots for heavy jobs (tests, builds, e2e runs): 1.1 of 2 in use (test 1.0, e2e 0.1 idle), 2 waiting; the next waits: CPU: new jobs run 2.1× slower");
});

test("the first waiting thread's row says why it waits; the others their place", () => {
  assert.equal(threadSlot([host], "t_c"), "waiting for a test slot (0 ahead): CPU: new jobs run 2.1× slower");
  assert.equal(threadSlot([host], "t_d"), "waiting for a build slot (1 ahead)");
});
