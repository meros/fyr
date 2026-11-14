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
