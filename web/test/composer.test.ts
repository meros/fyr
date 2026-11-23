// V0.composer-focus-reach (1): the composer of both fronts is one hook; what a key does and what the field says are decided here.
import { test } from "node:test";
import assert from "node:assert/strict";
import { composerKey, composerPlaceholder } from "../src/compose.ts";

const key = (k: string, mods: Partial<{ shiftKey: boolean; metaKey: boolean; ctrlKey: boolean; isComposing: boolean; repeat: boolean }> = {}) => ({ key: k, shiftKey: false, metaKey: false, ctrlKey: false, isComposing: false, repeat: false, ...mods });
const desk = { enterSends: true, answering: false };

test("V0.composer-focus-reach (1): on the desktop Enter sends; Shift+Enter, a held key and an IME's Enter are the field's", () => {
  assert.equal(composerKey(key("Enter"), desk), "send");
  assert.equal(composerKey(key("Enter", { shiftKey: true }), desk), "none");
  assert.equal(composerKey(key("Enter", { repeat: true }), desk), "none");
  assert.equal(composerKey(key("Enter", { isComposing: true }), desk), "none");
  assert.equal(composerKey(key("a"), desk), "none");
});

test("V0.composer-focus-reach (1): in the answer field Enter answers and ⌘/Ctrl+Enter queues the text as a message", () => {
  const ans = { enterSends: true, answering: true };
  assert.equal(composerKey(key("Enter"), ans), "answer");
  assert.equal(composerKey(key("Enter", { metaKey: true }), ans), "send");
  assert.equal(composerKey(key("Enter", { ctrlKey: true }), ans), "send");
});

test("V0.composer-focus-reach (1): on the phone Enter is a new line (the on-screen return key); Send is the button", () => {
  assert.equal(composerKey(key("Enter"), { enterSends: false, answering: false }), "none");
  assert.equal(composerKey(key("Enter", { metaKey: true }), { enterSends: false, answering: false }), "none");
});

test("V0.composer-focus-reach (1): one placeholder on both fronts: the question, why sending is off, steer or message", () => {
  assert.equal(composerPlaceholder({ running: false }, undefined, { index: 1 }), "Answer question 2 in your own words…");
  assert.equal(composerPlaceholder({ running: true }, "The host is offline", null), "The host is offline");
  assert.equal(composerPlaceholder({ running: true }, undefined, null), "Steer the run…");
  assert.equal(composerPlaceholder({ running: false }, undefined, null), "Message…");
});
