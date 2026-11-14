// Phone navigation as history entries (design/mobile/README.md, Layout and navigation). Pure.
// Each screen has a URL; a sheet adds an entry without changing the URL, so Back closes the sheet,
// then the thread. A thread opened from a deep link (a push) gets an inbox entry under it.

export type Screen = { name: "inbox" } | { name: "thread"; id: string; tab?: string } | { name: "new" } | { name: "search" } | { name: "project"; id: string };

export type SheetKind = "actions" | "snooze" | "thread-menu" | "details" | "menu" | "push" | "adopt" | "project" | "filter";

export interface Sheet {
  kind: SheetKind;
  /** the thread(s) the sheet acts on */
  ids?: string[];
  /** the project picker opens on New project from this thread (V0.30) */
  startNew?: boolean;
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
  const q = p.toString().replace(/=(&|$)/g, "$1");
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
  return INBOX;
}

/** The history to build on load: a deep link to anything but the inbox gets the inbox under it. */
export function initialEntries(search: string): Nav[] {
  const n = parseUrl(search);
  return n.screen.name === "inbox" ? [n] : [INBOX, n];
}

/** What Back does from here: close the sheet, else leave the screen for the inbox, else nothing (the app stays). */
export function backTarget(n: Nav): Nav | null {
  if (n.sheet) return { ...n, sheet: null };
  if (n.screen.name !== "inbox") return INBOX;
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
