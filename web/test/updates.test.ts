// SPEC V0.126 (#151): the update record's rows and the line over them.
import { test } from "node:test";
import assert from "node:assert/strict";
import { updateRow, updateState } from "../src/updates.ts";

const now = Date.parse("2026-11-16T23:30:00");

test("V0.126 a release that went live names its commit, why it ran and how long it took", () => {
  const r = updateRow({ build: "c705ef536566", commit: "c705ef5365660a8", trigger: "deployable", result: "installed", startedAt: "2026-11-16T23:10:00", endedAt: "2026-11-16T23:12:00" }, now);
  assert.equal(r.title, "Release c705ef5 · installed");
  assert.equal(r.desc, "new release · 23:10 · 2 min");
  assert.equal(r.fail, false);
});

test("V0.126 a local build that rolled back says its branch and the reason", () => {
  const r = updateRow({ build: "20261116-231000", commit: "1a2b3c4d", trigger: "agent", local: true, branch: "v0.124-key", result: "rolledBack", reason: "was not healthy: the database does not answer", startedAt: "2026-11-16T23:10:00", endedAt: "2026-11-16T23:11:30" }, now);
  assert.equal(r.title, "Local build of v0.124-key (1a2b3c4) · rolled back");
  assert.match(r.desc, /an agent asked · 23:10 · 2 min · was not healthy/);
  assert.equal(r.fail, true);
});

test("V0.126 the state line says when a local build pauses auto-update", () => {
  assert.equal(updateState({ updates: [], live: "c705ef536566" }), "Fyr runs c705ef536566.");
  assert.match(updateState({ updates: [], paused: { build: "20261116-2310", branch: "v0.124-key", at: "" } }), /local build of v0\.124-key runs .*Auto-update waits/);
});
