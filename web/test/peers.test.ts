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
