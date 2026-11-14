// SPEC 0.5 V0.37: the live view's fold, headline, staleness and the bridge's message checks.
import { test } from "node:test";
import assert from "node:assert/strict";
import { PRESS_MAX, bridgeMessage, defaultTab, liveViewOf, rowSummary, staleLabel, stateMessage, viewDimmed, viewHeight } from "../src/view.ts";
import { threadMenu } from "../src/menu.ts";
import { buildConversation } from "../src/conversation.ts";
import { decodeThread } from "../src/decode.ts";
import { thread } from "./helpers.ts";

const live = { pageId: "a1", state: { headline: "Tests pass", steps: ["a"] }, updatedAt: "2026-11-14T09:00:00Z", checkedAt: "2026-11-14T09:00:00Z", headline: "Tests pass" };

test("the view folds from the thread DTO; no page is no view", () => {
  const t = decodeThread({ id: "t1", liveView: true, live, attention: { state: "done", section: "needs" } });
  assert.deepEqual(liveViewOf(t), live);
  assert.equal(liveViewOf(thread("t2", "done", "needs")), null);
  assert.equal(liveViewOf(thread("t3", "done", "needs", { live: { state: { headline: "x" } } })), null, "state without a page");
  assert.deepEqual(liveViewOf(thread("t4", "done", "needs", { live: { pageId: "p", state: [] as never } }))?.state, {}, "state that is not an object");
});

test("the inbox row uses the headline when there is one", () => {
  assert.equal(rowSummary(thread("t", "done", "needs", { summary: "last words", live })), "Tests pass");
  assert.equal(rowSummary(thread("t", "done", "needs", { summary: "last words", live: { ...live, headline: "" } })), "last words");
  assert.equal(rowSummary(thread("t", "done", "needs", { summary: "last words" })), "last words");
});

test("dimmed while a run goes that has not touched the view", () => {
  const run = (startedAt: string, checkedAt: string) => thread("t", "working", "working", { running: true, runStartedAt: startedAt, live: { ...live, checkedAt } });
  assert.equal(viewDimmed(run("2026-11-14T09:05:00Z", "2026-11-14T09:00:00Z")), true);
  assert.equal(viewDimmed(run("2026-11-14T09:05:00Z", "2026-11-14T09:06:00Z")), false, "touched this run");
  assert.equal(viewDimmed(thread("t", "done", "needs", { live })), false, "no run");
});

test("staleness stamp", () => {
  const at = Date.parse(live.updatedAt);
  assert.equal(staleLabel(live.updatedAt, at + 10_000), "updated just now");
  assert.equal(staleLabel(live.updatedAt, at + 3 * 60_000), "updated 3 min ago");
  assert.equal(staleLabel(live.updatedAt, at + 5 * 3600_000), "updated 5 h ago");
  assert.equal(staleLabel("", at), "");
});

test("height: what the page reports, capped at 60 % of the pane", () => {
  assert.equal(viewHeight(300, 1000), 300);
  assert.equal(viewHeight(900, 1000), 600);
  assert.equal(viewHeight(0, 1000), 120, "before the page reports");
  assert.equal(viewHeight(5, 1000), 40);
});

test("bridge: only the view's own frame, only well-formed MCP Apps messages", () => {
  const frame = {};
  const other = {};
  const press = (params: unknown, id: unknown = 7) => ({ jsonrpc: "2.0", id, method: "ui/message", params });
  const text = (s: string) => ({ role: "user", content: [{ type: "text", text: s }] });
  assert.deepEqual(bridgeMessage(frame, frame, press(text(" Ship it "))), { kind: "press", text: "Ship it", id: 7 });
  assert.equal(bridgeMessage(other, frame, press(text("Ship it"))), null, "another window");
  assert.equal(bridgeMessage(frame, null, press(text("Ship it"))), null, "no frame");
  assert.equal(bridgeMessage(frame, frame, { ...press(text("x")), jsonrpc: "1.0" }), null, "not JSON-RPC 2.0");
  assert.equal(bridgeMessage(frame, frame, press(text("   "))), null, "empty");
  assert.equal(bridgeMessage(frame, frame, press(text("x".repeat(PRESS_MAX + 1)))), null, "too long");
  assert.equal(bridgeMessage(frame, frame, press({ role: "assistant", content: [{ type: "text", text: "x" }] })), null, "not the user's");
  assert.equal(bridgeMessage(frame, frame, press({ content: [{ type: "image", data: "x" }] })), null, "no text");
  assert.deepEqual(bridgeMessage(frame, frame, press(text("x"), { evil: 1 })), { kind: "press", text: "x", id: null }, "odd id dropped");
  assert.deepEqual(bridgeMessage(frame, frame, { jsonrpc: "2.0", method: "ui/notifications/size-changed", params: { height: 312.4 } }), { kind: "size", height: 312.4 });
  assert.equal(bridgeMessage(frame, frame, { jsonrpc: "2.0", method: "ui/notifications/size-changed", params: { height: "tall" } }), null);
  assert.deepEqual(bridgeMessage(frame, frame, { jsonrpc: "2.0", method: "ui/notifications/initialized" }), { kind: "ready" });
  assert.equal(bridgeMessage(frame, frame, stateMessage({ a: 1 })), null, "the app's own message");
  assert.equal(bridgeMessage(frame, frame, "ui/message"), null);
});

test("menu offers the live view; a press renders as a message from the view", () => {
  const items = threadMenu(thread("t", "done", "needs")).filter((x) => "id" in x) as { id: string; label: string }[];
  assert.equal(items.find((x) => x.id === "liveView")?.label, "Turn on live view");
  const on = threadMenu(thread("t", "done", "needs", { liveView: true })).filter((x) => "id" in x) as { id: string; label: string }[];
  assert.equal(on.find((x) => x.id === "liveView")?.label, "Turn off live view");
  const items2 = buildConversation([
    { seq: 1, at: "2026-11-14T09:00:00Z", subject: "t", type: "user", data: { id: "m1", text: "Ship it", from: "view" } },
    { seq: 2, at: "2026-11-14T09:00:01Z", subject: "t", type: "message.state", data: { id: "m1", state: "read" } },
  ]);
  const u = items2.find((x) => x.kind === "user");
  assert.equal(u?.kind === "user" && u.from, "view");
});

test("the view tools never show as tool rows", () => {
  const evs = [
    { seq: 1, at: "2026-11-14T09:00:00Z", subject: "t", type: "msg", data: { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "tool_use", id: "u1", name: "mcp__fyr__view_state", input: { patch: {} } }] } } },
    { seq: 2, at: "2026-11-14T09:00:01Z", subject: "t", type: "msg", data: { type: "user", parent_tool_use_id: null, message: { content: [{ type: "tool_result", tool_use_id: "u1", content: "Updated the live view." }] } } },
  ];
  assert.equal(buildConversation(evs).filter((x) => x.kind === "tools").length, 0);
});

// SPEC 0.5 V0.37: a thread with a live view opens on its View tab; the Conversation tab is
// always one tap away, and an explicit choice wins.
test("the start tab follows the live view", () => {
  const withView = thread("v", "done", "needs", { live: { pageId: "p", state: {} } });
  const plain = thread("c", "done", "needs");
  assert.equal(defaultTab(withView, undefined), "view");
  assert.equal(defaultTab(withView, "conv"), "conv");
  assert.equal(defaultTab(withView, "changes"), "changes");
  assert.equal(defaultTab(plain, undefined), "conv");
  assert.equal(defaultTab(plain, "view"), "conv", "no view: the View tab does not exist");
});
