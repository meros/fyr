// Preview cards in the log, the full-screen viewer and the filmstrip (SPEC 0.5 V0.19).

import { useEffect, useRef, useState } from "preact/hooks";
import { filmstrip, frameHeight, stepImage, type PreviewCard } from "./previews.ts";

/** One open viewer at a time: an image (with the thread's other images to swipe to) or an HTML frame. */
type Open = { card: PreviewCard; all: PreviewCard[] } | null;
let setOpenGlobal: ((o: Open) => void) | null = null;

function openViewer(card: PreviewCard, all: PreviewCard[]) {
  setOpenGlobal?.({ card, all });
}

export function previewDomId(seq: number): string {
  return `pv-${seq}`;
}

export function PreviewCardView({ card, all }: { card: PreviewCard; all: PreviewCard[] }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <figure class={`pv ${card.kind}`} id={previewDomId(card.seq)}>
      <figcaption class="pv-h">
        <span class="pv-t">{card.title || (card.kind === "html" ? "Preview" : "Screenshot")}</span>
        {card.kind === "html" ? (
          <button class="pv-open" onClick={() => openViewer(card, all)}>
            Open
          </button>
        ) : (
          card.width > 0 && (
            <span class="pv-sz">
              {card.width} × {card.height}
            </span>
          )
        )}
      </figcaption>
      {card.kind === "image" ? (
        <button class="pv-img" onClick={() => openViewer(card, all)} aria-label={`Open ${card.title || "screenshot"} full screen`}>
          <img src={card.src} alt={card.title} loading="lazy" width={card.width || undefined} height={card.height || undefined} />
        </button>
      ) : (
        <>
          <iframe class="pv-frame" src={card.src} sandbox="allow-scripts" loading="lazy" title={card.title || "Preview"} style={{ height: `${frameHeight(card.height, expanded)}px` }} />
          {card.height > frameHeight(card.height, false) && (
            <button class="pv-more" onClick={() => setExpanded(!expanded)}>
              {expanded ? "Show less" : "Show all"}
            </button>
          )}
        </>
      )}
      {(card.caption || card.url) && (
        <div class="pv-c">
          {card.caption && <span class="prose">{card.caption}</span>}
          {card.url && (
            <a class="pv-url" href={card.url} target="_blank" rel="noopener noreferrer">
              {card.url}
            </a>
          )}
        </div>
      )}
    </figure>
  );
}

