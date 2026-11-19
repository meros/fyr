// V0.automation-folds: a settings group that folds to its header line. The folds are remembered per device, keyed by
// the group and its host, so a group the owner opened stays open and one they closed stays closed.

export const SETTINGS_FOLD_KEY = "fyr.settingsFold";

/** What the device stored; anything else counts as nothing. */
export function parseFolds(raw: string | null): Record<string, boolean> {
  if (!raw) return {};
  try {
    const v = JSON.parse(raw) as unknown;
    if (!v || typeof v !== "object" || Array.isArray(v)) return {};
    const out: Record<string, boolean> = {};
    for (const [k, b] of Object.entries(v)) if (typeof b === "boolean") out[k] = b;
    return out;
  } catch {
    return {};
  }
}

/** Whether the group `id` is open: what the device remembers, else its default. */
export function foldOpen(folds: Record<string, boolean>, id: string, byDefault: boolean): boolean {
  return folds[id] ?? byDefault;
}

function read(): Record<string, boolean> {
  try {
    return parseFolds(localStorage.getItem(SETTINGS_FOLD_KEY));
  } catch {
    return {};
  }
}

export function readFold(id: string, byDefault: boolean): boolean {
  return foldOpen(read(), id, byDefault);
}

export function writeFold(id: string, open: boolean): void {
  try {
    localStorage.setItem(SETTINGS_FOLD_KEY, JSON.stringify({ ...read(), [id]: open }));
  } catch {
    // a private window: the fold lasts until the page closes
  }
}
