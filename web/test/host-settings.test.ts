// SPEC V0.126: from any front the owner configures every host it follows, as if connected to it directly. The
// per-host pages (Providers with System agents, Automation) show one group per host, in the owner's host order.
import { test } from "node:test";
import assert from "node:assert/strict";
import { automationSummaryHosts, groupLine, readOnlyNote, settingsHosts } from "../src/host-settings.ts";
import { pageUrl, parsePage, escapeTarget } from "../src/settings-nav.ts";
import { dayTime } from "../src/format.ts";
import { quotaFor } from "../src/quota.ts";
import { hid } from "./helpers.ts";

const now = Date.parse("2026-11-16T20:00:00Z");
const iso = (minAgo: number) => new Date(now - minAgo * 60_000).toISOString();

test("V0.126 (4): one install, or a front that lists one host, is one ungrouped part that names no host", () => {
  assert.deepEqual(settingsHosts(undefined), [{ name: "", label: "", online: true }], "slice 2b: no host, no id to send to");
  assert.deepEqual(settingsHosts([{ name: "desk", online: true }]), [{ name: "", label: "desk", online: true }]);
  assert.deepEqual(settingsHosts([{ name: "desk", online: true, id: hid("AAAA") }]), [{ id: "AAAA", name: "", label: "desk", online: true }]);
});

test("V0.127, V0.132: several hosts are one group each, in the owner's host order (the front's)", () => {
  const hs = settingsHosts(
    [
      { name: "lab", online: true, since: iso(40) },
      { name: "desk", online: true, since: iso(180) },
      { name: "host-b", online: false, since: iso(25) },
    ],
  );
  assert.deepEqual(hs.map((h) => [h.name, h.online]), [
    ["lab", true],
    ["desk", true],
    ["host-b", false],
  ]);
  assert.equal(hs[1].label, "desk");
});

test("V0.126 (4): a group's line: online, or offline since when; decision 0031 2.6: no group is \"This install\"", () => {
  const [self, lab, off] = settingsHosts(
    [
      { id: hid("AAAA"), name: "desk", online: true, since: iso(180) },
      { id: hid("BBBB"), name: "lab", online: true, since: iso(40) },
      { id: hid("CCCC"), name: "host-b", online: false, since: iso(25) },
    ],
  );
  assert.equal(groupLine(self, now), `Online since ${dayTime(iso(180), now)}`);
  assert.equal(groupLine(lab, now), `Online since ${dayTime(iso(40), now)}`);
  assert.equal(groupLine(off, now), `Offline since ${dayTime(iso(25), now)}`);
});

test("V0.126 (3): an offline host's settings are what Fyr last read, said with the time, and cannot be changed", () => {
  const [, off] = settingsHosts([{ id: hid("AAAA"), name: "desk", online: true }, { id: hid("CCCC"), name: "host-b", online: false, since: iso(25) }]);
  assert.equal(
    readOnlyNote(off, iso(26), now),
    `host-b is offline. These are its settings as Fyr last read them at ${dayTime(iso(26), now)}; nothing here can be changed until it is back.`,
  );
  assert.equal(readOnlyNote(off, null, now), "host-b is offline, and Fyr has not read its settings yet. They show when it is back.");
  const [self] = settingsHosts(undefined);
  assert.equal(readOnlyNote(self, null, now), "");
});

test("decision 0031: a part carries its host's install id, which every call of it names", () => {
  const parts = settingsHosts([{ id: hid("AAAA"), name: "desk", online: true }, { id: hid("BBBB"), name: "lab", online: true }]);
  assert.deepEqual(parts.map((p) => [p.name, p.id]), [["desk", "AAAA"], ["lab", "BBBB"]]);
  assert.deepEqual(settingsHosts([{ id: hid("AAAA"), name: "desk", online: true }]).map((p) => [p.name, p.id]), [["", "AAAA"]], "one host: one part with no name, with its id");
});

test("V0.126 (4): a provider's page names its host in the URL, so two hosts' 'claude' are two pages", () => {
  const r = { page: "providers" as const, host: "lab", sub: "claude" };
  assert.equal(pageUrl(r), "providers/lab/claude");
  assert.deepEqual(parsePage("providers/lab/claude"), r);
  assert.deepEqual(parsePage("providers/lab/new"), { page: "providers", host: "lab", sub: "new" });
  assert.deepEqual(parsePage(pageUrl({ page: "providers", host: "host b", sub: "z/ai" })), { page: "providers", host: "host b", sub: "z/ai" });
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
  const web = { repo: "acme/web", on: false };
  assert.equal(automationSummaryHosts([{ deploy: true, repos: [fyr] }]), "Auto-update on · auto-merge in 1 repo");
  assert.equal(automationSummaryHosts([{ deploy: true, repos: [fyr] }, { deploy: true, repos: [web] }]), "Auto-update on · auto-merge in 1 repo");
  assert.equal(automationSummaryHosts([{ deploy: true, repos: [fyr] }, { deploy: false, repos: [{ ...fyr }] }]), "Auto-update on 1 of 2 hosts · auto-merge in 2 repos");
  assert.equal(automationSummaryHosts([{ deploy: false, repos: [] }, { deploy: false, repos: [web] }]), "Auto-update off · auto-merge off");
  // A host not read yet leaves the line empty; an offline host never read is left out.
  assert.equal(automationSummaryHosts([{ deploy: true, repos: [fyr] }, { deploy: null, repos: null }]), "");
  assert.equal(automationSummaryHosts([{ deploy: true, repos: [fyr] }, { deploy: null, repos: null, skip: true }]), "Auto-update on · auto-merge in 1 repo");
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

test("V0.host-ids: a host with no install id is listed, needs an update, and says why nothing of it shows", () => {
  const [, nuc] = settingsHosts([
    { id: hid("F7QK-2M9X-AB34-CD56"), name: "desk", online: true, since: iso(180) },
    { name: "old-nuc", online: true, since: iso(300) },
  ]);
  assert.equal(nuc.noId, true);
  assert.equal(groupLine(nuc, now), "Needs an update");
  assert.match(readOnlyNote(nuc, null, now), /^old-nuc runs a Fyr too old to have an id, so/);
  assert.match(readOnlyNote(nuc, null, now), /Update Fyr on old-nuc itself \(run its install script there again, or update its package\)/, "V0.settings-ia: the note says how, not only that it is needed");
  assert.equal(settingsHosts([{ name: "desk", online: true }])[0].noId, undefined, "one host is one part, as before");
});
