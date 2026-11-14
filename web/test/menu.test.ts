// SPEC 0.5 V0.31: Fyr's own right-click menu, chosen by where the click lands.
import { test } from "node:test";
import assert from "node:assert/strict";
import { menuFor, threadMenu, type MenuEntry } from "../src/menu.ts";
import { thread } from "./helpers.ts";

const labels = (m: MenuEntry[]) => m.map((x) => ("sep" in x ? "—" : x.label));

test("a thread row gets the thread's actions in a fixed order", () => {
  const t = thread("t1", "seen", "seen");
  assert.deepEqual(labels(threadMenu(t)), ["Open", "—", "Settle", "Snooze…", "Mark unseen", "Archive", "—", "Project…", "Turn on live view", "Hand over…", "—", "Copy link"]); // V0.37 adds the live view, V0.45 the handover
});

test("the menu follows the thread's state", () => {
  const settled = thread("t2", "settled", "settled", { settledAt: "2026-01-01T00:00:00Z" });
  assert.ok(labels(threadMenu(settled)).includes("Unsettle"));
  const running = thread("t3", "working", "working", { running: true });
  assert.ok(labels(threadMenu(running)).includes("Stop run"));
  const snoozed = thread("t4", "snoozed", "snoozed", { view: { snoozeUntil: "2099-01-01T00:00:00Z" } });
  assert.ok(labels(threadMenu(snoozed)).includes("Unsnooze"));
});

test("an action the host says is unavailable is disabled with its reason", () => {
  const t = thread("t5", "asking", "needs", { unavailable: { "thread.settle": "A question is open" } });
  const settle = threadMenu(t).find((x) => !("sep" in x) && x.id === "settle");
  assert.ok(settle && !("sep" in settle));
  assert.equal(settle.disabled, "A question is open");
});

test("the target decides the menu", () => {
  assert.equal(menuFor({ kind: "editable" }), null, "text fields keep the browser menu");
  assert.deepEqual(menuFor({ kind: "none" }), [], "elsewhere: nothing opens");
  assert.deepEqual(labels(menuFor({ kind: "text", selection: "hej" })!), ["Copy"]);
  assert.deepEqual(labels(menuFor({ kind: "link", href: "https://x.y" })!), ["Open link", "Copy link"]);
  assert.deepEqual(labels(menuFor({ kind: "link", href: "https://x.y", selection: "x" })!), ["Copy", "—", "Open link", "Copy link"]);
  assert.deepEqual(labels(menuFor({ kind: "image", src: "/api/attachments/a" })!), ["Open image"]);
});

// SPEC 0.5 V0.36: a merged PR can be dismissed from the menu; an open one is not offered.
test("Dismiss PR for a merged PR only", () => {
  const merged = thread("t6", "seen", "seen", { pr: { url: "u", number: 15, state: "MERGED" } });
  assert.ok(labels(threadMenu(merged)).includes("Dismiss PR"));
  const open = thread("t7", "seen", "seen", { pr: { url: "u", number: 16, state: "OPEN" } });
  assert.ok(!labels(threadMenu(open)).includes("Dismiss PR"));
});
