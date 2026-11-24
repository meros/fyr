import { test } from "node:test";
import assert from "node:assert/strict";
import { setupBadge, setupCardLabel, setupFoldLine, setupNeedsCard, setupSummary, setupSummaryHosts, stepPill, type SetupReport } from "../src/setup.ts";

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
    { id: "peers", title: "Other hosts", status: "ok", optional: true, reason: "ok" },
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
      { id: "peers", title: "Other hosts", status: "missing", optional: true, reason: "opt" },
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

// V0.settings-ia: the nav's Setup line was the first online host's, beside a page with a tab per host.
test("V0.settings-ia: the Setup nav line counts every host that was read", () => {
  const ready = make();
  const half = make({ done: 4, ready: false, steps: [...make().steps.slice(0, 2), { id: "github", title: "GitHub", status: "missing", reason: "no gh" }, ...make().steps.slice(3)] });
  assert.equal(setupSummaryHosts([]), "");
  assert.equal(setupSummaryHosts([null, null]), "", "until one is read");
  assert.equal(setupSummaryHosts([ready]), "Ready");
  assert.equal(setupSummaryHosts([half]), "4 of 6", "one host: its own steps");
  assert.equal(setupSummaryHosts([ready, ready, null]), "Ready", "an unread host is left out");
  assert.equal(setupSummaryHosts([ready, half, ready, ready]), "Ready on 3 of 4 hosts");
  assert.equal(setupFoldLine(null), "");
  assert.equal(setupFoldLine(ready), "6 of 6 ready");
  assert.equal(setupFoldLine(half), "4 of 6 ready · 1 missing");
});

// V0.settings-ia: the line said "Ready" with one host offline and one too old to be read.
test("V0.settings-ia: the Setup nav line counts the hosts that cannot be asked, and says so", () => {
  const ready = make();
  const half = make({ done: 4, ready: false });
  assert.equal(setupSummaryHosts([ready, ready], 2), "Ready on 2 of 4 hosts · 2 not read");
  assert.equal(setupSummaryHosts([ready], 1), "Ready on 1 of 2 hosts · 1 not read", "one read host is not the whole answer");
  assert.equal(setupSummaryHosts([half, ready], 1), "Ready on 1 of 3 hosts · 1 not read");
  assert.equal(setupSummaryHosts([null], 2), "", "until one is read");
  assert.equal(setupSummaryHosts([ready, ready], 0), "Ready");
});
