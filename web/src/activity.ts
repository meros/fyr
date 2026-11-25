// V0.no-interrupt: what the owner is doing with the page now, for anything that would act by itself (a reload, a
// move to another thread). The rule is no-interrupt.ts `ownerBusy`; this file reads the DOM for it.

import { ownerBusy } from "./no-interrupt.ts";

let lastKey = 0;
if (typeof document !== "undefined") document.addEventListener("keydown", () => (lastKey = Date.now()), true);

/** When the owner last pressed a key in this page (0: never). */
export function lastKeyAt(): number {
  return lastKey;
}

/** The owner is busy with the page now: a dialog is open, the focused field holds text, or a key was pressed lately. */
export function busyNow(): boolean {
  const a = document.activeElement as HTMLInputElement | HTMLTextAreaElement | null;
  const field = a && (a.tagName === "TEXTAREA" || (a.tagName === "INPUT" && !["checkbox", "radio", "button"].includes(a.type))) ? a.value : null;
  return ownerBusy({
    hidden: document.visibilityState === "hidden",
    dialogOpen: !!document.querySelector("[role=dialog],[aria-modal=true]"),
    fieldText: field,
    lastKeyAt: lastKey,
    now: Date.now(),
  });
}

let pending = false;

/** Run `go` (a reload) once the owner is not busy: now, or at the first check every 2 s or when the page is hidden.
 *  A second call while one waits is dropped: the first reload serves both. */
export function whenIdle(go: () => void): void {
  if (pending) return;
  if (!busyNow()) return go();
  pending = true;
  const check = () => {
    if (busyNow()) return;
    clearInterval(iv);
    document.removeEventListener("visibilitychange", check);
    go();
  };
  const iv = setInterval(check, 2000);
  document.addEventListener("visibilitychange", check);
}
