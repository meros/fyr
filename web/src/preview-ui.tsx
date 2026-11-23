// Preview cards in the log, the full-screen viewer and the filmstrip (SPEC 0.5 V0.19, V0.47, V0.165).

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { previewsOf } from "./conversation.ts";
import { bringBackFilmstrip, filmstripView, previewsLabel, setFilmstripHidden, setFilmstripOpen, showsPreviewsChip, takeReveal, useFilmstrip } from "./filmstrip.ts";
import type { HostId } from "./hostid.ts";
import type { FyrEvent } from "./types.ts";
import { downloadUrl } from "./attach.ts";
import { FRAME_ALLOW, filmstrip, frameHeight, type PreviewCard } from "./previews.ts";
import { bridgeMessage } from "./view.ts";
import { requestFullscreen, useLayer } from "./fullscreen.ts";
import { FIT, FRAME_ZOOMS, clampView, doubleTap, stepFrameZoom, toggleView, wheelFactor, zoomAt, type Point, type Tap, type View } from "./zoom.ts";

/** An image the viewer shows: a preview card's, or an attachment the owner sent (V0.165). */
export interface ViewerImage {
  key: string;
  src: string;
  title: string;
  caption?: string;
  /** the file's name, for a source that carries none (V0.download-assets) */
  name?: string;
}
/** One open viewer at a time: the images it can swipe between and the one on screen. */
type Open = { items: ViewerImage[]; at: number } | null;
let setOpenGlobal: ((o: Open) => void) | null = null;

/** Open the viewer on `items[at]`, full screen where the browser can (call it inside the press). */
export function openImages(items: ViewerImage[], at: number) {
  if (!items[at]) return;
  requestFullscreen(document.documentElement);
  setOpenGlobal?.({ items, at });
}

function cardImages(all: PreviewCard[]): ViewerImage[] {
  return all
    .filter((x) => x.kind === "image")
    .sort((a, b) => a.seq - b.seq)
    .map((c) => ({ key: `pv-${c.seq}`, src: c.src, title: c.title, caption: c.caption }));
}

function openCard(card: PreviewCard, all: PreviewCard[]) {
  const items = cardImages(all.some((x) => x.seq === card.seq) ? all : [...all, card]);
  openImages(items, items.findIndex((x) => x.key === `pv-${card.seq}`));
}

function previewDomId(seq: number): string {
  return `pv-${seq}`;
}

/** V0.download-assets: Download, on an image's card, in the viewer and on a sent file. A link, so the browser saves
 *  it the way it saves any file (the phone's share sheet or Downloads, the desktop's Downloads). */
export function DownloadLink({ src, name, cls }: { src: string; name?: string; cls: string }) {
  if (!src) return null;
  return (
    <a class={cls} href={downloadUrl(src)} download={name ?? ""} aria-label="Download" title="Download" onPointerDown={(e) => e.stopPropagation()}>
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M12 4v11" />
        <path d="m7 10.5 5 5 5-5" />
        <path d="M5 20h14" />
      </svg>
    </a>
  );
}

/** A markdown image in a message (`.md-img`) opens in the viewer on a press (V0.download-assets), where it zooms
 *  and has Download. Put on the element that holds the rendered markdown. */
export function openMarkdownImage(e: MouseEvent) {
  const t = e.target as HTMLElement | null;
  if (!(t instanceof HTMLImageElement) || !t.classList.contains("md-img")) return;
  const src = t.getAttribute("src") ?? "";
  if (src) openImages([{ key: src, src, title: t.alt }], 0);
}

/** The Full screen button of every preview card and the live view (V0.165). */
export function FsButton({ onPress }: { onPress(): void }) {
  return (
    <button class="pv-fs" aria-label="Full screen" title="Full screen" onClick={onPress}>
      ⛶
    </button>
  );
}

