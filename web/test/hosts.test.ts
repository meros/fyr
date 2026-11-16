// SPEC 0.5 V0.51: Settings → Hosts lists the front's hosts; an offline host's threads say so and are read-only.
import { test } from "node:test";
import assert from "node:assert/strict";
import { decodeHosts, hostLabel, hostForProject, hostLine, hostStatus, idChangedLine, shortID, hostTag, multiHost, offlineLine, profilesOn } from "../src/hosts.ts";
import { attachmentUrl } from "../src/attach.ts";
import { previewCard, previewUrl } from "../src/previews.ts";
import { renderMarkdown, setMarkdownOrigin } from "../src/markdown.ts";
import { applySnapshot, initialStore } from "../src/store.ts";
import { clockTime } from "../src/format.ts";

test("an offline thread names its host and the time; an online one says nothing", () => {
  const since = "2026-11-14T18:40:00Z";
  assert.equal(offlineLine({ host: "host-a", hostOffline: since }), `host-a offline since ${clockTime(since)} · read-only`);
  assert.equal(offlineLine({ host: "host-a" }), "");
});

test("hosts decode leniently and read as words", () => {
  const hs = decodeHosts([{ name: "a", online: true, since: "2026-11-14T18:40:00Z" }, { name: "b", online: false }, { online: true }, null]);
  assert.equal(hs.length, 2);
  assert.equal(hostStatus(hs[0], "a"), "this machine · online");
  assert.equal(hostStatus(hs[1], "a"), "offline");
  assert.deepEqual(decodeHosts(undefined), []);
});

test("the store keeps the state's hosts, and none from an older host", () => {
  const base = { now: "2026-11-14T18:40:00Z", user: "owner", threads: [], actions: [], profiles: [], projects: [] };
  const s = applySnapshot(initialStore(), { ...base, hosts: [{ name: "a", online: true }] }, Date.now());
  assert.deepEqual(s.hosts.map((h) => h.name), ["a"]);
  assert.deepEqual(applySnapshot(initialStore(), base, Date.now()).hosts, []);
});

// SPEC 0.5 V0.54: rows and headers name the host only with more than one; a new thread uses its host's profiles.
test("the host shows with two hosts, never with one", () => {
  const one = decodeHosts([{ name: "a", online: true }]);
  const two = decodeHosts([{ name: "a", online: true }, { name: "b", online: false }]);
  assert.equal(multiHost(one), false);
  assert.equal(hostTag({ host: "a" }, one), "");
  assert.equal(hostTag({ host: "b" }, two), "b");
  assert.equal(hostTag({}, two), "");
});

test("profiles and deploy are per host; an older front's state falls back to the merged list", () => {
  const pa = { id: "pa", name: "A", color: "", models: ["m"], defaultModel: "m" };
  const pb = { id: "pb", name: "B", color: "", models: ["m"], defaultModel: "m" };
  const hs = decodeHosts([
    { name: "a", online: true, profiles: [pa] },
    { name: "b", online: true, profiles: [pb], deploy: { phase: "drain" } },
    { name: "c", online: true },
  ]);
  const all = [pa, pb];
  assert.deepEqual(profilesOn("b", hs, all).map((p) => p.id), ["pb"]);
  assert.deepEqual(profilesOn("c", hs, all).map((p) => p.id), ["pa", "pb"]);
  assert.deepEqual(profilesOn(undefined, [], all).map((p) => p.id), ["pa", "pb"]);
  assert.equal(hostForProject(undefined, hs), "a");
  assert.equal(hostForProject("b", hs), "b");
  assert.equal(hostStatus(hs[1], "a"), "online · updating Fyr");
  assert.equal(hs[0].deploy, undefined);
});

test("attachments, previews and markdown images load from the thread's host", () => {
  assert.equal(attachmentUrl("att_1", "host-b"), "/api/attachments/att_1?host=host-b");
  assert.equal(attachmentUrl("att_1"), "/api/attachments/att_1");
  assert.equal(previewCard(1, "", { kind: "html", attachmentId: "pv_1" }, "b")?.src, "/api/previews/pv_1?host=b");
  assert.equal(previewUrl("pv_1"), "/api/previews/pv_1");
  setMarkdownOrigin("http://fyr.test");
  const md = "![shot](http://fyr.test/api/attachments/att_9)";
  assert.match(renderMarkdown(md, "b"), /src="\/api\/attachments\/att_9\?host=b"/);
  assert.match(renderMarkdown(md), /src="\/api\/attachments\/att_9"/);
  setMarkdownOrigin(null);
});

// review 2026-11-15: the rail and the phone name the host the same way when the state has none.
test("hostLabel falls back to mock or local", () => {
  assert.equal(hostLabel("front", false), "front");
  assert.equal(hostLabel("", true), "mock");
  assert.equal(hostLabel("", false), "local");
});

test("V0.123: a host the front found on the tailnet says so in Settings → Hosts", () => {
  const hs = decodeHosts([
    { name: "laptop", online: true },
    { name: "server", online: true, found: true },
    { name: "desk", online: false, found: true },
  ]);
  assert.equal(hs[1].found, true);
  assert.equal(hs[0].found, undefined);
  assert.equal(hostStatus(hs[0], "laptop"), "this machine · online");
  assert.equal(hostStatus(hs[1], "laptop"), "online · found on the tailnet");
  assert.equal(hostStatus(hs[2], "laptop"), "offline · found on the tailnet");
});

test("V0.124: a host row shows its short install id, and says so when another install answers under its name", () => {
  const hs = decodeHosts([
    { name: "laptop", online: true, id: "F7QK-2M9X-AB34-CD56" },
    { name: "server", online: true, id: "BBBB-CCCC-DDDD-EEEE", idWas: "AAAA-CCCC-DDDD-EEEE" },
    { name: "old", online: true, id: "", idWas: 3 },
  ]);
  assert.equal(hs[0].id, "F7QK-2M9X-AB34-CD56");
  assert.equal(hs[0].idWas, undefined);
  assert.equal(hs[2].id, undefined);
  assert.equal(hs[2].idWas, undefined);
  assert.equal(shortID(hs[0].id), "F7QK");
  assert.equal(hostLine(hs[0], "laptop"), "this machine · online · id F7QK");
  assert.equal(hostLine(hs[2], "laptop"), "online");
  assert.equal(idChangedLine(hs[0]), "");
  assert.equal(idChangedLine(hs[1]), "Another install answers as server: id BBBB, it was AAAA. Trust it if you reinstalled or replaced this machine.");
});
