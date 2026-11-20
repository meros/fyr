import { test } from "node:test";
import assert from "node:assert/strict";
import { base64UrlToBytes, deviceLabel, openTargets, parsePayload, pushNext, pushStatus, rememberOpen, takeOpen, unsupportedReason, type PushEnv } from "../src/push.ts";

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
  assert.match(unsupportedReason(ios) ?? "", /Home Screen/);
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
  assert.deepEqual(parsePayload('{"title":"x","threadId":"t_1","host":"AAAA-1111-AAAA-1111","state":"asking"}'), { title: "x", body: undefined, threadId: "t_1", host: "AAAA-1111-AAAA-1111", state: "asking" });
});

test("push status reconciliation with the host", async () => {
  const { reconcilePush } = await import("../src/push.ts");
  assert.deepEqual(reconcilePush(false, null), { unsubscribe: false, subscribed: false });
  assert.deepEqual(reconcilePush(true, true), { unsubscribe: false, subscribed: true });
  assert.deepEqual(reconcilePush(true, false), { unsubscribe: true, subscribed: false }, "the host deleted it: drop it locally");
  assert.deepEqual(reconcilePush(true, null), { unsubscribe: false, subscribed: true }, "host unreachable: keep it");
});

// SPEC 0.5 V0.15, design/mobile rule 12: notification presentation per state.
import { notificationOptions, notificationIsStale } from "../src/push.ts";
test("notification options per state", () => {
  const ask = notificationOptions({ title: "Fix lasso", body: "2 questions · Which gesture?", threadId: "t1", state: "asking" }, 1000);
  assert.equal(ask.tag, "t1");
  assert.equal(ask.renotify, true);
  assert.equal(ask.requireInteraction, true);
  assert.equal(ask.timestamp, 1000);
  assert.deepEqual(ask.data, { threadId: "t1", state: "asking" }); // V0.notification-polish: the state it was shown for
  assert.deepEqual(ask.actions, [{ action: "open", title: "Answer" }]);
  // V0.client-refs: with the host, the tag and the tap name the thread's ref, so one id on two hosts is two threads.
  const onA = notificationOptions({ title: "x", threadId: "t1", host: "AAAA-1111-AAAA-1111", state: "asking" }, 1);
  const onB = notificationOptions({ title: "x", threadId: "t1", host: "BBBB-2222-BBBB-2222", state: "asking" }, 1);
  assert.equal(onA.tag, "AAAA-1111-AAAA-1111/t1");
  assert.deepEqual(onB.data, { threadId: "BBBB-2222-BBBB-2222/t1", state: "asking" });
  const failed = notificationOptions({ title: "x", body: "Failed · API", threadId: "t2", state: "failed" }, 1);
  assert.equal(failed.requireInteraction, false);
  assert.deepEqual(failed.actions, [{ action: "open", title: "Open" }]);
  const test = notificationOptions({ title: "Fyr", body: "works", state: "test" }, 1);
  assert.equal(test.tag, undefined);
  assert.deepEqual(test.actions, []);
});

test("a thread's notification closes when it no longer needs the user", () => {
  // V0.notification-polish: a notification closes when the thread leaves the state it was shown for, and stays while
  // the thread is still in it (a Done notification was closed by the next message about the done thread).
  for (const s of ["asking", "failed", "blocked", "done"]) assert.equal(notificationIsStale(s, s), false, s);
  assert.equal(notificationIsStale("asking", "working"), true);
  assert.equal(notificationIsStale("done", "seen"), true);
  assert.equal(notificationIsStale("done", "working"), true);
  assert.equal(notificationIsStale("blocked", "done"), true);
  assert.equal(notificationIsStale("failed", "asking"), true);
  // one shown by a build before this change carries no state: the old rule
  assert.equal(notificationIsStale(undefined, "asking"), false);
  assert.equal(notificationIsStale(undefined, "failed"), false);
  for (const s of ["working", "waiting", "done", "seen", "settled", "snoozed", "archived"]) assert.equal(notificationIsStale(undefined, s), true, s);
});

// SPEC 0.5 V0.15: a tapped notification opens its thread even when the app reloads for a
// new build before the thread is on screen (found live: taps opened the wrong thread).
test("a pending open survives a reload and is used once", () => {
  const m = new Map<string, string>();
  const st = { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k) };
  rememberOpen(st, "t_b");
  assert.equal(takeOpen(st), "t_b", "after the reload the thread is still there");
  assert.equal(takeOpen(st), null, "used once");
});

// The service worker tells every Fyr window, so the installed app gets it even when a browser tab is open too.
test("the open message goes to every window of the origin", () => {
  const wins = [{ url: "https://h:7443/?x", id: "a" }, { url: "https://other/", id: "b" }, { url: "https://h:7443/", id: "c" }];
  assert.deepEqual(openTargets(wins, "https://h:7443").map((w) => w.id), ["a", "c"]);
});

// V0.119: Settings → This device → Notifications: the row's words and whether its switch works.
import { pushRow, parsePushPrefs, FINISHED_DESC } from "../src/push.ts";
const PLAIN = "When a thread needs you: a question, an approval, a failure.";

test("the Notifications row per state", () => {
  assert.deepEqual(pushRow("off", null, false), { desc: PLAIN, error: false, disabled: false, on: false, test: false });
  assert.deepEqual(pushRow("on", null, false), { desc: PLAIN, error: false, disabled: false, on: true, test: true });
  assert.equal(pushRow("on", null, true).desc, "Sent to this device. It should arrive in a few seconds.");
  assert.deepEqual(pushRow("denied", null, false), { desc: "Blocked for this site. Allow them in the browser's site settings, then reload Fyr.", error: true, disabled: true, on: false, test: false });
  const ios = pushRow("unsupported", unsupportedReason({ secure: true, serviceWorker: true, pushManager: true, notification: true, ios: true, standalone: false }), false);
  assert.deepEqual(ios, { desc: "On an iPhone, add Fyr to the Home Screen first: Share, then Add to Home Screen.", error: false, disabled: true, on: false, test: false });
  assert.equal(pushRow("unsupported", null, false).desc, "Notifications are not available in this browser.");
});

test("V0.notification-polish: finished threads are a choice of the device, on until it says no", () => {
  assert.deepEqual(parsePushPrefs(null), { finished: true });
  assert.deepEqual(parsePushPrefs("not json"), { finished: true });
  assert.deepEqual(parsePushPrefs("{}"), { finished: true });
  assert.deepEqual(parsePushPrefs('{"finished":false}'), { finished: false });
  assert.deepEqual(parsePushPrefs('{"finished":true}'), { finished: true });
  assert.match(FINISHED_DESC, /finishes/);
});

test("V0.mac-notify: Fyr.app's window has no Web Push and still turns notifications on", () => {
  const wk: PushEnv = { secure: true, serviceWorker: false, pushManager: false, notification: false, ios: false, standalone: false };
  assert.match(unsupportedReason(wk) ?? "", /does not support/);
  assert.equal(unsupportedReason({ ...wk, native: true }), null);
});
