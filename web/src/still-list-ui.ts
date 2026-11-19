// V0.207: the browser side of still-list.ts, used once in app.tsx for both fronts: the scroll correction that keeps one
// row at its height on the screen when rows change above it. The correction is by hand because Safari, so the
// phone, has no `overflow-anchor`.
import { useLayoutEffect, useRef } from "preact/hooks";
import { anchorRow } from "./still-list.ts";

const LIST = ".list, .m-list";

interface Mark {
  id: string;
  top: number;
  list: HTMLElement;
}

function measure(open: string | null): Mark | null {
  const list = document.querySelector<HTMLElement>(LIST);
  if (!list) return null;
  const box = list.getBoundingClientRect();
  const rows = [...list.querySelectorAll<HTMLElement>("[data-id]")];
  const seen = (el: HTMLElement) => {
    const r = el.getBoundingClientRect();
    return r.bottom > box.top && r.top < box.bottom;
  };
  const byId = (id: string) => rows.find((r) => r.dataset.id === id);
  const id = anchorRow({
    hovered: list.querySelector<HTMLElement>("[data-id]:hover")?.dataset.id ?? null,
    open,
    firstVisible: rows.find(seen)?.dataset.id ?? null,
    visible: (x) => {
      const el = byId(x);
      return !!el && seen(el);
    },
    scrollTop: list.scrollTop,
  });
  const el = id ? byId(id) : undefined;
  return id && el ? { id, top: el.getBoundingClientRect().top, list } : null;
}

/** Keeps the anchor row at its height when the rows change. `order` is the drawn rows, as one string. */
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
    const el = [...m.list.querySelectorAll<HTMLElement>("[data-id]")].find((r) => r.dataset.id === m.id);
    if (el) m.list.scrollTop += el.getBoundingClientRect().top - m.top;
  }, [order]);
}
