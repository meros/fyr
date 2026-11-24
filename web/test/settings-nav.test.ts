// V0.119, V0.125, V0.settings-ia: the Settings nav: eight pages in two groups, one line of state each, the one count,
// the page ids.
import { test } from "node:test";
import assert from "node:assert/strict";
import { HOST_SECTIONS, NAV_GROUPS, PAGES, SETTINGS_HOLDS, escapeTarget, deviceSummary, hostJumps, hostNeeds, hostsSummary, needLine, needWords, pageUrl, parsePage, projectsSummary, providersNeed, providersSummary, settingsCounts, type HostNeeds } from "../src/settings-nav.ts";

const ready = { status: "ready" as const };
const login = { status: "needs-login" as const };
const key = { status: "needs-key" as const };

// The captions sort the pages by where a setting is kept, so each is true of every page under it: Sign-in is the
// devices signed in to a host, so it is not under the caption of this device (it was under "Yours").
test("V0.settings-ia: the pages in their two groups: Only on this device (This device), On your hosts (Sign-in … Setup last)", () => {
  assert.deepEqual(
    NAV_GROUPS.map((g) => [g.caption, g.pages.map((p) => p.title)]),
    [
      ["Only on this device", ["This device"]],
      ["On your hosts", ["Sign-in", "Providers", "Projects", "Automation", "Hosts", "Usage", "Setup"]],
    ],
  );
  assert.deepEqual(
    PAGES.map((p) => p.id),
    ["device", "devices", "providers", "projects", "automation", "hosts", "usage", "setup"],
    "the id of Sign-in stays devices, so its links keep working",
  );
  assert.equal(SETTINGS_HOLDS, "This device, sign-in, providers, projects, automation, hosts, usage, setup");
});

test("Providers: the count, and what needs the owner", () => {
  assert.equal(providersSummary(null, 0), "", "empty until the list is read");
  assert.equal(providersSummary([], 0), "No providers");
  assert.equal(providersSummary([ready], 0), "1 provider");
  assert.equal(providersSummary([ready, ready, ready], 0), "3 providers");
  assert.equal(providersSummary(null, 0, true), "Could not read", "V0.settings-ia: a failed read is said, not left blank");
  assert.equal(providersSummary([ready, login, ready], 0), "3 providers · 1 needs a login", "V0.settings-ia: the number has its noun");
  assert.equal(providersSummary([ready, key], 0), "2 providers · 1 needs a key");
  assert.equal(providersSummary([ready, login, key], 0), "3 providers · 2 need you");
  assert.equal(providersSummary([ready, ready], 1), "2 providers · 1 needs you", "one of Fyr's own calls that cannot run");
  assert.equal(providersSummary([login, ready], 1), "2 providers · 2 need you");
  assert.equal(providersNeed([ready, login], 0), "1 needs a login");
  assert.equal(providersNeed([ready], 0), "");
});

test("Projects: the projects only (V0.125: the hosts have their own page)", () => {
  assert.equal(projectsSummary(10), "10 projects");
  assert.equal(projectsSummary(1), "1 project");
  assert.equal(projectsSummary(0), "No projects");
});

test("V0.125: Hosts: the count, what is offline, what needs the owner", () => {
  const on = { name: "a", online: true };
  const off = { name: "b", online: false };
  const changed = { name: "c", online: true, id: "B7LA-0000-0000-0000", idWas: "AX2C-0000-0000-0000" };
  assert.equal(hostsSummary([], 0), "One host");
  assert.equal(hostsSummary(undefined, 0), "One host");
  assert.equal(hostsSummary([on], 0), "One host");
  assert.equal(hostsSummary([on, on, on], 0), "3 hosts");
  assert.equal(hostsSummary([on, off, on], 0), "3 hosts · 1 offline");
  assert.equal(hostsSummary([on, off, off], 0), "3 hosts · 2 offline");
  assert.equal(hostsSummary([on, on, changed], 1), "3 hosts · 1 needs you");
  assert.equal(hostsSummary([on, off, changed], 1), "3 hosts · 1 needs you", "needs you wins over offline");
  assert.equal(hostsSummary([changed, off, changed], 2), "3 hosts · 2 need you");
  assert.equal(hostsSummary([on, off, on], 2), "3 hosts · 2 need you", "V0.settings-ia: the line says the Hosts badge (a release held back counts)");
});

// V0.settings-ia: one count. The gear, the More row, the nav and the host tabs disagreed: the More row left out the
// devices and setup, the Hosts badge left out a failed update its own tabs counted.
const host = (key: string, p: Partial<HostNeeds> = {}): HostNeeds => ({ key, online: true, providers: [], blocked: 0, idChanged: false, update: false, setup: 0, ...p });

