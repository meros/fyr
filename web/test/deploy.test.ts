// SPEC 0.5 V0.39: the deploy banner says where an update is, in words.
import { test } from "node:test";
import assert from "node:assert/strict";
import { deployLine } from "../src/deploy.ts";
import { thread } from "./helpers.ts";

const now = Date.parse("2026-11-14T15:10:00Z");
const p = (phase: string, extra: Record<string, unknown> = {}) => ({ phase, startedAt: "2026-11-14T15:06:00Z", at: "2026-11-14T15:09:00Z", build: "20261114-160324", ...extra });
const threads = { t_a: thread("t_a", "working", "working", { title: "Plan a custom client" }), t_b: thread("t_b", "working", "working", { title: "Map perf" }) };

test("each phase reads as a sentence with the time so far", () => {
  assert.deepEqual(deployLine(p("build"), threads, now, "live"), { text: "Updating Fyr · building the new version · 4 min", tone: "busy" });
  assert.equal(deployLine(p("test"), threads, now, "live")?.text, "Updating Fyr · running the tests · 4 min");
  assert.equal(deployLine(p("drain", { blocking: ["t_a", "t_b"] }), threads, now, "live")?.text, "Updating Fyr · waiting for 2 threads to finish: Plan a custom client, Map perf");
  assert.equal(deployLine(p("drain", { blocking: ["t_a"] }), threads, now, "live")?.text, "Updating Fyr · waiting for “Plan a custom client” to finish its turn");
  assert.equal(deployLine(p("switch"), threads, now, "live")?.text, "Restarting Fyr · this page reloads by itself");
});

test("while the host is away during an update, the banner says it is restarting", () => {
  assert.equal(deployLine(p("drain", { blocking: ["t_a"] }), threads, now, "offline")?.text, "Restarting Fyr · back in a few seconds");
});

test("the end: updated, or failed with the first line of the reason", () => {
  assert.deepEqual(deployLine(p("done"), threads, now, "live"), { text: "Fyr is updated · build 20261114-160324", tone: "ok" });
  assert.deepEqual(deployLine(p("failed", { detail: "tests failed, nothing was switched: exit 2\nFAIL TestX" }), threads, now, "live"), { text: "The update failed: tests failed, nothing was switched: exit 2", tone: "fail", more: "tests failed, nothing was switched: exit 2\nFAIL TestX" });
  assert.equal(deployLine(null, threads, now, "live"), null);
});
