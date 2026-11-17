// SPEC 0.5 V0.51: Settings → Hosts lists the front's hosts; an offline host's threads say so and are read-only.
import { test } from "node:test";
import assert from "node:assert/strict";
import { decodeHosts, hostLabel, hostForProject, hostLine, hostState, hostRows, howIn, idChangedLine, routeOf, shortID, hostTag, multiHost, offlineLine, profilesOn } from "../src/hosts.ts";
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
  assert.equal(hostState(hs[0], Date.parse("2026-11-14T18:41:00Z")), `online since ${clockTime(hs[0].since)}`);
  assert.equal(hostState(hs[1], Date.now()), "offline");
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
  assert.equal(hostState(hs[1], Date.now()), "online · updating Fyr");
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

test("V0.123: a host the front found on the tailnet is flagged", () => {
  const hs = decodeHosts([
    { name: "laptop", online: true },
    { name: "server", online: true, found: true },
  ]);
  assert.equal(hs[1].found, true);
  assert.equal(hs[0].found, undefined);
});

test("V0.124: a host keeps its install id, and the id it had when another install answers under its name", () => {
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
  assert.equal(idChangedLine(hs[0]), "");
  // V0.125: the advice to trust it moved to the host's page.
  assert.equal(idChangedLine(hs[1]), "Another install answers as server: id BBBB, it was AAAA.");
});

// SPEC 0.5 V0.125 (decision 0026): Settings → Hosts. A host's route from the URL the front dials.
test("V0.125: routeOf names the network that carries a link", () => {
  assert.equal(routeOf("http://127.0.0.1:7777"), "this machine");
  assert.equal(routeOf("http://127.4.5.6:7777"), "this machine");
  assert.equal(routeOf("http://localhost:7777"), "this machine");
  assert.equal(routeOf("http://[::1]:7777"), "this machine");
  assert.equal(routeOf("http://100.64.0.9:7778", true), "this machine", "this install is this machine whatever its URL");
  assert.equal(routeOf("http://100.64.0.9:7778"), "tailnet");
  assert.equal(routeOf("http://100.127.255.1:7778"), "tailnet");
  assert.equal(routeOf("http://100.128.0.1:7778"), "by address", "outside 100.64/10");
  assert.equal(routeOf("https://desk.tail1234.ts.net"), "tailnet");
  assert.equal(routeOf("http://10.1.2.3:7778"), "LAN");
  assert.equal(routeOf("http://172.16.0.4:7778"), "LAN");
  assert.equal(routeOf("http://172.31.255.4:7778"), "LAN");
  assert.equal(routeOf("http://172.32.0.4:7778"), "by address");
  assert.equal(routeOf("http://192.168.1.10:7778"), "LAN");
  assert.equal(routeOf("http://169.254.3.3:7778"), "LAN");
  assert.equal(routeOf("http://[fd12:3456::1]:7778"), "LAN");
  assert.equal(routeOf("http://old-nuc.lan:7778"), "LAN");
  assert.equal(routeOf("http://nas.local:7778"), "LAN");
  assert.equal(routeOf("http://box.home.arpa:7778"), "LAN");
  assert.equal(routeOf("http://host-a:7778"), "LAN", "a bare name");
  assert.equal(routeOf("https://fyr.example.net"), "by address");
  assert.equal(routeOf("http://203.0.113.5:7778"), "by address");
  assert.equal(routeOf(undefined), "", "a front older than V0.125 sends no url");
  assert.equal(routeOf(undefined, false, true), "tailnet", "a found host without a url");
  assert.equal(routeOf("not a url"), "");
});

