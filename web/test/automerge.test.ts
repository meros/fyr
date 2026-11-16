import { test } from "node:test";
import assert from "node:assert/strict";
import { autoMergeLine, repoKey, sortRepos } from "../src/automerge.ts";
import { buildConversation } from "../src/conversation.ts";

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

// SPEC V0.111 (6): a deploy Fyr approved by the auto-deploy setting says so on the change.
test("a deploy auto-approved by the setting says so", () => {
  assert.equal(autoMergeLine({ autoApproved: { deploy: true, head: "c0ffee1234567890", at: "2026-11-16T12:00:00Z" } }), "Deploy auto-approved by the setting (c0ffee1)");
});

// SPEC V0.111 (2), (4): a held or waiting deploy ask is one line in the thread.
test("a held deploy ask is a line in the thread", () => {
  const items = buildConversation([
    { seq: 1, at: "2026-11-16T14:00:00Z", subject: "t", type: "effect.error", data: { kind: "autoDeploy", error: "Not auto-approved: the checkout has changes that are not committed. The deploy waits for your press." } },
  ]);
  assert.equal(items.length, 1);
  assert.equal(items[0].kind, "effect");
  assert.equal((items[0] as { line: string }).line, "Not auto-approved: the checkout has changes that are not committed. The deploy waits for your press.");
});
