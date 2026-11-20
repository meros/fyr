// SPEC 0.5 V0.51: Settings → Hosts lists the front's hosts; an offline host's threads say so and are read-only.
import { test } from "node:test";
import assert from "node:assert/strict";
import { decodeHosts, hostLine, hostState, howIn, idChangedLine, routeOf, hostRoute, shortID, hostTag, multiHost, offlineLine, profilesOf } from "../src/hosts.ts";
import { rowProvider, threadProvider } from "../src/hosts.ts";
import { attachmentUrl } from "../src/attach.ts";
import { previewCard, previewUrl } from "../src/previews.ts";
import { renderMarkdown, setMarkdownOrigin } from "../src/markdown.ts";
import { applySnapshot, initialStore, profilesOn } from "../src/store.ts";
import { hid, snapshot } from "./helpers.ts";
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

test("the store keeps the front's hosts; a host without an install id is listed and has no state", () => {
  const s = applySnapshot(initialStore(), snapshot({ hosts: [{ name: "a", online: true, id: hid("HA") }, { name: "old", online: true }] }), Date.now());
  assert.deepEqual(s.hostList.map((h) => h.name), ["a", "old"]);
  assert.deepEqual(Object.keys(s.hosts), ["HA"]);
  assert.deepEqual(applySnapshot(initialStore(), snapshot({ hosts: [] }), Date.now()).hostList, []);
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

test("profiles and deploy are per host; slice 2b: no merged list, and a host not known has none", () => {
  const pa = { id: "pa", name: "A", color: "", models: ["m"], defaultModel: "m" };
  const pb = { id: "pb", name: "B", color: "", models: ["m"], defaultModel: "m" };
  const hs = decodeHosts([
    { name: "a", online: true, id: "HA", profiles: [pa] },
    { name: "b", online: true, id: "HB", profiles: [pb], deploy: { phase: "drain" } },
    { name: "c", online: true, id: "HC" },
  ]);
  const s = applySnapshot(initialStore(), snapshot({ hosts: hs }), Date.now());
  assert.deepEqual(profilesOn(s, hid("HB")).map((p) => p.id), ["pb"]);
  assert.deepEqual(profilesOn(s, hid("HC")), [], "a host with no profiles has none, not another host's");
  assert.deepEqual(profilesOn(s, undefined), []);
  assert.deepEqual(profilesOf({ hostId: hid("HA") }, { HA: [pa] }).map((p) => p.id), ["pa"]);
  assert.deepEqual(profilesOf({}, { HA: [pa] }), [], "a record with no host has no profiles");
  assert.equal(hostState(hs[1], Date.now()), "online · updating Fyr");
  assert.equal(hs[0].deploy, undefined);
});

test("attachments, previews and markdown images load from the thread's host", () => {
  const HB = hid("HB");
  assert.equal(attachmentUrl(HB, "att_1"), "/api/h/HB/attachments/att_1");
  assert.equal(previewCard(1, "", { kind: "html", attachmentId: "pv_1" }, HB)?.src, "/api/h/HB/previews/pv_1");
  assert.equal(previewUrl(HB, "pv_1"), "/api/h/HB/previews/pv_1");
  setMarkdownOrigin("http://fyr.test");
  const md = "![shot](http://fyr.test/api/attachments/att_9)";
  assert.match(renderMarkdown(md, HB), /src="\/api\/h\/HB\/attachments\/att_9"/);
  assert.doesNotMatch(renderMarkdown(md), /<img/, "without a host there is nowhere to read it from: a link");
  setMarkdownOrigin(null);
});

// review 2026-11-15: the rail and the phone name the host the same way when the state has none.
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
  assert.equal(routeOf(undefined, true), "tailnet", "a found host without a url");
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
  assert.equal(hostLine(hs[0], now), `online since ${clockTime(at(16, 8, 12))} · this machine · id F7QK`);
  assert.equal(hostLine(hs[1], now), `offline since ${clockTime(at(16, 19, 40))} · tailnet · id M3RS`);
  assert.equal(hostLine(hs[2], now), `online since ${clockTime(at(16, 8, 31))} · updating Fyr · LAN · id K2VD`);
  assert.equal(hostLine(hs[3], now), `online since ${clockTime(at(16, 9, 0))} · LAN · needs an update`);
  assert.equal(hostLine(hs[4], now), "Another install answers as lab: id B7LA, it was AX2C.");
  const day = new Date(2026, 10, 9, 6, 0).toLocaleDateString(undefined, { day: "numeric", month: "short" });
  assert.equal(hostLine(hs[5], now), `online since ${day} ${clockTime(at(9, 6, 0))} · by address · id V9PS`);
  const mon = new Date(2026, 10, 16, 6, 0).toLocaleDateString(undefined, { weekday: "short" });
  assert.equal(hostState({ name: "x", online: true, since: at(16, 6, 0) }, new Date(2026, 10, 17, 9, 0).getTime()), `online since ${mon} ${clockTime(at(16, 6, 0))}`);
  assert.equal(hostLine(hs[6], now), "online · needs an update", "an older front: no since, no url, no id");
  assert.equal(decodeHosts([{ name: "a", online: true, url: 3 }])[0].url, undefined);
});

