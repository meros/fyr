// V0.sensitive-pr: a sensitive PR is never merged without the owner's confirmed press.
import { test } from "node:test";
import assert from "node:assert/strict";
import { greenMerges, type BoardItem } from "../src/board.ts";
import { press } from "../src/confirm.ts";
import { describeEffect } from "../src/effects.ts";
import { confirmSensitive, decisionSensitive, sensitiveLine, sensitiveMergeAsk } from "../src/sensitive.ts";
import type { Ask } from "../src/confirm.ts";
import type { Change, Decision, Offer } from "../src/types.ts";
import { thread } from "./helpers.ts";

const merge: Offer = { id: "fyr.merge", label: "Merge", run: "fyr", effect: "pr.merge", by: "observed", digest: "d1" };
const ch = (id: string, n: number, sensitive?: string): Change => ({
  id, key: id, kind: "code", title: id, phase: "review", phaseBy: "observed", createdAt: "2026-11-23T09:00:00Z", updatedAt: "2026-11-23T09:00:00Z",
  pr: { url: `https://github.com/a/b/pull/${n}`, number: n, head: `h${n}aaaaaaaa`, state: "OPEN" }, ready: "Merge", offers: [merge], ...(sensitive ? { sensitive } : {}),
});
const mergeAsk = (args: Record<string, unknown>): Decision => ({ id: "d1", kind: "effect", effect: { action: "pr.merge", args } }) as Decision;

test("V0.sensitive-pr: Merge all green leaves a sensitive PR out", () => {
  const t = thread("t1", "seen", "seen");
  const items: BoardItem[] = [ch("c1", 1), ch("c2", 2, "promotes to production")].map((c) => ({ change: c, thread: t }));
  assert.deepEqual(greenMerges(items).map((m) => m.change), ["c1"]);
});

test("V0.sensitive-pr: the reason comes from the ask, else from its change", () => {
  const t = { changes: [ch("c1", 1, 'it has the GitHub label "sensitive"'), ch("c2", 2)] };
  assert.equal(decisionSensitive(t, mergeAsk({ change: "c2", sensitive: " promotes to production " })), "promotes to production");
  assert.equal(decisionSensitive(t, mergeAsk({ change: "c1" })), 'it has the GitHub label "sensitive"');
  assert.equal(decisionSensitive(t, mergeAsk({ change: "c2" })), "");
  assert.equal(decisionSensitive(t, { id: "d2", kind: "effect", effect: { action: "fyr.deploy", args: { sensitive: "x" } } } as Decision), "");
});

test("V0.sensitive-pr: a sensitive merge asks first, in red, and goes only on yes", async () => {
  const asked: Ask[] = [];
  const no = async (a: Ask) => (asked.push(a), false);
  assert.equal(await confirmSensitive("promotes to production", no, "abcdef1234"), false);
  assert.equal(await confirmSensitive("", no), true, "a plain merge does not ask here");
  assert.equal(asked.length, 1);
  assert.equal(asked[0].danger, true);
  assert.match(asked[0].text ?? "", /promotes to production/);
  assert.match(asked[0].text ?? "", /abcdef1/);
  assert.deepEqual(sensitiveMergeAsk("x").confirm, "Merge");
  assert.equal(sensitiveLine("promotes to production"), "Sensitive: promotes to production. Fyr never merges it by itself.");
});

test("V0.sensitive-pr: a sensitive Merge button confirms with the reason; cancel sends nothing", async () => {
  const sent: string[] = [];
  let seen: Ask | null = null;
  const ok = await press("c1", merge, undefined, async (a) => ((seen = a), false), (action) => sent.push(action), "h1aaaaaaaa", "promotes to production");
  assert.equal(ok, false);
  assert.deepEqual(sent, []);
  assert.equal(seen!.danger, true);
  assert.match(seen!.text ?? "", /promotes to production/);
});

test("V0.sensitive-pr: the merge ask's card carries the agent's reason", () => {
  assert.equal(describeEffect(mergeAsk({ pr: "https://github.com/a/b/pull/1", sensitive: "promotes to production" })).sensitive, "promotes to production");
  assert.equal(describeEffect(mergeAsk({ pr: "https://github.com/a/b/pull/1" })).sensitive, undefined);
});
