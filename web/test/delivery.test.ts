// SPEC 0.5 V0.33: message delivery in the conversation fold.
import { test } from "node:test";
import assert from "node:assert/strict";
import { buildConversation, deliveryLabel, type Item } from "../src/conversation.ts";

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