test("V0.125: a host row's line: state since, route, short id", () => {
  const now = new Date(2026, 10, 16, 21, 0).getTime();
  const at = (d: number, h: number, m: number) => new Date(2026, 10, d, h, m).toISOString();
  const hs = decodeHosts([
    { name: "desk", online: true, since: at(16, 8, 12), id: "F7QK-2M9X-AB34-CD56", url: "http://127.0.0.1:7777" },
    { name: "host-b", online: false, since: at(16, 19, 40), error: "unreachable", found: true, id: "M3RS-QX7P-5LKA-2WNE", url: "http://100.64.0.9:7778" },
    { name: "build", online: true, since: at(16, 8, 31), deploy: { phase: "test" }, id: "K2VD-7HPA-M4QS-9TXE", url: "http://build-box.lan:7778" },
    { name: "old-nuc", online: true, since: at(16, 9, 0), url: "http://old-nuc.lan:7778" },
    { name: "lab", online: true, since: at(16, 20, 12), id: "B7LA-4QZE-9KDF-3HXW", idWas: "AX2C-7RTM-8PLQ-6VYD", url: "http://100.64.0.7:7778" },
    { name: "vps", online: true, since: at(9, 6, 0), id: "V9PS-4LQA-J7RN-2XWF", url: "https://vps.example.net" },
    { name: "older", online: true },
  ]);
  assert.equal(hs[0].url, "http://127.0.0.1:7777");
  assert.equal(hostLine(hs[0], "desk", now), `online since ${clockTime(at(16, 8, 12))} · this machine · id F7QK`);
  assert.equal(hostLine(hs[1], "desk", now), `offline since ${clockTime(at(16, 19, 40))} · tailnet · id M3RS`);
  assert.equal(hostLine(hs[2], "desk", now), `online since ${clockTime(at(16, 8, 31))} · updating Fyr · LAN · id K2VD`);
  assert.equal(hostLine(hs[3], "desk", now), `online since ${clockTime(at(16, 9, 0))} · LAN · no id`);
  assert.equal(hostLine(hs[4], "desk", now), "Another install answers as lab: id B7LA, it was AX2C.");
  const day = new Date(2026, 10, 9, 6, 0).toLocaleDateString(undefined, { day: "numeric", month: "short" });
  assert.equal(hostLine(hs[5], "desk", now), `online since ${day} ${clockTime(at(9, 6, 0))} · by address · id V9PS`);
  const mon = new Date(2026, 10, 16, 6, 0).toLocaleDateString(undefined, { weekday: "short" });
  assert.equal(hostState({ name: "x", online: true, since: at(16, 6, 0) }, new Date(2026, 10, 17, 9, 0).getTime()), `online since ${mon} ${clockTime(at(16, 6, 0))}`);
  assert.equal(hostLine(hs[6], "desk", now), "online · no id", "an older front: no since, no url, no id");
  assert.equal(decodeHosts([{ name: "a", online: true, url: 3 }])[0].url, undefined);
});

test("V0.125, V0.132: the Hosts page lists the hosts in the owner's order (the front's), and this install alone when the front lists no hosts", () => {
  const hs = decodeHosts([{ name: "lab", online: true }, { name: "desk", online: true, id: "F7QK-2M9X-AB34-CD56" }]);
  assert.deepEqual(hostRows(hs, "desk", "F7QK-2M9X-AB34-CD56").map((h) => h.name), ["lab", "desk"]);
  assert.deepEqual(hostRows([], "desk", "F7QK-2M9X-AB34-CD56"), [{ name: "desk", online: true, id: "F7QK-2M9X-AB34-CD56" }]);
  assert.deepEqual(hostRows(undefined, "desk", ""), [{ name: "desk", online: true }], "a front older than V0.124 sends no id");
});

test("V0.125: how this browser gets in, from the page's host and the state's user", () => {
  assert.equal(howIn("desk.tail1234.ts.net", "owner@github"), "Through Tailscale Serve, as owner@github. Any browser signed in to your tailnet with that login can open this Fyr.");
  assert.equal(howIn("localhost", "owner"), "On this machine itself (localhost). A browser on this machine is always allowed.");
  assert.equal(howIn("127.0.0.1", "owner"), "On this machine itself (localhost). A browser on this machine is always allowed.");
  assert.equal(howIn("[::1]", "owner"), "On this machine itself (localhost). A browser on this machine is always allowed.");
  assert.equal(howIn("fyr.example.net", "owner@github"), "At fyr.example.net, as owner@github. What stands in front of Fyr there decides who gets in.");
  assert.equal(howIn("fyr.example.net", ""), "At fyr.example.net. What stands in front of Fyr there decides who gets in.");
});
