// V0.159: Settings → Hosts, the installs panel: the words.
import { test } from "node:test";
import assert from "node:assert/strict";
import { candidateWords, inviteFromUrl, needsInvite, others, typedInvite, viaWords, type PeerState } from "../src/peers.ts";

const state: PeerState = {
  members: [
    { id: "AAAA-1111-2222-3333", name: "desk", self: true, drive: true, via: "paired" },
    { id: "BBBB-1111-2222-3333", name: "mac", drive: true, via: "paired" },
    { id: "CCCC-1111-2222-3333", name: "box", drive: false, via: "vouched", by: "mac" },
  ],
  candidates: [],
};

test("how an install came in", () => {
  assert.equal(viaWords(state.members[0]), "This install");
  assert.equal(viaWords(state.members[1]), "Paired");
  assert.equal(viaWords(state.members[2]), "Vouched for by mac");
  assert.equal(viaWords({ via: "vouched" }), "Vouched for by a member");
});

test("why a node is not paired", () => {
  assert.equal(candidateWords({ kind: "tagged", online: true }), "A tagged node: pair it with an invite");
  assert.equal(candidateWords({ kind: "other-user", online: false }), "Another tailnet user's · offline");
  assert.match(candidateWords({ kind: "waiting", online: true }), /Pairs by itself/);
  assert.equal(candidateWords({ kind: "shared", online: true }), "Shared in from another tailnet");
  assert.equal(candidateWords({ kind: "expired", online: true }), "Its key has expired");
  assert.equal(needsInvite({ kind: "tagged" }), true);
  assert.equal(needsInvite({ kind: "waiting" }), false);
});

test("the others, and the summary", () => {
  assert.deepEqual(others(state).map((m) => m.name), ["mac", "box"]);
});

test("an invite as typed and from the address", () => {
  assert.equal(typedInvite(" fyr1-f7qk-2m9x-ab34-cd56-abcdefgh23 "), "FYR1-F7QK-2M9X-AB34-CD56-ABCDEFGH23");
  assert.equal(inviteFromUrl("?settings=hosts&pair=fyr1-f7qk-2m9x-ab34-cd56-abcdefgh23"), "FYR1-F7QK-2M9X-AB34-CD56-ABCDEFGH23");
  assert.equal(inviteFromUrl("?pair=hello"), "");
  assert.equal(inviteFromUrl(""), "");
});

import { linkLine, linkWhy } from "../src/hosts.ts";
import { memberLink } from "../src/peers.ts";

test("a link in words: up from either side, down with the reason", () => {
  const NOW = Date.parse("2026-11-17T12:00:00Z");
  const since = new Date(NOW - 5 * 60_000).toISOString();
  assert.match(linkLine({ state: "up", via: "peer", opened: "them", since }, NOW), /^Link up, opened by it, since /);
  assert.match(linkLine({ state: "up", via: "peer", opened: "here", since }, NOW), /^Link up, opened from here/);
  assert.equal(linkLine({ state: "down", via: "peer", error: { kind: "timeout", text: "" } }, NOW), "Link down: no answer in time (a firewall or an ACL drops silently)");
  assert.equal(linkLine({ state: "down", via: "peer", error: { kind: "not-a-member", text: "its roster has not reached it" } }, NOW), "Link down: its roster has not reached it");
  assert.equal(linkLine({ state: "connecting", via: "peer" }, NOW), "Link connecting…");
  assert.equal(linkLine({ state: "up", via: "pipe" }, NOW), "", "this install's own host has no link line");
  assert.equal(linkLine(undefined, NOW), "");
  assert.equal(linkLine({ state: "up", via: "whois" }, NOW), "Old link (tailscaled's whois)");
  assert.equal(linkWhy({ error: { kind: "removed", text: "" } }), "it was removed from the roster");
  assert.equal(memberLink({ self: true }, NOW), "");
  assert.match(memberLink({ link: { state: "up", via: "peer", opened: "them" } }, NOW), /opened by it/);
});

import { configNote } from "../src/peers.ts";

test("V0.180: a config that changed under fyrd is said, with how to apply it", () => {
  assert.equal(configNote(state), null);
  assert.equal(configNote(null), null);
  const n = configNote({ ...state, configChanged: "config changed; restart fyrd to apply" });
  assert.ok(n);
  assert.match(n.title, /config changed; restart fyrd to apply/i);
  assert.match(n.how, /systemctl reload fyr/);
  assert.match(n.how, /agents/);
});

import { candidateWarn, foundBy, inviteId, pairAsks } from "../src/peers.ts";

// #259: an install found on the LAN: its id, every address and who found it, never the raw kind.
test("a found install in words", () => {
  const c = { kind: "lan", online: true, id: "K8VD-3N2Q-7HWX-5RTE", addr: "192.0.2.31:7778", addrs: ["192.0.2.31:7778"], source: "mdns" };
  assert.equal(candidateWords(c), "K8VD-3N2Q-7HWX-5RTE · 192.0.2.31:7778 · found by mDNS");
  assert.equal(candidateWords({ ...c, addrs: ["192.0.2.31:7778", "10.0.0.31:7778"], source: "hand" }), "K8VD-3N2Q-7HWX-5RTE · 192.0.2.31:7778, 10.0.0.31:7778 · added by hand");
  assert.equal(candidateWords({ kind: "lan", online: true, id: "K8VD-3N2Q-7HWX-5RTE", addr: "192.0.2.31:7778" }), "K8VD-3N2Q-7HWX-5RTE · 192.0.2.31:7778 · found by mDNS");
  assert.doesNotMatch(candidateWords(c), /\blan\b/);
  assert.equal(foundBy(undefined), "found by mDNS");
  assert.equal(needsInvite(c), false);
});

test("a spoof or a duplicate says so, and Pair on a spoof asks first", () => {
  const plain: { addrs: string[]; spoof?: string; dup?: boolean } = { addrs: ["192.0.2.31:7778"] };
  assert.equal(candidateWarn(plain), "");
  assert.equal(pairAsks(plain), false);
  const at = { ...plain, spoof: "address:lab:AX2C-1111-2222-3333" };
  assert.equal(candidateWarn(at), "This address is lab's (AX2C). Pair it only if you reinstalled lab. Pair asks for its code either way.");
  assert.equal(pairAsks(at), true);
  const named = { ...plain, spoof: "name:lab:AX2C-1111-2222-3333" };
  assert.equal(candidateWarn(named), "You already have a host named lab with another id (AX2C). Pair asks for its code either way.");
  assert.equal(pairAsks(named), true);
  const dup = { ...plain, dup: true, addrs: ["192.0.2.31:7778", "192.0.2.32:7778"] };
  assert.match(candidateWarn(dup), /^One id at 2 addresses: one install on two networks, or two machines with one id\./);
  assert.equal(pairAsks(dup), false, "a duplicate is warned of; the code still decides");
  assert.match(candidateWarn({ ...dup, spoof: "address:lab:AX2C-1111-2222-3333" }), /^This address is lab's/, "a spoof's line wins");
});

test("the install an invite names", () => {
  assert.equal(inviteId("fyr1-k8vd-3n2q-7hwx-5rte-abcdefgh23"), "K8VD-3N2Q-7HWX-5RTE");
  assert.equal(inviteId("FYR1-K8VD"), "");
  assert.equal(inviteId(""), "");
});
