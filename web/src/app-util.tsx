// Helpers the app shell and its hooks share: the page parameters, the theme, the phone query, storage.


import { storedGet, storedSet } from "./prefs.ts";
import { useEffect, useState } from "preact/hooks";
import { type Theme } from "./inbox.tsx";
import { VIEWPORT_VV_EVENTS, VIEWPORT_WINDOW_EVENTS, rereadLater, viewportVars } from "./viewport.ts";
import { PHONE_QUERY } from "./motion-ui.tsx";

/** Run when the browser is idle (V0.38: storing never competes with input). */
export const idle = (fn: () => void) => ((globalThis as { requestIdleCallback?: (f: () => void, o?: object) => void }).requestIdleCallback ?? ((f: () => void) => setTimeout(f, 0)))(fn, { timeout: 3000 });


/** True at phone width (design/mobile, V0.14 breakpoint). */
export function usePhone(): boolean {
  const mq = typeof window.matchMedia === "function" ? window.matchMedia(PHONE_QUERY) : null;
  const [phone, setPhone] = useState(!!mq?.matches);
  /* eslint-disable react-hooks/exhaustive-deps -- mq is a new MediaQueryList for the same query on each render; the first one is listened to for the hook's life */
  useEffect(() => {
    if (!mq) return;
    const f = () => setPhone(mq.matches);
    mq.addEventListener("change", f);
    return () => mq.removeEventListener("change", f);
  }, []);
  /* eslint-enable react-hooks/exhaustive-deps */
  return phone;
}

/**
 * V0.165: the app's chrome does not zoom. iOS Safari ignores user-scalable=no in the viewport, so its gesture events
 * are stopped here too; previews zoom inside their own viewer, from pointer events, which this does not touch.
 */
export function useStaticPage() {
  useEffect(() => {
    const no = (e: Event) => e.preventDefault();
    for (const t of ["gesturestart", "gesturechange"]) document.addEventListener(t, no, { passive: false });
    return () => {
      for (const t of ["gesturestart", "gesturechange"]) document.removeEventListener(t, no);
    };
  }, []);
}

/**
 * The phone layout height follows the visual viewport (SPEC V0.17): --app-h, --app-top and kb-open on
 * <html>, re-read on every event that can change them, and again on the next frames after a resume.
 */
export function useViewportVars(on: boolean) {
  useEffect(() => {
    if (!on) return;
    const root = document.documentElement;
    const vv = window.visualViewport;
    const read = () => {
      const v = viewportVars({ innerHeight: window.innerHeight, vvHeight: vv?.height, vvOffsetTop: vv?.offsetTop });
      root.style.setProperty("--app-h", v.height);
      root.style.setProperty("--app-top", v.top);
      root.classList.toggle("kb-open", v.keyboard);
      // With the keyboard up, keep the field being typed in on screen.
      const a = document.activeElement as HTMLElement | null;
      if (v.keyboard && a && (a.tagName === "INPUT" || a.tagName === "TEXTAREA")) requestAnimationFrame(() => a.scrollIntoView({ block: "nearest" }));
    };
    const timers: ReturnType<typeof setTimeout>[] = [];
    const onEvent = (e: Event) => {
      read();
      if (rereadLater(e.type, document.visibilityState)) {
        requestAnimationFrame(() => requestAnimationFrame(read));
        timers.push(setTimeout(read, 350));
      }
    };
    read();
    for (const t of VIEWPORT_WINDOW_EVENTS) window.addEventListener(t, onEvent);
    for (const t of VIEWPORT_VV_EVENTS) vv?.addEventListener(t, onEvent);
    document.addEventListener("visibilitychange", onEvent);
    return () => {
      for (const t of VIEWPORT_WINDOW_EVENTS) window.removeEventListener(t, onEvent);
      for (const t of VIEWPORT_VV_EVENTS) vv?.removeEventListener(t, onEvent);
      document.removeEventListener("visibilitychange", onEvent);
      timers.forEach(clearTimeout);
      root.style.removeProperty("--app-h");
      root.style.removeProperty("--app-top");
      root.classList.remove("kb-open");
    };
  }, [on]);
}

/** Events per page of a thread timeline. */
export const PAGE = 400;

export const params = new URLSearchParams(location.search);

export type Dialog = "new" | "snooze" | "picker" | "handover" | "settings" | "changes" | null;
export type Zone = "list" | "thread";

export function readTheme(): Theme {
  const t = storedGet("fyr.theme");
  return t === "light" || t === "dark" ? t : "system";
}

export function applyTheme(t: Theme) {
  if (t === "system") document.documentElement.removeAttribute("data-theme");
  else document.documentElement.setAttribute("data-theme", t);
  storedSet("fyr.theme", t === "system" ? null : t);
}

export function isTextTarget(t: EventTarget | null): boolean {
  const el = t as HTMLElement | null;
  if (!el || !el.tagName) return false;
  const tag = el.tagName;
  if (tag === "TEXTAREA" || tag === "SELECT") return true;
  if (tag === "INPUT") {
    const type = (el as HTMLInputElement).type;
    return type !== "checkbox" && type !== "radio" && type !== "button";
  }
  return el.isContentEditable;
}