/**
 * V0.165: an HTML page (a preview or the live view) full screen in place: the same frame, so it does not reload and
 * the live view's bridge keeps working. The page zooms by steps (it lays out at width / zoom and is scaled up),
 * which keeps the sandbox as it is; a page may also handle its own pinch.
 */
export function useFrameFullscreen(box: { current: HTMLElement | null }) {
  const [on, setOn] = useState(false);
  const [zoom, setZoom] = useState(1);
  const leave = useLayer(on, () => {
    setOn(false);
    setZoom(1);
  });
  const enter = () => {
    requestFullscreen(box.current);
    setOn(true);
  };
  const frameStyle = (inline?: Record<string, string>) =>
    on ? { width: `${100 / zoom}%`, height: `${100 / zoom}%`, transform: `scale(${zoom})`, transformOrigin: "0 0" } : inline;
  return { on, enter, leave, zoom, setZoom, frameStyle };
}

/** The bar over a full-screen page: its title, the zoom steps and Close. */
export function FsBar({ title, fs }: { title: string; fs: ReturnType<typeof useFrameFullscreen> }) {
  return (
    <div class="fz-bar">
      <span class="pv-ft">{title}</span>
      <button class="fz-out" aria-label="Zoom out" title="Zoom out" disabled={fs.zoom <= FRAME_ZOOMS[0]} onClick={() => fs.setZoom(stepFrameZoom(fs.zoom, -1))}>
        −
      </button>
      <button class="fz-reset" title="Actual size" onClick={() => fs.setZoom(1)}>
        {Math.round(fs.zoom * 100)} %
      </button>
      <button class="fz-in" aria-label="Zoom in" title="Zoom in" disabled={fs.zoom >= FRAME_ZOOMS[FRAME_ZOOMS.length - 1]} onClick={() => fs.setZoom(stepFrameZoom(fs.zoom, 1))}>
        +
      </button>
      <button class="fz-x pv-x" aria-label="Close" title="Close" onClick={fs.leave}>
        ×
      </button>
    </div>
  );
}

