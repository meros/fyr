// V0.159: Settings → Hosts, the installs panel: the words.
import { test } from "node:test";
import assert from "node:assert/strict";
import { candidateWords, inviteFromUrl, needsInvite, others, pairedSummary, typedInvite, viaWords, type PeerState } from "../src/peers.ts";

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
  assert.equal(pairedSummary(state), "2 paired");
  assert.equal(pairedSummary({ members: [state.members[0]], candidates: [] }), "");
  assert.equal(pairedSummary(null), "");
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
