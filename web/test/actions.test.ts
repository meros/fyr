// V0.one-action-list (#452): one list of a thread's actions behind the desktop's row menu, its header (buttons and
// ⋯ menu) and the phone's sheets. An action reachable on one front is reachable on the other, unless it says why not.
import { test } from "node:test";
import assert from "node:assert/strict";
import { DESK_HEADER_BUTTONS, actionGroups, actionsFor, deskThreadMenu, menuKey, runAction, settleRun, settleToast, threadActions, type ActionHandlers, type ActionId, type Front, type ThreadAction, type Where } from "../src/actions.ts";
import { thread } from "./helpers.ts";

const states = [
  thread("seen", "seen", "seen"),
  thread("unread", "done", "needs"),
  thread("running", "working", "working", { running: true }),
  thread("failed", "failed", "needs"),
  thread("asking", "asking", "needs", { unavailable: { "thread.settle": "A question is open" } }),
  thread("settled", "settled", "settled", { settledAt: "2026-01-01T00:00:00Z" }),
  thread("snoozed", "snoozed", "snoozed", { view: { snoozeUntil: "2099-01-01T00:00:00Z" } }),
  thread("archived", "archived", "archived", { archivedAt: "2026-01-01T00:00:00Z" }),
  thread("pinned", "seen", "seen", { pinnedAt: "2026-11-16T10:00:00Z", liveView: true }),
  thread("merged", "seen", "seen", { pr: { url: "u", number: 1, state: "MERGED" } }),
  thread("armed", "working", "working", { running: true, settleWhenDone: true }),
];

/** What a front puts in reach of the owner here: the desktop thread pane has header buttons beside its ⋯ menu. */
function reachable(front: Front, t: (typeof states)[number], where: Where): Set<ActionId> {
  if (front === "desktop" && where === "thread") {
    const all = actionsFor("desktop", t, "thread").map((a) => a.id);
    return new Set([...deskThreadMenu(t).map((a) => a.id), ...all.filter((id) => DESK_HEADER_BUTTONS.includes(id))]);
  }
  return new Set(actionsFor(front, t, where).map((a) => a.id));
}

test("every action is reachable on both fronts, unless it says why not", () => {
  const seen = new Set<ActionId>();
  for (const t of states)
    for (const where of ["row", "thread"] as const) {
      const all = threadActions(t, where);
      const desk = reachable("desktop", t, where);
      const phone = reachable("phone", t, where);
      for (const a of all) {
        seen.add(a.id);
        const on = { desktop: desk.has(a.id), phone: phone.has(a.id) };
        if (on.desktop && on.phone) continue;
        assert.ok(a.only && a.only.why.length > 20, `${t.id}/${where}: ${a.id} is on ${on.desktop ? "the desktop" : "the phone"} only, and says no reason`);
        assert.ok(on[a.only.front], `${t.id}/${where}: ${a.id} is meant for the ${a.only.front} but is not there`);
      }
    }
  // The matrix reaches every action, so the check above covers them all.
  const ids: ActionId[] = ["open", "retry", "pin", "unpin", "settle", "unsettle", "settleWhenDone", "snooze", "unsnooze", "markSeen", "markUnseen", "archive", "unarchive", "project", "liveView", "handover", "regenTitle", "interrupt", "dismissPR", "details", "select", "copyLink"];
  assert.deepEqual([...seen].sort(), [...ids].sort());
});

test("the drift #452 found is gone: the phone has the desktop's entries and the desktop has Retry", () => {
  const phone = (t: (typeof states)[number]) => actionsFor("phone", t, "thread").map((a) => a.label);
  const at = (id: string) => states.find((t) => t.id === id)!;
  assert.ok(phone(at("settled")).includes("Unsettle"));
  assert.ok(phone(at("snoozed")).includes("Unsnooze"));
  assert.ok(phone(at("archived")).includes("Unarchive"));
  assert.ok(phone(at("running")).includes("Stop run"));
  assert.ok(phone(at("merged")).includes("Dismiss PR"));
  assert.ok(phone(at("seen")).includes("Copy link"));
  assert.ok(actionsFor("desktop", at("failed"), "row").some((a) => a.id === "retry"), "the row menu offers Retry");
});

