// V0.composer-focus-reach (2): the one focus rule of every modal panel (DialogScrim, BottomSheet, and through them the
// confirmation): on open the focus moves into the panel, Tab and ⇧Tab stay inside it, and on close the focus goes
// back to where it was. Panels can nest (a confirmation over a dialog): only the newest one traps.

import { useEffect, useState } from "preact/hooks";
import type { RefObject } from "preact";

/** What Tab can reach inside a panel. */
export const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"]), [contenteditable=""], [contenteditable="true"]';

/** Where Tab (or ⇧Tab with `back`) goes from `active` in `list`, wrapping at both ends. From outside the list it goes
 *  to the first control (the last with `back`); null when the panel has nothing to focus. */
export function tabTarget<T>(list: readonly T[], active: T | null, back: boolean): T | null {
  if (!list.length) return null;
  const i = active === null ? -1 : list.indexOf(active);
  if (i < 0) return back ? list[list.length - 1] : list[0];
  return list[(i + (back ? -1 : 1) + list.length) % list.length];
}

/** The open traps, newest last: only the newest acts, so a confirmation over a dialog keeps Tab to itself. */
const stack: object[] = [];
export function pushTrap(t: object): void {
  stack.push(t);
}
export function dropTrap(t: object): void {
  const i = stack.lastIndexOf(t);
  if (i >= 0) stack.splice(i, 1);
}
export const isTopTrap = (t: object): boolean => stack[stack.length - 1] === t;

const shown = (el: HTMLElement) => el.getClientRects().length > 0;
const focusables = (panel: HTMLElement) => [...panel.querySelectorAll<HTMLElement>(FOCUSABLE)].filter(shown);

/**
 * Traps the focus in `panel` while the component is mounted.
 * - open: unless a child already took the focus (an autofocused field, the confirmation's primary button), the focus
 *   goes to the first control, or with `initial: "panel"` to the panel itself;
 * - Tab, ⇧Tab: wrap inside the panel;
 * - close: the focus goes back to the element that had it before the panel opened, if it is still in the page.
 */
export function useFocusTrap(panel: RefObject<HTMLElement | null>, initial: "first" | "panel" = "first"): void {
  // Read during the first render, before the panel is in the page, so an autofocused child is not taken for it.
  const [before] = useState(() => (typeof document === "undefined" ? null : (document.activeElement as HTMLElement | null)));
  useEffect(() => {
    const el = panel.current;
    if (!el) return;
    const me = {};
    pushTrap(me);
    if (!el.contains(document.activeElement)) {
      const first = initial === "first" ? focusables(el)[0] : undefined;
      if (first) first.focus();
      else {
        if (!el.hasAttribute("tabindex")) el.setAttribute("tabindex", "-1");
        el.focus({ preventScroll: true });
      }
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Tab" || e.defaultPrevented || !isTopTrap(me)) return;
      const active = document.activeElement as HTMLElement | null;
      const list = focusables(el);
      const next = tabTarget(list, active && el.contains(active) ? active : null, e.shiftKey);
      e.preventDefault();
      next?.focus();
    };
    window.addEventListener("keydown", onKey, true);
    return () => {
      window.removeEventListener("keydown", onKey, true);
      dropTrap(me);
      // Back to the opener, unless the focus already moved on to something else that is outside the panel.
      const now = document.activeElement;
      const moved = now && now !== document.body && !el.contains(now);
      if (before?.isConnected && !moved) before.focus({ preventScroll: true });
    };
  }, []);
}
