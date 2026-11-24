// V0.sensitive-pr: a sensitive PR is never merged without the owner's confirmed press.
import { test } from "node:test";
import assert from "node:assert/strict";
import { greenMerges, type BoardItem } from "../src/board.ts";
import { press } from "../src/confirm.ts";
import { describeEffect } from "../src/effects.ts";
import { decisionSensitive, mergeAfterAsk, sensitiveLine } from "../src/sensitive.ts";
import { mergeConfirm, mergeTitle, shortHead, type MergeAsk } from "../src/pr.ts";
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
  // V0.dialogs-review: it asks in the Merge dialog (pr.ts `MergeAsk`), which is red for a sensitive merge.
  const asked: MergeAsk[] = [];
  const no = async (a: MergeAsk) => (asked.push(a), false);
  let plain = 0;
  const go = () => (plain++, true);
  const m = (sensitive?: string): MergeAsk => ({ number: 212, sha: "abcdef1234", sensitive, run: async () => null });
  assert.equal(await mergeAfterAsk(m("promotes to production"), no, go), false);
  assert.equal(plain, 0, "nothing goes on a no");
  assert.equal(await mergeAfterAsk(m(), no, go), true, "a plain merge does not ask here");
  assert.equal(plain, 1);
  assert.equal(asked.length, 1);
  assert.match(asked[0].sensitive ?? "", /promotes to production/);
  assert.match(shortHead(asked[0].sha), /abcdef1/);
  assert.equal(mergeTitle(asked[0]), "Merge #212, a sensitive PR", "the title names what is merged");
  assert.equal(mergeConfirm(asked[0], "squash"), "Merge #212");
  assert.equal(await mergeAfterAsk(m("x"), async () => true, go), true, "and goes on yes");
  assert.equal(sensitiveLine("promotes to production"), "Sensitive: promotes to production. Fyr never merges it by itself.");
});

test("V0.sensitive-pr: a sensitive Merge button confirms with the reason; cancel sends nothing", async () => {
  // The button's merge is the Merge dialog's `run`: it is called only when the owner confirms there.
  const sent: string[] = [];
  let seen = null as MergeAsk | null;
  const ask: MergeAsk = { number: 7, sha: "h1aaaaaaaa", sensitive: "promotes to production", run: async () => (sent.push("action.run"), null) };
  const ok = await mergeAfterAsk(ask, async (a) => ((seen = a), false), () => (sent.push("plain"), true));
  assert.equal(ok, false);
  assert.deepEqual(sent, []);
  assert.match(seen!.sensitive ?? "", /promotes to production/);
  // A press that is not a merge still confirms through the confirmation, and a cancel sends nothing.
  const ran: string[] = [];
  assert.equal(await press("c1", { ...merge, effect: "pr.rerunFailed" }, undefined, async () => false, (action) => void ran.push(action)), false);
  assert.equal(ran.length, 0);
});

test("V0.sensitive-pr: the merge ask's card carries the agent's reason", () => {
  assert.equal(describeEffect(mergeAsk({ pr: "https://github.com/a/b/pull/1", sensitive: "promotes to production" })).sensitive, "promotes to production");
  assert.equal(describeEffect(mergeAsk({ pr: "https://github.com/a/b/pull/1" })).sensitive, undefined);
});
