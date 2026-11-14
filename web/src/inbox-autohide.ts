// SPEC 0.5 V0.48: the Settled section closes by itself after 30 s, unless it is the selected
// section in the rail. Pure: the app holds the deadline and runs one timer.
import type { SectionKey } from "./store.ts";

export const AUTO_HIDE_MS = 30_000;

export function autoHide(s: { open: boolean; deadline: number | null; active: SectionKey | null }, now: number): { close: boolean; wait: number | null } {
  if (!s.open || s.deadline === null || s.active === "settled") return { close: false, wait: null };
  const left = s.deadline - now;
  return left <= 0 ? { close: true, wait: 0 } : { close: false, wait: left };
}
