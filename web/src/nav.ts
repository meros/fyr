// Phone navigation as history entries (design/mobile/README.md, Layout and navigation). Pure.
// Each screen has a URL; a sheet adds an entry without changing the URL, so Back closes the sheet,
// then the thread. A thread opened from a deep link (a push) gets an inbox entry under it.

import { parsePage, pageUrl, type PageId } from "./settings-nav.ts";

/** V0.119: Settings is the nav screen (no page), a page, or a page's sub-page (a provider, Add provider, a project). */
export type Screen = { name: "inbox" } | { name: "thread"; id: string; tab?: string } | { name: "new" } | { name: "search" } | { name: "project"; id: string } | { name: "settings"; page?: PageId; sub?: string; host?: string; /** V0.settings-ia: the section of a host's page a link lands on */ focus?: string } | { name: "changes" } | { name: "status" };

export type SheetKind = "actions" | "snooze" | "thread-menu" | "details" | "menu" | "project" | "filter" | "handover" | "load";

export interface Sheet {
  kind: SheetKind;
  /** the thread(s) the sheet acts on */
  ids?: string[];
}

export interface Nav {
  screen: Screen;
  sheet: Sheet | null;
}

export const INBOX: Nav = { screen: { name: "inbox" }, sheet: null };

/** Parameters that survive navigation (the mock switch). */
const KEEP = ["mock"];

export function urlFor(n: Nav, currentSearch = ""): string {
  const cur = new URLSearchParams(currentSearch);
  const p = new URLSearchParams();
  for (const k of KEEP) {
    const v = cur.get(k);
    if (v !== null) p.set(k, v);
  }
  const s = n.screen;
  if (s.name === "thread") {
    p.set("open", s.id);
    if (s.tab) p.set("tab", s.tab);
  } else if (s.name === "new") p.set("new", "");
  else if (s.name === "search") p.set("search", "");
  else if (s.name === "project") p.set("project", s.id);
  else if (s.name === "settings") p.set("settings", s.page ? pageUrl({ page: s.page, sub: s.sub, host: s.host, focus: s.focus }) : "");
  else if (s.name === "changes") p.set("changes", ""); // V0.64
  else if (s.name === "status") p.set("status", ""); // V0.status-home
  const q = p.toString().replace(/=(&|$)/g, "$1").replace(/(settings=[^&]*)%2F/, "$1/");
  return q ? `/?${q}` : "/";
}

export function parseUrl(search: string): Nav {
  const p = new URLSearchParams(search);
  const open = p.get("open");
  if (open) return { screen: { name: "thread", id: open, tab: p.get("tab") ?? undefined }, sheet: null };
  const project = p.get("project");
  if (project) return { screen: { name: "project", id: project }, sheet: null };
  if (p.has("new")) return { screen: { name: "new" }, sheet: null };
  if (p.has("search")) return { screen: { name: "search" }, sheet: null };
  if (p.has("settings")) {
    const v = p.get("settings") ?? "";
    if (!v) return { screen: { name: "settings" }, sheet: null };
    const r = parsePage(v);
    return { screen: { name: "settings", page: r.page, ...(r.sub ? { sub: r.sub } : {}), ...(r.host ? { host: r.host } : {}), ...(r.focus ? { focus: r.focus } : {}) }, sheet: null };
  }
  if (p.has("changes")) return { screen: { name: "changes" }, sheet: null };
  if (p.has("status")) return { screen: { name: "status" }, sheet: null };
  return INBOX;
}

/** The history to build on load: a deep link to anything but the inbox gets the inbox under it; a settings page gets
 *  every level above it too (V0.119), so Back goes one level up. */
export function initialEntries(search: string): Nav[] {
  const n = parseUrl(search);
  if (n.screen.name === "inbox") return [n];
  const up: Nav[] = [];
  for (let x = backTarget(n); x && x.screen.name !== "inbox"; x = backTarget(x)) up.unshift(x);
  return [INBOX, ...up, n];
}

/** What Back does from here: close the sheet, else one settings level up, else leave the screen for the inbox, else
 *  nothing (the app stays). */
export function backTarget(n: Nav): Nav | null {
  if (n.sheet) return { ...n, sheet: null };
  const s = n.screen;
  if (s.name === "settings" && s.sub && s.page) return { screen: { name: "settings", page: s.page }, sheet: null };
  if (s.name === "settings" && s.page) return { screen: { name: "settings" }, sheet: null };
  if (s.name !== "inbox") return INBOX;
  return null;
}

/**
 * U8 (phone): settling or archiving the open thread returns to the inbox, through Back so the thread entry
 * is popped. V0.203: Settle always settles, so both always leave.
 */
export function leavesThread(t: unknown): boolean {
  return !!t;
}

/** The bottom bar shows on Inbox and Search only. */
export function showsBottomBar(n: Nav, selecting: boolean): boolean {
  return !selecting && (n.screen.name === "inbox" || n.screen.name === "search");
}

/** A history entry's state, or the URL's when the entry has none (first load, external link). */
export function navFromHistory(state: unknown, search: string): Nav {
  if (!state || typeof state !== "object" || !("screen" in state)) return parseUrl(search);
  // A layer's entry (back-layer.ts) is its screen's state with a mark: the mark is the entry's, not the screen's.
  const { screen, sheet } = state as Nav;
  return { screen, sheet: sheet ?? null };
}

/** The browser's history as far as the phone's navigation uses it. */
interface HistoryLike {
  replaceState(state: unknown, title: string, url: string): void;
  pushState(state: unknown, title: string, url: string): void;
}

/** Build the history once: the entries of a deep link (a push) get the inbox under it, so Back never leaves the
 *  app. Returns the entry the page is on. `state` is the entry's state as the page loads: a reload over a layer
 *  (V0.dialogs-review (4), back-layer.ts: the folder picker, a confirmation) is on that layer's entry, whose screen
 *  already has its entries under it, so only the layer's mark goes and nothing is pushed on top of them. The caller
 *  then steps back off that entry, which is now a second one of the screen (back-layer.ts `seedPhoneHistory`). */
export function seedHistory(h: HistoryLike, search: string, state?: unknown): Nav {
  const entries = initialEntries(search);
  const last = entries[entries.length - 1];
  if (state && typeof state === "object" && "backLayer" in state && "screen" in state) {
    h.replaceState(last, "", urlFor(last, search));
    return last;
  }
  h.replaceState(entries[0], "", urlFor(entries[0], search));
  for (const e of entries.slice(1)) h.pushState(e, "", urlFor(e, search));
  return last;
}

/** Back and Forward on the phone: the entry the browser went to is the screen. `slide` plays the back slide when the
 *  screen changes, unless the system's own swipe back already did (V0.native-feel: a second slide would replay it).
 *  Returns the listener's remover: the listener is added on every call, so a front that left the phone layout and came
 *  back has one again. */
export function listenPopState(
  target: Pick<Window, "addEventListener" | "removeEventListener">,
  io: { search(): string; current(): Nav; set(n: Nav): void; slide(apply: () => void): void },
): () => void {
  const onPop = (e: Event) => {
    const ev = e as PopStateEvent & { hasUAVisualTransition?: boolean };
    const n = navFromHistory(ev.state, io.search());
    if (!ev.hasUAVisualTransition && JSON.stringify(n.screen) !== JSON.stringify(io.current().screen)) io.slide(() => io.set(n));
    else io.set(n);
  };
  target.addEventListener("popstate", onPop);
  return () => target.removeEventListener("popstate", onPop);
}
