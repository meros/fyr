import { test } from "node:test";
import assert from "node:assert/strict";
import { base64UrlToBytes, deviceLabel, parsePayload, pushNext, pushStatus, unsupportedReason, type PushEnv } from "../src/push.ts";

const desktop: PushEnv = { secure: true, serviceWorker: true, pushManager: true, notification: true, ios: false, standalone: false };

test("base64url decodes to the raw key bytes, with and without padding", () => {
  assert.deepEqual([...base64UrlToBytes("AQID")], [1, 2, 3]);
  assert.deepEqual([...base64UrlToBytes("-_8")], [0xfb, 0xff]);
  assert.deepEqual([...base64UrlToBytes("-_8=")], [0xfb, 0xff]);
  const key = base64UrlToBytes("BDJF2xtzzliD3JVf2tm-yBnwg5fyq3ePXxiuCwxmswDZykUsCBJdi4cRLPO3Q3EvmJ_7ngetcIYIviMRUSCaUV0");
  assert.equal(key.length, 65, "an uncompressed P-256 point");
  assert.equal(key[0], 4);
  assert.throws(() => base64UrlToBytes("not*base64"));
});

test("status: unsupported, denied, on, off", () => {
  assert.equal(pushStatus(desktop, "default", false), "off");
  assert.equal(pushStatus(desktop, "granted", false), "off", "permission alone is not on");
  assert.equal(pushStatus(desktop, "granted", true), "on");
  assert.equal(pushStatus(desktop, "denied", true), "denied");
  assert.equal(pushStatus({ ...desktop, pushManager: false }, "granted", true), "unsupported");
  assert.equal(pushStatus({ ...desktop, secure: false }, "default", false), "unsupported");
});

test("iOS needs the app on the home screen first", () => {
  const ios = { ...desktop, ios: true };
  assert.match(unsupportedReason(ios) ?? "", /home screen/);
  assert.equal(pushStatus(ios, "default", false), "unsupported");
  assert.equal(unsupportedReason({ ...ios, standalone: true }), null);
  assert.equal(pushStatus({ ...ios, standalone: true }, "default", false), "off");
});

test("transitions of the bell", () => {
  assert.equal(pushNext("off", { type: "permission", result: "denied" }), "denied");
  assert.equal(pushNext("off", { type: "permission", result: "granted" }), "off", "on only after subscribing");
  assert.equal(pushNext("off", { type: "subscribed" }), "on");
  assert.equal(pushNext("off", { type: "failed" }), "off");
  assert.equal(pushNext("on", { type: "unsubscribed" }), "off");
  assert.equal(pushNext("on", { type: "failed" }), "on", "a failed turn-off keeps it on");
  assert.equal(pushNext("unsupported", { type: "subscribed" }), "unsupported");
});

test("device labels and payload parsing", () => {
  assert.equal(deviceLabel("Mozilla/5.0 (Linux; Android 15; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Mobile Safari/537.36"), "Android Chrome");
  assert.equal(deviceLabel("Mozilla/5.0 (iPhone; CPU iPhone OS 26_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1"), "iPhone Safari");
  assert.equal(deviceLabel("Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"), "Linux Firefox");
  assert.deepEqual(parsePayload('{"title":"Asking","body":"Which gesture?","threadId":"t_1","state":"asking"}'), { title: "Asking", body: "Which gesture?", threadId: "t_1", state: "asking" });
  assert.deepEqual(parsePayload("hello"), { title: "Fyr", body: "hello" });
});

test("push status reconciliation with the host", async () => {
  const { reconcilePush } = await import("../src/push.ts");
  assert.deepEqual(reconcilePush(false, null), { unsubscribe: false, subscribed: false });
  assert.deepEqual(reconcilePush(true, true), { unsubscribe: false, subscribed: true });
  assert.deepEqual(reconcilePush(true, false), { unsubscribe: true, subscribed: false }, "the host deleted it: drop it locally");
  assert.deepEqual(reconcilePush(true, null), { unsubscribe: false, subscribed: true }, "host unreachable: keep it");
});

// SPEC 0.5 V0.15, design/mobile rule 12: notification presentation per state.
import { notificationOptions, shouldClearNotification } from "../src/push.ts";
test("notification options per state", () => {
  const ask = notificationOptions({ title: "Fix lasso", body: "2 questions · Which gesture?", threadId: "t1", state: "asking" }, 1000);
  assert.equal(ask.tag, "t1");
  assert.equal(ask.renotify, true);
  assert.equal(ask.requireInteraction, true);
  assert.equal(ask.timestamp, 1000);
  assert.deepEqual(ask.actions, [{ action: "open", title: "Answer" }]);
  const failed = notificationOptions({ title: "x", body: "Failed · API", threadId: "t2", state: "failed" }, 1);
  assert.equal(failed.requireInteraction, false);
  assert.deepEqual(failed.actions, [{ action: "open", title: "Open" }]);
  const test = notificationOptions({ title: "Fyr", body: "works", state: "test" }, 1);
  assert.equal(test.tag, undefined);
  assert.deepEqual(test.actions, []);
});

test("a thread's notification closes when it no longer needs the user", () => {
  assert.equal(shouldClearNotification("asking"), false);
  assert.equal(shouldClearNotification("failed"), false);
  for (const s of ["working", "waiting", "done", "seen", "settled", "snoozed", "archived"]) assert.equal(shouldClearNotification(s), true, s);
});
