// Navigation history: go, replace and back (the phone's screens are history entries). (split from the App of app.tsx; takes and returns plain objects).

// The app shell: state, commands and the keyboard.

import { useRef } from "preact/hooks";
import { backTarget, leavesThread, urlFor, type Nav } from "./nav.ts";
import { navTransition } from "./native.ts";
import type { useAppState } from "./app-state.tsx";

export function useNavHistory(c: Pick<ReturnType<typeof useAppState>, "threadsRef" | "reopenRef" | "navGoRef" | "nav" | "setNav">) {
  const { threadsRef, reopenRef, navGoRef, nav, setNav } = c;
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
