import { test } from "node:test";
import assert from "node:assert/strict";
import { setupBadge, setupCardLabel, setupNeedsCard, setupSummary, stepPill, type SetupReport } from "../src/setup.ts";

const make = (over: Partial<SetupReport> = {}): SetupReport => ({
  os: "darwin",
  host: "mac",
  done: 6,
  total: 6,
  ready: true,
  steps: [
    { id: "machine", title: "This Mac", status: "ok", reason: "ok" },
    { id: "claude", title: "Claude", status: "ok", reason: "ok" },
    { id: "github", title: "GitHub", status: "ok", reason: "ok" },
    { id: "project", title: "First project", status: "ok", reason: "ok" },
    { id: "phone", title: "Phone", status: "ok", optional: true, reason: "ok" },
    { id: "peers", title: "Other installs", status: "ok", optional: true, reason: "ok" },
  ],
  ...over,
});

test("setupSummary and setupNeedsCard follow real host readiness", () => {
  assert.equal(setupSummary(null), "");
  assert.equal(setupNeedsCard(null), false);

  const ready = make();
  assert.equal(setupSummary(ready), "Ready");
  assert.equal(setupNeedsCard(ready), false);
  assert.equal(setupBadge(ready), 0);

  const partial = make({
    done: 3,
    ready: false,
    steps: [
      { id: "machine", title: "This Mac", status: "ok", reason: "ok" },
      { id: "claude", title: "Claude", status: "fixable", reason: "sign in" },
      { id: "github", title: "GitHub", status: "ok", reason: "ok" },
      { id: "project", title: "First project", status: "fixable", reason: "pick" },
      { id: "phone", title: "Phone", status: "ok", optional: true, reason: "ok" },
      { id: "peers", title: "Other installs", status: "missing", optional: true, reason: "opt" },
    ],
  });
  assert.equal(setupSummary(partial), "3 of 6");
  assert.equal(setupNeedsCard(partial), true);
  assert.equal(setupCardLabel(partial), "Set up · 3 of 6");
  assert.equal(setupBadge(partial), 2, "only required incomplete steps count in the badge");
});

test("stepPill names each status cleanly", () => {
  assert.deepEqual(stepPill({ id: "a", title: "A", status: "ok", reason: "" }), { text: "Ready", cls: "ok" });
  assert.deepEqual(stepPill({ id: "a", title: "A", status: "fixable", reason: "" }), { text: "Fixable", cls: "fix" });
  assert.deepEqual(stepPill({ id: "a", title: "A", status: "missing", reason: "" }), { text: "Missing", cls: "miss" });
  assert.deepEqual(stepPill({ id: "a", title: "A", status: "missing", optional: true, reason: "" }), { text: "Optional", cls: "opt" });
});
