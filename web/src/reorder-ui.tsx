// SPEC V0.119 (4), V0.reorder-one: the one reorder control, for every list the owner orders (a provider's models, the
// projects, the hosts, the queue of Waiting to run). One hook does the moving; the lists only draw their rows.
//
// Desktop: a grip on each row. Drag it: the row follows the pointer, the others slide out of its way, the list scrolls
// by itself near its edges, Esc puts it back. Alt+↑/↓ on the focused grip moves one place.
// Phone: a Reorder button puts the list in a mode with ↑, ↓ and a handle of 44 px on each row. Touch has no hover to
// reveal a grip and a drag on a row fights the scroll, so the handle drags only in the mode, where it is the only
// thing under the finger that moves.
// Every move shows on this device at once and saves at once; a refusal puts the order back and says why.

import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { dropIndex, edgeSpeed, move, moveLabel, shiftOf, slotOffset, stepOf } from "./reorder.ts";
import { haptic } from "./native.ts";
import { useSet } from "./settings-rows.tsx";

/** What a list does with a new order: save it. A string is why it did not save (the order goes back). */
export type OnMove = (next: string[], from: number, to: number) => Promise<string | null | void> | string | null | void;

export interface Reorder {
  /** the order to draw: the one just made on this device until the list catches up, else the list's own */
  ids: string[];
  /** why the last move did not save */
  err: string | null;
  /** `items` in the order to draw */
  sort<T>(items: readonly T[], key: (x: T) => string): T[];
  /** move the row at `from` to `to` (↑ and ↓) */
  move(from: number, to: number): void;
  /** props for a row's grip (the desktop grip, the phone's handle) */
  grip(i: number, name: string): Record<string, unknown>;
  /** the row's root: the drag measures and moves it */
  row(i: number): { ref: (el: HTMLElement | null) => void; class: string };
}

const SETTLE_MS = 160;
/** a mouse must travel this far before the row lifts, so a press on the grip stays a click */
const SLOP = 4;

interface Drag {
  els: HTMLElement[];
  from: number;
  to: number;
  stop(): void;
}

/** The scroller the row is in: the nearest ancestor that scrolls, else the page. */
function scrollerOf(el: HTMLElement): HTMLElement {
  for (let p = el.parentElement; p; p = p.parentElement) {
    const oy = getComputedStyle(p).overflowY;
    if ((oy === "auto" || oy === "scroll") && p.scrollHeight > p.clientHeight) return p;
  }
  return (document.scrollingElement as HTMLElement) ?? document.documentElement;
}

function clear(els: HTMLElement[]) {
  for (const el of els) {
    el.classList.remove("ro-lift", "ro-shift", "ro-settle");
    el.style.transform = "";
  }
  document.documentElement.classList.remove("ro-dragging");
}

