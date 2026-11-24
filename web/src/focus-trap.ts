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
/** V0.dialogs-review (1): how many modal panels are open. The desktop's key handler reads it: while a panel is open
 *  that the app's `dialog` state does not know (the Merge dialog, a confirmation), no app key acts under it. */
export const openTraps = (): number => stack.length;

/** V0.dialogs-review (10): where the focus starts in a panel: its first control that is not the header's × (marked
 *  `data-close`), so Enter or Space right after opening takes the first choice instead of closing; the × only when
 *  the panel has nothing else. */
export function firstFocus<T>(list: readonly T[], isClose: (x: T) => boolean): T | undefined {
  return list.find((x) => !isClose(x)) ?? list[0];
}

const shown = (el: HTMLElement) => el.getClientRects().length > 0;
const focusables = (panel: HTMLElement) => [...panel.querySelectorAll<HTMLElement>(FOCUSABLE)].filter(shown);

/**
 * Traps the focus in `panel` while the component is mounted.
 * - open: unless a child already took the focus (an autofocused field, the confirmation's primary button), the focus
 *   goes to the first control that is not the header's × (`firstFocus`), or with `initial: "panel"` to the panel itself;
 * - Tab, ⇧Tab: wrap inside the panel;
 * - close: the focus goes back to the element that had it before the panel opened, if it is still in the page.
 */
export function useFocusTrap(panel: RefObject<HTMLElement | null>, initial: "first" | "panel" = "first"): void {
  // Read during the first render, before the panel is in the page, so an autofocused child is not taken for it.
  const [before] = useState(() => (typeof document === "undefined" ? null : (document.activeElement as HTMLElement | null)));
  /* eslint-disable react-hooks/exhaustive-deps -- the trap is set once, for the panel's life: panel is the caller's ref object, initial does not change, and before is the element focused before the panel opened */
  useEffect(() => {
    const el = panel.current;
    if (!el) return;
    const me = {};
    pushTrap(me);
    if (!el.contains(document.activeElement)) {
      const first = initial === "first" ? firstFocus(focusables(el), (x) => x.hasAttribute("data-close")) : undefined;
      if (first) first.focus();
      else {
        if (!el.hasAttribute("tabindex")) el.setAttribute("tabindex", "-1");
        el.focus({ preventScroll: true });
      }
    }
    const move = (e: KeyboardEvent, from: HTMLElement | null) => {
      e.preventDefault();
      tabTarget(focusables(el), from && el.contains(from) ? from : null, e.shiftKey)?.focus();
    };
    // Tab inside the panel: heard on the panel itself, after its controls had the key. A control that uses Tab itself
    // (the folder picker's completion, the slash menu) prevents the default, and the trap leaves that key alone; a Tab
    // the trap takes goes no further, so the keys of the screen behind (the ask card's Tab) do not act on it.
    const inside = (e: KeyboardEvent) => {
      if (e.key !== "Tab" || e.defaultPrevented || !isTopTrap(me)) return;
      e.stopPropagation();
      move(e, document.activeElement as HTMLElement | null);
    };
    // Tab with the focus outside the panel (on the page behind, or nowhere): into the panel.
    const outside = (e: KeyboardEvent) => {
      if (e.key !== "Tab" || !isTopTrap(me) || el.contains(document.activeElement)) return;
      move(e, null);
    };
    el.addEventListener("keydown", inside);
    window.addEventListener("keydown", outside, true);
    return () => {
      el.removeEventListener("keydown", inside);
      window.removeEventListener("keydown", outside, true);
      dropTrap(me);
      // Back to the opener, unless the focus already moved on to something else that is outside the panel.
      const now = document.activeElement;
      const moved = now && now !== document.body && !el.contains(now);
      if (before?.isConnected && !moved) before.focus({ preventScroll: true });
    };
  }, []);
  /* eslint-enable react-hooks/exhaustive-deps */
}
