// SPEC 0.5 V0.50: Settings → Providers helpers.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createArgs, EMPTY_FORM, effortsFor, importable, keyLine, loginActive, loginButton, loginLine, modelsSummary, parseModelIds, startEffort, whereText } from "../src/providers.ts";
import type { LoginDTO } from "../src/providers.ts";
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
  assert.equal(whereText({ kind: "claude-account", configDir: "~/.claude-norban" }), "~/.claude-norban");
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

// SPEC 0.5 V0.50: a token account shows that it logs in with a token, not a folder.
test("V0.50 a token account's where text names the token", () => {
  assert.equal(whereText({ kind: "claude-account", configDir: "~/.claude", tokenLogin: true }), "token login");
});

// SPEC 0.5 V0.53: login through Fyr.
test("the login button and line", () => {
  const l = (state: LoginDTO["state"], kind: LoginDTO["kind"] = "token", message?: string): LoginDTO => ({ kind, state, expires: "", message });
  assert.equal(loginButton({ canLogin: false, status: "needs-key" }), null);
  assert.equal(loginButton({ canLogin: true, status: "needs-login", tokenLogin: true }), "Log in");
  assert.equal(loginButton({ canLogin: true, status: "ready", tokenLogin: true }), "Get a new token");
  assert.equal(loginButton({ canLogin: true, status: "ready" }), "Log in again");
  assert.equal(loginButton({ canLogin: true, status: "needs-login", login: l("waiting") }), null, "one login per provider");
  assert.equal(loginButton({ canLogin: true, status: "needs-login", login: l("failed") }), "Log in");
  assert.ok(loginActive(l("checking")) && !loginActive(l("done")) && !loginActive(undefined));
  assert.match(loginLine(l("waiting")), /paste the code/);
  assert.equal(loginLine(l("done")), "Logged in. Fyr keeps the token on this host.");
  assert.equal(loginLine(l("done", "folder")), "Logged in. Claude keeps the login in this account's folder.");
  assert.equal(loginLine(l("timeout", "token", "The login timed out after 10 minutes. Start it again.")), "The login timed out after 10 minutes. Start it again.");
});

// V0.119: the provider row's line, the usage sentence, and the Models rows' edits.
import { listLine, modelsAfter, removeLine, usageLine } from "../src/providers.ts";

test("the list row's line: kind, where, model count", () => {
  const m = (n: number) => Array.from({ length: n }, (_, i) => ({ id: `m${i}` }));
  assert.equal(listLine({ kind: "claude-account", models: m(3) }), "Claude account · ~/.claude · 3 models");
  assert.equal(listLine({ kind: "claude-account", tokenLogin: true, models: m(1) }), "Claude account · token login · 1 model");
  assert.equal(listLine({ kind: "endpoint", baseURL: "https://api.deepseek.com/anthropic", models: m(2) }), "Endpoint · api.deepseek.com · 2 models");
  assert.equal(listLine({ kind: "endpoint", baseURL: "https://x.io", models: [] }), "Endpoint · x.io · no models");
});

test("the usage sentence per quota kind", () => {
  assert.equal(usageLine({ quota: "anthropic-oauth", threads: 9, imported: "profile claudeAgent" }), "Read from the Claude login · imported from profile claudeAgent · used by 9 threads");
  assert.equal(usageLine({ quota: "anthropic-oauth", threads: 1 }), "Read from the Claude login · used by 1 thread");
  assert.equal(usageLine({ quota: "deepseek", threads: 0 }), "Read from the DeepSeek balance");
  assert.equal(usageLine({ quota: "openrouter", threads: 0 }), "Read from the OpenRouter credits");
  assert.equal(usageLine({ quota: "none", threads: 0 }), "This provider reports no usage.");
  assert.equal(usageLine({ quota: "", threads: 2 }), "This provider reports no usage. · used by 2 threads");
  assert.equal(usageLine({ quota: "zai", threads: 0 }), "Read from the provider (zai)");
});

test("the remove row says who uses the provider", () => {
  assert.equal(removeLine(9), "9 threads use it. Hand them over first.");
  assert.equal(removeLine(1), "1 thread uses it. Hand it over first.");
  assert.equal(removeLine(0), "No thread uses it.");
});

test("modelsAfter: add, remove, move, star", () => {
  const ms = ["a", "b", "c"];
  assert.deepEqual(modelsAfter(ms, "a", { add: "  d " }), { models: ["a", "b", "c", "d"] });
  assert.deepEqual(modelsAfter(ms, "a", { add: "b" }), { error: "b is already in the list." });
  assert.deepEqual(modelsAfter(ms, "a", { add: "  " }), { error: "Write a model id." });
  assert.deepEqual(modelsAfter(ms, "a", { remove: "b" }), { models: ["a", "c"] });
  assert.deepEqual(modelsAfter(ms, "a", { remove: "a" }), { error: "Star another model first." }, "the default is not removed");
  assert.deepEqual(modelsAfter(ms, "a", { move: [2, 0] }), { models: ["c", "a", "b"] });
  assert.deepEqual(modelsAfter(ms, "a", { star: "c" }), { models: ms, defaultModel: "c" });
  assert.deepEqual(modelsAfter(ms, "a", { star: "zz" }), { error: "zz is not in the list." });
});
