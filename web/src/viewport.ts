// The phone layout's height (SPEC 0.5 V0.17). One source: window.visualViewport, re-read on every
// event that can change it, never a height measured once. Pure; app.tsx wires the events.

/** Events that make the app re-read the visual viewport. */
export const VIEWPORT_WINDOW_EVENTS = ["resize", "pageshow", "orientationchange", "focusin", "focusout"] as const;
export const VIEWPORT_VV_EVENTS = ["resize", "scroll"] as const;

/** A keyboard is open when the visible area is this much shorter than the layout viewport. */
export const KEYBOARD_MIN_PX = 120;

export interface ViewportSizes {
  /** window.innerHeight (the layout viewport) */
  innerHeight: number;
  /** visualViewport.height and offsetTop; undefined when the browser has no visualViewport */
  vvHeight?: number;
  vvOffsetTop?: number;
}

export interface ViewportVars {
  /** --app-h: the height of the app */
  height: string;
  /** --app-top: where the app starts (iOS scrolls the layout viewport when the keyboard opens) */
  top: string;
  /** the kb-open class: the keyboard covers the bottom, so the bottom safe-area inset does not apply */
  keyboard: boolean;
}

export function viewportVars(s: ViewportSizes): ViewportVars {
  if (s.vvHeight === undefined || !(s.vvHeight > 0)) return { height: "100dvh", top: "0px", keyboard: false };
  const h = Math.round(s.vvHeight);
  const top = Math.max(0, Math.round(s.vvOffsetTop ?? 0));
  return { height: `${h}px`, top: `${top}px`, keyboard: s.innerHeight - s.vvHeight > KEYBOARD_MIN_PX };
}

/** visibilitychange to visible (a resume) also re-reads on the next frames: some browsers report the new size late. */
export function rereadLater(event: string, visibility?: string): boolean {
  return event === "pageshow" || event === "orientationchange" || event === "focusin" || event === "focusout" || (event === "visibilitychange" && visibility === "visible");
}
