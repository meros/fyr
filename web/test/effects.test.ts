// SPEC 0.5 V0.27: decision cards for effects in plain words, details behind a toggle.
import { test } from "node:test";
import assert from "node:assert/strict";
import { describeEffect } from "../src/effects.ts";
import type { Decision } from "../src/types.ts";

const eff = (action: string, args: Record<string, unknown>, reason = ""): Decision => ({ id: "d1", kind: "effect", createdAt: "", effect: { action, args, reason } }) as Decision;

test("a deploy reads as updating Fyr, with what happens and no JSON in the main text", () => {
  const e = describeEffect(eff("fyr.deploy", { dir: "/home/u/git/fyr-deploy" }, "The Changes tab and fixes."));
  assert.equal(e.label, "Update Fyr");
  assert.equal(e.title, "Update Fyr to a new version");
  assert.equal(e.why, "The Changes tab and fixes.");
  assert.match(e.happens, /builds and tests/);
  assert.match(e.happens, /back/);
  assert.equal(e.yes, "Update Fyr");
  assert.equal(e.no, "Not now");
  for (const s of [e.title, e.why, e.happens]) assert.doesNotMatch(s, /fyr\.deploy|[{}]|\/home/);
  assert.deepEqual(e.details.args, { dir: "/home/u/git/fyr-deploy" });
  assert.equal(e.details.action, "fyr.deploy");
});

test("a merge names the PR and its title, and the method in words", () => {
  const d = eff("pr.merge", { pr: "https://github.com/meros/fyr/pull/15", method: "squash" });
  const e = describeEffect(d, { url: "https://github.com/meros/fyr/pull/15", number: 15, title: "Fyr v0" });
  assert.equal(e.label, "Merge #15");
  assert.equal(e.title, "Merge pull request #15: Fyr v0");
  assert.match(e.happens, /one commit/);
  assert.equal(e.yes, "Merge");
  const bare = describeEffect(eff("pr.merge", { pr: "https://github.com/a/b/pull/7" }));
  assert.equal(bare.title, "Merge pull request #7");
});

test("an unknown effect falls back to its name in words", () => {
  const e = describeEffect(eff("dns.flush_cache", {}));
  assert.equal(e.title, "Dns flush cache");
  assert.equal(e.yes, "Approve");
  assert.equal(e.no, "Not now");
});