/** Mounted once by the app: the full-screen viewer. Esc and Back close it; swipe or arrows move between images. */
export function PreviewViewer() {
  const [open, setOpen] = useState<Open>(null);
  const [scale, setScale] = useState(1);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const gesture = useRef<{ dist: number; scale: number; x0: number; y0: number; pan: { x: number; y: number } } | null>(null);
  const pushed = useRef(false);

  useEffect(() => {
    setOpenGlobal = (o) => {
      setScale(1);
      setPan({ x: 0, y: 0 });
      setOpen(o);
    };
    return () => {
      setOpenGlobal = null;
    };
  }, []);

  const close = () => {
    if (pushed.current) {
      pushed.current = false;
      history.back(); // popstate closes
    } else setOpen(null);
  };
  const go = (dir: 1 | -1) => {
    if (!open || open.card.kind !== "image") return;
    const next = stepImage(open.all, open.card.seq, dir);
    if (next) {
      setScale(1);
      setPan({ x: 0, y: 0 });
      setOpen({ ...open, card: next });
    }
  };

  // A history entry, so the system Back closes the viewer and nothing else.
  useEffect(() => {
    if (!open) return;
    if (!pushed.current) {
      history.pushState({ ...(history.state ?? {}), viewer: true }, "");
      pushed.current = true;
    }
    const onPop = () => {
      pushed.current = false;
      setOpen(null);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
      else if (e.key === "ArrowRight") go(1);
      else if (e.key === "ArrowLeft") go(-1);
      else return;
      e.preventDefault();
      e.stopImmediatePropagation();
    };
    window.addEventListener("popstate", onPop);
    window.addEventListener("keydown", onKey, true);
    return () => {
      window.removeEventListener("popstate", onPop);
      window.removeEventListener("keydown", onKey, true);
    };
  }, [open]);

  if (!open) return null;
  const c = open.card;

  const down = (e: PointerEvent) => {
    (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    const ps = [...pointers.current.values()];
    gesture.current = { dist: ps.length === 2 ? Math.hypot(ps[0].x - ps[1].x, ps[0].y - ps[1].y) : 0, scale, x0: e.clientX, y0: e.clientY, pan };
  };
  const move = (e: PointerEvent) => {
    if (!pointers.current.has(e.pointerId)) return;
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    const g = gesture.current;
    if (!g) return;
    const ps = [...pointers.current.values()];
    if (ps.length === 2 && g.dist > 0) {
      const d = Math.hypot(ps[0].x - ps[1].x, ps[0].y - ps[1].y);
      setScale(Math.min(5, Math.max(1, (g.scale * d) / g.dist)));
    } else if (ps.length === 1 && scale > 1) {
      setPan({ x: g.pan.x + e.clientX - g.x0, y: g.pan.y + e.clientY - g.y0 });
    }
  };
  const up = (e: PointerEvent) => {
    const g = gesture.current;
    pointers.current.delete(e.pointerId);
    if (pointers.current.size === 0 && g && scale <= 1.02 && g.dist === 0) {
      const dx = e.clientX - g.x0;
      const dy = e.clientY - g.y0;
      if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy) * 1.5) go(dx < 0 ? 1 : -1);
    }
    if (scale < 1.02) {
      setScale(1);
      setPan({ x: 0, y: 0 });
    }
    gesture.current = null;
  };
  const imgs = open.all.filter((x) => x.kind === "image").sort((a, b) => a.seq - b.seq);
  const pos = imgs.findIndex((x) => x.seq === c.seq);

  return (
    <div class="pv-full" role="dialog" aria-label={c.title || "Preview"}>
      <div class="pv-fh">
        <span class="pv-ft">{c.title}</span>
        {c.kind === "image" && imgs.length > 1 && (
          <span class="pv-fn">
            {pos + 1} / {imgs.length}
          </span>
        )}
        <button class="pv-x" aria-label="Close" onClick={close}>
          ×
        </button>
      </div>
      {c.kind === "image" ? (
        <div class="pv-stage" onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerCancel={up} onDblClick={() => (setScale(scale > 1 ? 1 : 2.5), setPan({ x: 0, y: 0 }))}>
          <img src={c.src} alt={c.title} draggable={false} style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${scale})` }} />
          {pos > 0 && (
            <button class="pv-nav l" aria-label="Previous preview" onClick={() => go(-1)}>
              ‹
            </button>
          )}
          {pos >= 0 && pos < imgs.length - 1 && (
            <button class="pv-nav r" aria-label="Next preview" onClick={() => go(1)}>
              ›
            </button>
          )}
        </div>
      ) : (
        <iframe class="pv-fframe" src={c.src} sandbox="allow-scripts" title={c.title || "Preview"} />
      )}
      {c.caption && <div class="pv-fc prose">{c.caption}</div>}
    </div>
  );
}

/** The thread's previews, newest first; a tap scrolls to the card. `collapsible` is the phone's "N previews ▸" line. */
export function Filmstrip({ cards, collapsible }: { cards: PreviewCard[]; collapsible?: boolean }) {
  const [open, setOpen] = useState(false);
  if (!cards.length) return null;
  const list = filmstrip(cards);
  const strip = (
    <div class="fs-row">
      {list.map((c) => (
        <button
          class={`fs-item ${c.kind}`}
          title={c.title}
          onClick={() => document.getElementById(previewDomId(c.seq))?.scrollIntoView({ block: "center", behavior: "smooth" })}
        >
          {c.kind === "image" ? <img src={c.src} alt="" loading="lazy" /> : <span class="fs-html">HTML</span>}
          <span class="fs-t">{c.title || (c.kind === "html" ? "Preview" : "Screenshot")}</span>
        </button>
      ))}
    </div>
  );
  if (!collapsible) return <div class="fs">{strip}</div>;
  return (
    <div class={`fs phone${open ? " open" : ""}`}>
      <button class="fs-sum" onClick={() => setOpen(!open)} aria-expanded={open}>
        <span class="ell">
          {list.length} {list.length === 1 ? "preview" : "previews"}
        </span>
        <span class="chev">{open ? "▾" : "▸"}</span>
      </button>
      {open && strip}
    </div>
  );
}
