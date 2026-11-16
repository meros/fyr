// Per-device preferences in localStorage, with change listeners. "Show tool details" (V0.21).

const DETAILS_KEY = "fyr.toolDetails";

function read(): boolean {
  try {
    return localStorage.getItem(DETAILS_KEY) === "1";
  } catch {
    return false;
  }
}

let details = typeof localStorage === "undefined" ? false : read();
const listeners = new Set<(on: boolean) => void>();

export function toolDetails(): boolean {
  return details;
}

export function setToolDetails(on: boolean): void {
  details = on;
  try {
    if (on) localStorage.setItem(DETAILS_KEY, "1");
    else localStorage.removeItem(DETAILS_KEY);
  } catch {
    /* storage blocked: this page only */
  }
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
  try {
    const v = localStorage.getItem(LIST_KEY);
    return v === "1" ? true : v === "0" ? false : null;
  } catch {
    return null;
  }
}

export function setListPref(on: boolean): void {
  try {
    localStorage.setItem(LIST_KEY, on ? "1" : "0");
  } catch {
    /* the choice lasts this session */
  }
}

// ---- the Changes board (SPEC 0.5 V0.78, #54) ----

const BOARD_SETTLED_KEY = "fyr.board.settled";

/** Show the changes of settled and snoozed threads on the board; off by default. */
export function boardSettledPref(): boolean {
  try {
    return localStorage.getItem(BOARD_SETTLED_KEY) === "1";
  } catch {
    return false;
  }
}

export function setBoardSettledPref(on: boolean): void {
  try {
    if (on) localStorage.setItem(BOARD_SETTLED_KEY, "1");
    else localStorage.removeItem(BOARD_SETTLED_KEY);
  } catch {
    /* the choice lasts this session */
  }
}
