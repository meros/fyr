// Phone navigation as history entries (design/mobile/README.md, Layout and navigation). Pure.
// Each screen has a URL; a sheet adds an entry without changing the URL, so Back closes the sheet,
// then the thread. A thread opened from a deep link (a push) gets an inbox entry under it.

import { parsePage, pageUrl, type PageId } from "./settings-nav.ts";

/** V0.119: Settings is the nav screen (no page), a page, or a page's sub-page (a provider, Add provider, a project). */
export type Screen = { name: "inbox" } | { name: "thread"; id: string; tab?: string } | { name: "new" } | { name: "search" } | { name: "project"; id: string } | { name: "settings"; page?: PageId; sub?: string; host?: string } | { name: "changes" };

export type SheetKind = "actions" | "snooze" | "thread-menu" | "details" | "menu" | "adopt" | "project" | "filter" | "handover" | "load";

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
  else if (s.name === "settings") p.set("settings", s.page ? pageUrl({ page: s.page, sub: s.sub, host: s.host }) : "");
  else if (s.name === "changes") p.set("changes", ""); // V0.64
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
    return { screen: { name: "settings", page: r.page, ...(r.sub ? { sub: r.sub } : {}), ...(r.host ? { host: r.host } : {}) }, sheet: null };
  }
  if (p.has("changes")) return { screen: { name: "changes" }, sheet: null };
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
 * is popped. "Settle when done" (a run is going) and "settle when merged" (an open PR) keep the thread open.
 */
export function leavesThread(action: "settle" | "archive", t: { running?: boolean; pr?: { state?: string } | null } | undefined): boolean {
  if (!t) return false;
  if (action === "archive") return true;
  if (t.running) return false;
  if (t.pr && (t.pr.state ?? "OPEN") === "OPEN") return false;
  return true;
}

/** The bottom bar shows on Inbox and Search only. */
export function showsBottomBar(n: Nav, selecting: boolean): boolean {
  return !selecting && (n.screen.name === "inbox" || n.screen.name === "search");
}

/** A history entry's state, or the URL's when the entry has none (first load, external link). */
export function navFromHistory(state: unknown, search: string): Nav {
  if (state && typeof state === "object" && "screen" in state) return state as Nav;
  return parseUrl(search);
}
