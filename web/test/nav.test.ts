import { test } from "node:test";
import assert from "node:assert/strict";
import { INBOX, backTarget, initialEntries, navFromHistory, parseUrl, showsBottomBar, urlFor } from "../src/nav.ts";

test("each screen has a URL and keeps the mock switch", () => {
  assert.equal(urlFor(INBOX), "/");
  assert.equal(urlFor({ screen: { name: "thread", id: "t_1" }, sheet: null }), "/?open=t_1");
  assert.equal(urlFor({ screen: { name: "thread", id: "t_1", tab: "changes" }, sheet: null }), "/?open=t_1&tab=changes");
  assert.equal(urlFor({ screen: { name: "new" }, sheet: null }), "/?new");
  assert.equal(urlFor({ screen: { name: "search" }, sheet: null }, "?mock=rail&open=x"), "/?mock=rail&search");
  assert.equal(urlFor({ screen: { name: "thread", id: "t 1" }, sheet: { kind: "snooze", ids: ["t 1"] } }), "/?open=t+1", "a sheet does not change the URL");
  assert.deepEqual(parseUrl("?open=t_1&tab=pr"), { screen: { name: "thread", id: "t_1", tab: "pr" }, sheet: null });
  assert.deepEqual(parseUrl("?new"), { screen: { name: "new" }, sheet: null });
  assert.deepEqual(parseUrl("?mock=1"), INBOX);
});

test("a deep link from a push gets the inbox under it", () => {
  const e = initialEntries("?open=t_9");
  assert.equal(e.length, 2);
  assert.deepEqual(e[0], INBOX);
  assert.equal(e[1].screen.name, "thread");
  assert.deepEqual(initialEntries("?mock=rail"), [INBOX]);
});

test("Back closes the sheet, then the screen, then stays in the app", () => {
  const sheet = { screen: { name: "thread" as const, id: "t" }, sheet: { kind: "snooze" as const, ids: ["t"] } };
  const thread = backTarget(sheet)!;
  assert.deepEqual(thread, { screen: { name: "thread", id: "t" }, sheet: null });
  assert.deepEqual(backTarget(thread), INBOX);
  assert.equal(backTarget(INBOX), null);
});

test("bottom bar on Inbox and Search only, never while selecting", () => {
  assert.equal(showsBottomBar(INBOX, false), true);
  assert.equal(showsBottomBar({ screen: { name: "search" }, sheet: null }, false), true);
  assert.equal(showsBottomBar({ screen: { name: "thread", id: "t" }, sheet: null }, false), false);
  assert.equal(showsBottomBar({ screen: { name: "new" }, sheet: null }, false), false);
  assert.equal(showsBottomBar(INBOX, true), false);
  assert.deepEqual(navFromHistory(null, "?open=x"), parseUrl("?open=x"));
  assert.deepEqual(navFromHistory(INBOX, "?open=x"), INBOX);
});
