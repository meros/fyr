// Navigation history of the phone (its screens are history entries): go, replace, back, and leaving a settled thread.
//
// Reads:   data.threadsRef; route.nav, setNav, navGoRef, reopenRef.
// Returns: navGo, navReplace, navBack (push, replace, pop an entry), navRef (the entry now), leaveOpenThread (U8: after
//          a settle or archive of the open thread, Back to the inbox).
// Phone only in effect: the desktop has its own history (desk-history.ts, see app-nav.tsx).

import { useRef } from "preact/hooks";
import { backTarget, leavesThread, urlFor, type Nav } from "./nav.ts";
import { navTransition } from "./native.ts";

import type { AppState } from "./app-state.tsx";

export function useNavHistory(s: Pick<AppState, "data" | "route">) {
  const { threadsRef } = s.data;
  const { reopenRef, navGoRef, nav, setNav } = s.route;
  // ---- phone navigation ----
  const navGo = (n: Nav) => {
    history.pushState(n, "", urlFor(n, location.search));
    // A sheet opens over the screen; only a new screen slides (V0.32).
    if (n.screen !== nav.screen && JSON.stringify(n.screen) !== JSON.stringify(nav.screen)) navTransition("fwd", () => setNav(n));
    else setNav(n);
  };
  navGoRef.current = navGo;
  const navRef = useRef(nav);
  navRef.current = nav;
  /** U8: after settling or archiving the open thread on a phone, go back to the inbox (pop, not push). */
  const leaveOpenThread = (ids: string[]) => {
    const s = navRef.current.screen;
    if (s.name !== "thread" || ids.length !== 1 || ids[0] !== s.id) return;
    if (!leavesThread(threadsRef.current[s.id])) return;
    reopenRef.current = s.id;
    navBack();
  };
  const navReplace = (n: Nav) => {
    history.replaceState(n, "", urlFor(n, location.search));
    setNav(n);
  };
  const navBack = () => {
    if (history.state && typeof history.state === "object" && "screen" in history.state && history.length > 1) history.back();
    else {
      const t = backTarget(nav);
      if (t) navReplace(t);
    }
  };

  return { navGo, navRef, leaveOpenThread, navReplace, navBack };
}

export type NavHistory = ReturnType<typeof useNavHistory>;
