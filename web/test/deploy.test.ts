// SPEC 0.5 V0.39: the deploy banner says where an update is, in words.
import { test } from "node:test";
import assert from "node:assert/strict";
import { deployLine, bannerFolded, updateKey, canRestartNow, restartNowAsk } from "../src/deploy.ts";
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

// SPEC 0.5 V0.81: a client-only update says so, never waits for threads,
// and does not claim a restart while the page reconnects.
test("a client-only update says it updates the web client", () => {
  assert.deepEqual(deployLine(p("client"), threads, now, "live"), { text: "Updating the web client · this page reloads by itself", tone: "busy" });
  assert.equal(deployLine(p("client"), threads, now, "connecting")?.text, "Updating the web client · this page reloads by itself");
  assert.equal(deployLine(p("done"), threads, now, "live")?.text, "Fyr is updated · build 20261114-160324");
});

// SPEC 0.5 V0.39, #81: a busy banner folds to a chip and stays folded through the update's phases; the end opens it.
test("the update banner folds while busy and opens when the update ends", () => {
  const p = (phase: string, build = "b1") => ({ phase, build, startedAt: "2026-11-16T07:00:00Z" });
  const k = updateKey(p("drain"));
  assert.equal(k, updateKey(p("test")), "one update keeps one key through its phases");
  assert.notEqual(k, updateKey(p("drain", "b2")), "another build is another update");
  assert.equal(bannerFolded(p("drain"), "busy", k), true);
  assert.equal(bannerFolded(p("switch"), "busy", k), true, "still folded in a later phase");
  assert.equal(bannerFolded(p("done"), "ok", k), false, "done opens it");
  assert.equal(bannerFolded(p("failed"), "fail", k), false, "a failure opens it");
  assert.equal(bannerFolded(p("drain", "b2"), "busy", k), false, "a new update starts open");
  assert.equal(bannerFolded(p("drain"), "busy", null), false);
});

// SPEC 0.5 V0.83: Restart now shows only while the update waits for at least one running thread, with a live host;
// its confirmation names the threads whose turns end and says each gets "continue".
test("Restart now shows only while the update drains running threads", () => {
  assert.equal(canRestartNow(p("drain", { blocking: ["t_a"] }), "live"), true);
  assert.equal(canRestartNow(p("drain", { blocking: [] }), "live"), false, "nothing to wait for");
  assert.equal(canRestartNow(p("drain"), "live"), false);
  for (const ph of ["build", "test", "switch", "client", "done", "failed"]) assert.equal(canRestartNow(p(ph, { blocking: ["t_a"] }), "live"), false, ph);
  assert.equal(canRestartNow(p("drain", { blocking: ["t_a"] }), "offline"), false, "the host is away");
  assert.equal(canRestartNow(null, "live"), false);
});

test("the Restart now confirmation names the cut threads and the continue", () => {
  const a = restartNowAsk(p("drain", { blocking: ["t_a", "t_b"] }), threads);
  assert.equal(a.title, "Restart Fyr now?");
  assert.equal(a.confirm, "Restart now");
  assert.equal(a.danger, true);
  assert.match(a.text ?? "", /Plan a custom client, Map perf/);
  assert.match(a.text ?? "", /“continue”/);
  const one = restartNowAsk(p("drain", { blocking: ["t_a"] }), threads);
  assert.match(one.text ?? "", /“Plan a custom client” ends its turn/);
});
