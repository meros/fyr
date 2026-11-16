import { test } from "node:test";
import assert from "node:assert/strict";
import { autoMergeLine, repoKey, sortRepos } from "../src/automerge.ts";

// SPEC V0.102: the board says a merge was auto-approved by the repo setting.
test("a change auto-approved by the repo setting says so", () => {
  assert.equal(autoMergeLine({ autoApproved: { repo: "acme/app", head: "a1b2c3d4e5f6", at: "2026-11-16T12:00:00Z" } }), "Merge auto-approved by the repo setting (acme/app, a1b2c3d)");
  assert.equal(autoMergeLine({}), null);
});

test("a repo name is owner/name in lower case", () => {
  assert.equal(repoKey(" Acme/App "), "acme/app");
  assert.equal(repoKey("acme"), "");
  assert.equal(repoKey("https://github.com/acme/app"), "");
});

test("opted-in repos come first", () => {
  assert.deepEqual(
    sortRepos([{ repo: "b/x", on: false }, { repo: "c/y", on: true }, { repo: "a/z", on: false }]).map((r) => r.repo),
    ["c/y", "a/z", "b/x"],
  );
});
