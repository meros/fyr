// V0.145: the desktop's thread history. Each thread the owner opens is one browser history entry, so Back and Forward
// (the mouse's side buttons, Alt+Left and Alt+Right, the browser's arrows) move between the threads opened. Pure but
// for the two helpers at the end that touch `history`.

/** A desktop history entry. It is also a phone Nav (nav.ts), so a window that narrows to a phone reads it. */
export interface DeskEntry {
  screen: { name: "thread"; id: string; tab?: string };
  sheet: null;
  /** the entry's place in Fyr's stack; tells a pop's direction */
  desk: number;
}

/** Who opened the thread: the owner (a click, a link, a notification, j/k, g1–g5, a new thread) or Fyr itself (the
 *  first thread on load, back and forward). */
export type OpenCause = "owner" | "auto";

export function isDeskEntry(s: unknown): s is DeskEntry {
  if (!s || typeof s !== "object") return false;
  const e = s as Partial<DeskEntry>;
  return typeof e.desk === "number" && e.screen?.name === "thread" && typeof e.screen.id === "string";
}

export function deskEntry(id: string, desk: number, tab?: string): DeskEntry {
  return { screen: tab ? { name: "thread", id, tab } : { name: "thread", id }, sheet: null, desk };
}

/** The URL of a thread: the current one with `open` (and `tab`) set; every other parameter (the mock's) stays. */
export function deskUrl(search: string, id: string, tab?: string): string {
  const p = new URLSearchParams(search);
  p.delete("dialog");
  p.set("open", id);
  if (tab) p.set("tab", tab);
  else p.delete("tab");
  return `/?${p.toString()}`;
}

/**
 * What opening thread `id` does to the history: the owner opening another thread adds an entry; the same thread again
 * adds none; Fyr opening a thread adds none. The page's first entry (no entry of ours yet) is replaced, so it is the
 * thread on screen.
 */
export function openStep(cur: unknown, id: string, cause: OpenCause): "push" | "replace" | "none" {
  if (!isDeskEntry(cur)) return "replace";
  if (cur.screen.id === id) return "none";
  return cause === "owner" ? "push" : "none";
}

/** The tab a deep link names for thread `id`, kept in the page's first entry. */
export function seedTab(search: string, id: string): string | undefined {
  const p = new URLSearchParams(search);
  return (p.get("open") === id && p.get("tab")) || undefined;
}

/** A tab picked in the open thread is written into its entry (no new entry), so Back to it shows that tab. */
export function tabStep(cur: unknown, id: string, tab: string): DeskEntry | null {
  if (!isDeskEntry(cur) || cur.screen.id !== id || cur.screen.tab === tab) return null;
  return deskEntry(id, cur.desk, tab);
}

/**
 * What a pop (Back or Forward) does on the desktop. `at` is the entry Fyr was on, `gone` says whether a thread is
 * closed (archived) or deleted. Another thread opens; a gone one is skipped in the same direction, except below the
 * first entry, which would leave Fyr: there the thread on screen stays. An entry that is not ours (a preview viewer's
 * own, the page before Fyr) does nothing.
 */
export function popStep(state: unknown, at: number, onScreen: string | null, gone: (id: string) => boolean): { open: string } | { skip: -1 | 1 } | null {
  if (!isDeskEntry(state)) return null;
  const id = state.screen.id;
  if (gone(id)) {
    const dir = state.desk < at ? -1 : 1;
    if (dir === -1 && state.desk === 0) return null;
    return { skip: dir };
  }
  if (id === onScreen) return null;
  return { open: id };
}

// ---- the two helpers that touch the browser ----

/** Record that thread `id` opened on the desktop (openStep says how). */
export function recordOpen(id: string, cause: OpenCause): void {
  if (typeof history === "undefined") return;
  const cur: unknown = history.state;
  const step = openStep(cur, id, cause);
  if (step === "none") {
    // V0.status-home: the thread the owner closed opens again in its own entry; its URL names it again.
    if (isDeskEntry(cur) && cur.screen.id === id && new URLSearchParams(location.search).get("open") !== id) history.replaceState(cur, "", deskUrl(location.search, id, cur.screen.tab));
    return;
  }
  if (step === "push") {
    history.pushState(deskEntry(id, isDeskEntry(cur) ? cur.desk + 1 : 0), "", deskUrl(location.search, id));
    return;
  }
  // The first entry keeps a deep link's tab (`?open=<id>&tab=pr`).
  const tab = seedTab(location.search, id);
  history.replaceState(deskEntry(id, 0, tab), "", deskUrl(location.search, id, tab));
}

/** Write the open thread's tab into its entry (desktop only: the phone's tab is in its own Nav). */
export function rememberTab(id: string, tab: string): void {
  if (typeof history === "undefined") return;
  const e = tabStep(history.state, id, tab);
  if (e) history.replaceState(e, "", deskUrl(location.search, id, tab));
}

/** V0.status-home: the URL with no thread open: `open` and `tab` gone, every other parameter (the mock's) kept. */
export function homeUrl(search: string): string {
  const p = new URLSearchParams(search);
  p.delete("open");
  p.delete("tab");
  p.delete("dialog");
  const q = p.toString();
  return q ? `/?${q}` : "/";
}

/** V0.status-home: the owner closed the thread. The entry stays (Back and Forward still reach the thread), only its URL
 *  says no thread, so a reload shows the status screen. */
export function leaveDeskThread(): void {
  if (typeof history === "undefined") return;
  history.replaceState(history.state, "", homeUrl(location.search));
}
