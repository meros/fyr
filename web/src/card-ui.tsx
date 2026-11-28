// V0.load-card, V0.hover-cards: a card (card.ts) drawn, and the desktop's hover card that holds one. The phone has no
// hover: each place that opens a card on hover on the desktop shows the same CardView as text on the phone (the load
// sheet, the thread's Details, a quota line's tap).

import type { ComponentChildren, RefObject } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { createPortal } from "preact/compat";
import type { Card } from "./card.ts";
import { anchorGone, placePopover, type Box, type Side } from "./popover.ts";

/** A card: the head and the verdict in its level's colour, the sentence, the groups of rows, the quiet last line. */
export function CardView({ c, head = true }: { c: Card; /** false: the place around it already names it */ head?: boolean }) {
  const h = head && c.head;
  return (
    <div class={`lcard ${c.level ?? "calm"}`}>
      {(h || c.verdict) && (
        <div class="lcv">
          {h && <b class={c.headMono ? "mono ell" : "ell"}>{c.head}</b>}
          {c.verdict && <span class="lcl">{c.verdict}</span>}
        </div>
      )}
      {c.summary && <p class="lcs">{c.summary}</p>}
      {c.sections.map((sec) => (
        <section class={`lcsec${h || c.verdict || c.summary ? "" : " first"}`}>
          {sec.title && <h4>{sec.title}</h4>}
          {sec.list ? (
            <ul class="lclist">
              {sec.rows.map((r) => (
                <li class={r.hot ? "hot" : undefined}>
                  <span class="lcval">{r.value}</span>
                  {r.note && <span class="lcn">{r.note}</span>}
                </li>
              ))}
            </ul>
          ) : (
            <dl>
              {sec.rows.map((r) => (
                <>
                  <dt>{r.label}</dt>
                  <dd class={r.hot ? "hot" : undefined}>
                    <span class="lcval">{r.value}</span>
                    {r.note && <span class="lcn">{r.note}</span>}
                  </dd>
                </>
              ))}
            </dl>
          )}
        </section>
      ))}
      {c.source && <p class="lcsrc">{c.source}</p>}
    </div>
  );
}

/** Where a popover opens: beside its anchor (the rail, on the left edge) or under it (a chip in a header). */
type HoverPlace = Side;

/** The anchor's box, read again whenever the card is placed (a getter follows the anchor as the page scrolls or
 *  resizes; a DOMRect is a fixed point). */
export type AnchorAt = DOMRect | (() => DOMRect | null);

/** V0.popover-place: the desktop's popover, a hover card or a click card (`interactive`). On document.body (the rail's
 *  containment and a header's overflow would clip it); placed by placePopover against the visible area, again on every
 *  scroll, resize and change of its own size, so it stays at its anchor; a nub points at the anchor. When the anchor
 *  leaves the view (scrolled away, or gone) it calls `onGone` and is hidden meanwhile. Not for the phone: a tap opens a
 *  bottom sheet there (FolderChip), and a hover card never opens from a touch (useHover). */
export function HoverCard({ at, place = "right", interactive = false, popRef, onGone, children }: { at: AnchorAt; place?: HoverPlace; interactive?: boolean; popRef?: RefObject<HTMLDivElement | null>; onGone?: () => void; children: ComponentChildren }) {
  const inner = useRef<HTMLDivElement>(null);
  const ref = popRef ?? inner;
  const body = useRef<HTMLDivElement>(null);
  const live = useRef({ at, onGone });
  live.current = { at, onGone };
  useLayoutEffect(() => {
    const el = ref.current;
    const box = body.current;
    if (!el || !box) return;
    let frame = 0;
    const place1 = () => {
      frame = 0;
      const a = live.current.at;
      const r = typeof a === "function" ? a() : a;
      const area = visibleArea();
      if (anchorGone(r, area)) {
        el.style.visibility = "hidden";
        live.current.onGone?.();
        return;
      }
      // the card's own height without the cap (the body scrolls past it): its content plus its border
      const card = { width: el.offsetWidth, height: box.scrollHeight + (el.offsetHeight - box.clientHeight) };
      const p = placePopover(r!, card, area, place);
      el.style.left = `${p.left}px`;
      el.style.top = `${p.top}px`;
      box.style.maxHeight = `${Math.max(0, p.maxHeight - (el.offsetHeight - box.clientHeight))}px`;
      el.style.setProperty("--nub", `${p.nub}px`);
      el.dataset.side = p.side;
      el.style.visibility = "";
    };
    const later = () => {
      if (!frame) frame = requestAnimationFrame(place1);
    };
    place1();
    const ro = typeof ResizeObserver === "function" ? new ResizeObserver(later) : null;
    ro?.observe(box.firstElementChild ?? box);
    window.addEventListener("scroll", later, true);
    window.addEventListener("resize", later);
    window.visualViewport?.addEventListener("resize", later);
    window.visualViewport?.addEventListener("scroll", later);
    return () => {
      if (frame) cancelAnimationFrame(frame);
      ro?.disconnect();
      window.removeEventListener("scroll", later, true);
      window.removeEventListener("resize", later);
      window.visualViewport?.removeEventListener("resize", later);
      window.visualViewport?.removeEventListener("scroll", later);
    };
  }, [place, ref]);
  return createPortal(
    <div ref={ref} class={`lcpop${interactive ? " pop-click" : ""}`} role={interactive ? "dialog" : "tooltip"} style={{ visibility: "hidden" }}>
      <div class="lcpop-in" ref={body}>
        <div>{children}</div>
      </div>
    </div>,
    document.body,
  );
}

