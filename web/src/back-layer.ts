// V0.dialogs-review (4): what the phone's Back closes when something lies over a screen without being a navigation
// entry of its own: the folder picker, a confirmation, a dialog shown as a sheet, the project list and the New project
// step inside New thread. Each such layer gets one history entry while it is up, so Back closes the top-most layer and
// the screen under it stays. The entry carries the screen's own state and URL, so the navigation listener (nav.ts
// `listenPopState`) sees the same screen and changes nothing.
//
// One rule holds it together: an entry of a layer says how many layers lie over the screen at that entry (`backLayer`
// in its state), and the stack of open layers says how many should. Whenever the browser lands on an entry, the two
// are compared, whoever moved it:
// - fewer in the history than open: the owner went Back over layers, and those close;
// - more in the history than open: entries are left over (a layer closed on the page, Forward onto an entry whose
//   layer is gone), and the stack goes back by that many;
// - the same: nothing to do.
// So no popstate is taken for something it is not, and no flag has to guess who caused it.
// A page that loads on a layer's entry (a reload over a layer) has no popstate to compare at: `leftOnLoad` says how
// many entries are left over there, and the stack steps back by that many before anything else (`leftOver`).
//
// A navigation never lands on a layer's entry, and never under a traversal that is on its way (its pushState would be
// undone by it): it closes the open layers first and waits until the history is where the stack says (`settled`).
// What waits runs after the popstate event, in a turn of its own, so it does not depend on which popstate listener
// the browser calls first.

import { useLayoutEffect, useRef } from "preact/hooks";
import { seedHistory, type Nav } from "./nav.ts";

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
export const TRAVERSE_MS = 600;

const MARK = "backLayer";

/** How many layers lie over the screen at a history entry with this state. */
export function layerDepth(state: unknown): number {
  const n = state && typeof state === "object" ? (state as Record<string, unknown>)[MARK] : undefined;
  return typeof n === "number" && n > 0 ? n : 0;
}

/** How many entries are left over when the page loads on an entry with this state (a reload over a layer): the
 *  entries of the layers that were up, none of which is open now, and under them the entry of the navigation's sheet
 *  they stood over (nav.ts: a sheet is an entry of its own; the loaded page shows the screen without it). */
export function leftOnLoad(state: unknown): number {
  const depth = layerDepth(state);
  return depth && (state as { sheet?: unknown }).sheet ? depth + 1 : depth;
}

export function createLayers(h: HistoryLike, later: (fn: () => void, ms: number) => unknown = setTimeout) {
  const stack: Layer[] = [];
  /** entries to take out: more layers in the history than are open */
  let owed = 0;
  /** the traversals of ours that are on their way, oldest first; each is given up by its own timer */
  const flights: object[] = [];
  let starting = false;
  /** what waits for the history to be where the stack says: a new layer's entry, a navigation */
  const queue: (() => void)[] = [];

  const pushed = () => stack.filter((x) => x.pushed).length;
  const idle = () => !flights.length && !owed;
  const go = (n: number) => {
    const mine = {};
    flights.push(mine);
    h.go(-n);
    later(() => {
      const i = flights.indexOf(mine);
      if (i < 0) return; // it landed
      flights.splice(i, 1);
      settle();
    }, TRAVERSE_MS);
  };
  /** Bring the history to the stack, then run what waits. */
  const settle = () => {
    if (flights.length) return; // its landing settles
    if (owed) {
      const n = owed;
      owed = 0;
      return go(n);
    }
    while (queue.length && idle()) queue.shift()!();
  };
  /** `n` entries are left over. Gathered for a turn, so two layers that close together are one traversal. */
  const take = (n: number) => {
    if (!n) return;
    owed += n;
    if (starting) return;
    starting = true;
    void Promise.resolve().then(() => {
      starting = false;
      settle();
    });
  };
  const whenIdle = (fn: () => void) => {
    if (idle() && !queue.length) fn();
    else queue.push(fn);
  };
  /** Close every open layer on the page (top-most first); their entries are left over. */
  const closeAll = () => {
    const open = stack.splice(0);
    take(open.filter((x) => x.pushed).length);
    for (const l of open.reverse()) l.onBack();
  };

  return {
    /** A layer came up: one entry for it, at once unless the history is still on its way. Returns its handle for
     *  `close`. */
    open(onBack: () => void): Layer {
      const l: Layer = { onBack, pushed: false };
      stack.push(l);
      whenIdle(() => {
        if (!stack.includes(l)) return; // closed while it waited
        l.pushed = true;
        h.pushState({ ...(h.state && typeof h.state === "object" ? h.state : {}), [MARK]: pushed() }, "");
      });
      return l;
    },
    /** The page closed the layer (and with it every layer above it): their entries go. A layer Back already closed
     *  has none left, and nothing happens. */
    close(l: Layer): void {
      const i = stack.indexOf(l);
      if (i >= 0) take(stack.splice(i).filter((x) => x.pushed).length);
    },
    /** The browser landed on the entry with `state`. Layers the owner went Back over close; entries left over are
     *  taken out. True when a traversal of ours was on its way. */
    popped(state: unknown): boolean {
      const ours = flights.shift() !== undefined; // traversals land in the order they were asked for
      const depth = layerDepth(state);
      const gone: Layer[] = [];
      for (let over = pushed() - depth; over > 0; ) {
        const l = stack.pop()!;
        if (l.pushed) over--;
        gone.push(l);
      }
      owed = Math.max(0, depth - pushed());
      for (const l of gone) l.onBack();
      // After the event: the other popstate listeners (the navigation's) have had this entry first.
      later(settle, 0);
      return ours;
    },
    /** `n` entries are left over from before the page loaded (`leftOnLoad`): the history steps back by that many,
     *  and a layer that comes up meanwhile gets its entry after that. */
    leftOver: (n: number): void => take(n),
    /** A Back control of the page (the header's arrow or ×, a sheet's close) leaves the screen: one step back, over
     *  the entries of every layer above it, which close. */
    leave(): void {
      const n = pushed() + owed + 1;
      owed = 0;
      const open = stack.splice(0);
      go(n);
      for (const l of open.reverse()) l.onBack();
    },
    /** Run a navigation once the history is where the stack says. The open layers close first: a new entry must not
     *  sit on top of a layer's, and a layer must not stay on the screen without one. */
    settled(fn: () => void): void {
      closeAll();
      whenIdle(fn);
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
  window.addEventListener("popstate", (e) => void l.popped((e as PopStateEvent).state));
  shared = l;
  return l;
}

let seeded: Nav | null = null;

/** The phone's history, built once per page (nav.ts `seedHistory`), and the entry the page is on. Whoever needs the
 *  history first builds it: the navigation (app-nav.tsx), or a layer that is up in the first render (a layer's entry
 *  pushed before the seed would end up under the seeded entries). */
export function seedPhoneHistory(): Nav {
  if (seeded) return seeded;
  // A reload over a layer: seedHistory makes that entry the screen's, which leaves a second entry of the screen that
  // a Back would spend without anything changing. The stack takes it out.
  const over = leftOnLoad(history.state);
  seeded = seedHistory(history, location.search, history.state);
  if (over) layers()?.leftOver(over);
  return seeded;
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
    seedPhoneHistory();
    const l = ls.open(() => back.current());
    return () => ls.close(l);
  }, [on]);
}
