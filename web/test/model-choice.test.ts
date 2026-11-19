// V0.model-choice (#18, #23): one readable choice per provider and model, the nearest model and effort on Hand over,
// a project's default effort, and the note when a project's default provider cannot run.
import { test } from "node:test";
import assert from "node:assert/strict";
import { choiceLabel, handoverPick, mappingLine, modelChoices, modelName, nearestEffort, nearestModel, parseChoice } from "../src/model-choice.ts";
import { chooseProject, initialForm, settleForm, type FormCtx } from "../src/new-thread.ts";
import { projectDefaults } from "../src/projects.ts";
import type { Profile, Project } from "../src/types.ts";

const E = ["low", "medium", "high", "xhigh", "max"];
const claude: Profile = {
  id: "claude",
  name: "Claude",
  color: "",
  models: ["claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"],
  defaultModel: "claude-opus-5-5",
  defaultEffort: "high",
  modelCaps: [
    { id: "claude-opus-5-5", efforts: E },
    { id: "claude-sonnet-5-5", efforts: ["low", "medium", "high"] },
    { id: "claude-haiku-5-5" },
  ],
};
const acme: Profile = { id: "acme", name: "Claude (acme)", color: "", models: ["claude-sonnet-5-5"], modelCaps: [{ id: "claude-sonnet-5-5", efforts: ["low", "medium", "high"] }] };
const glm: Profile = { id: "glm", name: "GLM", color: "", models: ["glm-4.6"], modelCaps: [{ id: "glm-4.6", name: "GLM 4.6" }] };
const bare: Profile = { id: "bare", name: "Bare", color: "", models: [] };
const dead: Profile = { id: "dead", name: "Work", color: "", models: ["claude-opus-5-5"], unavailable: "Needs a login." };

test("a model reads as its provider's name for it, else the id made readable", () => {
  assert.equal(modelName(claude, "claude-opus-5-5"), "Opus 5.5");
  assert.equal(modelName(glm, "glm-4.6"), "GLM 4.6");
  assert.equal(modelName(glm, "kimi-k2"), "kimi-k2");
  assert.equal(choiceLabel(claude, "claude-opus-5-5"), "Claude · Opus 5.5");
  assert.equal(choiceLabel(bare, ""), "Bare · its default model");
});

test("the choices: every provider's models, a bare provider once, one that cannot run shown but not pickable", () => {
  const cs = modelChoices([claude, bare, dead]);
  assert.deepEqual(
    cs.map((c) => [c.label, !!c.why]),
    [
      ["Claude · Opus 5.5", false],
      ["Claude · Sonnet 5.5", false],
      ["Claude · Haiku 5.5", false],
      ["Bare · its default model", false],
      ["Work · needs setup", true],
    ],
  );
  assert.deepEqual(parseChoice(cs[1].value), { profile: "claude", model: "claude-sonnet-5-5" });
  // a picked model the owner turned off stays, marked
  const off = modelChoices([acme], { profile: "acme", model: "claude-opus-5-5" });
  assert.equal(off.at(-1)?.label, "Claude (acme) · Opus 5.5 (off)");
});

test("Hand over takes the same model, else the same family, else the target's default", () => {
  assert.equal(nearestModel("claude-sonnet-5-5", acme), "claude-sonnet-5-5");
  assert.equal(nearestModel("claude-sonnet-4-6", acme), "claude-sonnet-5-5");
  assert.equal(nearestModel("claude-opus-5-5", glm), "glm-4.6");
  assert.equal(nearestModel("claude-opus-5-5", bare), "");
});

test("the effort is the same when the model takes it, else the nearest, else the default", () => {
  assert.equal(nearestEffort("high", ["low", "medium", "high"]), "high");
  assert.equal(nearestEffort("max", ["low", "medium", "high"]), "high");
  assert.equal(nearestEffort("xhigh", ["low", "max"]), "max");
  assert.equal(nearestEffort("", ["low", "high"], "high"), "high");
  assert.equal(nearestEffort("high", []), "");
});

test("Hand over names the mapping", () => {
  const t = { model: "claude-opus-5-5", effort: "max" };
  const pick = handoverPick(t, claude, acme);
  assert.deepEqual(pick, { model: "claude-sonnet-5-5", effort: "high" });
  assert.equal(mappingLine(t, claude, acme, pick), "Opus 5.5 · max → Sonnet 5.5 · high: the nearest Claude (acme) has.");
  assert.equal(mappingLine(t, claude, acme, { model: "claude-sonnet-5-5", effort: "low" }), "Opus 5.5 · max → Sonnet 5.5 · low.");
  assert.equal(mappingLine({ model: "claude-sonnet-5-5", effort: "high" }, claude, acme, { model: "claude-sonnet-5-5", effort: "high" }), "Keeps Sonnet 5.5 · high.");
  // no effort on the thread: the source's default effort is what it ran with
  assert.deepEqual(handoverPick({ model: "claude-opus-5-5" }, claude, acme), { model: "claude-sonnet-5-5", effort: "high" });
});

const project = (o: Partial<Project>): Project => ({ id: "p1", name: "Web", color: "blue", folder: "/src/web", ...o }) as Project;

test("a project's default effort applies when its model takes it", () => {
  assert.equal(projectDefaults(project({ profile: "claude", model: "claude-opus-5-5", effort: "max" }), [claude]).effort, "max");
  assert.equal(projectDefaults(project({ profile: "claude", model: "claude-haiku-5-5", effort: "max" }), [claude]).effort, undefined);
  const ctx: FormCtx = { projects: [project({ profile: "claude", model: "claude-opus-5-5", effort: "low" })], profiles: [claude, acme] };
  const f = settleForm(initialForm(), ctx, "p1", {});
  assert.equal(f.effortPick, "low");
  assert.equal(f.note, "");
});

test("a project default that cannot run says so, and which provider the thread starts on", () => {
  const ctx: FormCtx = { projects: [project({ profile: "dead" })], profiles: [dead, acme] };
  const f = settleForm(initialForm(), ctx, "p1", {});
  assert.equal(f.profile, "acme");
  assert.equal(f.note, "Web's default, Work, cannot run (Needs a login); it starts on Claude (acme).");
  // No project: nothing to say
  assert.equal(chooseProject(f, null, ctx).note, "");
});
