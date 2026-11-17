// SPEC 0.5 V0.31: Fyr's own right-click menu, chosen by where the click lands.
import { test } from "node:test";
import assert from "node:assert/strict";
import { anchorMoved, menuFor, threadMenu, type MenuEntry } from "../src/menu.ts";
import { thread } from "./helpers.ts";

const labels = (m: MenuEntry[]) => m.map((x) => ("sep" in x ? "—" : x.label));

test("a thread row gets the thread's actions in a fixed order", () => {
  const t = thread("t1", "seen", "seen");
  assert.deepEqual(labels(threadMenu(t)), ["Open", "—", "Pin", "Settle", "Snooze…", "Mark unseen", "Archive", "—", "Project…", "Turn on live view", "Hand over…", "Regenerate title", "—", "Copy link"]); // V0.37 adds the live view, V0.45 the handover, V0.108 the title
});

test("the menu follows the thread's state", () => {
  const settled = thread("t2", "settled", "settled", { settledAt: "2026-01-01T00:00:00Z" });
  assert.ok(labels(threadMenu(settled)).includes("Unsettle"));
  const running = thread("t3", "working", "working", { running: true });
  assert.ok(labels(threadMenu(running)).includes("Stop run"));
  const snoozed = thread("t4", "snoozed", "snoozed", { view: { snoozeUntil: "2099-01-01T00:00:00Z" } });
  assert.ok(labels(threadMenu(snoozed)).includes("Unsnooze"));
});

// V0.104: the owner pins and unpins from the menu.
test("a pinned thread offers Unpin", () => {
  const t = thread("t6", "working", "working", { pinnedAt: "2026-11-16T10:00:00Z" });
  const m = labels(threadMenu(t));
  assert.ok(m.includes("Unpin") && !m.includes("Pin"));
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

// V0.31, V0.106: Settle is in a row's menu in every state the host allows it, and a scroll elsewhere (the open thread
// following its run) does not close the menu under the pointer.
test("Settle is offered for a row in each state, with the row's own id", () => {
  const settleOf = (t: ReturnType<typeof thread>) => threadMenu(t).find((x) => !("sep" in x) && x.id === "settle") as { disabled?: string } | undefined;
  for (const t of [
    thread("t_idle", "seen", "seen"),
    thread("t_run", "working", "working", { running: true }),
    thread("t_done", "done", "needs"),
    thread("t_fail", "failed", "needs"),
    thread("t_pin", "seen", "seen", { pinnedAt: "2026-11-16T10:00:00Z" }),
    thread("t_snz", "snoozed", "snoozed", { view: { snoozeUntil: "2099-01-01T00:00:00Z" } }),
  ]) {
    const s = settleOf(t);
    assert.ok(s, `${t.id}: no Settle`);
    assert.equal(s.disabled, undefined, `${t.id}: Settle disabled`);
  }
  const asking = thread("t_ask", "asking", "needs", { unavailable: { "thread.settle": "A question is open" } });
  assert.equal(settleOf(asking)?.disabled, "A question is open");
});

test("a scroll closes the menu only when it moves what the menu was opened on", () => {
  assert.equal(anchorMoved(120, 120), false, "the open thread scrolled; the row stayed");
  assert.equal(anchorMoved(120, 121), false, "subpixel jitter");
  assert.equal(anchorMoved(120, 80), true, "the list scrolled the row away");
  assert.equal(anchorMoved(null, 80), true, "no anchor: any scroll closes, as before");
  assert.equal(anchorMoved(120, null), false, "the row re-rendered; the menu still acts on the thread's id");
});
