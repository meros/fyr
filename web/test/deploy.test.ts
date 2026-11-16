// SPEC 0.5 V0.39: the deploy banner says where an update is, in words.
import { test } from "node:test";
import assert from "node:assert/strict";
import { deployLine, deployShown, OK_SHOW_MS, OK_FRESH_MS, canRestartNow, restartNowAsk } from "../src/deploy.ts";
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

// SPEC 0.5 V0.118 (2): the inline status is calm. Building, testing, waiting and restarting show; a success shows a few
// seconds from when this page first sees it and only while it is fresh; a client-only update shows nothing beyond the
// reload; a failure stays until it is dismissed. (V0.118 retires the #81 fold: the inline line is already small.)
test("the inline status shows busy and failed, a success briefly, a client-only update not at all", () => {
  const show = (q: ReturnType<typeof p> & { client?: boolean }, seenAt: number | null = null, conn = "live") => deployShown(q, deployLine(q, threads, now, conn), seenAt, now);
  for (const ph of ["build", "test", "drain", "switch"]) assert.equal(show(p(ph)), true, ph);
  assert.equal(show(p("client")), false, "the reload is the only sign of a client-only update");
  assert.equal(show(p("failed", { detail: "x" })), true);
  assert.equal(show(p("failed", { detail: "x" }), now - 10 * 60_000), true, "a failure stays");
  const done = p("done", { at: new Date(now - 3000).toISOString() });
  assert.equal(show(done), true, "a fresh success shows");
  assert.equal(show(done, now - 1000), true, "for a few seconds");
  assert.equal(show(done, now - OK_SHOW_MS), false, "then goes by itself");
  assert.equal(show({ ...done, client: true }), false, "a client-only success shows nothing");
  assert.equal(show(p("done", { at: new Date(now - OK_FRESH_MS - 1000).toISOString() })), false, "a success read late (a page opened later) shows nothing");
  assert.equal(show(p("drain"), null, "offline"), true, "restarting while the host is away");
  assert.equal(deployShown(null, null, null, now), false);
  assert.ok(OK_SHOW_MS >= 2000 && OK_SHOW_MS <= 8000, "a few seconds");
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
