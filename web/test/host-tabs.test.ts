// V0.host-tabs: unit tests for the shared tabbed host switcher helpers, Settings URL parsing with host on Automation,
// linked project member ordering, per-host quota filtering, and Automation badge counts.
import { test } from "node:test";
import assert from "node:assert/strict";
import { activeTabId, automationBadge, orderedMembers, quotasForHost, stepTabIndex } from "../src/host-tabs.ts";
import { pageUrl, parsePage } from "../src/settings-nav.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { HostLoad } from "../src/host-load.ts";
import type { Quota } from "../src/quota.ts";
import type { Project } from "../src/types.ts";
import type { UpdatesView } from "../src/updates.ts";
import { hid } from "./helpers.ts";

test("activeTabId keeps the picked host while present and falls back to the first in order", () => {
  const items = [{ id: "lab" }, { id: "desk" }, { id: "host-b" }];
  assert.equal(activeTabId(items, "desk"), "desk");
  assert.equal(activeTabId(items, "missing"), "lab");
  assert.equal(activeTabId(items, null), "lab");
  assert.equal(activeTabId([], "desk"), "");
});

test("stepTabIndex navigates horizontally with wrap-around and Home/End", () => {
  assert.equal(stepTabIndex(3, 0, "ArrowRight"), 1);
  assert.equal(stepTabIndex(3, 2, "ArrowRight"), 0);
  assert.equal(stepTabIndex(3, 0, "ArrowLeft"), 2);
  assert.equal(stepTabIndex(3, 2, "ArrowLeft"), 1);
  assert.equal(stepTabIndex(3, 1, "Home"), 0);
  assert.equal(stepTabIndex(3, 1, "End"), 2);
  assert.equal(stepTabIndex(3, 1, "Enter"), null);
  assert.equal(stepTabIndex(1, 0, "ArrowRight"), null);
});

test("automationBadge counts actionable items per host", () => {
  assert.equal(automationBadge(null, undefined), 0);
  const badUpd = { current: "a", updates: [], bad: { commit: "1234567", why: "failed" } } as unknown as UpdatesView;
  assert.equal(automationBadge(badUpd, undefined), 1);
  const lowGh = { current: "a", updates: [], github: { rate: { limit: 5000, remaining: 100, reset: "2026-11-21T12:00:00Z", graphqlRemaining: 5000, at: "2026-11-21T11:00:00Z" }, since: "2026-11-21T11:00:00Z", uses: null, calls: 0 } } as unknown as UpdatesView;
  assert.equal(automationBadge(lowGh, undefined), 1);
  const busyLoad = { host: "lab", cores: 4, load: [1, 1, 1], threads: [], leftovers: [{ id: "1" }, { id: "2" }], slots: { undeclared: [{ thread: "t1" }] } } as unknown as HostLoad;
  assert.equal(automationBadge(badUpd, busyLoad), 4);
});

test("orderedMembers keeps a linked project's host tabs in the owner's host order even when a host is offline", () => {
  const hosts = [
    { id: hid("A"), name: "desk", online: false },
    { id: hid("B"), name: "lab", online: true },
    { id: hid("C"), name: "host-b", online: true },
  ] as HostInfo[];
  const members = [
    { id: "B/p1", hostId: hid("B"), host: "lab", name: "acme" },
    { id: "C/p2", hostId: hid("C"), host: "host-b", name: "acme" },
    { id: "A/p0", hostId: hid("A"), host: "desk", name: "acme" },
  ] as Project[];
  assert.deepEqual(
    orderedMembers(members, hosts).map((m) => m.host),
    ["desk", "lab", "host-b"],
  );
});

test("quotasForHost filters to one host when named and keeps unstamped quotas from older fronts", () => {
  const list: Quota[] = [
    { profile: "claude", host: "desk", kind: "anthropic-oauth" },
    { profile: "claude", host: "lab", kind: "anthropic-oauth" },
    { profile: "zai", host: "lab", kind: "endpoint" },
    { profile: "legacy", kind: "endpoint" },
  ];
  assert.deepEqual(quotasForHost(list, undefined).length, 4);
  assert.deepEqual(
    quotasForHost(list, "lab").map((q) => q.profile),
    ["claude", "zai", "legacy"],
  );
  assert.deepEqual(
    quotasForHost(list, "desk").map((q) => q.profile),
    ["claude", "legacy"],
  );
});

test("parsePage and pageUrl round-trip Automation deep-links that name a host", () => {
  const r = { page: "automation" as const, host: "lab", sub: "leftovers" };
  assert.equal(pageUrl(r), "automation/lab/leftovers");
  assert.deepEqual(parsePage("automation/lab/leftovers"), r);
  assert.deepEqual(parsePage("automation/leftovers"), { page: "automation", sub: "leftovers" });
});
