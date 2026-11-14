// SPEC 0.5 V0.45: the handover dialog says which rung runs; the divider says which one ran.
import { test } from "node:test";
import assert from "node:assert/strict";
import { dividerLine, handoverTargets, parsePlan, planGuess, rungDone, rungText, targetModel } from "../src/handover.ts";
import { buildConversation } from "../src/conversation.ts";
import { threadMenu } from "../src/menu.ts";
import { thread } from "./helpers.ts";
import type { Profile } from "../src/types.ts";

const profiles: Profile[] = [
  { id: "claude", name: "Claude", color: "", models: ["opus", "haiku"], defaultModel: "opus" },
  { id: "norban", name: "Claude (norban)", color: "", models: ["sonnet"] },
  { id: "bare", name: "Bare", color: "", models: [] },
];

test("the dialog names each rung in the owner's words", () => {
  assert.equal(rungText("resume"), "Full history: this conversation continues as it is");
  assert.equal(rungText("adopt"), "The new agent reads the old conversation");
  assert.equal(rungText("brief"), "The current agent writes a handover first");
});

test("the targets are the other profiles, each with its own model", () => {
  const t = thread("t1", "seen", "seen");
  assert.deepEqual(handoverTargets(t, profiles).map((p) => p.id), ["norban", "bare"]);
  assert.equal(targetModel(profiles[1]), "sonnet");
  assert.equal(targetModel(profiles[2]), "");
});

test("the plan comes from the host; without one, a session means resume", () => {
  assert.deepEqual(parsePlan({ rung: "adopt", note: "No session transcript" }), { rung: "adopt", note: "No session transcript" });
  assert.equal(parsePlan({ rung: "other" }), null);
  assert.equal(planGuess(thread("t", "seen", "seen", { sessionId: "s1" })).rung, "resume");
  assert.equal(planGuess(thread("t", "seen", "seen")).rung, "adopt");
});

test("the divider names both profiles and the time", () => {
  assert.equal(dividerLine({ from: "claude", to: "norban" }, profiles, "17:20"), "Handed over from Claude to Claude (norban) · 17:20");
  assert.equal(rungDone("resume"), "Full history");
  assert.equal(rungDone("adopt"), "The new agent read the old conversation");
});

test("a handover event becomes a divider; a failed one a failed line", () => {
  const items = buildConversation([
    { seq: 1, at: "2026-11-14T16:20:00Z", subject: "t", type: "handover", data: { from: "claude", to: "norban", rung: "adopt", note: "Resume failed: x" } },
    { seq: 2, at: "2026-11-14T16:21:00Z", subject: "t", type: "effect.error", data: { kind: "handover", error: "Dead could not start" } },
  ]);
  assert.equal(items[0].kind, "handover");
  const h = items[0] as Extract<(typeof items)[number], { kind: "handover" }>;
  assert.equal(h.from, "claude");
  assert.equal(h.rung, "adopt");
  assert.equal(h.note, "Resume failed: x");
  assert.equal(items[1].kind, "effect");
  assert.match((items[1] as { line: string }).line, /^Handover failed: Dead could not start/);
});

test("the thread menu offers Hand over…, disabled with the host's reason", () => {
  const t = thread("t1", "working", "working", { unavailable: { "thread.handover": "Wait for the run to end" } });
  const it = threadMenu(t).find((x) => !("sep" in x) && x.id === "handover");
  assert.ok(it && !("sep" in it));
  assert.equal(it.label, "Hand over…");
  assert.equal(it.disabled, "Wait for the run to end");
});
