// V0.207: the browser side of still-list.ts, used once in app.tsx for both fronts: the scroll correction that keeps one
// row at its height on the screen when rows change above it. The correction is by hand because Safari, so the
// phone, has no `overflow-anchor`. V0.open-row-glides: then every row in view that changed its place glides from where
// it was to where it is, so the eye can follow a row that moved, the open one above all.
import { useLayoutEffect, useRef } from "preact/hooks";
import { reducedMotion } from "./motion-ui.tsx";
import { anchorRow } from "./still-list.ts";

const LIST = ".list, .m-list";
const GLIDE_MS = 220;
const EASE = "cubic-bezier(.2,.8,.2,1)";

interface Mark {
  id: string | null;
  top: number;
  list: HTMLElement;
  /** each row's top on the screen before the change */
  tops: Map<string, number>;
}

const rowsOf = (list: HTMLElement) => [...list.querySelectorAll<HTMLElement>("[data-id]")];

function measure(open: string | null): Mark | null {
  const list = document.querySelector<HTMLElement>(LIST);
  if (!list) return null;
  const box = list.getBoundingClientRect();
  const rows = rowsOf(list);
  const tops = new Map<string, number>();
  for (const r of rows) if (!tops.has(r.dataset.id!)) tops.set(r.dataset.id!, r.getBoundingClientRect().top);
  const seen = (el: HTMLElement) => {
    const r = el.getBoundingClientRect();
    return r.bottom > box.top && r.top < box.bottom;
  };
  const byId = (id: string) => rows.find((r) => r.dataset.id === id);
  const at = open ? rows.findIndex((r) => r.dataset.id === open) : -1;
  const id = anchorRow({
    hovered: list.querySelector<HTMLElement>("[data-id]:hover")?.dataset.id ?? null,
    open,
    firstVisible: rows.find(seen)?.dataset.id ?? null,
    visible: (x) => {
      const el = byId(x);
      return !!el && seen(el);
    },
    scrollTop: list.scrollTop,
    besideOpen: at < 0 ? null : (rows[at - 1] ?? rows[at + 1])?.dataset.id ?? null,
  });
  const el = id ? byId(id) : undefined;
  return { id: el ? id : null, top: el ? el.getBoundingClientRect().top : 0, list, tops };
}

/** Each row in view whose top moved slides from its old top to its new one. A row that was not drawn before appears. */
function glide(m: Mark): void {
  if (reducedMotion()) return;
  const box = m.list.getBoundingClientRect();
  for (const el of rowsOf(m.list)) {
    const was = m.tops.get(el.dataset.id!);
    if (was === undefined || !el.animate) continue;
    const r = el.getBoundingClientRect();
    const dy = was - r.top;
    if (Math.abs(dy) < 1 || r.bottom < box.top || r.top > box.bottom) continue;
    // From far off the screen the row comes in from just past the edge, not across the whole list.
    const from = Math.max(-box.height, Math.min(box.height, dy));
    el.animate([{ translate: `0 ${from}px` }, { translate: "0 0" }], { duration: GLIDE_MS, easing: EASE });
  }
}

/** Keeps the anchor row at its height when the rows change, then lets the moved rows glide. `order` is the drawn rows,
 *  as one string. */
export function useListAnchor(order: string, open: string | null): void {
  const last = useRef(order);
  // Read during the render, while the screen still shows the old rows.
  const mark = useRef<Mark | null>(null);
  if (order !== last.current) mark.current = measure(open);
  useLayoutEffect(() => {
    last.current = order;
    const m = mark.current;
    mark.current = null;
    if (!m) return;
    const el = m.id ? rowsOf(m.list).find((r) => r.dataset.id === m.id) : undefined;
    if (el) m.list.scrollTop += el.getBoundingClientRect().top - m.top;
    glide(m);
  }, [order]);
}