export function PreviewCardView({ card, all }: { card: PreviewCard; all: PreviewCard[] }) {
  const [expanded, setExpanded] = useState(false);
  const [reported, setReported] = useState(0);
  const [width, setWidth] = useState(0);
  const box = useRef<HTMLElement>(null);
  const frame = useRef<HTMLIFrameElement>(null);
  const html = card.kind === "html";
  const fs = useFrameFullscreen(box);

  // V0.47: 16:10 of the card's width until the page reports its own height (ui/notifications/size-changed).
  useLayoutEffect(() => {
    const el = box.current;
    if (!html || !el) return;
    const measure = () => setWidth(el.clientWidth);
    measure();
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(measure) : null;
    ro?.observe(el);
    return () => ro?.disconnect();
  }, [html]);
  useEffect(() => {
    if (!html) return;
    const on = (e: MessageEvent) => {
      const m = bridgeMessage(e.source, frame.current?.contentWindow, e.data);
      if (m?.kind === "size") setReported(m.height);
    };
    window.addEventListener("message", on);
    return () => window.removeEventListener("message", on);
  }, [html]);

  const h = frameHeight(card.height, expanded, reported, width);
  const title = card.title || (html ? "Preview" : "Screenshot");
  return (
    <figure class={`pv ${card.kind}${fs.on ? " isfs" : ""}`} id={previewDomId(card.seq)} ref={box}>
      {fs.on ? (
        <FsBar key="bar" title={title} fs={fs} />
      ) : (
        <figcaption key="head" class="pv-h">
          <span class="pv-t">{title}</span>
          {!html && card.width > 0 && (
            <span class="pv-sz">
              {card.width} × {card.height}
            </span>
          )}
          {!html && <DownloadLink cls="pv-dl pv-cdl" src={card.src} name={card.title} />}
          <FsButton onPress={() => (html ? fs.enter() : openCard(card, all))} />
        </figcaption>
      )}
      {card.kind === "image" ? (
        <button class="pv-img" onClick={() => openCard(card, all)} aria-label={`Open ${card.title || "screenshot"} full screen`}>
          <img src={card.src} alt={card.title} loading="lazy" decoding="async" width={card.width || undefined} height={card.height || undefined} />
        </button>
      ) : (
        <>
          {/* keyed: the header's swap with the bar must never take this wrapper, or the frame reloads */}
          <div key="frame" class="fzw">
            <iframe
              ref={frame}
              class="pv-frame"
              src={card.src}
              sandbox="allow-scripts"
              allow={FRAME_ALLOW}
              allowFullScreen
              loading="lazy"
              title={title}
              style={fs.frameStyle({ height: `${h}px` })}
            />
          </div>
          {!fs.on && frameHeight(card.height, true, reported, width) > frameHeight(card.height, false, reported, width) && (
            <button key="more" class="pv-more" onClick={() => setExpanded(!expanded)}>
              {expanded ? "Show less" : "Show all"}
            </button>
          )}
        </>
      )}
      {!fs.on && (card.caption || card.url) && (
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

/**
 * Mounted once by the app: the image viewer (V0.19, V0.165). Fitted by default, never cut off; pinch, wheel or a
 * trackpad pinch zoom about the fingers or the pointer, a drag pans, a double tap or double click toggles fit and
 * 100 %; swipe or the arrow keys move between the images. Esc, Close and Back leave.
 */
export function PreviewViewer() {
  const [open, setOpen] = useState<Open>(null);
  const [view, setView] = useState<View>(FIT);
  const [natural, setNatural] = useState({ width: 0, height: 0 });
  const stage = useRef<HTMLDivElement>(null);
  const pointers = useRef(new Map<number, Point>());
  const gesture = useRef<{ d0: number; m0: Point; v0: View; p0: Point; moved: boolean } | null>(null);
  const lastTap = useRef<Tap | null>(null);
  const lastPointer = useRef("mouse");

  const reset = () => {
    setView(FIT);
    setNatural({ width: 0, height: 0 });
  };
  useEffect(() => {
    setOpenGlobal = (o) => {
      reset();
      setOpen(o);
    };
    return () => {
      setOpenGlobal = null;
    };
  }, []);

  const close = useLayer(!!open, () => setOpen(null));
  const go = (dir: 1 | -1) => {
    if (!open || !open.items[open.at + dir]) return;
    reset();
    setOpen({ ...open, at: open.at + dir });
  };
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowRight") go(1);
      else if (e.key === "ArrowLeft") go(-1);
      else return;
      e.preventDefault();
      e.stopImmediatePropagation();
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [open]);

  if (!open) return null;
  const c = open.items[open.at];

  const geom = () => {
    const r = stage.current?.getBoundingClientRect();
    const st = { width: r?.width ?? 1, height: r?.height ?? 1 };
    const f = natural.width > 0 && natural.height > 0 ? Math.min(1, st.width / natural.width, st.height / natural.height) : 1;
    const fitted = natural.width > 0 ? { width: natural.width * f, height: natural.height * f } : st;
    return { st, fitted, left: r?.left ?? 0, top: r?.top ?? 0 };
  };
  const local = (e: { clientX: number; clientY: number }, g = geom()): Point => ({ x: e.clientX - g.left, y: e.clientY - g.top });

  const start = () => {
    const ps = [...pointers.current.values()];
    const g = geom();
    const m = ps.length === 2 ? { x: (ps[0].x + ps[1].x) / 2, y: (ps[0].y + ps[1].y) / 2 } : ps[0];
    gesture.current = { d0: ps.length === 2 ? Math.hypot(ps[0].x - ps[1].x, ps[0].y - ps[1].y) : 0, m0: { x: m.x - g.left, y: m.y - g.top }, v0: view, p0: ps[0], moved: gesture.current?.moved ?? false };
  };
  const down = (e: PointerEvent) => {
    lastPointer.current = e.pointerType;
    if (e.pointerType === "mouse" && e.button !== 0) return;
    (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
    if (pointers.current.size === 0) gesture.current = null;
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    start();
  };
  const move = (e: PointerEvent) => {
    if (!pointers.current.has(e.pointerId)) return;
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    const gs = gesture.current;
    if (!gs) return;
    const ps = [...pointers.current.values()];
    const g = geom();
    if (ps.length === 2 && gs.d0 > 0) {
      gs.moved = true;
      const d = Math.hypot(ps[0].x - ps[1].x, ps[0].y - ps[1].y);
      const m = { x: (ps[0].x + ps[1].x) / 2 - g.left, y: (ps[0].y + ps[1].y) / 2 - g.top };
      const z = zoomAt(gs.v0, d / gs.d0, gs.m0, g.st, g.fitted);
      setView(clampView({ ...z, x: z.x + m.x - gs.m0.x, y: z.y + m.y - gs.m0.y }, g.st, g.fitted));
    } else if (ps.length === 1) {
      const dx = e.clientX - gs.p0.x;
      const dy = e.clientY - gs.p0.y;
      if (Math.hypot(dx, dy) > 8) gs.moved = true;
      if (gs.v0.scale > 1) setView(clampView({ ...gs.v0, x: gs.v0.x + dx, y: gs.v0.y + dy }, g.st, g.fitted));
    }
  };
  const up = (e: PointerEvent) => {
    if (!pointers.current.has(e.pointerId)) return;
    const gs = gesture.current;
    pointers.current.delete(e.pointerId);
    if (pointers.current.size > 0) {
      start(); // a finger lifted from a pinch: the other one pans on from here
      return;
    }
    gesture.current = null;
    if (!gs) return;
    const dx = e.clientX - gs.p0.x;
    const dy = e.clientY - gs.p0.y;
    if (gs.d0 === 0 && gs.v0.scale <= 1.01 && Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy) * 1.5) {
      lastTap.current = null;
      go(dx < 0 ? 1 : -1);
      return;
    }
    // A double tap on touch (a phone sends no dependable dblclick with touch-action: none).
    if (e.pointerType !== "mouse" && !gs.moved && e.type === "pointerup") {
      const tap = { t: e.timeStamp, x: e.clientX, y: e.clientY };
      if (doubleTap(lastTap.current, tap)) {
        lastTap.current = null;
        const g = geom();
        setView(toggleView(view, local(e, g), g.st, g.fitted, natural.width));
      } else lastTap.current = tap;
    }
  };
  const dbl = (e: MouseEvent) => {
    if (lastPointer.current !== "mouse") return;
    const g = geom();
    setView(toggleView(view, local(e, g), g.st, g.fitted, natural.width));
  };
  const wheel = (e: WheelEvent) => {
    e.preventDefault();
    const g = geom();
    setView(zoomAt(view, wheelFactor(e.deltaY, e.deltaMode, e.ctrlKey), local(e, g), g.st, g.fitted));
  };

  return (
    <div class="pv-full" role="dialog" aria-label={c.title || "Preview"}>
      <div class="pv-fh">
        <span class="pv-ft">{c.title}</span>
        {open.items.length > 1 && (
          <span class="pv-fn">
            {open.at + 1} / {open.items.length}
          </span>
        )}
        <DownloadLink cls="pv-dl pv-x" src={c.src} name={c.name ?? c.title} />
        <button class="pv-x" aria-label="Close" onClick={close}>
          ×
        </button>
      </div>
      <div
        class={`pv-stage${view.scale > 1 ? " zoomed" : ""}`}
        ref={stage}
        data-scale={String(Math.round(view.scale * 1000) / 1000)}
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={up}
        onPointerCancel={up}
        onDblClick={dbl}
        onWheel={wheel}
      >
        <img
          key={c.key}
          src={c.src}
          alt={c.title}
          draggable={false}
          onLoad={(e) => {
            const i = e.currentTarget as HTMLImageElement;
            setNatural({ width: i.naturalWidth, height: i.naturalHeight });
          }}
          style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}
        />
        {open.at > 0 && (
          <button class="pv-nav l" aria-label="Previous preview" onPointerDown={(e) => e.stopPropagation()} onClick={() => go(-1)}>
            ‹
          </button>
        )}
        {open.at < open.items.length - 1 && (
          <button class="pv-nav r" aria-label="Next preview" onPointerDown={(e) => e.stopPropagation()} onClick={() => go(1)}>
            ›
          </button>
        )}
      </div>
      {c.caption && <div class="pv-fc prose">{c.caption}</div>}
    </div>
  );
}

/**
 * The thread's previews, newest first; a tap scrolls to the card. V0.filmstrip-fold: on both fronts one "N previews"
 * line until a press opens it, with a × that hides it for this thread on this device (the header's PreviewsChip
 * brings it back). `phone` only sizes it for a finger: the line and the × are 44 px there, 28 px on the desktop.
 */
export function Filmstrip({ cards, threadRef, phone }: { cards: PreviewCard[]; threadRef: string; phone?: boolean }) {
  const { hidden, open } = useFilmstrip(threadRef);
  const root = useRef<HTMLDivElement>(null);
  const view = filmstripView(cards.length, hidden, open);
  useEffect(() => {
    if (view === "open" && takeReveal(threadRef)) root.current?.scrollIntoView({ block: "nearest" });
  });
  if (view === "none" || view === "hidden") return null;
  const list = filmstrip(cards);
  return (
    <div ref={root} class={`fs${phone ? " phone" : ""}${open ? " open" : ""}`}>
      <div class="fs-line">
        <button class="fs-sum" onClick={() => setFilmstripOpen(threadRef, !open)} aria-expanded={open}>
          <span class="ell">{previewsLabel(list.length)}</span>
          <span class="chev">{open ? "▾" : "▸"}</span>
        </button>
        <button class="fs-x" aria-label="Hide previews" title="Hide previews in this thread (the previews chip in the header brings them back)" onClick={() => setFilmstripHidden(threadRef, true)}>
          ×
        </button>
      </div>
      {open && (
        <div class="fs-row">
          {list.map((c) => (
            <button
              key={c.seq}
              class={`fs-item ${c.kind}`}
              title={c.title}
              onClick={() => document.getElementById(previewDomId(c.seq))?.scrollIntoView({ block: "center", behavior: "smooth" })}
            >
              {c.kind === "image" ? <img src={c.src} alt="" loading="lazy" /> : <span class="fs-html">HTML</span>}
              <span class="fs-t">{c.title || (c.kind === "html" ? "Preview" : "Screenshot")}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * V0.filmstrip-fold: the header chip that brings a hidden filmstrip back, with the thread's preview count; absent while
 * the strip shows. A press shows the strip open in the conversation (`onShow` moves there). The phone's chip sits in a
 * 44 px button, as the folder chip beside it; the desktop's is the 24 px chip.
 */
export function PreviewsChip({ threadRef, host, events, phone, onShow }: { threadRef: string; host?: HostId; events: FyrEvent[] | undefined; phone?: boolean; onShow(): void }) {
  const api = useApi();
  const { hidden } = useFilmstrip(threadRef);
  const count = useMemo(() => (hidden ? previewsOf(events ?? [], host, api).length : 0), [hidden, events, host]);
  if (!showsPreviewsChip(count, hidden)) return null;
  const label = previewsLabel(count);
  const press = () => {
    bringBackFilmstrip(threadRef);
    onShow();
  };
  return (
    <button type="button" class={phone ? "m-pjb m-pvbtn" : "chip pvc"} aria-label={`${label} hidden. Show them`} title={phone ? undefined : "Show the previews again"} onClick={press}>
      {phone ? <span class="m-chip pvc">{label}</span> : <span>{label}</span>}
    </button>
  );
}