test("decision 0031 2.6: the route says \"this machine\" for the host the front's link is the pipe to, whatever its URL; no host is the front's by name", () => {
  assert.equal(hostRoute({ url: "http://100.64.0.3:7778", link: { state: "up", via: "pipe" } }), "this machine");
  assert.equal(hostRoute({ url: "http://100.64.0.3:7778", link: { state: "up", via: "peer" } }), "tailnet");
  assert.equal(hostRoute({ found: true }), "tailnet");
  assert.deepEqual(decodeHosts([{ name: "desk", online: true, link: { state: "up", via: "pipe" } }, { name: "x", online: true, link: "up" }]).map((h) => h.link), [{ state: "up", via: "pipe" }, undefined]);
});

test("V0.125: how this browser gets in, from the page's host and the state's user", () => {
  assert.equal(howIn("desk.tail1234.ts.net", "owner@github"), "Through Tailscale Serve, as owner@github. Any browser signed in to your tailnet with that login can open this Fyr.");
  assert.equal(howIn("localhost", "owner"), "On this machine itself (localhost). A browser on this machine is always allowed.");
  assert.equal(howIn("127.0.0.1", "owner"), "On this machine itself (localhost). A browser on this machine is always allowed.");
  assert.equal(howIn("[::1]", "owner"), "On this machine itself (localhost). A browser on this machine is always allowed.");
  assert.equal(howIn("fyr.example.net", "owner@github"), "At fyr.example.net, as owner@github. What stands in front of Fyr there decides who gets in.");
  assert.equal(howIn("fyr.example.net", ""), "At fyr.example.net. What stands in front of Fyr there decides who gets in.");
});

test("V0.206: a thread's provider is its host's, and a row names it only when there is more than one name", () => {
  const prof = (id: string, name: string, extra = {}) => ({ id, name, color: "#123456", models: [], ...extra });
  const [A, B] = [hid("HA"), hid("HB")];
  const byHost = { HA: [prof("p", "Claude")], HB: [prof("p", "DeepSeek", { unavailable: "No key" })] } as never;
  assert.deepEqual(threadProvider({ hostId: B, profile: "p" }, byHost), { name: "DeepSeek", color: "#123456", title: "Provider DeepSeek · No key" });
  assert.equal(rowProvider({ hostId: A, profile: "p" }, byHost)?.name, "Claude");
  // One name, on one host or on two: the row says nothing.
  assert.equal(rowProvider({ hostId: A, profile: "p" }, { HA: [prof("p", "Claude")] } as never), null);
  const same = { HA: [prof("p", "Claude")], HB: [prof("q", "Claude")] } as never;
  assert.equal(rowProvider({ hostId: B, profile: "q" }, same), null);
  // A provider since removed shows its id, which is a second name.
  assert.equal(rowProvider({ hostId: A, profile: "gone" }, { HA: [prof("p", "Claude")] } as never)?.name, "gone");
  assert.equal(rowProvider({ hostId: A, profile: "" }, { HA: [prof("p", "Claude")] } as never), null);
});

test("V0.host-updates: a host's row says how far behind it is and how its last update ended", async () => {
  const { updateLine, updateRows, badLine } = await import("../src/hosts.ts");
  const now = Date.parse("2026-11-19T15:00:00");
  const [h] = decodeHosts([
    {
      name: "host-b",
      online: true,
      update: { running: "9b1e04a77d2c", wanted: "c705ef5365660a8f", behind: 3, last: { build: "c705ef5", commit: "c705ef5365660a8f", result: "failed", reason: "the smoke test failed", startedAt: "2026-11-19T14:00:00" } },
    },
  ]);
  assert.deepEqual(updateLine(h), { text: "3 commits behind · last update failed", bad: true });
  assert.match(hostLine(h, now), /online · 3 commits behind · last update failed · needs an update/);
  const rows = updateRows(h.update!, now);
  assert.deepEqual(
    rows.map((r) => r.title),
    ["Runs", "Newest release", "Last update"],
  );
  assert.equal(rows[1].desc, "c705ef5 · 3 commits ahead of what runs");
  assert.match(rows[2].desc, /^release c705ef5 failed · .* · the smoke test failed$/);
  assert.equal(rows[2].bad, true);

  const held = { ...h.update!, bad: { commit: "c705ef5365660a8f", why: "exited with code 1", at: "2026-11-19T14:02:00" } };
  assert.equal(updateLine({ update: held }).text, "3 commits behind · holds back c705ef5");
  assert.equal(badLine(held.bad, now), `Release c705ef5 is held back here since ${clockTime("2026-11-19T14:02:00")}: exited with code 1. Fyr skips it until a newer release.`);

  assert.deepEqual(updateLine({ update: { behind: 0, running: "a", wanted: "a" } }), { text: "up to date", bad: false });
  assert.equal(updateLine({ update: { behind: 0, last: { build: "x", result: "failed", startedAt: "" } } }).bad, false, "a failure the host has since got past is not red");
  assert.equal(updateLine({ update: { behind: -1, paused: true } }).text, "local build");
  assert.equal(updateLine({}).text, "", "a host without update state says nothing");
  assert.equal(decodeHosts([{ name: "x", online: true, update: { running: "a" } }])[0].update!.behind, -1);
});