export function useReorder(ids: string[], onMove: OnMove): Reorder {
  const [pending, setPending] = useState<string[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const key = ids.join("\n");
  // The list caught up with the order made here (or moved on without it): draw the list's own again.
  useEffect(() => setPending(null), [key]);
  const shown = pending && pending.length === ids.length && pending.every((x) => ids.includes(x)) ? pending : ids;
  const shownKey = shown.join("\n");
  const rows = useRef<(HTMLElement | null)[]>([]);
  const grips = useRef<Map<string, HTMLElement>>(new Map());
  const focusId = useRef<string | null>(null);
  const drag = useRef<Drag | null>(null);
  const settled = useRef<HTMLElement[] | null>(null);
  const cb = useRef(onMove);
  cb.current = onMove;
  const now = useRef(shown);
  now.current = shown;

  // After a drop the rows stand where they land by transform; the new order puts them there for real, in the same
  // frame, so nothing jumps. A move that changes nothing clears at once (finish).
  useLayoutEffect(() => {
    if (settled.current) clear(settled.current), (settled.current = null);
    const id = focusId.current; // the keyboard: the moved row's grip keeps the focus
    if (id) (focusId.current = null), grips.current.get(id)?.focus();
  }, [shownKey]);
  // A list that changes under a drag (a poll, another device): the places measured are wrong, so the drag ends.
  useEffect(() => () => drag.current?.stop(), [key]);
  useEffect(() => () => drag.current?.stop(), []);

  const doMove = (from: number, to: number) => {
    const cur = now.current;
    if (from === to || from < 0 || to < 0 || from >= cur.length || to >= cur.length) return false;
    const next = move(cur, from, to);
    setPending(next);
    setErr(null);
    const r = cb.current(next, from, to);
    const done = (e: string | null | void) => {
      if (!e) return;
      setErr(e);
      setPending((p) => (p === next ? null : p));
    };
    if (r && typeof (r as Promise<unknown>).then === "function") void (r as Promise<string | null | void>).then(done, (x) => done(String(x)));
    else done(r as string | null | void);
    return true;
  };

  const startDrag = (i: number, e: PointerEvent) => {
    if (e.pointerType === "mouse" && e.button !== 0) return;
    if (drag.current) return;
    const els = rows.current.slice(0, now.current.length);
    if (els.some((el) => !el) || els.length < 2) return;
    const list = els as HTMLElement[];
    e.preventDefault();
    const rects = list.map((el) => el.getBoundingClientRect());
    const tops = rects.map((r) => r.top);
    const bottoms = rects.map((r) => r.bottom);
    const mids = rects.map((r) => r.top + r.height / 2);
    const step = stepOf(tops, bottoms, i);
    const sc = scrollerOf(list[i]);
    const page = sc === document.scrollingElement || sc === document.documentElement;
    const st0 = sc.scrollTop;
    const y0 = e.clientY;
    let y = y0;
    let lifted = e.pointerType !== "mouse"; // touch and pen: the handle is only a handle, so it lifts at once
    let raf = 0;
    const d: Drag = { els: list, from: i, to: i, stop: () => end(false) };
    const dy = () => y - y0 + (sc.scrollTop - st0);
    const paint = () => {
      const off = dy();
      const to = dropIndex(mids, i, mids[i] + off);
      if (to !== d.to && e.pointerType !== "mouse") haptic();
      d.to = to;
      list[i].style.transform = `translateY(${off}px)`;
      for (let j = 0; j < list.length; j++) if (j !== i) list[j].style.transform = shiftOf(j, i, to, step) ? `translateY(${shiftOf(j, i, to, step)}px)` : "";
    };
    const lift = () => {
      lifted = true;
      list[i].classList.add("ro-lift");
      for (let j = 0; j < list.length; j++) if (j !== i) list[j].classList.add("ro-shift");
      document.documentElement.classList.add("ro-dragging");
      if (e.pointerType !== "mouse") haptic("firm");
      paint();
      raf = requestAnimationFrame(tick);
    };
    // Near the scroller's top or bottom the list scrolls by itself, and the held row stays under the pointer.
    const tick = () => {
      const box = page ? { top: 0, bottom: innerHeight } : sc.getBoundingClientRect();
      const v = edgeSpeed(y, box.top, box.bottom);
      if (v) {
        const before = sc.scrollTop;
        sc.scrollTop = before + v;
        if (sc.scrollTop !== before) paint();
      }
      raf = requestAnimationFrame(tick);
    };
    const onMoveP = (ev: PointerEvent) => {
      y = ev.clientY;
      if (!lifted) {
        if (Math.abs(y - y0) < SLOP) return;
        lift();
        return;
      }
      ev.preventDefault();
      paint();
    };
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key !== "Escape") return;
      ev.preventDefault();
      ev.stopPropagation(); // Esc puts the row back; it does not close the page as well
      end(false);
    };
    const off = () => {
      cancelAnimationFrame(raf);
      window.removeEventListener("pointermove", onMoveP);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", cancel);
      window.removeEventListener("keydown", onKey, true);
      drag.current = null;
    };
    // Let go: the row glides into its place, then the order changes under it.
    const end = (keep: boolean) => {
      off();
      if (!lifted) return;
      // the press ended a drag: the click it makes opens nothing
      const eat = (ev: MouseEvent) => (ev.stopPropagation(), ev.preventDefault());
      window.addEventListener("click", eat, { capture: true, once: true });
      setTimeout(() => window.removeEventListener("click", eat, true), 0);
      const to = keep ? d.to : i;
      list[i].classList.add("ro-settle");
      // the transforms are in the list's own coordinates, so a scroll since the lift changes nothing here
      for (let j = 0; j < list.length; j++) if (j === i) list[j].style.transform = `translateY(${slotOffset(tops, bottoms, i, to)}px)`;
      else if (to === i) list[j].style.transform = "";
      setTimeout(() => {
        if (to !== i && doMove(i, to)) settled.current = list;
        else clear(list);
        // a list that never redraws (the save failed at once and nothing changed) still lets go
        setTimeout(() => settled.current === list && (clear(list), (settled.current = null)), 1000);
      }, SETTLE_MS);
    };
    const up = () => end(true);
    const cancel = () => end(false);
    drag.current = d;
    window.addEventListener("pointermove", onMoveP, { passive: false });
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", cancel);
    window.addEventListener("keydown", onKey, true);
    if (lifted) lift();
  };

  return {
    ids: shown,
    err,
    sort: (items, k) => {
      const at = new Map(shown.map((x, n) => [x, n]));
      return [...items].sort((a, b) => (at.get(k(a)) ?? Infinity) - (at.get(k(b)) ?? Infinity));
    },
    move: (from, to) => void doMove(from, to),
    grip: (i, name) => ({
      class: "grip",
      role: "button",
      tabIndex: 0,
      "aria-label": moveLabel(name, i, shown.length),
      title: "Drag, or Alt and the arrow keys",
      ref: (el: HTMLElement | null) => {
        if (el) grips.current.set(shown[i], el);
        else grips.current.delete(shown[i]);
      },
      onPointerDown: (e: PointerEvent) => startDrag(i, e),
      onKeyDown: (e: KeyboardEvent) => {
        if (!e.altKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
        e.preventDefault();
        focusId.current = shown[i];
        if (!doMove(i, i + (e.key === "ArrowUp" ? -1 : 1))) focusId.current = null;
      },
    }),
    row: (i) => ({ ref: (el: HTMLElement | null) => void (rows.current[i] = el), class: "ro-row" }),
  };
}

