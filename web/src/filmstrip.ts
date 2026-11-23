// V0.filmstrip-fold (#61): the filmstrip's fold and hide. The strip is one "N previews" line on both fronts until a
// press opens it; its × hides it for the thread on this device, and the "N previews" chip of the header brings it back.

import { useEffect, useState } from "preact/hooks";
import { storedGet, storedSet } from "./prefs.ts";

/** Where a thread's hide is kept: one key per thread ref (decision 0031: `<hostId>/<threadId>`), so two hosts' threads
 *  never share one, and a host's rotation moves the key with its other refs (host-rotation.ts). */
export function filmstripKey(threadRef: string): string {
  return `fyr.filmstrip.${threadRef}`;
}

/** What the conversation shows of its previews: nothing, the one line, or the line with the strip under it. */
export type FilmstripView = "none" | "hidden" | "folded" | "open";

export function filmstripView(count: number, hidden: boolean, open: boolean): FilmstripView {
  if (count <= 0) return "none";
  if (hidden) return "hidden";
  return open ? "open" : "folded";
}

/** The fold line's and the header chip's text. */
export function previewsLabel(count: number): string {
  return `${count} ${count === 1 ? "preview" : "previews"}`;
}

/** The header chip stands only while the strip is hidden and there is something to bring back. */
export function showsPreviewsChip(count: number, hidden: boolean): boolean {
  return filmstripView(count, hidden, false) === "hidden";
}

const listeners = new Set<(threadRef: string) => void>();
/** This page's choices: where storage is blocked or full, a hide still lasts until the page reloads. */
const page = new Map<string, boolean>();
/** The strips the owner opened, this page: the fold is not stored, but a tab switch does not close it. */
const opened = new Set<string>();

export function filmstripHidden(threadRef: string): boolean {
  if (!threadRef) return false;
  return page.get(threadRef) ?? storedGet(filmstripKey(threadRef)) === "hidden";
}

/** Hide or show the thread's strip; the strip and the header chip of every open front follow. */
export function setFilmstripHidden(threadRef: string, hidden: boolean): void {
  if (!threadRef) return;
  page.set(threadRef, hidden);
  if (hidden) opened.delete(threadRef);
  storedSet(filmstripKey(threadRef), hidden ? "hidden" : null);
  listeners.forEach((f) => f(threadRef));
}

/** The strip the header chip brought back: it scrolls into view once it is drawn. */
let reveal = "";

export function filmstripOpen(threadRef: string): boolean {
  return opened.has(threadRef);
}

export function setFilmstripOpen(threadRef: string, open: boolean): void {
  if (open) opened.add(threadRef);
  else opened.delete(threadRef);
  listeners.forEach((f) => f(threadRef));
}

/** The header chip's press: the strip comes back open, and is scrolled to where it stands. */
export function bringBackFilmstrip(threadRef: string): void {
  if (!threadRef) return;
  reveal = threadRef;
  opened.add(threadRef);
  setFilmstripHidden(threadRef, false);
}

/** True once, for the strip the chip brought back. */
export function takeReveal(threadRef: string): boolean {
  if (!threadRef || reveal !== threadRef) return false;
  reveal = "";
  return true;
}

/** The thread's hide and fold, kept in step with the other component that shows it (the strip and the header chip). */
export function useFilmstrip(threadRef: string): { hidden: boolean; open: boolean } {
  const [, bump] = useState(0);
  useEffect(() => {
    const f = (r: string) => r === threadRef && bump((n) => n + 1);
    listeners.add(f);
    return () => void listeners.delete(f);
  }, [threadRef]);
  return { hidden: filmstripHidden(threadRef), open: filmstripOpen(threadRef) };
}
