// SPEC V0.126 (#151): the update record's rows and the line over them.
import { test } from "node:test";
import assert from "node:assert/strict";
import { changeLines, changesLabel, releaseSource, updateRow, updateState } from "../src/updates.ts";

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

test("V0.update-changelog a record's changelog: a count to open it, each commit's title without its PR, and the older ones", () => {
  const u = {
    build: "3be8b2f47894", commit: "3be8b2f47894aaaa", from: "d93e1ba4c5d6", trigger: "deployable", result: "installed", startedAt: "2026-11-20T09:21:00", changesAll: 5,
    changes: [
      { commit: "3be8b2f47894aaaa", title: "An opened subagent shows its prompt (#424)" },
      { commit: "64b8fd3112a3bbbb", title: "No PR here" },
    ],
  };
  assert.equal(changesLabel(u), "5 changes");
  assert.equal(changesLabel({ ...u, changes: [u.changes[0]], changesAll: 1 }), "1 change");
  assert.equal(changesLabel({ ...u, changes: undefined, changesAll: undefined }), "");
  const ls = changeLines(u, "o/r");
  assert.deepEqual(ls[0], { title: "An opened subagent shows its prompt", url: "https://github.com/o/r/commit/3be8b2f47894aaaa", sha: "3be8b2f", pr: 424, prUrl: "https://github.com/o/r/pull/424" });
  assert.equal(ls[1].title, "No PR here");
  assert.equal(ls[1].pr, undefined);
  assert.deepEqual(ls[2], { title: "and 3 older", url: "https://github.com/o/r/compare/d93e1ba4c5d6...3be8b2f47894aaaa", sha: "" });
});

test("V0.release-without-gh the release source row names the repo and the key's fingerprint, and a bad key", () => {
  const fp = "SHA256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU";
  assert.deepEqual(releaseSource({ updates: [], repo: "meros/fyr", key: { fingerprint: fp, builtIn: true } }), { title: "meros/fyr", desc: `signed with ${fp} (built in)`, bad: false });
  assert.deepEqual(releaseSource({ updates: [], repo: "fork/fyr", key: { fingerprint: fp, builtIn: false } }), { title: "fork/fyr", desc: `signed with ${fp} (updateKey)`, bad: false });
  assert.deepEqual(releaseSource({ updates: [], repo: "fork/fyr", key: { error: "updateKey is not an ed25519 public key" } }), { title: "fork/fyr", desc: "updateKey is not an ed25519 public key", bad: true });
  assert.equal(releaseSource({ updates: [] }), null); // an older host says neither
});