test("the ⋯ menu leaves out what the header shows as buttons", () => {
  for (const t of states) for (const a of deskThreadMenu(t)) assert.ok(!DESK_HEADER_BUTTONS.includes(a.id), `${t.id}: ${a.id} is both a button and in ⋯`);
  assert.ok(!deskThreadMenu(states[0]).some((a) => a.id === "open"), "the open thread is not offered Open");
});

test("a disabled entry carries the host's reason and runs nothing", () => {
  const asking = states.find((t) => t.id === "asking")!;
  const settle = threadActions(asking, "row").find((a) => a.id === "settle")!;
  assert.equal(settle.disabled, "A question is open");
  const calls: string[] = [];
  const h: ActionHandlers = { command: (c) => calls.push(c), sheet: (s) => calls.push(s), navigate: () => calls.push("nav"), copy: (x) => calls.push(x) };
  runAction(settle, asking.id, h);
  assert.deepEqual(calls, []);
});

test("one dispatch: commands, sheets, the front's own handling and Copy link", () => {
  const t = states[0];
  const calls: unknown[] = [];
  const h: ActionHandlers = {
    command: (c, id, args) => calls.push(["cmd", c, id, args]),
    sheet: (s, id) => calls.push(["sheet", s, id]),
    navigate: (id) => calls.push(["nav", id]),
    copy: (x) => calls.push(["copy", x]),
    own: { settle: (id) => calls.push(["settle", id]) },
  };
  const by = (id: ActionId): ThreadAction => threadActions(t, "row").find((a) => a.id === id)!;
  for (const id of ["liveView", "snooze", "open", "settle", "copyLink"] as const) runAction(by(id), t.id, h, "https://fyr");
  assert.deepEqual(calls, [
    ["cmd", "thread.setView", "seen", { on: true }],
    ["sheet", "snooze", "seen"],
    ["nav", "seen"],
    ["settle", "seen"],
    ["copy", "https://fyr/?open=seen"],
  ]);
});

test("the settle toast names a stopped run, from one place", () => {
  assert.equal(settleToast(states.find((t) => t.id === "running")), "Settled; run stopped");
  assert.equal(settleToast(states[0]), undefined);
});

test("V0.dialogs-review: the ⋯ menu, the row menu and the phone's sheet show one order, Archive last", () => {
  // V0.26 put Project… first in the ⋯ menu; one list in three orders was the cost (review round 2, 12).
  for (const t of states) {
    const all = threadActions(t, "thread").map((a) => a.id);
    const menu = deskThreadMenu(t).map((a) => a.id);
    assert.deepEqual(menu, all.filter((id) => menu.includes(id)), `${t.id}: the ⋯ menu keeps the list's order`);
    for (const where of ["row", "thread"] as Where[])
      for (const front of ["desktop", "phone"] as Front[]) {
        const list = actionsFor(front, t, where);
        assert.ok(["archive", "unarchive"].includes(list.at(-1)!.id), `${t.id} ${front} ${where}: what takes the thread away is last`);
        assert.equal(list.at(-1)!.group, "end");
        assert.equal(actionGroups(list).flat().length, list.length);
        assert.equal(new Set(actionGroups(list).map((g) => g[0].group)).size, actionGroups(list).length, "each group is one run of the list");
      }
  }
});

test("V0.dialogs-review: a menu shows a key only when the key acts while the menu is open", () => {
  const t = states.find((x) => x.id === "running") ?? states[0];
  const row = actionsFor("desktop", t, "row");
  assert.equal(menuKey(row.find((a) => a.id === "open")!), undefined, "Enter takes the focused entry");
  const stop = row.find((a) => a.id === "interrupt");
  if (stop) assert.equal(menuKey(stop), undefined, "Esc closes the menu");
  assert.equal(menuKey(row.find((a) => a.id === "archive")!), "#");
  assert.equal(menuKey({ key: "s", disabled: "Nothing to snooze" }), undefined, "an entry that is off has no key");
});

test("settleRun: the one run option both fronts pass to thread.settle", () => {
  assert.equal(settleRun({ running: true })?.label(), "Settled; run stopped");
  assert.equal(settleRun({ running: false }), undefined);
  assert.equal(settleRun(undefined), undefined);
});
