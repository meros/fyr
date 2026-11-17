// V0.157: Settings → Devices: the words, the counts, the notice.
import { test } from "node:test";
import assert from "node:assert/strict";
import { autoNotice, codeShow, deviceLine, devicesNeed, devicesSummary, howWords, modeLine, splitDevices, typedCode, type AuthState, type DeviceRow, type ModeRow } from "../src/devices.ts";

const NOW = Date.parse("2026-11-17T12:00:00Z");
const ago = (min: number) => new Date(NOW - min * 60_000).toISOString();
const dev = (o: Partial<DeviceRow>): DeviceRow => ({ id: "d", name: "Chrome", how: "local", created: ago(1000), lastSeen: ago(5), ...o });
const mode = (o: Partial<ModeRow>): ModeRow => ({ way: "browser", mode: "observe", hand: false, since: ago(2 * 24 * 60), refused: 0, success: {}, ready: false, ...o });

test("how a device got in", () => {
  assert.equal(howWords("tailnet"), "Tailscale");
  assert.equal(howWords("code"), "Approval code");
  assert.equal(howWords("odd"), "odd");
});

test("a device's line", () => {
  assert.equal(deviceLine(dev({ how: "tailnet", lastSeen: ago(12) }), NOW), "Tailscale · last used 12 min ago");
  assert.equal(deviceLine(dev({ how: "link", lastSeen: ago(0) }), NOW), "One-time link · last used now");
});

test("the code as shown and as typed", () => {
  assert.equal(codeShow("ABCD1234"), "ABCD-1234");
  assert.equal(codeShow(undefined), "");
  assert.equal(typedCode("abcd-12 34zz"), "ABCD1234");
});

test("the nav's line and count", () => {
  const s = (devices: DeviceRow[]): AuthState => ({ devices, modes: [], audit: [] });
  assert.equal(devicesSummary(null), "");
  assert.equal(devicesSummary(s([dev({})])), "1 device");
  assert.equal(devicesSummary(s([dev({}), dev({ id: "e" })])), "2 devices");
  assert.equal(devicesSummary(s([dev({}), dev({ id: "w", pending: true })])), "1 device · 1 waiting");
  assert.equal(devicesNeed(s([dev({ pending: true })])), 1);
  assert.equal(devicesNeed(null), 0);
  const sp = splitDevices(s([dev({}), dev({ id: "w", pending: true })]));
  assert.deepEqual([sp.signedIn.length, sp.waiting.length], [1, 1]);
});

test("the browser way in, as one line", () => {
  assert.match(modeLine(mode({ mode: "enforce" }), NOW), /Every browser has to be signed in/);
  assert.match(modeLine(mode({ refused: 0 }), NOW), /In 2 days, nothing would have been refused/);
  assert.match(modeLine(mode({ refused: 3, why: "no sign-in seen yet by tailnet" }), NOW), /3 requests without a session would have been refused.*Not yet: no sign-in seen yet by tailnet/);
  assert.match(modeLine(mode({ ready: true }), NOW), /turns itself on at the next check/);
  assert.match(modeLine(mode({ hand: true }), NOW), /Set back by hand/);
  assert.equal(modeLine(undefined, NOW), "");
});

test("the notice after the install enforced by itself", () => {
  const detail = "browser moved to enforce after 3 clean days; `fyr auth observe` goes back";
  assert.match(autoNotice([{ at: ago(60), event: "mode", by: "auto", detail }], NOW) ?? "", /fyr auth observe/);
  assert.equal(autoNotice([{ at: ago(60 * 24 * 8), event: "mode", by: "auto", detail }], NOW), null, "a week later it is gone");
  assert.equal(autoNotice([{ at: ago(60), event: "mode", by: "owner.sock", detail }], NOW), null, "set by hand is not a notice");
  assert.equal(autoNotice([], NOW), null);
});
