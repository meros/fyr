// Presence (SPEC 0.5 V0.20): the host does not push for a thread the user
// is watching, so the client reports the thread it shows while visible.

import { browserStorage, storedGet, storedSet } from "./prefs.ts";

/** The thread to report: the open one while the page is visible, else "". */
export function presenceTarget(openId: string | null, visibility: DocumentVisibilityState | string): string {
  return openId && visibility === "visible" ? openId : "";
}

export interface BeatState {
  last: string;
  visible: boolean;
  at: number;
}

/**
 * Beat at once on a change of the thread shown or of the page's visibility, and every 20 s while the page is visible,
 * on the inbox too: the front counts the owner away after 10 min without a beat from any device and then batches done
 * and blocked pushes (V0.push-summary-batch). A hidden page sends nothing more.
 */
export function shouldBeat(s: BeatState, target: string, visible: boolean, now: number): boolean {
  if (target !== s.last || visible !== s.visible) return true;
  return visible && now - s.at >= 20_000;
}

/** A stable id for this browser, so beats from two devices do not mix. */
export function deviceId(): string {
  if (!browserStorage()) return "nostorage";
  let id = storedGet("fyr.device");
  if (!id) {
    id = Math.random().toString(36).slice(2) + Date.now().toString(36);
    storedSet("fyr.device", id);
  }
  return id;
}
