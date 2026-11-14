// Presence (SPEC 0.5 V0.20): the host does not push for a thread the user
// is watching, so the client reports the thread it shows while visible.

/** The thread to report: the open one while the page is visible, else "". */
export function presenceTarget(openId: string | null, visibility: DocumentVisibilityState | string): string {
  return openId && visibility === "visible" ? openId : "";
}

export interface BeatState {
  last: string;
  at: number;
}

/** Beat at once on a change, and every 20 s while a thread is shown. */
export function shouldBeat(s: BeatState, target: string, now: number): boolean {
  if (target !== s.last) return true;
  return target !== "" && now - s.at >= 20_000;
}

/** A stable id for this browser, so beats from two devices do not mix. */
export function deviceId(): string {
  try {
    let id = localStorage.getItem("fyr.device");
    if (!id) {
      id = Math.random().toString(36).slice(2) + Date.now().toString(36);
      localStorage.setItem("fyr.device", id);
    }
    return id;
  } catch {
    return "nostorage";
  }
}
