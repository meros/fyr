// SPEC V0.157, decision 0030 3.4: a device that was revoked (or whose session ended) must land on the signed-out
// page, not sit on "offline". The page's stream fails when the host refuses it, and the client asks once whether
// that is why: an /api/state answer of 401 with signedOut reloads the page, which the front then answers with the
// sign-in page. Reloading is rate limited so a page that cannot be signed in does not reload in a loop.

import { frontPath } from "./urls.ts";

/** Whether a 401 body is the front's "this device is not signed in". */
export function isSignedOut(status: number, body: unknown): boolean {
  return status === 401 && typeof body === "object" && body !== null && (body as { signedOut?: unknown }).signedOut === true;
}

/** Reload at most once in 10 s. */
export function mayReload(last: number | null, now: number): boolean {
  return last === null || !(now - last < 10_000);
}

const KEY = "fyr.signedOutReload";

/** Ask whether the host refuses this device; if it says signed out, reload (once in 10 s). */
export async function reloadIfSignedOut(): Promise<boolean> {
  try {
    const r = await fetch(frontPath("/state"), { headers: { Accept: "application/json" } });
    if (r.status !== 401) return false;
    if (!isSignedOut(r.status, await r.json().catch(() => null))) return false;
    let last: number | null = null;
    try {
      last = Number(sessionStorage.getItem(KEY)) || null;
      if (!mayReload(last, Date.now())) return false;
      sessionStorage.setItem(KEY, String(Date.now()));
    } catch {
      /* no storage: reload anyway, once per call */
    }
    location.reload();
    return true;
  } catch {
    return false;
  }
}
