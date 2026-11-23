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

test("U8: settle or archive of the open thread returns to the inbox, unless it stays open", async () => {
  const { leavesThread } = await import("../src/nav.ts");
  assert.equal(leavesThread({ running: true }), true, "V0.203: Settle always settles");
  assert.equal(leavesThread({ pr: { state: "OPEN" } }), true);
  assert.equal(leavesThread(undefined), false);
});

// SPEC 0.5 V0.50: Settings has its own URL; Back leaves it for the inbox.
test("settings screen url", () => {
  const n = { screen: { name: "settings" as const }, sheet: null };
  assert.equal(urlFor(n, "?mock=rail"), "/?mock=rail&settings");
  assert.deepEqual(parseUrl("?settings"), n);
  assert.deepEqual(backTarget(n), INBOX);
});

// V0.119: Settings is a nav screen and a page screen per page and sub-page; Back goes one level up.
test("settings pages to and from the URL", () => {
  const nav = { screen: { name: "settings" as const }, sheet: null };
  const prov = { screen: { name: "settings" as const, page: "providers" as const }, sheet: null };
  const one = { screen: { name: "settings" as const, page: "providers" as const, sub: "work" }, sheet: null };
  assert.equal(urlFor(nav, "?mock=rail"), "/?mock=rail&settings", "?settings alone is the nav");
  assert.equal(urlFor(prov), "/?settings=providers");
  assert.equal(urlFor(one), "/?settings=providers/work");
  assert.equal(urlFor({ screen: { name: "settings", page: "providers", sub: "new" }, sheet: null }), "/?settings=providers/new");
  assert.equal(urlFor({ screen: { name: "settings", page: "device" }, sheet: null }), "/?settings=device");
  assert.deepEqual(parseUrl("?settings"), nav);
  assert.deepEqual(parseUrl("?settings="), nav);
  assert.deepEqual(parseUrl("?settings=providers"), prov);
  assert.deepEqual(parseUrl("?settings=providers/work"), one);
  assert.deepEqual(parseUrl("?settings=providers%2Fwork"), one);
  assert.deepEqual(parseUrl("?mock=rail&settings=nonsense"), prov, "an unknown page falls back to providers");
  assert.deepEqual(parseUrl("?project=p_fyr"), { screen: { name: "project", id: "p_fyr" }, sheet: null }, "?project= is unchanged");
  assert.equal(urlFor({ screen: { name: "project", id: "p_fyr" }, sheet: null }), "/?project=p_fyr");
});

test("a deep link into settings builds every level under it, so Back goes one level up", () => {
  const e = initialEntries("?settings=providers/work");
  assert.deepEqual(
    e.map((n) => n.screen),
    [{ name: "inbox" }, { name: "settings" }, { name: "settings", page: "providers" }, { name: "settings", page: "providers", sub: "work" }],
  );
  assert.deepEqual(backTarget({ screen: { name: "settings", page: "providers", sub: "work" }, sheet: null }), { screen: { name: "settings", page: "providers" }, sheet: null });
  assert.deepEqual(backTarget({ screen: { name: "settings", page: "device" }, sheet: null }), { screen: { name: "settings" }, sheet: null });
  assert.deepEqual(backTarget({ screen: { name: "settings" }, sheet: null }), INBOX);
});

import { listenPopState, seedHistory, type Nav } from "../src/nav.ts";

function fakeWindow() {
  const ls = new Set<(e: Event) => void>();
  return {
    ls,
    addEventListener: (_: string, f: (e: Event) => void) => void ls.add(f),
    removeEventListener: (_: string, f: (e: Event) => void) => void ls.delete(f),
    pop(state: unknown, extra: object = {}) {
      for (const f of [...ls]) f({ state, ...extra } as unknown as Event);
    },
  };
}

test("seedHistory: a deep link gets the inbox under it, and the entry it lands on is returned", () => {
  const calls: string[] = [];
  const h = { replaceState: (s: unknown) => calls.push(`replace ${(s as Nav).screen.name}`), pushState: (s: unknown) => calls.push(`push ${(s as Nav).screen.name}`) };
  const at = seedHistory(h, "?open=t1");
  assert.equal(calls[0], "replace inbox");
  assert.deepEqual(calls, ["replace inbox", "push thread"]);
  assert.equal(at.screen.name, "thread");
});

test("listenPopState: after the phone layout leaves and comes back the listener is there again", () => {
  const w = fakeWindow();
  const seen: string[] = [];
  const io = { search: () => "", current: () => ({ screen: { name: "inbox" }, sheet: null }) as Nav, set: (n: Nav) => seen.push(n.screen.name), slide: (f: () => void) => f() };
  let off = listenPopState(w as never, io);
  w.pop({ screen: { name: "new" }, sheet: null });
  off(); // phone -> desktop
  assert.equal(w.ls.size, 0);
  w.pop({ screen: { name: "search" }, sheet: null });
  off = listenPopState(w as never, io); // desktop -> phone
  w.pop({ screen: { name: "status" }, sheet: null });
  assert.deepEqual(seen, ["new", "status"]);
  off();
});

test("listenPopState: the back slide plays only when the screen changed and the system did not slide already", () => {
  const w = fakeWindow();
  const log: string[] = [];
  const cur = { screen: { name: "thread", id: "a" }, sheet: null } as Nav;
  listenPopState(w as never, { search: () => "", current: () => cur, set: (n) => log.push(`set ${n.screen.name}`), slide: (f) => (log.push("slide"), f()) });
  w.pop({ screen: { name: "inbox" }, sheet: null });
  w.pop({ screen: { name: "inbox" }, sheet: null }, { hasUAVisualTransition: true });
  w.pop({ screen: { name: "thread", id: "a" }, sheet: { kind: "actions" } });
  assert.deepEqual(log, ["slide", "set inbox", "set inbox", "set thread"]);
});
