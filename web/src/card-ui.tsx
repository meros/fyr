// V0.load-card, V0.hover-cards: a card (card.ts) drawn, and the desktop's hover card that holds one. The phone has no
// hover: each place that opens a card on hover on the desktop shows the same CardView as text on the phone (the load
// sheet, the thread's Details, a quota line's tap).

import type { ComponentChildren } from "preact";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import { createPortal } from "preact/compat";
import type { Card } from "./card.ts";

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

/** Where a hover card opens: beside its anchor (the rail, on the left edge) or under it (a chip in a header). */
export type HoverPlace = "right" | "below";

/** The desktop's hover card: on document.body (the rail's containment and a header's overflow would clip it), beside or
 *  under its anchor, kept inside the window. Not for the phone: no hover there. */
export function HoverCard({ at, place = "right", children }: { at: DOMRect; place?: HoverPlace; children: ComponentChildren }) {
  const ref = useRef<HTMLDivElement>(null);
  const start = place === "right" ? { left: at.right + 10, top: at.top } : { left: at.left, top: at.bottom + 6 };
  const [pos, setPos] = useState(start);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const left = Math.max(8, Math.min(start.left, window.innerWidth - el.offsetWidth - 8));
    // under the anchor when it fits, else above it
    const top = place === "below" && start.top + el.offsetHeight > window.innerHeight - 8 ? Math.max(8, at.top - 6 - el.offsetHeight) : Math.max(8, Math.min(start.top, window.innerHeight - el.offsetHeight - 8));
    if (left !== pos.left || top !== pos.top) setPos({ left, top });
  });
  return createPortal(
    <div ref={ref} class="lcpop" role="tooltip" style={{ left: `${pos.left}px`, top: `${pos.top}px` }}>
      {children}
    </div>,
    document.body,
  );
}

/** Opens a hover card on mouse enter and keyboard focus of its anchor, closes it on leave and blur. `at` is the
 *  anchor's box while open; with `edge` (a selector of an ancestor, the rail) its right side is that ancestor's, so a
 *  small button's card opens past the rail, not over it. */
export function useHover(edge?: string): { at: DOMRect | null; close(): void; bind: { onMouseEnter(e: Event): void; onMouseLeave(): void; onFocus(e: Event): void; onBlur(): void } } {
  const [at, setAt] = useState<DOMRect | null>(null);
  const open = (e: Event) => {
    const el = e.currentTarget as HTMLElement;
    const b = el.getBoundingClientRect();
    const right = edge ? (el.closest(edge)?.getBoundingClientRect().right ?? b.right) : b.right;
    setAt(new DOMRect(b.left, b.top, Math.max(b.width, right - b.left), b.height));
  };
  const close = () => setAt(null);
  return { at, close, bind: { onMouseEnter: open, onMouseLeave: close, onFocus: open, onBlur: close } };
}