/** The part of the window the owner sees: the visual viewport when the phone's keyboard or a pinch shrinks it. */
function visibleArea(): Box {
  const v = window.visualViewport;
  return v ? { left: v.offsetLeft, top: v.offsetTop, width: v.width, height: v.height } : { left: 0, top: 0, width: window.innerWidth, height: window.innerHeight };
}

// V0.popover-place: the last pointer the owner used. A tap on a phone also sends mouseenter and focus to what it hit;
// those must not open a hover card, which has nothing to close it on a touch screen (it stuck over the page).
let lastPointer = "mouse";
let tracking = false;
function trackPointer() {
  if (tracking || typeof document === "undefined") return;
  tracking = true;
  const on = (e: PointerEvent) => (lastPointer = e.pointerType || "mouse");
  document.addEventListener("pointerover", on, true);
  document.addEventListener("pointerdown", on, true);
  document.addEventListener("keydown", () => (lastPointer = "key"), true);
}

/** How long the mouse rests on an anchor before its card opens: a mouse that crosses the rail does not flash cards. */
const HOVER_DELAY = 180;

/** Opens a hover card on mouse rest and keyboard focus of its anchor, closes it on leave and blur; never from a touch.
 *  `at` follows the anchor while open; with `edge` (a selector of an ancestor, the rail) its right side is that
 *  ancestor's, so a small button's card opens past the rail, not over it. */
export function useHover(edge?: string): { at: (() => DOMRect | null) | null; close(): void; bind: { onMouseEnter(e: Event): void; onMouseLeave(): void; onFocus(e: Event): void; onBlur(): void } } {
  trackPointer();
  const [el, setEl] = useState<HTMLElement | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const at = el
    ? () => {
        if (!el.isConnected) return null;
        const b = el.getBoundingClientRect();
        const right = edge ? (el.closest(edge)?.getBoundingClientRect().right ?? b.right) : b.right;
        return new DOMRect(b.left, b.top, Math.max(b.width, right - b.left), b.height);
      }
    : null;
  const close = () => {
    clearTimeout(timer.current);
    setEl(null);
  };
  const enter = (e: Event) => {
    if (lastPointer === "touch" || lastPointer === "pen") return;
    const t = e.currentTarget as HTMLElement;
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setEl(t), HOVER_DELAY);
  };
  const focus = (e: Event) => {
    if (lastPointer === "touch" || lastPointer === "pen") return;
    clearTimeout(timer.current);
    setEl(e.currentTarget as HTMLElement);
  };
  return { at, close, bind: { onMouseEnter: enter, onMouseLeave: close, onFocus: focus, onBlur: close } };
}

/** A click card's way out: a press outside the anchor and the card, or Escape (V0.dialogs-review (21): Esc closes the
 *  popover and nothing else; heard first, in the capture phase, and stopped, so the app's Esc under it does not act). */
export function usePopoverDismiss(open: boolean, refs: RefObject<HTMLElement | null>[], close: () => void) {
  const live = useRef(close);
  live.current = close;
  /* eslint-disable react-hooks/exhaustive-deps -- refs are refs; the listeners live while the card is open */
  useEffect(() => {
    if (!open) return;
    const onDown = (e: Event) => {
      const target = e.target as Node;
      if (refs.some((r) => r.current?.contains(target))) return;
      live.current();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.stopPropagation();
      e.preventDefault();
      live.current();
    };
    document.addEventListener("pointerdown", onDown);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("pointerdown", onDown);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [open]);
  /* eslint-enable react-hooks/exhaustive-deps */
}