test("V0.settings-ia: the one count: the nav's badges are the sums of the host tabs', and the total is their sum", () => {
  const c = settingsCounts(
    [
      host("desk", { providers: [ready, login], blocked: 1 }),
      host("host-b", { providers: [key], update: true, setup: 2 }),
      host("lab", { providers: null, idChanged: true, update: true }),
    ],
    1,
  );
  assert.deepEqual(c.tabs.providers, { desk: 2, "host-b": 1, lab: 0 }, "a provider that is not ready, and one of Fyr's own calls that cannot run; an unread host counts nothing");
  assert.deepEqual(c.tabs.hosts, { desk: 0, "host-b": 1, lab: 2 }, "a changed id and a release held back");
  assert.deepEqual(c.tabs.setup, { desk: 0, "host-b": 2, lab: 0 });
  assert.deepEqual(c.pages, { providers: 3, hosts: 3, setup: 2, devices: 1 });
  assert.equal(c.total, 9);
  assert.equal(needLine(c), "Sign-in 1 · Providers 3 · Hosts 3 · Setup 2", "the pages by their nav titles, in the nav's order");
});

test("V0.settings-ia: nothing waits: no count, no line; one host keys its part \"\"", () => {
  const c = settingsCounts([host("")], 0);
  assert.equal(c.total, 0);
  assert.equal(needLine(c), "");
  assert.deepEqual(c.tabs.hosts, { "": 0 });
  assert.equal(needWords(0), "");
  assert.equal(needWords(1), "1 needs you");
  assert.equal(needWords(4), "4 need you");
  assert.equal(settingsCounts([], -1).total, 0);
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
  assert.deepEqual(parsePage("hosts"), { page: "hosts" });
  assert.deepEqual(parsePage("hosts/lab"), { page: "hosts", sub: "lab" });
  assert.deepEqual(parsePage("nonsense/x"), { page: "providers" }, "an unknown page falls back to providers");
  assert.deepEqual(parsePage(""), { page: "providers" });
  assert.deepEqual(parsePage(null), { page: "providers" });
  assert.deepEqual(parsePage("projects/new"), { page: "projects", sub: "new" }, "V0.settings-ia: New project is a sub-page");
  assert.deepEqual(parsePage("hosts/lab/leftovers"), { page: "hosts", sub: "lab", focus: "leftovers" }, "V0.settings-ia: a section of a host's page");
  assert.deepEqual(parsePage("hosts/my%20host/diagnostics"), { page: "hosts", sub: "my host", focus: "diagnostics" });
  assert.equal(pageUrl({ page: "hosts", sub: "my host", focus: "updates" }), "hosts/my%20host/updates");
  assert.equal(pageUrl({ page: "hosts", focus: "updates" }), "hosts", "a focus without its host is not a link");
  assert.deepEqual(parsePage("device/x"), { page: "device" }, "a page without sub-pages drops the sub");
  for (const s of ["providers", "providers/abc", "providers/lab/abc", "projects/p_x", "projects/new", "device", "automation", "hosts", "hosts/lab", "hosts/lab/leftovers"]) assert.equal(pageUrl(parsePage(s)), s);
});

test("V0.settings-ia: a link into Automation from before the host's page held its own still lands", () => {
  assert.deepEqual(parsePage("automation/lab/leftovers"), { page: "hosts", sub: "lab", focus: "leftovers" }, "the rail's count, with several hosts");
  assert.deepEqual(parsePage("automation/leftovers"), { page: "hosts", focus: "leftovers" }, "one host: its name was not in the link; the shell opens the first host");
  assert.deepEqual(parsePage("automation"), { page: "automation" }, "the page itself is still there");
  assert.equal(pageUrl(parsePage("automation/lab/leftovers")), "hosts/lab/leftovers");
});

// V0.119 (amends the V0.58 Project dialog): Esc on a sub-page goes to its list; on a page it closes Settings.
test("Esc goes one level up, then closes", () => {
  assert.deepEqual(escapeTarget({ page: "projects", sub: "p_fyr" }), { page: "projects" });
  assert.deepEqual(escapeTarget({ page: "providers", sub: "new" }), { page: "providers" });
  assert.deepEqual(escapeTarget({ page: "hosts", sub: "lab" }), { page: "hosts" });
  assert.deepEqual(escapeTarget({ page: "projects", sub: "new" }), { page: "projects" }, "V0.settings-ia: Esc in New project goes back to the list, not out of Settings");
  assert.deepEqual(escapeTarget({ page: "hosts", sub: "lab", focus: "leftovers" }), { page: "hosts" }, "a focus is a place on the host's page, not a level");
  assert.equal(escapeTarget({ page: "automation" }), null);
  assert.equal(escapeTarget({ page: "hosts" }), null);
  assert.equal(escapeTarget({ page: "device" }), null);
});

