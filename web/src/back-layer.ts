// V0.dialogs-review (4): what the phone's Back closes when something lies over a screen without being a navigation
// entry of its own: the folder picker, a confirmation, a dialog shown as a sheet, the project list and the New project
// step inside New thread. Each such layer gets one history entry while it is up, so Back closes the top-most layer and
// the screen under it stays. The entry carries the screen's own state and URL, so the navigation listener (nav.ts
// `listenPopState`) sees the same screen and changes nothing.
//
// The layers form a stack. A layer the owner closes on the page (a button, a pick) takes its entry out again with
// history.go; that traversal is asynchronous, so a navigation asked for meanwhile waits for it (`settled`), else its
// pushState would land before the traversal and be undone by it.

import { useLayoutEffect, useRef } from "preact/hooks";

interface HistoryLike {
  readonly state: unknown;
  pushState(state: unknown, title: string): void;
  go(delta: number): void;
}

interface Layer {
  onBack(): void;
  /** its history entry exists */
  pushed: boolean;
}

/** A traversal the stack asked for that never ends in a popstate (nothing to go back to) is given up after this. */
const TRAVERSE_MS = 600;

export function createLayers(h: HistoryLike, later: (fn: () => void, ms: number) => unknown = setTimeout) {
  const stack: Layer[] = [];
  /** entries to take out, gathered in one turn so two layers that close together are one traversal */
  let owed = 0;
  /** a traversal of ours is on its way: its popstate is not the owner's Back */
  let moving = false;
  let flushing = false;
  let waiting: (() => void)[] = [];

  const done = () => {
    moving = false;
    if (owed) return flush();
    const run = waiting;
    waiting = [];
    for (const fn of run) fn();
  };
  const flush = () => {
    flushing = false;
    if (moving || !owed) return;
    const n = owed;
    owed = 0;
    moving = true;
    h.go(-n);
    later(() => moving && done(), TRAVERSE_MS);
  };
  const take = (n: number) => {
    owed += n;
    if (flushing || moving) return;
    flushing = true;
    void Promise.resolve().then(flush);
  };

  return {
    /** A layer came up: one entry for it. Returns its handle for `close`. */
    open(onBack: () => void): Layer {
      const l: Layer = { onBack, pushed: false };
      stack.push(l);
      const push = () => {
        if (!stack.includes(l)) return; // closed while it waited
        l.pushed = true;
        h.pushState({ ...(h.state && typeof h.state === "object" ? h.state : {}), layer: stack.indexOf(l) + 1 }, "");
      };
      // A layer that opens while another one's entry is on its way out waits for that traversal.
      if (moving || owed) waiting.push(push);
      else push();
      return l;
    },
    /** The page closed the layer (and with it every layer above it): their entries go. A layer Back already closed
     *  has none left, and nothing happens. */
    close(l: Layer): void {
      const i = stack.indexOf(l);
      if (i < 0) return;
      take(stack.splice(i).filter((x) => x.pushed).length);
    },
    /** The browser went to another entry. True when it was a traversal of ours; otherwise it was the owner's Back, and
     *  the top-most layer closes. */
    popped(): boolean {
      if (moving) {
        done();
        return true;
      }
      stack.pop()?.onBack();
      return false;
    },
    /** Leave every layer without telling it (the screen under them is being left): how many entries to step over. */
    drop(): number {
      const n = stack.filter((x) => x.pushed).length + owed;
      stack.length = 0;
      owed = 0;
      return n;
    },
    /** Run a navigation once no traversal of ours is on its way, the open layers' entries taken out first (a new
     *  entry must not sit on top of a layer's). */
    settled(fn: () => void): void {
      if (stack.length) take(stack.splice(0).filter((x) => x.pushed).length);
      if (moving || owed) waiting.push(fn);
      else fn();
    },
    depth: (): number => stack.length,
  };
}

let shared: ReturnType<typeof createLayers> | null = null;

/** The page's one stack; null where there is no history (a test). */
export function layers(): ReturnType<typeof createLayers> | null {
  if (shared || typeof history === "undefined" || typeof window === "undefined") return shared;
  const l = createLayers({
    get state() {
      return history.state as unknown;
    },
    pushState: (s, t) => history.pushState(s, t),
    go: (n) => history.go(n),
  });
  window.addEventListener("popstate", () => void l.popped());
  shared = l;
  return l;
}

/** While `on`, the layer is a history entry: Back calls `onBack` (which closes it). Phone only: the caller passes
 *  `on` false on the desktop, where Esc closes a layer and Back walks the opened threads (desk-history.ts). */
export function useBackLayer(on: boolean, onBack: () => void): void {
  const back = useRef(onBack);
  back.current = onBack;
  // A layout effect: the entry exists as soon as the layer shows, so an early Back closes the layer, not the screen.
  useLayoutEffect(() => {
    const ls = on ? layers() : null;
    if (!ls) return;
    const l = ls.open(() => back.current());
    return () => ls.close(l);
  }, [on]);
}
