// SPEC 0.5 V0.170: each host's mark: a glyph from the machine it runs on and a colour from its install id.
import { test } from "node:test";
import assert from "node:assert/strict";
import { HOST_ICON_NAMES, hostColors, hostGlyph, hostMarkOf } from "../src/host-mark.ts";
import { decodeHosts, type HostInfo } from "../src/hosts.ts";
import { PALETTE_NAMES } from "../src/projects.ts";
import { decodeHostMarks } from "../src/store.ts";

const H = (name: string, extra: Partial<HostInfo> = {}): HostInfo => ({ name, online: true, ...extra });

test("the glyph follows the machine: a laptop, a desktop's monitor, else a server", () => {
  assert.equal(hostGlyph("laptop"), "laptop");
  assert.equal(hostGlyph("desktop"), "monitor");
  assert.equal(hostGlyph("server"), "server");
  assert.equal(hostGlyph(undefined), "server", "a host older than V0.170, or not seen yet");
  assert.equal(hostGlyph("toaster"), "server");
});

test("decodeHosts reads machine and os", () => {
  const [h] = decodeHosts([{ name: "a", online: true, machine: "laptop", os: "darwin" }]);
  assert.equal(h.machine, "laptop");
  assert.equal(h.os, "darwin");
  assert.equal(decodeHosts([{ name: "b", online: true, machine: 3 }])[0].machine, undefined);
});

test("colours: from the install id, from the palette (never amber), stable, and different for each host", () => {
  const hosts = [H("meros-laptop", { id: "F7QK-2M9X-AB34-CD56" }), H("meros-server", { id: "M3RS-QX7P-5LKA-2WNE" }), H("MacBook-Air-7", { id: "B7LA-4QZE-9KDF-3HXW" })];
  const c = hostColors(hosts);
  assert.equal(c.size, 3);
  for (const v of c.values()) assert.ok(PALETTE_NAMES.includes(v));
  assert.equal(new Set(c.values()).size, 3, "three hosts, three colours");
  // The same on another front that lists them in another order.
  assert.deepEqual([...hostColors([hosts[2], hosts[0], hosts[1]])].sort(), [...c].sort());
  // Renaming a host keeps its colour: it follows the id.
  assert.equal(hostColors([{ ...hosts[0], name: "laptop2" }, hosts[1], hosts[2]]).get("laptop2"), c.get("meros-laptop"));
  // Without an id (a host older than V0.124), the name.
  assert.ok(PALETTE_NAMES.includes(hostColors([H("old-nuc")]).get("old-nuc")!));
});

test("ten or more hosts still each get a colour", () => {
  const many = Array.from({ length: 12 }, (_, i) => H(`h${i}`, { id: `ID${i}` }));
  const c = hostColors(many);
  assert.equal(c.size, 12);
  assert.equal(new Set(c.values()).size, 10, "the palette's ten, then they repeat");
});

test("hostMarkOf: glyph, colour, offline, and the words for its tooltip", () => {
  const hosts = [H("desk", { id: "A", machine: "desktop" }), H("srv", { id: "B", machine: "server", online: false })];
  const m = hostMarkOf("srv", hosts);
  assert.equal(m.glyph, "server");
  assert.equal(m.offline, true);
  assert.equal(m.title, "srv · offline");
  assert.equal(hostMarkOf("desk", hosts).title, "desk");
  const gone = hostMarkOf("gone", hosts);
  assert.equal(gone.glyph, "server");
  assert.ok(PALETTE_NAMES.includes(gone.color), "a host the front no longer lists: a colour from its name");
});

test("one host: this install's own machine, from the state", () => {
  assert.equal(hostMarkOf("solo", [], undefined, { name: "solo", machine: "laptop" }).glyph, "laptop");
  assert.equal(hostMarkOf("other", [], undefined, { name: "solo", machine: "laptop" }).glyph, "server");
});

// SPEC 0.5 V0.171: the owner's icon and colour, by install id, override the automatic ones.
test("the owner's mark: a glyph name or an emoji for the icon, a palette name for the colour", () => {
  const hosts = [H("desk", { id: "A", machine: "laptop" }), H("srv", { id: "B", machine: "server" })];
  const marks = { A: { icon: "house", color: "teal" }, B: { icon: "🍓" } };
  const a = hostMarkOf("desk", hosts, hostColors(hosts, marks), undefined, marks);
  assert.deepEqual([a.glyph, a.emoji, a.color, a.auto], ["house", undefined, "teal", false]);
  const b = hostMarkOf("srv", hosts, hostColors(hosts, marks), undefined, marks);
  assert.deepEqual([b.glyph, b.emoji, b.auto], [null, "🍓", false]);
  // Unknown values are ignored: the automatic glyph and colour.
  const odd = { A: { icon: "not-an-icon-name", color: "amber" } };
  const c = hostMarkOf("desk", hosts, hostColors(hosts, odd), undefined, odd);
  assert.equal(c.glyph, "laptop");
  assert.notEqual(c.color, "amber");
  assert.equal(hostMarkOf("desk", hosts).auto, true);
});

test("an owner's colour is kept free: the automatic colours of the other hosts avoid it", () => {
  const hosts = [H("a", { id: "A" }), H("b", { id: "B" }), H("c", { id: "C" })];
  const auto = hostColors(hosts);
  // Give c the colour a had automatically: a then moves to another hue, and no two hosts share one.
  const marks = { C: { color: auto.get("a")! } };
  const c = hostColors(hosts, marks);
  assert.equal(c.get("c"), auto.get("a"));
  assert.equal(new Set(c.values()).size, 3);
});

test("the icons the owner can pick", () => {
  assert.deepEqual(HOST_ICON_NAMES, ["laptop", "monitor", "server", "house", "cloud", "cpu", "database", "terminal", "box"]);
});

// SPEC 0.5 V0.208: who set each field and the last setup run come through; a chosen automatic mark is kept.
test("the marks decode who set them", () => {
  const m = decodeHostMarks({
    A: { icon: "cloud", color: "rose", by: { icon: "agent", color: "owner" }, setup: { at: "2026-11-18T10:00:00Z", error: "no agent" } },
    B: { by: { icon: "owner", color: "someone" } },
    C: {},
  });
  assert.deepEqual(m, {
    A: { icon: "cloud", color: "rose", by: { icon: "agent", color: "owner" }, setup: { at: "2026-11-18T10:00:00Z", error: "no agent" } },
    B: { by: { icon: "owner" } },
  });
});