// V0.settings-ia: the rule of the count. A badge on an offline host could not be cleared by any press, and the gear
// counted it; a chip sat on a host that was not counted.
test("V0.settings-ia: only what the owner can act on now counts: nothing a host holds while it is offline, but its changed id", () => {
  const c = settingsCounts([host("desk", { providers: [login], update: true, setup: 1, blocked: 1 }), host("host-b", { online: false, providers: [login, key], blocked: 2, update: true, setup: 3, idChanged: true })], 0);
  assert.deepEqual(c.tabs.providers, { desk: 2, "host-b": 0 }, "no login can be done on a host that is not reachable");
  assert.deepEqual(c.tabs.hosts, { desk: 1, "host-b": 1 }, "host-b's held-back release does not count; its changed id does (Trust goes to the front)");
  assert.deepEqual(c.tabs.setup, { desk: 1, "host-b": 0 });
  assert.equal(c.total, 5);
  const off = settingsCounts([host("desk", { providers: [login], update: true, setup: 1, blocked: 1, idChanged: true })], 2, true);
  assert.deepEqual([off.total, off.tabs.providers, off.tabs.hosts, off.tabs.setup, off.pages.devices], [0, { desk: 0 }, { desk: 0 }, { desk: 0 }, 0], "with no connection no press reaches anything: nothing counts");
});

test("V0.settings-ia: a host's needs from what was read of it; the row and the settings read are one reading", () => {
  const bad = { commit: "9b3e1d2c4a5f", why: "was not healthy" };
  const none = { providers: null, blocked: 0, setup: null };
  assert.deepEqual(hostNeeds({ name: "desk", online: true }, { update: { behind: 1, bad } }, { ...none, providers: [login], blocked: 1 }), { key: "desk", online: true, providers: [login], blocked: 1, idChanged: false, update: true, setup: 0 });
  assert.equal(hostNeeds({ name: "", online: true }, undefined, { ...none, view: { bad } }).update, true, "a front with one host has no row: the settings read says it");
  assert.equal(hostNeeds({ name: "desk", online: true }, { update: { behind: 0 } }, { ...none, view: { bad } }).update, false, "the host's own summary wins");
  assert.equal(hostNeeds({ name: "lab", online: true }, { idWas: "AX2C-0000-0000-0000" as never }, none).idChanged, true);
  assert.equal(hostNeeds({ name: "old", online: true, noId: true }, {}, none).online, false, "a host too old to be read cannot be pressed on: it counts as offline");
  const half = { os: "", host: "", done: 4, total: 6, ready: false, steps: [{ id: "gh", title: "GitHub", status: "missing" as const, reason: "" }, { id: "p", title: "Phone", status: "missing" as const, optional: true, reason: "" }] };
  assert.equal(hostNeeds({ name: "desk", online: true }, {}, { ...none, setup: half }).setup, 1, "required steps that are not ready");
});

test("V0.settings-ia: one host that needs the owner says so in the nav's line", () => {
  assert.equal(hostsSummary([{ online: true }], 1), "One host · 1 needs you");
  assert.equal(hostsSummary([{ online: true }], 0), "One host");
});

test("V0.settings-ia: the sections of a host's page and its jump links", () => {
  assert.deepEqual(HOST_SECTIONS.map((s) => s.title), ["Updates", "Load", "Providers and setup", "Diagnostics", "Look", "About"]);
  assert.deepEqual(hostJumps({ updates: true, load: false, more: true, diagnostics: true, look: true, about: true }, { updates: "held back", diagnostics: "" }), [
    { id: "updates", title: "Updates", word: "held back" },
    { id: "more", title: "Providers and setup" },
    { id: "diagnostics", title: "Diagnostics" },
    { id: "look", title: "Look" },
    { id: "about", title: "About" },
  ], "a section that is not on the page has no link; a word only when it is not fine");
  const j = hostJumps({ updates: true, load: true, more: true }, { updates: "held back", load: "memory short", more: "1 on Providers" }, { updates: true, more: false, look: true });
  assert.deepEqual(j, [
    { id: "updates", title: "Updates", word: "held back", counted: true },
    { id: "load", title: "Load", word: "memory short" },
    { id: "more", title: "Providers and setup", word: "1 on Providers" },
  ], "only a word this host's tab counts has the count's colour; state is grey, and a section without a word is never counted");
});

test("V0.settings-ia: a link to a host's tab of Setup or Automation; the setup checks are on the Setup page only", () => {
  assert.deepEqual(parsePage("setup/lab"), { page: "setup", host: "lab" });
  assert.deepEqual(parsePage("automation/lab"), { page: "automation", host: "lab" });
  assert.deepEqual(parsePage("hosts/lab/setup"), { page: "setup", host: "lab" }, "the section that held the checks is the Setup page's tab");
  assert.deepEqual(parsePage("automation/updates"), { page: "hosts", focus: "updates" }, "a section's name is an old link, not a host");
  assert.equal(pageUrl({ page: "setup", host: "my host" }), "setup/my%20host");
  assert.equal(pageUrl({ page: "automation", host: "lab" }), "automation/lab");
  assert.equal(pageUrl({ page: "providers", host: "lab" }), "providers", "providers/<x> is a provider's page; its tab is not in the link");
  for (const s of ["setup", "setup/lab", "automation/lab", "hosts/lab/about", "hosts/lab/more"]) assert.equal(pageUrl(parsePage(s)), s);
  assert.equal(escapeTarget({ page: "setup", host: "lab" }), null, "a tab is not a level");
});
