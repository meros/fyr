// V0.app-shell-tidy: settle, archive and snooze leave the thread only once the host took the command, on both fronts.
import { test } from "node:test";
import assert from "node:assert/strict";
import { leaveAfterHost } from "../src/leave.ts";

test("leaveAfterHost: leaves with what the host took, after it answered", async () => {
  const left: string[][] = [];
  let answer!: (ids: string[]) => void;
  const ran = new Promise<string[]>((r) => (answer = r));
  const done = leaveAfterHost(ran, (ids) => left.push(ids));
  await Promise.resolve();
  assert.deepEqual(left, [], "nothing moves while the host has not answered");
  answer(["a"]);
  assert.equal(await done, true);
  assert.deepEqual(left, [["a"]]);
});

test("leaveAfterHost: a refused or failed command leaves the owner where they were", async () => {
  let called = false;
  assert.equal(await leaveAfterHost(Promise.resolve([]), () => (called = true)), false);
  assert.equal(called, false);
});

test("leaveAfterHost: of several threads, only those the host took leave", async () => {
  let got: string[] = [];
  await leaveAfterHost(Promise.resolve(["b"]), (ids) => (got = ids));
  assert.deepEqual(got, ["b"]);
});
