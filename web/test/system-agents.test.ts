import { test } from "node:test";
import assert from "node:assert/strict";
import { jobLine, modelOptions, providerOptions, type SystemAgentsView } from "../src/system-agents.ts";

const view: SystemAgentsView = {
  default: {},
  providers: [
    { id: "claude", name: "Claude", models: ["claude-opus-5-5", "claude-haiku-5-5"], haiku: "claude-haiku-5-5" },
    { id: "zai", name: "Z.ai", models: ["glm-4.6"] },
    { id: "off", name: "Off", models: [], unavailable: "no login yet" },
  ],
  jobs: [
    { id: "title", name: "Thread titles", choice: {}, profile: "claude", model: "claude-haiku-5-5" },
    { id: "setup", name: "Project setup", override: true, choice: { profile: "zai" }, error: "Z.ai has no Haiku model: pick a model in Settings → System agents" },
  ],
};

// SPEC V0.115 (2): the provider list starts with the host's default provider, and says which providers cannot run.
test("the provider choices name the default and the providers that cannot run", () => {
  assert.deepEqual(providerOptions(view), [
    { value: "", label: "Default (Claude)" },
    { value: "claude", label: "Claude" },
    { value: "zai", label: "Z.ai" },
    { value: "off", label: "Off (cannot run: no login yet)" },
  ]);
});

// SPEC V0.115 (2), (4): no model is the provider's Haiku; a provider without one says so, so the default never
// silently becomes a bigger model.
test("the model choices start with the provider's Haiku", () => {
  assert.deepEqual(modelOptions(view, ""), [
    { value: "", label: "Haiku (claude-haiku-5-5)" },
    { value: "claude-opus-5-5", label: "claude-opus-5-5" },
    { value: "claude-haiku-5-5", label: "claude-haiku-5-5" },
  ]);
  assert.deepEqual(modelOptions(view, "zai")[0], { value: "", label: "Haiku (none on Z.ai)" });
});

// SPEC V0.115 (2): each job says what it runs on now, or why it cannot run.
test("a job line says what the job runs on, or why not", () => {
  assert.deepEqual(jobLine(view, view.jobs[0]), { text: "Runs on Claude · claude-haiku-5-5", error: false });
  assert.deepEqual(jobLine(view, view.jobs[1]), { text: "Z.ai has no Haiku model: pick a model in Settings → System agents", error: true });
});
