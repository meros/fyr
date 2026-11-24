// The phone's navigation history: go, replace and back (its screens and sheets are history entries), and leaving the
// open thread after it settled, archived or snoozed (U8). A hook of the App in app.tsx; it takes and returns plain
// objects.

import { useRef } from "preact/hooks";
import { backTarget, leavesThread, urlFor, type Nav } from "./nav.ts";
import { navTransition } from "./native.ts";
import type { useAppState } from "./app-state.tsx";
import { layers } from "./back-layer.ts";

/** V0.dialogs-review (4): a navigation closes the open layers and waits until their entries are out
 *  (back-layer.ts). With no layer it runs at once, as before. */
const settled = (fn: () => void) => {
  const ls = layers();
  if (ls) ls.settled(fn);
  else fn();
};

export function useNavHistory(c: Pick<ReturnType<typeof useAppState>, "threadsRef" | "reopenRef" | "navGoRef" | "nav" | "setNav">) {
  const { threadsRef, reopenRef, navGoRef, nav, setNav } = c;
  // ---- phone navigation ----
  const navRef = useRef(nav);
  navRef.current = nav;
  const navGo = (n: Nav) =>
    settled(() => {
      history.pushState(n, "", urlFor(n, location.search));
      const at = navRef.current;
      // A sheet opens over the screen; only a new screen slides (V0.32).
      if (n.screen !== at.screen && JSON.stringify(n.screen) !== JSON.stringify(at.screen)) navTransition("fwd", () => setNav(n));
      else setNav(n);
    });
  navGoRef.current = navGo;
  /** U8, V0.app-shell-tidy: after the host took a settle, archive or snooze of the open thread on a phone, go back to
   *  the inbox (pop, not push). */
  const leaveOpenThread = (ids: string[]) => {
    const s = navRef.current.screen;
    if (s.name !== "thread" || ids.length !== 1 || ids[0] !== s.id) return;
    if (!leavesThread(threadsRef.current[s.id])) return;
    reopenRef.current = s.id;
    navBack();
  };
  const navReplace = (n: Nav) =>
    settled(() => {
      history.replaceState(n, "", urlFor(n, location.search));
      setNav(n);
    });
  /** Back from a control of the page (the header's arrow or ×, a sheet's close): it leaves the screen or the sheet, so
   *  the layers over it (V0.dialogs-review (4): the project list in New thread) go in the same step. The system's
   *  Back is the browser's own and closes one layer at a time (back-layer.ts). */
  const navBack = () => {
    if (history.state && typeof history.state === "object" && "screen" in history.state && history.length > 1) {
      const ls = layers();
      if (ls) ls.leave();
      else history.go(-1);
    } else {
      const t = backTarget(nav);
      if (t) navReplace(t);
    }
  };

  return { navGo, navRef, leaveOpenThread, navReplace, navBack };
}
