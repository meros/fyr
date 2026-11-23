// V0.automation-folds: a settings group that folds to its header line. The folds are remembered per device, keyed by
// the group and its host, so a group the owner opened stays open and one they closed stays closed.

import { flags } from "./decode.ts";
import { storedJson, storedSet } from "./prefs.ts";

const SETTINGS_FOLD_KEY = "fyr.settingsFold";

/** Whether the group `id` is open: what the device remembers, else its default. */
export function foldOpen(folds: Record<string, boolean>, id: string, byDefault: boolean): boolean {
  return folds[id] ?? byDefault;
}

/** What the device stored: its boolean fields; anything else counts as nothing. */
function read(): Record<string, boolean> {
  return storedJson(SETTINGS_FOLD_KEY, flags, {});
}

export function readFold(id: string, byDefault: boolean): boolean {
  return foldOpen(read(), id, byDefault);
}

export function writeFold(id: string, open: boolean): void {
  storedSet(SETTINGS_FOLD_KEY, JSON.stringify({ ...read(), [id]: open }));
}
