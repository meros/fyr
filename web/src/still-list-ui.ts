// V0.207: the browser side of still-list.ts, used once in app.tsx for both fronts: where the owner's hand is, and the
// scroll correction that keeps one row at its height on the screen. The correction is by hand because Safari, so the
// phone, has no `overflow-anchor`.
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { KEY_HOLD_MS, NO_HAND, TOUCH_HOLD_MS, anchorRow, holding, type Hand } from "./still-list.ts";

const LIST = ".list, .m-list";

function inList(t: EventTarget | null): boolean {
  return t instanceof Element && !!t.closest(LIST);
}

/** Whether the owner's hand is on the list, and `keyed` for the j/k handler. */
export function useHand(): { hold: boolean; inside: boolean; keyed(): void } {
  const [hand, setHand] = useState<Hand>(NO_HAND);
  const [, tick] = useState(0);
  useEffect(() => {
    const over = (e: PointerEvent) => {
      if (e.pointerType !== "mouse") return;
      const inside = inList(e.target);
      setHand((h) => (h.inside === inside ? h : { ...h, inside }));
    };
    const gone = () => setHand((h) => (h.inside ? { ...h, inside: false } : h));
    const down = (e: TouchEvent) => {
      if (inList(e.target)) setHand((h) => ({ ...h, touching: true }));
    };
    const up = () => setHand((h) => (h.touching ? { ...h, touching: false, until: Date.now() + TOUCH_HOLD_MS } : h));
    const scrolled = (e: Event) => {
      if (inList(e.target)) setHand((h) => ({ ...h, until: Date.now() + TOUCH_HOLD_MS }));
    };
    document.addEventListener("pointerover", over, true);
    document.documentElement.addEventListener("mouseleave", gone);
    document.addEventListener("touchstart", down, { capture: true, passive: true });
    document.addEventListener("touchend", up, true);
    document.addEventListener("touchcancel", up, true);
    document.addEventListener("scroll", scrolled, { capture: true, passive: true });
    return () => {
      document.removeEventListener("pointerover", over, true);
      document.documentElement.removeEventListener("mouseleave", gone);
      document.removeEventListener("touchstart", down, true);
      document.removeEventListener("touchend", up, true);
      document.removeEventListener("touchcancel", up, true);
      document.removeEventListener("scroll", scrolled, true);
    };
  }, []);
  // The render after `until` ends the hold.
  useEffect(() => {
    const left = hand.until - Date.now();
    if (left <= 0) return;
    const id = setTimeout(() => tick((n) => n + 1), left + 10);
    return () => clearTimeout(id);
  }, [hand.until]);
  return { hold: holding(hand, Date.now()), inside: hand.inside, keyed: () => setHand((h) => ({ ...h, until: Date.now() + KEY_HOLD_MS })) };
}

interface Mark {
  id: string;
  top: number;
  list: HTMLElement;
}

function measure(open: string | null, inside: boolean): Mark | null {
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
    hovered: inside ? (list.querySelector<HTMLElement>("[data-id]:hover")?.dataset.id ?? null) : null,
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
export function useListAnchor(order: string, open: string | null, inside: boolean): void {
  const last = useRef(order);
  // Read during the render, while the screen still shows the old rows.
  const mark = useRef<Mark | null>(null);
  if (order !== last.current) mark.current = measure(open, inside);
  useLayoutEffect(() => {
    last.current = order;
    const m = mark.current;
    mark.current = null;
    if (!m) return;
    const el = [...m.list.querySelectorAll<HTMLElement>("[data-id]")].find((r) => r.dataset.id === m.id);
    if (el) m.list.scrollTop += el.getBoundingClientRect().top - m.top;
  }, [order]);
}
