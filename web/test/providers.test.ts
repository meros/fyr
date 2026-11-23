// SPEC 0.5 V0.50: Settings → Providers helpers.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createArgs, EMPTY_FORM, effortsFor, importable, keyLine, loginActive, loginButton, loginLine, parseModelIds, startEffort, whereText } from "../src/providers.ts";
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
});

test("where a provider is", () => {
  assert.equal(whereText({ kind: "claude-account" }), "~/.claude");
  assert.equal(whereText({ kind: "claude-account", configDir: "~/.claude-acme" }), "~/.claude-acme");
  assert.equal(whereText({ kind: "agy-account" }), "~/.gemini");
  assert.equal(whereText({ kind: "agy-account", configDir: "~/.gemini-work" }), "~/.gemini-work");
  assert.equal(whereText({ kind: "endpoint", cli: "agy" }), "Gemini API");
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
  assert.deepEqual(createArgs({ ...EMPTY_FORM, kind: "agy-account", name: "Antigravity", configDir: "~/.gemini-work" }), { args: { kind: "agy-account", name: "Antigravity", configDir: "~/.gemini-work" } });
  assert.deepEqual(createArgs({ ...EMPTY_FORM, kind: "endpoint", cli: "agy", name: "Gemini API", key: " gk " }), { args: { kind: "endpoint", cli: "agy", name: "Gemini API", key: "gk" } });
  assert.deepEqual(createArgs({ ...EMPTY_FORM, kind: "endpoint", cli: "agy", name: "Gemini Proxy", baseURL: "not-a-url" }), { error: "The base URL starts with https://" });
  assert.deepEqual(createArgs({ ...EMPTY_FORM, kind: "endpoint", cli: "agy", name: "Gemini Proxy", baseURL: "https://proxy.example" }), { args: { kind: "endpoint", cli: "agy", name: "Gemini Proxy", baseURL: "https://proxy.example" } });
  const ep = { ...EMPTY_FORM, kind: "endpoint" as const, name: "Router" };
  assert.deepEqual(createArgs(ep), { error: "The base URL starts with https://" });
  assert.deepEqual(createArgs({ ...ep, baseURL: "https://openrouter.ai/api" }), { args: { kind: "endpoint", name: "Router", baseURL: "https://openrouter.ai/api" } }, "V0.120: model ids are optional; Fyr asks the endpoint");
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
  assert.equal(listLine({ kind: "agy-account", models: m(2) }), "Antigravity account · ~/.gemini · 2 models");
  assert.equal(listLine({ kind: "endpoint", cli: "agy", models: m(2) }), "Antigravity (API key) · Gemini API · 2 models");
  assert.equal(listLine({ kind: "endpoint", baseURL: "https://api.deepseek.com/anthropic", models: m(2) }), "Claude (custom endpoint) · api.deepseek.com · 2 models");
  assert.equal(listLine({ kind: "endpoint", baseURL: "https://x.io", models: [] }), "Claude (custom endpoint) · x.io · no models");
});

test("the usage sentence per quota kind", () => {
  assert.equal(usageLine({ quota: "anthropic-oauth", threads: 9, imported: "profile claudeAlt" }), "Read from the Claude login · imported from profile claudeAlt · used by 9 threads");
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
  assert.deepEqual(modelsAfter(ms, "a", { order: ["b", "c", "a"] }), { models: ["b", "c", "a"] });
  assert.deepEqual(modelsAfter(ms, "a", { order: ["b", "a"] }), { error: "The list changed. Try again." }, "an order only reorders");
  assert.deepEqual(modelsAfter(ms, "a", { star: "c" }), { models: ms, defaultModel: "c" });
  assert.deepEqual(modelsAfter(ms, "a", { star: "zz" }), { error: "zz is not in the list." });
});

// ---- V0.120: the model chooser ----
import { checkErrorLine, checkLine, modelParts, needsCheck, newCount, offLimit, projectModelOff, withCurrent } from "../src/providers.ts";
import type { ProviderDTO } from "../src/providers.ts";

