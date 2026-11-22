// Per-device preferences in localStorage, with change listeners. "Show tool details" (V0.21).

/** The browser's storage, or null where there is none (a test, a blocked page): reading the global may itself throw. */
export function browserStorage(): Storage | null {
  try {
    return typeof localStorage !== "undefined" ? localStorage : null;
  } catch {
    return null;
  }
}

/** One stored value, or null when none is kept or storage is blocked. The one way the client reads localStorage. */
export function storedGet(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

/** Keep a value; null forgets it. Storage blocked: the choice lasts this page only. */
export function storedSet(key: string, value: string | null): void {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    /* blocked: this page only */
  }
}

const DETAILS_KEY = "fyr.toolDetails";

function read(): boolean {
  return storedGet(DETAILS_KEY) === "1";
}

let details = typeof localStorage === "undefined" ? false : read();
const listeners = new Set<(on: boolean) => void>();

export function toolDetails(): boolean {
  return details;
}

export function setToolDetails(on: boolean): void {
  details = on;
  storedSet(DETAILS_KEY, on ? "1" : null);
  listeners.forEach((f) => f(on));
}

export function onToolDetails(f: (on: boolean) => void): () => void {
  listeners.add(f);
  return () => listeners.delete(f);
}

// ---- the thread list (SPEC 0.5 V0.40) ----

/** Below this window width the list starts hidden, so the open thread keeps its room. */
export const LIST_MIN_WIDTH = 1100;
const LIST_KEY = "fyr.list";

/** The owner's choice wins; without one, the width decides; with no thread open the list always shows. */
export function listShown(pref: boolean | null, width: number, threadOpen: boolean): boolean {
  if (!threadOpen) return true;
  return pref ?? width >= LIST_MIN_WIDTH;
}

export function listPref(): boolean | null {
  const v = storedGet(LIST_KEY);
  return v === "1" ? true : v === "0" ? false : null;
}

export function setListPref(on: boolean): void {
  storedSet(LIST_KEY, on ? "1" : "0");
}

// ---- the thread list's width (SPEC V0.184) ----

export const LIST_WIDTH_DEFAULT = 372;
export const LIST_WIDTH_MIN = 280;
export const LIST_WIDTH_MAX = 640;
/** What the list leaves the conversation, when the window has room for both. */
export const CONVERSATION_MIN_WIDTH = 480;
const RAIL_WIDTH = 76;
const LIST_WIDTH_STEP = 16;
const LIST_WIDTH_BIG_STEP = 64;
// A width names no thread, project or host, so this key is the browser's alone (decision 0031 slice 3 is about keys
// that name a record).
const LIST_WIDTH_KEY = "fyr.listWidth";

/** The widest the list may be in this window. Never under its minimum: where both do not fit, the conversation gives way. */
export function listWidthMax(win: number): number {
  return Math.max(LIST_WIDTH_MIN, Math.min(LIST_WIDTH_MAX, win - RAIL_WIDTH - CONVERSATION_MIN_WIDTH));
}

export function clampListWidth(w: number, win: number): number {
  return Math.min(listWidthMax(win), Math.max(LIST_WIDTH_MIN, Math.round(w)));
}

export function parseListWidth(v: string | null): number | null {
  if (!v || !/^\d+$/.test(v)) return null;
  const n = Number(v);
  return n > 0 ? n : null;
}

/** The width a key on the handle asks for; null when the key is not the handle's. */
export function listWidthForKey(key: string, shift: boolean, w: number, win: number): number | null {
  const step = shift ? LIST_WIDTH_BIG_STEP : LIST_WIDTH_STEP;
  if (key === "ArrowRight") return clampListWidth(w + step, win);
  if (key === "ArrowLeft") return clampListWidth(w - step, win);
  if (key === "Home") return LIST_WIDTH_MIN;
  if (key === "End") return listWidthMax(win);
  return null;
}

/** The width the owner set in this browser, as set: the window limits what is applied, not what is kept. */
export function listWidthPref(): number | null {
  return parseListWidth(storedGet(LIST_WIDTH_KEY));
}

/** null goes back to the default. */
export function setListWidthPref(w: number | null): void {
  storedSet(LIST_WIDTH_KEY, w === null ? null : String(w));
}

// ---- the Changes board (SPEC 0.5 V0.78, #54) ----

const BOARD_SETTLED_KEY = "fyr.board.settled";

/** Show the changes of settled and snoozed threads on the board; off by default. */
export function boardSettledPref(): boolean {
  return storedGet(BOARD_SETTLED_KEY) === "1";
}

export function setBoardSettledPref(on: boolean): void {
  storedSet(BOARD_SETTLED_KEY, on ? "1" : null);
}
