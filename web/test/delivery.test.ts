// SPEC 0.5 V0.33: message delivery in the conversation fold.
import { test } from "node:test";
import assert from "node:assert/strict";
import { buildConversation, deliveryLabel, viewChat, type Item } from "../src/conversation.ts";

let seq = 0;
const ev = (type: string, data: unknown) => ({ seq: ++seq, at: new Date(1_700_000_000_000 + seq * 1000).toISOString(), subject: "t", type, data });
const say = (text: string) => ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "text", text }] } });
const state = (id: string, s: string) => ev("message.state", { id, state: s });
type User = Extract<Item, { kind: "user" }>;
const users = (items: Item[]) => items.filter((i): i is User => i.kind === "user");

test("V0.33: a read message stands where the agent read it; unread ones wait at the end", () => {
  const items = buildConversation([
    ev("user", { id: "m1", text: "fix it" }),
    state("m1", "read"),
    say("Working on it"),
    ev("user", { id: "m2", text: "also the tests" }),
    state("m2", "queued"),
    say("Ran the build"),
    state("m2", "read"),
    say("Now the tests"),
    ev("user", { id: "m3", text: "and the docs" }),
    state("m3", "queued"),
    ev("user", { id: "m4", text: "one more" }),
    say("Still going"),
  ]);
  const order = items.map((i) => (i.kind === "user" ? `u:${i.text}` : i.kind === "assistant" ? `a:${i.text}` : i.kind));
  assert.deepEqual(order, ["u:fix it", "a:Working on it", "a:Ran the build", "u:also the tests", "a:Now the tests", "a:Still going", "u:and the docs", "u:one more"]);
  const u = users(items);
  assert.deepEqual(
    u.map((x) => [x.delivery, x.pending]),
    [
      ["read", false],
      ["read", false],
      ["queued", true],
      ["sent", true],
    ],
  );
  // Only the last read message says Read.
  assert.deepEqual(u.map((x) => deliveryLabel(x)), ["", "Read", "Waiting", "Sent"]);
});

test("V0.33: cancelled and lost messages stay in place, not pending, with their words", () => {
  const items = buildConversation([
    ev("user", { id: "m1", text: "go" }),
    state("m1", "read"),
    say("Going"),
    ev("user", { id: "m2", text: "never mind" }),
    state("m2", "queued"),
    state("m2", "cancelled"),
    say("Done going"),
    ev("user", { id: "m3", text: "after restart" }),
    state("m3", "lost"),
  ]);
  const u = users(items);
  assert.deepEqual(u.map((x) => [x.text, x.delivery, x.pending]), [
    ["go", "read", false],
    ["never mind", "cancelled", false],
    ["after restart", "lost", false],
  ]);
  assert.deepEqual(u.map((x) => deliveryLabel(x)), ["Read", "Cancelled", "Not delivered"]);
  assert.equal(items[items.length - 1].kind, "user");
});

test("V0.33: messages from before delivery tracking have no state and stay where they were sent", () => {
  const items = buildConversation([ev("user", { text: "old" }), say("reply")]);
  const u = users(items);
  assert.equal(u[0].delivery, "");
  assert.equal(u[0].pending, false);
  assert.equal(deliveryLabel(u[0]), "");
  assert.equal(items[0].kind, "user");
});

// SPEC 0.5 V0.33: the thread's outbox is the truth about what still waits. A message the host no
// longer holds never shows as Waiting, even when its read event never reached this device
// (found live: two answered messages stayed "Waiting" on the phone and could not be cancelled).
test("a message not in the outbox is not waiting, even without its read event", () => {
  const T = Date.parse("2026-11-14T17:39:00Z");
  let seq = 0;
  const ev = (type: string, data: unknown) => ({ seq: ++seq, at: new Date(T + seq * 1000).toISOString(), subject: "t", type, data });
  const evs = [ev("user", { id: "m1", text: "Where do we present quota?" }), ev("message.state", { id: "m1", state: "queued" })];
  const stale = buildConversation(evs);
  assert.equal(stale.find((i) => i.kind === "user" && i.id === "m1" && i.pending) !== undefined, true, "setup: without the outbox it looks waiting");
  const fixed = buildConversation(evs, { outbox: [] });
  const it = fixed.find((i) => i.kind === "user" && i.id === "m1") as Extract<Item, { kind: "user" }>;
  assert.equal(it.pending, false);
  assert.equal(it.delivery, "read");
  const still = buildConversation(evs, { outbox: ["m1"] });
  assert.equal((still.find((i) => i.kind === "user" && i.id === "m1") as Extract<Item, { kind: "user" }>).pending, true, "in the outbox: still waiting");
});

// SPEC 0.5 V0.49: the View tab shows the last message typed there and the agent's spoken reply.
test("the View tab's chat: my last message there and the reply after it", () => {
  const T = Date.parse("2026-11-14T18:00:00Z");
  let seq = 0;
  const ev = (type: string, data: unknown) => ({ seq: ++seq, at: new Date(T + seq * 1000).toISOString(), subject: "t", type, data });
  const say = (text: string) => ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "text", text }] } });
  const evs = [
    ev("user", { id: "a", text: "first, in conversation" }), ev("message.state", { id: "a", state: "read" }), say("ok"),
    ev("user", { id: "b", text: "Can you also check iPad?", from: "view-typed" }), ev("message.state", { id: "b", state: "read" }),
    say("Yes, I'll run the iPad layout next and add it to the view."), say("Done: the iPad layout passes."),
  ];
  const c = viewChat(buildConversation(evs, { outbox: [] }));
  assert.equal(c?.mine.text, "Can you also check iPad?");
  assert.equal(c?.reply, "Yes, I'll run the iPad layout next and add it to the view.", "the first spoken answer after it");
  const waiting = viewChat(buildConversation([...evs.slice(0, 3), ev("user", { id: "c", text: "and Android", from: "view-typed" }), ev("message.state", { id: "c", state: "queued" })], { outbox: ["c"] }));
  assert.equal(waiting?.mine.text, "and Android");
  assert.equal(waiting?.mine.delivery, "queued");
  assert.equal(waiting?.reply, null);
  assert.equal(viewChat(buildConversation(evs.slice(0, 3))), null, "nothing typed on the View tab yet");
});

// SPEC 0.5 V0.33 (found live twice): a message whose read event never reached this device is read
// (the host's outbox no longer holds it) and stands where it was sent, not at the end of the log.
test("V0.33 a read message without its read event keeps its place", () => {
  const say = (text: string) => ev("msg", { type: "assistant", parent_tool_use_id: null, message: { content: [{ type: "text", text }] } });
  const evs = [say("before"), ev("user", { id: "m", text: "Make these more alike" }), ev("message.state", { id: "m", state: "queued" }), say("after 1"), say("after 2")];
  const items = buildConversation(evs, { outbox: [] });
  const texts = items.map((i: any) => i.text).filter(Boolean);
  assert.deepEqual(texts, ["before", "Make these more alike", "after 1", "after 2"]);
  const m = items.find((i: any) => i.kind === "user") as any;
  assert.equal(m.delivery, "read");
  assert.equal(m.pending, false);
});