const T = Date.parse("2026-11-16T12:00:00Z");
const dto = (o: Partial<ProviderDTO> = {}): ProviderDTO => ({
  id: "claude", name: "Claude", kind: "claude-account", cli: "claude", auth: "cli-login", efforts: [], quota: "anthropic-oauth", status: "ready", keySet: false, threads: 0,
  models: [{ id: "claude-opus-5-5", name: "Opus 5.5" }, { id: "claude-sonnet-5-5", name: "Sonnet 5.5" }, { id: "claude-opus-5-4", name: "Opus 5.4" }],
  defaultModel: "claude-opus-5-5",
  catalog: [
    { id: "claude-opus-5-5", name: "Opus 5.5", firstSeen: "2026-11-01T00:00:00Z" },
    { id: "claude-sonnet-5-5", name: "Sonnet 5.5", firstSeen: "2026-11-01T00:00:00Z" },
    { id: "claude-opus-5-4", name: "Opus 5.4", firstSeen: "2026-11-01T00:00:00Z", gone: true },
    { id: "claude-sonnet-5-4", name: "Sonnet 5.4", firstSeen: "2026-11-01T00:00:00Z" },
    { id: "claude-fable-5-2", name: "Fable 5.2", firstSeen: "2026-11-15T00:00:00Z", new: true },
    { id: "my-tune", firstSeen: "2026-11-15T00:00:00Z", manual: true },
  ],
  checkedAt: "2026-11-16T11:48:00Z",
  ...o,
});

test("V0.120 (4): the two parts from a DTO: On in the owner's order with ★ and marks, Off by name", () => {
  const m = modelParts(dto());
  assert.deepEqual(m.on.map((x) => x.id), ["claude-opus-5-5", "claude-sonnet-5-5", "claude-opus-5-4"]);
  assert.ok(m.on[0].star && !m.on[1].star);
  assert.ok(m.on[2].gone);
  assert.deepEqual(m.off.map((x) => x.id), ["claude-fable-5-2", "my-tune", "claude-sonnet-5-4"], "by name: Fable, my-tune, Sonnet");
  assert.ok(m.off[0].isNew && m.off[1].manual);
  assert.equal(m.total, 6);
  // A provider never checked: the stored list is on, nothing is off.
  const old = modelParts(dto({ catalog: [], checkedAt: undefined }));
  assert.equal(old.on.length, 3);
  assert.equal(old.off.length, 0);
});

test("V0.120 (4): search filters both parts by name and id", () => {
  const m = modelParts(dto(), "SONNET");
  assert.deepEqual(m.on.map((x) => x.id), ["claude-sonnet-5-5"]);
  assert.deepEqual(m.off.map((x) => x.id), ["claude-sonnet-5-4"]);
  assert.equal(m.matched, 2);
  assert.equal(modelParts(dto(), "tune").matched, 1);
  assert.equal(offLimit(false), 8);
  assert.equal(offLimit(true), 3);
});

test("V0.120 (1): the header line per state", () => {
  assert.equal(checkLine(dto(), T), "Checked 12 min ago");
  assert.equal(checkLine(dto({ checkedAt: "2026-11-16T11:59:40Z" }), T), "Checked just now");
  assert.equal(checkLine(dto({ checking: true }), T), "Checking…");
  assert.equal(checkLine(dto({ status: "needs-login" }), T), "Not checked: needs a login");
  assert.equal(checkLine(dto({ status: "needs-key" }), T), "Not checked: needs a key");
  assert.equal(checkLine(dto({ checkedAt: undefined }), T), "Not checked yet");
  const y = new Date(T - 24 * 3600_000);
  y.setHours(14, 2, 0, 0);
  assert.equal(checkLine(dto({ checkedAt: y.toISOString(), checkError: "x" }), T), "Last checked yesterday 14:02");
  assert.equal(checkErrorLine(dto({ checkedAt: y.toISOString(), checkError: "api.deepseek.com/v1/models answered 401" }), T), "Could not list the models: api.deepseek.com/v1/models answered 401. The list below is the one from yesterday; it still works.");
  assert.equal(checkErrorLine(dto({ checkedAt: undefined, checkError: "no answer in 15s" }), T), "Could not list the models: no answer in 15s.");
  assert.equal(checkErrorLine(dto(), T), null);
  // The page checks on open when the last check is over 10 minutes old; never for one that needs a login.
  assert.equal(needsCheck(dto(), T), true);
  assert.equal(needsCheck(dto({ checkedAt: "2026-11-16T11:55:00Z" }), T), false);
  assert.equal(needsCheck(dto({ status: "needs-key" }), T), false);
  assert.equal(needsCheck(dto({ checking: true }), T), false);
});

