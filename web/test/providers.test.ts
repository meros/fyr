// SPEC 0.5 V0.47: Settings → Providers helpers.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createArgs, EMPTY_FORM, effortsFor, importable, keyLine, modelsSummary, parseModelIds, startEffort, whereText } from "../src/providers.ts";
import type { Profile } from "../src/types.ts";

test("a key shows only as set, its source and last 4", () => {
  assert.equal(keyLine({ kind: "endpoint", keySet: false }), "Key not set");
  assert.equal(keyLine({ kind: "endpoint", keySet: true, keyLast4: "4f2a" }), "Key set · ••••4f2a");
  assert.equal(keyLine({ kind: "endpoint", keySet: true, keyLast4: "4f2a", keyFrom: "OPENROUTER_API_KEY" }), "Key from OPENROUTER_API_KEY · ••••4f2a");
  assert.equal(keyLine({ kind: "claude-account", keySet: false }), "");
  assert.equal(keyLine({ kind: "claude-account", keySet: true }), "Login token set");
});

test("model ids from a text field", () => {
  assert.deepEqual(parseModelIds(" a/b \n\n c, a/b ,d"), ["a/b", "c", "d"]);
  assert.equal(modelsSummary([{ id: "x", name: "Opus 5.5" }, { id: "y" }, { id: "z" }, { id: "w" }]), "Opus 5.5, y + 2");
  assert.equal(modelsSummary([]), "No models yet");
});

test("where a provider is", () => {
  assert.equal(whereText({ kind: "claude-account" }), "~/.claude");
  assert.equal(whereText({ kind: "claude-account", configDir: "~/.claude-acme" }), "~/.claude-acme");
  assert.equal(whereText({ kind: "endpoint", baseURL: "https://openrouter.ai/api" }), "openrouter.ai");
});

test("effort shows only when the model takes it", () => {
  const p: Profile = { id: "a", name: "A", color: "", models: ["opus", "ds"], defaultEffort: "high",
    modelCaps: [{ id: "opus", efforts: ["low", "high", "max"] }, { id: "ds" }] };
  assert.deepEqual(effortsFor(p, "ds"), []);
  assert.deepEqual(effortsFor(p, "opus"), ["low", "high", "max"]);
  assert.equal(startEffort(p, "opus"), "high");
  assert.equal(startEffort(p, "opus", "max"), "max");
  assert.equal(startEffort(p, "ds", "max"), "");
  assert.deepEqual(effortsFor(undefined, "x"), []);
});

test("the Add provider form", () => {
  assert.deepEqual(createArgs({ ...EMPTY_FORM }), { error: "Give it a name" });
  assert.deepEqual(createArgs({ ...EMPTY_FORM, name: "Work", configDir: "~/.claude-work" }), { args: { kind: "claude-account", name: "Work", configDir: "~/.claude-work" } });
  const ep = { ...EMPTY_FORM, kind: "endpoint" as const, name: "Router" };
  assert.deepEqual(createArgs(ep), { error: "The base URL starts with https://" });
  assert.deepEqual(createArgs({ ...ep, baseURL: "https://openrouter.ai/api" }), { error: "Add at least one model id" });
  assert.deepEqual(createArgs({ ...ep, baseURL: "https://openrouter.ai/api", models: "z-ai/glm", key: " sk " }), {
    args: { kind: "endpoint", name: "Router", baseURL: "https://openrouter.ai/api", models: ["z-ai/glm"], key: "sk" },
  });
});

test("only finds not imported get an Import button", () => {
  const f = (id: string, imported: boolean) => ({ id, name: id, kind: "endpoint" as const, source: "", models: [], status: "ready" as const, imported });
  assert.deepEqual(importable([f("a", true), f("b", false)]).map((x) => x.id), ["b"]);
});
