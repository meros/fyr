// SPEC V0.126: from any front the owner configures every host it follows, as if connected to it directly. The
// per-host pages (Providers with System agents, Automation) show one group per host, in the owner's host order.
import { test } from "node:test";
import assert from "node:assert/strict";
import { automationSummaryHosts, groupLine, readOnlyNote, settingsHosts, settingsPath, withHost } from "../src/host-settings.ts";
import { pageUrl, parsePage, escapeTarget } from "../src/settings-nav.ts";
import { dayTime } from "../src/format.ts";
import { quotaFor } from "../src/quota.ts";

const now = Date.parse("2026-11-16T20:00:00Z");
const iso = (minAgo: number) => new Date(now - minAgo * 60_000).toISOString();

test("V0.126 (4): one install, or a front that lists one host, is one ungrouped part that names no host", () => {
  assert.deepEqual(settingsHosts(undefined, "desk"), [{ name: "", label: "desk", self: true, online: true }]);
  assert.deepEqual(settingsHosts([{ name: "desk", online: true }], "desk"), [{ name: "", label: "desk", self: true, online: true }]);
});

test("V0.127, V0.132: several hosts are one group each, in the owner's host order (the front's)", () => {
  const hs = settingsHosts(
    [
      { name: "lab", online: true, since: iso(40) },
      { name: "desk", online: true, since: iso(180) },
      { name: "meros-server", online: false, since: iso(25) },
    ],
    "desk",
  );
  assert.deepEqual(hs.map((h) => [h.name, h.self, h.online]), [
    ["lab", false, true],
    ["desk", true, true],
    ["meros-server", false, false],
  ]);
  assert.equal(hs[1].label, "desk");
});

test("V0.126 (4): a group's line: this install, online, or offline since when", () => {
  const [self, lab, off] = settingsHosts(
    [
      { name: "desk", online: true, since: iso(180) },
      { name: "lab", online: true, since: iso(40) },
      { name: "meros-server", online: false, since: iso(25) },
    ],
    "desk",
  );
  assert.equal(groupLine(self, now), "This install");
  assert.equal(groupLine(lab, now), `Online since ${dayTime(iso(40), now)}`);
  assert.equal(groupLine(off, now), `Offline since ${dayTime(iso(25), now)}`);
});

test("V0.126 (3): an offline host's settings are what Fyr last read, said with the time, and cannot be changed", () => {
  const [, off] = settingsHosts([{ name: "desk", online: true }, { name: "meros-server", online: false, since: iso(25) }], "desk");
  assert.equal(
    readOnlyNote(off, iso(26), now),
    `meros-server is offline. These are its settings as Fyr last read them at ${dayTime(iso(26), now)}; nothing here can be changed until it is back.`,
  );
  assert.equal(readOnlyNote(off, null, now), "meros-server is offline, and Fyr has not read its settings yet. They show when it is back.");
  const [self] = settingsHosts(undefined, "desk");
  assert.equal(readOnlyNote(self, null, now), "");
});

test("V0.126 (2): every per-host read and command names its host; the one ungrouped part names none", () => {
  assert.equal(settingsPath("/api/providers", "lab"), "/api/providers?host=lab");
  assert.equal(settingsPath("/api/providers/found", "meros server"), "/api/providers/found?host=meros%20server");
  assert.equal(settingsPath("/api/autoDeploy", ""), "/api/autoDeploy");
  assert.deepEqual(withHost({ id: "claude", name: "C" }, "lab"), { id: "claude", name: "C", host: "lab" });
  assert.deepEqual(withHost({ id: "claude" }, ""), { id: "claude" });
});

test("V0.126 (4): a provider's page names its host in the URL, so two hosts' 'claude' are two pages", () => {
  const r = { page: "providers" as const, host: "lab", sub: "claude" };
  assert.equal(pageUrl(r), "providers/lab/claude");
  assert.deepEqual(parsePage("providers/lab/claude"), r);
  assert.deepEqual(parsePage("providers/lab/new"), { page: "providers", host: "lab", sub: "new" });
  assert.deepEqual(parsePage(pageUrl({ page: "providers", host: "meros server", sub: "z/ai" })), { page: "providers", host: "meros server", sub: "z/ai" });
  // One host: as before.
  assert.equal(pageUrl({ page: "providers", sub: "claude" }), "providers/claude");
  assert.deepEqual(parsePage("providers/claude"), { page: "providers", sub: "claude" });
  // A host's page is unchanged.
  assert.deepEqual(parsePage("hosts/lab"), { page: "hosts", sub: "lab" });
  // Esc from a host's provider goes to the Providers page.
  assert.deepEqual(escapeTarget(r), { page: "providers" });
});

test("V0.126 (4): the Automation line over several hosts", () => {
  const fyr = { repo: "meros/fyr", on: true };
  const web = { repo: "norban/web", on: false };
  assert.equal(automationSummaryHosts([{ deploy: true, repos: [fyr] }]), "Deploys on · merges in 1 repo");
  assert.equal(automationSummaryHosts([{ deploy: true, repos: [fyr] }, { deploy: true, repos: [web] }]), "Deploys on · merges in 1 repo");
  assert.equal(automationSummaryHosts([{ deploy: true, repos: [fyr] }, { deploy: false, repos: [{ ...fyr }] }]), "Deploys on 1 of 2 hosts · merges in 2 repos");
  assert.equal(automationSummaryHosts([{ deploy: false, repos: [] }, { deploy: false, repos: [web] }]), "Deploys off · merges off");
  // A host not read yet leaves the line empty; an offline host never read is left out.
  assert.equal(automationSummaryHosts([{ deploy: true, repos: [fyr] }, { deploy: null, repos: null }]), "");
  assert.equal(automationSummaryHosts([{ deploy: true, repos: [fyr] }, { deploy: null, repos: null, skip: true }]), "Deploys on · merges in 1 repo");
});

test("V0.126 (4): a provider's quota is its host's; without a host the first host's, as before", () => {
  const qs = [
    { profile: "claude", kind: "anthropic-oauth", host: "desk", error: "desk's" },
    { profile: "claude", kind: "anthropic-oauth", host: "lab", error: "lab's" },
  ];
  assert.equal(quotaFor(qs, "claude", "lab")?.error, "lab's");
  assert.equal(quotaFor(qs, "claude")?.error, "desk's");
  assert.equal(quotaFor(qs, "claude", "old-nuc"), undefined);
  // An older front stamps no host.
  assert.equal(quotaFor([{ profile: "claude", kind: "x" }], "claude", "lab")?.kind, "x");
});
