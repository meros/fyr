// Full screen for previews and the live view (SPEC 0.5 V0.165), on desktop and phone alike. The browser's Fullscreen
// API where it has one (desktop, Android Chrome); else, as on iPhone Safari and the iOS home-screen app, an in-app
// layer that covers the screen. Either way the layer is a history entry, so Back (the phone's back gesture) leaves it,
// and Escape leaves it too.

import { useLayoutEffect, useRef } from "preact/hooks";

function canFullscreen(): boolean {
  return typeof document !== "undefined" && !!document.fullscreenEnabled;
}

/** Ask for the browser's full screen on `el`; must run inside the press that asked for it. */
export function requestFullscreen(el: Element | null | undefined): void {
  if (!el || !canFullscreen() || document.fullscreenElement) return;
  el.requestFullscreen?.({ navigationUI: "hide" })?.catch?.(() => {});
}

function exitFullscreen(): void {
  if (typeof document !== "undefined" && document.fullscreenElement) void document.exitFullscreen?.().catch?.(() => {});
}

/**
 * A full-screen layer's life while `on`: one history entry (Back leaves), Escape leaves, and leaving the browser's
 * full screen (its own Escape, a system gesture) leaves. `onLeft` runs once the layer is gone. Returns leave(), for a
 * Close button.
 */
export function useLayer(on: boolean, onLeft: () => void): () => void {
  const pushed = useRef(false);
  const left = useRef(onLeft);
  left.current = onLeft;
  const leave = () => {
    exitFullscreen();
    if (pushed.current) {
      pushed.current = false;
      history.back(); // popstate ends it
    } else left.current();
  };
  const leaveRef = useRef(leave);
  leaveRef.current = leave;
  // A layout effect: the entry exists as soon as the layer shows, so an early Back leaves the layer, not the page.
  useLayoutEffect(() => {
    if (!on) return;
    if (!pushed.current) {
      history.pushState({ ...(history.state ?? {}), layer: true }, "");
      pushed.current = true;
    }
    const onPop = () => {
      pushed.current = false;
      exitFullscreen();
      left.current();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.preventDefault();
      e.stopImmediatePropagation();
      leaveRef.current();
    };
    const onFs = () => {
      if (!document.fullscreenElement) leaveRef.current();
    };
    window.addEventListener("popstate", onPop);
    window.addEventListener("keydown", onKey, true);
    document.addEventListener("fullscreenchange", onFs);
    return () => {
      window.removeEventListener("popstate", onPop);
      window.removeEventListener("keydown", onKey, true);
      document.removeEventListener("fullscreenchange", onFs);
    };
  }, [on]);
  return leave;
}
