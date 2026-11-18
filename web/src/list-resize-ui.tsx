// The handle on the thread list's edge (SPEC V0.184): drag, keys, a double press resets. The width lives in a CSS
// variable on .app, written straight from here, so a drag does not render the whole app on every move.

import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { LIST_WIDTH_DEFAULT, LIST_WIDTH_MIN, clampListWidth, listWidthForKey, listWidthMax, listWidthPref, setListWidthPref } from "./prefs.ts";

const DRAGGING = "list-resizing";
/** Under this a press is a press, not a drag. */
const DRAG_SLOP = 2;
const DOUBLE_PRESS_MS = 400;

interface Drag {
  pointer: number;
  x: number;
  from: number;
  at: number;
  moved: boolean;
}

export function ListResizer({ win }: { win: number }) {
  const ref = useRef<HTMLDivElement>(null);
  const [pref, setPref] = useState<number | null>(() => listWidthPref());
  const drag = useRef<Drag | null>(null);
  const lastPress = useRef(-Infinity);
  const width = clampListWidth(pref ?? LIST_WIDTH_DEFAULT, win);

  const show = (w: number) => {
    const el = ref.current;
    el?.closest<HTMLElement>(".app")?.style.setProperty("--list-w", `${w}px`);
    el?.setAttribute("aria-valuenow", String(w));
  };
  useLayoutEffect(() => show(width), [width]);
  useEffect(() => () => document.documentElement.classList.remove(DRAGGING), []);

  const keep = (w: number | null) => {
    setPref(w);
    setListWidthPref(w);
    show(clampListWidth(w ?? LIST_WIDTH_DEFAULT, win));
  };

  const finish = (d: Drag, kept: boolean) => {
    drag.current = null;
    document.documentElement.classList.remove(DRAGGING);
    if (kept) keep(d.at);
    else show(width);
  };

  return (
    <div
      ref={ref}
      class="lrz"
      role="separator"
      tabIndex={0}
      aria-orientation="vertical"
      aria-label="Resize the thread list"
      aria-valuenow={width}
      aria-valuemin={LIST_WIDTH_MIN}
      aria-valuemax={listWidthMax(win)}
      title="Drag to resize, double-click to reset"
      // No focus ring and no text selection from a press; the keyboard reaches the handle by Tab.
      onMouseDown={(e) => e.preventDefault()}
      onPointerDown={(e) => {
        if (e.button !== 0 || drag.current) return;
        e.currentTarget.setPointerCapture(e.pointerId);
        drag.current = { pointer: e.pointerId, x: e.clientX, from: width, at: width, moved: false };
        document.documentElement.classList.add(DRAGGING);
      }}
      onPointerMove={(e) => {
        const d = drag.current;
        if (!d || d.pointer !== e.pointerId) return;
        const dx = e.clientX - d.x;
        if (!d.moved && Math.abs(dx) < DRAG_SLOP) return;
        d.moved = true;
        d.at = clampListWidth(d.from + dx, win);
        show(d.at);
      }}
      onPointerUp={(e) => {
        const d = drag.current;
        if (!d || d.pointer !== e.pointerId) return;
        finish(d, d.moved);
        if (d.moved) {
          lastPress.current = -Infinity;
          return;
        }
        // Timed here and not with dblclick, so that a double tap on a touch screen resets too.
        if (e.timeStamp - lastPress.current < DOUBLE_PRESS_MS) {
          lastPress.current = -Infinity;
          keep(null);
        } else lastPress.current = e.timeStamp;
      }}
      onPointerCancel={(e) => {
        if (drag.current?.pointer === e.pointerId) finish(drag.current, false);
      }}
      onLostPointerCapture={(e) => {
        if (drag.current?.pointer === e.pointerId) finish(drag.current, false);
      }}
      onKeyDown={(e) => {
        if (e.metaKey || e.ctrlKey || e.altKey) return;
        const next = e.key === "Enter" ? null : listWidthForKey(e.key, e.shiftKey, width, win);
        if (next === null && e.key !== "Enter") return;
        e.preventDefault();
        e.stopPropagation(); // these keys are the handle's while it has the focus, not the app's shortcuts
        keep(next);
      }}
    />
  );
}
