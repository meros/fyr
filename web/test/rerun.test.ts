// V0.rerun-classes (decision 0036 (F)): the owner's rerunnable classes in Settings → Automation (desktop and phone, one
// component), what the guard did to each in the last hour, and the guard's history lines for a kill and a cap.
import { test } from "node:test";
import assert from "node:assert/strict";
import { guardChart, guardLog, rerunRows, type Guard } from "../src/guard.ts";
import { mockRerunCommand } from "../src/mock-load.ts";
import type { HostLoad } from "../src/host-load.ts";

const clock = (iso: string) => iso.slice(11, 16);
const NOW = Date.parse("2026-11-19T14:00:00Z");
const guard = (over: Partial<Guard> = {}): Guard => ({ short: false, max: 8, refreezeSec: 60, events: [], ...over });
const host = (g: Guard, classes: string[]): HostLoad => ({ host: "laptop", rerun: { classes }, slots: { guard: g } }) as unknown as HostLoad;

test("R1 each rerunnable class says what the guard did to it in the last hour", () => {
  const g = guard({ rerun: [{ class: "make test", kills: 3, cappedUntil: "2026-11-19T14:41:00Z" }, { class: "tsc", kills: 1 }] });
  assert.deepEqual(rerunRows(host(g, ["make test", "tsc", "yarn lint"]), clock), [
    { class: "make test", line: "Killed 3 times in the last hour; frozen instead until 14:41" },
    { class: "tsc", line: "Killed once in the last hour" },
    { class: "yarn lint", line: "Not killed in the last hour" },
  ]);
  assert.deepEqual(rerunRows({ host: "old" } as HostLoad, clock), [], "an older host sends no list");
});

test("R2 the guard's history names a kill and a cap, and a kill ends the frozen stretch", () => {
  const g = guard({
    events: [
      { at: "2026-11-19T13:55:00Z", kind: "freeze", work: "job", thread: "t_a" },
      { at: "2026-11-19T13:57:00Z", kind: "kill", work: "job", thread: "t_a", class: "make test", headroom: 0.04 },
      { at: "2026-11-19T13:58:00Z", kind: "capped", class: "make test" },
    ],
  });
  const lines = guardLog(g, () => "Map perf", clock).map((l) => l.text);
  assert.deepEqual(lines.slice(0, 2), [
    "Stopped killing make test for an hour: killed 3 times; it is frozen instead",
    "Killed the job of Map perf (make test) to free memory; it runs again when admitted (4 % free)",
  ]);
  const c = guardChart(g, NOW, () => "Map perf");
  assert.equal(c.lanes.length, 1);
  assert.ok(!c.lanes[0].live, "the killed job is not shown as frozen now");
});

test("R3 the mock host adds and removes a class as the host does", () => {
  const h = { host: "laptop", rerun: { classes: [] } } as unknown as HostLoad;
  assert.ok(mockRerunCommand(h, { add: "  make   test " }).ok);
  assert.deepEqual(h.rerun?.classes, ["make test"]);
  assert.equal(mockRerunCommand(h, { remove: "tsc" }).error, "no such rerunnable class");
  assert.ok(mockRerunCommand(h, { remove: "make test" }).ok);
  assert.deepEqual(h.rerun?.classes, []);
});
