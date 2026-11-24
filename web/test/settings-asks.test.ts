// V0.settings-ia: what the confirmation dialog says before the presses in Settings that cannot be taken back and reach
// beyond this host's own small things.
import { test } from "node:test";
import assert from "node:assert/strict";
import { removeHostAsk, requireSignInAsk } from "../src/settings-asks.ts";

test("Remove a host names the host, says what ends, and its button is red and names the act", () => {
  const a = removeHostAsk("host-b");
  assert.equal(a.title, "Remove host-b?");
  assert.match(a.text!, /pairing ends for ever: the link to host-b closes/);
  assert.equal(a.confirm, "Remove host");
  assert.equal(a.danger, true);
});

test("Require sign-in asks only when it would refuse someone", () => {
  assert.equal(requireSignInAsk({ refused: 0 }), null, "nothing would be refused: the switch acts at once");
  const a = requireSignInAsk({ refused: 3 })!;
  assert.equal(a.title, "Require sign-in?");
  assert.match(a.text!, /^3 requests without a session got in/);
  assert.match(requireSignInAsk({ refused: 1 })!.text!, /^1 request without a session got in/);
  assert.equal(a.confirm, "Require sign-in");
  assert.equal(a.danger, undefined, "it can be turned off again");
});
