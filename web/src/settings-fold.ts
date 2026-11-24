// V0.automation-folds: a settings group that folds to its header line. The folds are remembered per device, keyed by
// the group and its host, so a group the owner opened stays open and one they closed stays closed.

import { flags } from "./decode.ts";
import { storedJson, storedSet } from "./prefs.ts";

const SETTINGS_FOLD_KEY = "fyr.settingsFold";

/** What the device stored: its boolean fields; anything else counts as nothing. */
function read(): Record<string, boolean> {
  return storedJson(SETTINGS_FOLD_KEY, flags, {});
}

/** What the device remembers for the group `id`; undefined when the owner never opened or closed it here. */
export function storedFold(id: string): boolean | undefined {
  return read()[id];
}

/** V0.settings-ia: whether a fold is open now. In this order: what the owner pressed on this visit; `force` (a link
 *  landed on it, or it reports something wrong now: it opens whatever the device remembers, and one press closes it);
 *  what the device remembers; the default, which may change while the page is up (a report that arrives late). */
export function foldNow(a: { touched: boolean | null; force?: boolean; stored: boolean | undefined; byDefault: boolean }): boolean {
  if (a.touched !== null) return a.touched;
  if (a.force) return true;
  return a.stored ?? a.byDefault;
}

export function writeFold(id: string, open: boolean): void {
  storedSet(SETTINGS_FOLD_KEY, JSON.stringify({ ...read(), [id]: open }));
}