test("V0.120 (2): turning models on and off; the default stays on; a hand id can be forgotten when off", () => {
  const ms = ["a", "b"];
  assert.deepEqual(modelsAfter(ms, "a", { on: "c" }), { models: ["a", "b", "c"] }, "on goes to the end of On");
  assert.deepEqual(modelsAfter(ms, "a", { off: "b" }), { models: ["a"] });
  assert.deepEqual(modelsAfter(ms, "a", { off: "a" }), { error: "Star another model first." });
  assert.deepEqual(modelsAfter(ms, "a", { forget: "x" }), { forget: ["x"] });
  assert.deepEqual(modelsAfter(ms, "a", { forget: "b" }), { error: "Turn it off first." });
});

test("V0.120 (4): the Providers row adds the New count", () => {
  assert.equal(newCount(dto()), 1);
  assert.equal(listLine(dto()), "Claude account · ~/.claude · 3 of 6 models on · 1 new", "found live 16 Nov: the row shows what the provider reports");
  assert.equal(listLine({ kind: "endpoint", baseURL: "https://openrouter.ai/api", models: [{ id: "m0" }], catalog: Array.from({ length: 380 }, (_, i) => ({ id: `m${i}`, firstSeen: "" })) }), "Claude (custom endpoint) · openrouter.ai · 1 of 380 models on");
});

test("V0.120 (5): a model in use that is off shows as (off) in its picker, and nowhere else", () => {
  const opts = [{ value: "", label: "Default" }, { value: "claude-opus-5-5", label: "opus 5.5" }];
  assert.deepEqual(withCurrent(opts, "claude-opus-5-4"), [...opts, { value: "claude-opus-5-4", label: "opus 5.4 (off)" }]);
  assert.equal(withCurrent(opts, "claude-opus-5-5"), opts);
  assert.equal(withCurrent(opts, ""), opts);
  const claude = { id: "claude", name: "Claude", color: "", models: ["claude-opus-5-5"] } as Profile;
  assert.equal(projectModelOff({ name: "x", profile: "claude", model: "claude-opus-5-4" }, [claude]), "opus 5.4 is turned off in Claude. New threads here start on Claude's default until you pick another.");
  assert.equal(projectModelOff({ name: "x", profile: "claude", model: "claude-opus-5-5" }, [claude]), null);
  assert.equal(projectModelOff({ name: "x", profile: "gone", model: "m" }, [claude]), null);
});

// Found live 16 Nov: a check that only has Fyr's known list says so in the foot (not as an error); a failed check's
// reason wins; otherwise the group's own sentence.
import { modelsFoot } from "../src/providers.ts";
test("V0.120: the Models foot: a failed check, the known-list note, else what the group does", () => {
  const known = "Claude answered with its short names only (its own list was not ready), so this is Fyr's known list. Refresh in a minute.";
  assert.deepEqual(modelsFoot(dto({ checkNote: known }), T), { text: known, error: false });
  assert.deepEqual(modelsFoot(dto({ checkNote: known, checkError: "claude ended without listing its models" }), T), { text: "Could not list the models: claude ended without listing its models. The list below is the one from today; it still works.", error: true });
  assert.deepEqual(modelsFoot(dto(), T), { text: "★ is the default for a new thread. Fyr asks the provider for its models when you open this page and once a day.", error: false });
});
