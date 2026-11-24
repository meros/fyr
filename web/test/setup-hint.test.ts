// V0.tailscale-operator-hint: the host setup card names the fix for Tailscale's 401 on `serve`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { setupErrorHint, TAILSCALE_OPERATOR_FIX } from "../src/setup-hint.ts";

test("a 401 from tailscale serve names the operator fix", () => {
  const e = "tailscale serve (port 7443): sending serve config: 401 Unauthorized: must be root, or be an operator and able to run 'sudo tailscale' to serve a path or Unix socket";
  assert.equal(setupErrorHint(e), TAILSCALE_OPERATOR_FIX);
});

test("other errors get no hint", () => {
  assert.equal(setupErrorHint("gh repo list: not logged in"), null);
  assert.equal(setupErrorHint("tailscale serve: no such host"), null);
});
