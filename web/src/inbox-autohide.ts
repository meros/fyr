// SPEC 0.5 V0.48, V0.196: Snoozed and Settled, opened from their header, close by themselves after 10 s, unless the
// current thread is in them. Pure: the app holds the deadlines and runs one timer.
import type { SectionKey } from "./store.ts";

export const AUTO_HIDE_MS = 10_000;

export const AUTO_HIDE_SECTIONS: readonly SectionKey[] = ["snoozed", "settled"];

/** The sections running down, with their deadline (ms). One named in the rail or the strip has none: it stays open. */
export type HideAt = Partial<Record<SectionKey, number>>;

/** What is due now: the sections to close, else how long until the next one is (null: nothing is running down). */
export function hideStep(s: { at: HideAt; open(k: SectionKey): boolean; active: SectionKey | null }, now: number): { close: SectionKey[]; wait: number | null } {
  const close: SectionKey[] = [];
  let wait: number | null = null;
  for (const k of AUTO_HIDE_SECTIONS) {
    const at = s.at[k];
    if (at === undefined || !s.open(k) || s.active === k) continue;
    const left = at - now;
    if (left <= 0) close.push(k);
    else wait = wait === null ? left : Math.min(wait, left);
  }
  return { close, wait };
}

/** The deadlines the headers show as a clock: the current thread's section is not running down. */
export function hideClocks(at: HideAt, active: SectionKey | null): HideAt {
  const out = { ...at };
  if (active) delete out[active];
  return out;
}
