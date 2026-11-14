// SPEC 0.5 V0.32: small things that make the installed app feel native on a
// phone: a short vibration on a committed action, and a slide between screens.

/** Android only (iOS has no vibrate); a no-op on a desktop or without a touch screen. */
export function haptic(kind: "light" | "firm" = "light"): void {
  try {
    if (typeof matchMedia === "function" && !matchMedia("(pointer: coarse)").matches) return;
    navigator.vibrate?.(kind === "light" ? 8 : 16);
  } catch {
    /* not allowed: nothing to do */
  }
}

export type NavDir = "fwd" | "back";

type VTDoc = Document & { startViewTransition?: (cb: () => Promise<void> | void) => unknown };

/** Run a screen change inside a view transition that slides by direction. Without
 *  support, or with reduced motion, the change happens at once. */
export function navTransition(dir: NavDir, update: () => void): void {
  const doc = document as VTDoc;
  const reduce = typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (!doc.startViewTransition || reduce) {
    update();
    return;
  }
  document.documentElement.dataset.nav = dir;
  const vt = doc.startViewTransition(async () => {
    update();
    // Preact renders on the next microtask; wait one frame so the new screen is in the DOM.
    await new Promise<void>((r) => requestAnimationFrame(() => r()));
  }) as { ready?: Promise<unknown>; finished?: Promise<unknown>; updateCallbackDone?: Promise<unknown> } | undefined;
  // A newer screen change skips this transition; the update still ran, so the rejection is expected.
  for (const p of [vt?.ready, vt?.finished, vt?.updateCallbackDone]) p?.catch(() => {});
}
