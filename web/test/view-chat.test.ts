// SPEC 0.5 V0.57: a dismissed View-tab chat stays hidden until a new message is typed there.
import { test } from "node:test";
import assert from "node:assert/strict";
import { chatHidden, chatKey } from "../src/view-chat.ts";

test("V0.57 the dismissed message stays hidden; a new one shows again", () => {
  const d = { t1: chatKey({ id: "m1", key: "k1" }) };
  assert.equal(chatHidden(d, "t1", chatKey({ id: "m1", key: "k1" })), true);
  assert.equal(chatHidden(d, "t1", chatKey({ id: "m2", key: "k2" })), false);
  assert.equal(chatHidden(d, "t2", "m1"), false);
  assert.equal(chatKey({ key: "k3" }), "k3");
});