/** The desktop's grip. None on the phone (its Reorder mode has MoveControls) and none when nothing can change. */
export function Grip(props: Record<string, unknown>) {
  const env = useSet();
  if (env.phone || env.readOnly) return null;
  return <span {...props}>⠿</span>;
}

/** The phone's Reorder mode, at the end of each row: ↑, ↓ and a handle to drag, 44 px each. */
export function MoveControls({ ro, i, name }: { ro: Reorder; i: number; name: string }) {
  const n = ro.ids.length;
  const g = ro.grip(i, name);
  return (
    <span class="mv">
      <button type="button" class="xb" aria-label={`Move ${name} up`} disabled={i === 0} onClick={() => ro.move(i, i - 1)}>
        ↑
      </button>
      <button type="button" class="xb" aria-label={`Move ${name} down`} disabled={i === n - 1} onClick={() => ro.move(i, i + 1)}>
        ↓
      </button>
      <span {...g} class="grip mvh" title="Drag">
        ⠿
      </span>
    </span>
  );
}

/** The phone's Reorder and Done, the same in every list. */
export function ReorderButton({ on, set, class: cls }: { on: boolean; set(on: boolean): void; class?: string }) {
  return (
    <button
      type="button"
      class={cls ?? (on ? "btn p reo" : "btn reo")}
      aria-pressed={on}
      onClick={(e) => {
        e.stopPropagation(); // a section header folds on a press; this button does not
        set(!on);
      }}
    >
      {on ? "Done" : "Reorder"}
    </button>
  );
}
