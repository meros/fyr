// V0.119: the Settings nav: four pages, one line of state each, the badge, the page ids.
import { test } from "node:test";
import assert from "node:assert/strict";
import { PAGES, escapeTarget, automationSummary, deviceSummary, navBadge, pageUrl, parsePage, projectsSummary, providersNeed, providersSummary } from "../src/settings-nav.ts";

const ready = { status: "ready" as const };
const login = { status: "needs-login" as const };
const key = { status: "needs-key" as const };

test("the four pages, in order", () => {
  assert.deepEqual(
    PAGES.map((p) => [p.id, p.title]),
    [
      ["providers", "Providers"],
      ["automation", "Automation"],
      ["projects", "Projects"],
      ["device", "This device"],
    ],
  );
});

test("Providers: the count, and what needs the owner", () => {
  assert.equal(providersSummary(null, 0), "", "empty until the list is read");
  assert.equal(providersSummary([], 0), "No providers");
  assert.equal(providersSummary([ready], 0), "1 provider");
  assert.equal(providersSummary([ready, ready, ready], 0), "3 providers");
  assert.equal(providersSummary([ready, login, ready], 0), "3 · 1 needs a login");
  assert.equal(providersSummary([ready, key], 0), "2 · 1 needs a key");
  assert.equal(providersSummary([ready, login, key], 0), "3 · 2 need you");
  assert.equal(providersSummary([ready, ready], 1), "2 · 1 needs you", "a system job that cannot run");
  assert.equal(providersSummary([login, ready], 1), "2 · 2 need you");
  assert.equal(providersNeed([ready, login], 0), "1 needs a login");
  assert.equal(providersNeed([ready], 0), "");
  assert.equal(navBadge([ready, login, key], 1), 3);
  assert.equal(navBadge([ready], 0), 0);
  assert.equal(navBadge(null, 0), 0);
});

test("Automation: deploys and the repos merged without asking", () => {
  assert.equal(automationSummary(null, null), "");
  assert.equal(automationSummary(true, []), "Deploys on · merges off");
  assert.equal(automationSummary(false, [{ repo: "a/b", on: false }]), "Deploys off · merges off");
  assert.equal(automationSummary(true, [{ repo: "a/b", on: true }]), "Deploys on · merges in 1 repo");
  assert.equal(automationSummary(true, ["a/b", "c/d", "e/f"].map((repo) => ({ repo, on: true }))), "Deploys on · merges in 3 repos");
});

test("Projects: projects and hosts", () => {
  assert.equal(projectsSummary(10, 1), "10 projects · 1 host");
  assert.equal(projectsSummary(1, 3), "1 project · 3 hosts");
  assert.equal(projectsSummary(0, 0), "No projects · 1 host", "no hosts listed is this host");
});

test("This device: notifications and theme", () => {
  assert.equal(deviceSummary("off", "system"), "Notifications off · theme auto");
  assert.equal(deviceSummary("on", "light"), "Notifications on · theme light");
  assert.equal(deviceSummary("denied", "dark"), "Notifications blocked · theme dark");
  assert.equal(deviceSummary("unsupported", "system"), "No notifications here · theme auto");
});

test("a page id to and from its URL form", () => {
  assert.deepEqual(parsePage("providers/abc"), { page: "providers", sub: "abc" });
  assert.deepEqual(parsePage("providers/new"), { page: "providers", sub: "new" });
  assert.deepEqual(parsePage("device"), { page: "device" });
  assert.deepEqual(parsePage("projects/p_fyr"), { page: "projects", sub: "p_fyr" });
  assert.deepEqual(parsePage("nonsense/x"), { page: "providers" }, "an unknown page falls back to providers");
  assert.deepEqual(parsePage(""), { page: "providers" });
  assert.deepEqual(parsePage(null), { page: "providers" });
  assert.deepEqual(parsePage("automation/x"), { page: "automation" }, "a page without sub-pages drops the sub");
  for (const s of ["providers", "providers/abc", "projects/p_x", "device", "automation"]) assert.equal(pageUrl(parsePage(s)), s);
});

// V0.119 (amends the V0.58 Project dialog): Esc on a sub-page goes to its list; on a page it closes Settings.
test("Esc goes one level up, then closes", () => {
  assert.deepEqual(escapeTarget({ page: "projects", sub: "p_fyr" }), { page: "projects" });
  assert.deepEqual(escapeTarget({ page: "providers", sub: "new" }), { page: "providers" });
  assert.equal(escapeTarget({ page: "device" }), null);
});
