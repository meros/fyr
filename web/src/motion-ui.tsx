// V0.native-feel: how the app moves. Every control and sheet of both fronts goes through here:
// - presses answer at once and let go softly (installPress, motion.css),
// - sheets follow the finger and carry its speed into their spring (useSheetMotion),
// - what closes animates out by every route, Back included, because the leaving copy is made as it unmounts,
// - a tab or segment's mark slides to the new choice (SlideMark).
// Transforms and opacity only, played by the compositor. With reduced motion everything lands at once.

import type { ComponentChildren, RefObject } from "preact";
import { useEffect, useLayoutEffect, useRef } from "preact/hooks";
import { useFocusTrap } from "./focus-trap.ts";
import { addSample, DRAG_START_PX, EXIT_SPRING, frameMs, pathUntil, releaseVelocity, rubberBand, type Sample, SHEET_SPRING, sheetCloses, SNAP_SPRING, type Spring, springPath } from "./motion.ts";

type ElRef<T extends HTMLElement = HTMLElement> = RefObject<T | null>;

export const PHONE_QUERY = "(max-width: 760px)";
const matches = (q: string) => typeof matchMedia === "function" && matchMedia(q).matches;
export const reducedMotion = () => matches("(prefers-reduced-motion: reduce)");

const EASE_OUT = "cubic-bezier(.22,1,.36,1)";
const y3d = (y: number) => `translate3d(0,${y}px,0)`;
export const x3d = (x: number) => `translate3d(${x}px,0,0)`;

function playPath(el: HTMLElement, path: number[], css: (n: number) => string): Animation | null {
  if (reducedMotion() || !el.animate || path.length < 2) return null;
  return el.animate(path.map((n) => ({ transform: css(n) })), { duration: frameMs(path), easing: "linear" });
}

/** Moves `el` along a spring; its resting transform is set at once, so an interrupted animation has nothing to undo. */
export function springTo(el: HTMLElement, from: number, to: number, v: number, s: Spring, css: (n: number) => string): Animation | null {
  el.style.transform = to === 0 ? "" : css(to);
  return playPath(el, springPath(from, to, v, s), css);
}

/** A spring that stops as soon as `edge` is reached: for what leaves the screen. */
export function springAway(el: HTMLElement, from: number, edge: number, v: number, css: (n: number) => string): Animation | null {
  el.style.transform = css(edge);
  const over = Math.abs(edge - from) * 0.4 + 60;
  return playPath(el, pathUntil(springPath(from, edge + Math.sign(edge - from || 1) * over, v, EXIT_SPRING), edge), css);
}

const fade = (el: HTMLElement, from: number, to: number, duration: number) => (reducedMotion() || !el.animate ? null : el.animate([{ opacity: from }, { opacity: to }], { duration, easing: "ease-out" }));
const translateY = (el: Element) => new DOMMatrixReadOnly(getComputedStyle(el).transform).m42;

/** A copy that plays the leaving animation after the real element is gone. It cannot be used and is hidden from readers. */
function ghostOf(el: HTMLElement): HTMLElement {
  const g = el.cloneNode(true) as HTMLElement;
  g.setAttribute("inert", "");
  g.setAttribute("aria-hidden", "true");
  g.dataset.ghost = "";
  const from = [el, ...el.querySelectorAll<HTMLElement>("*")];
  const to = [g, ...g.querySelectorAll<HTMLElement>("*")];
  from.forEach((n, i) => {
    if (n.scrollTop) to[i].scrollTop = n.scrollTop;
    if (n instanceof HTMLInputElement || n instanceof HTMLTextAreaElement) (to[i] as HTMLInputElement).value = n.value;
  });
  return g;
}
const ghostHome = (el: Element) => el.closest(".m-app") ?? document.body;

// ---- sheets and dialogs ----

type SheetKind = "sheet" | "dialog";
/** What just closed, for the sheet that opens in the same render: "a sheet never opens a second sheet; it replaces itself". */
let replaced: { ghost: HTMLElement; top: number; kind: SheetKind } | null = null;

/** How far a sheet can be pulled above its rest, at most. */
const PULL_UP_PX = 120;
/** Where the mouse can take hold of a sheet: its handle and its title, so text in it can still be selected. */
const MOUSE_HOLD = ".m-grab,.m-shh,.fsh,.dh";
const NO_DRAG = "input,textarea,select,[contenteditable],[data-nodrag]";
const CONTROL = `${NO_DRAG},button,a,label,summary`;

function scrolledAbove(target: Element, panel: HTMLElement): boolean {
  for (let n: Element | null = target; n; n = n === panel ? null : n.parentElement) {
    if (n.scrollTop > 0) return true;
  }
  return false;
}
function canScroll(target: Element, panel: HTMLElement): boolean {
  for (let n: Element | null = target; n; n = n === panel ? null : n.parentElement) {
    if (n.scrollHeight > n.clientHeight + 1 && /auto|scroll/.test(getComputedStyle(n).overflowY)) return true;
  }
  return false;
}

function useSheetMotion(root: ElRef, back: ElRef, panel: ElRef, kind: SheetKind, onClose?: () => void) {
  const close = useRef(onClose);
  close.current = onClose;
  const flung = useRef(0);

  useLayoutEffect(() => {
    const r = root.current!;
    const b = back.current!;
    const p = panel.current!;
    const prev = replaced?.kind === kind ? replaced : null;
    if (prev) {
      // One sheet became another: the scrim stays and the edge moves from the old height to the new one.
      prev.ghost.remove();
      replaced = null;
      if (kind === "sheet") springTo(p, Math.max(prev.top - p.getBoundingClientRect().top, -60), 0, 0, SNAP_SPRING, y3d);
    } else if (!reducedMotion() && p.animate) {
      fade(b, 0, 1, kind === "sheet" ? 260 : 160);
      if (kind === "sheet") springTo(p, p.offsetHeight, 0, 0, SHEET_SPRING, y3d);
      else p.animate([{ opacity: 0, scale: 0.97, translate: "0 6px" }, { opacity: 1, scale: 1, translate: "0 0" }], { duration: 200, easing: EASE_OUT });
    }
    return () => {
      if (reducedMotion() || !r.isConnected || !p.animate) return;
      const y = translateY(p);
      const opacity = +getComputedStyle(b).opacity;
      const height = p.offsetHeight;
      const top = p.getBoundingClientRect().top;
      const g = ghostOf(r);
      const gp = g.querySelector<HTMLElement>(":scope > :not(.sheet-back)")!;
      const gb = g.querySelector<HTMLElement>(":scope > .sheet-back")!;
      gp.getAnimations().forEach((a) => a.cancel());
      ghostHome(r).appendChild(g);
      const copy = { ghost: g, top, kind };
      replaced = copy;
      setTimeout(() => {
        if (replaced === copy) replaced = null;
      }, 0);
      gp.style.transform = y3d(y);
      const a = kind === "sheet" ? springAway(gp, y, height + 24, Math.max(flung.current, 0), y3d) : gp.animate([{ opacity: 1, scale: 1 }, { opacity: 0, scale: 0.98 }], { duration: 120, easing: "ease-in", fill: "forwards" });
      const ms = Math.min(a ? Number(a.effect?.getComputedTiming().duration) || 200 : 0, 360);
      gb.style.opacity = "0";
      fade(gb, opacity, 0, ms);
      const done = () => g.remove();
      a?.finished.then(done, done);
      setTimeout(done, 600);
    };
  }, []);

  useEffect(() => {
    if (kind !== "sheet") return;
    const p = panel.current!;
    const b = back.current!;
    let g: { x0: number; y0: number; base: number; on: boolean; off: boolean; target: Element; list: Sample[] } | null = null;

    const y = () => translateY(p);
    const paint = (n: number) => {
      p.style.transform = y3d(n);
      b.style.opacity = String(Math.max(0, 1 - Math.max(n, 0) / (p.offsetHeight || 1)));
    };
    const start = (x: number, yy: number, target: Element) => {
      g = { x0: x, y0: yy, base: 0, on: false, off: target.closest(NO_DRAG) !== null, target, list: [] };
    };
    /** True while the sheet owns the gesture. */
    const move = (x: number, yy: number, t: number): boolean => {
      if (!g || g.off) return false;
      const dx = x - g.x0;
      const dy = yy - g.y0;
      if (!g.on) {
        if (Math.hypot(dx, dy) < DRAG_START_PX) return false;
        const down = dy > 0 && !scrolledAbove(g.target, p);
        const up = dy < 0 && !canScroll(g.target, p);
        if (Math.abs(dx) > Math.abs(dy) || !(down || up)) {
          g.off = true;
          return false;
        }
        g.on = true;
        g.base = y();
        g.y0 = yy;
        p.getAnimations().forEach((a) => a.cancel());
        b.getAnimations().forEach((a) => a.cancel());
        p.style.willChange = "transform";
        (document.activeElement as HTMLElement | null)?.blur?.();
      }
      g.list = addSample(g.list, t, yy);
      const raw = g.base + yy - g.y0;
      paint(raw < 0 ? rubberBand(raw, PULL_UP_PX) : raw);
      return true;
    };
    const end = (t: number) => {
      const was = g;
      g = null;
      if (!was?.on) return;
      p.style.willChange = "";
      const v = releaseVelocity(was.list, t);
      const at = y();
      const settle = () => {
        const from = y();
        const opacity = +getComputedStyle(b).opacity;
        b.style.opacity = "";
        springTo(p, from, 0, 0, SHEET_SPRING, y3d);
        fade(b, opacity, 1, 200);
      };
      if (sheetCloses(at, v, p.offsetHeight) && close.current) {
        flung.current = v;
        close.current();
        // Back closes the sheet a moment later; if nothing did, it returns.
        setTimeout(() => p.isConnected && settle(), 500);
      } else {
        b.style.opacity = "";
        const opacity = Math.max(0, 1 - Math.max(at, 0) / (p.offsetHeight || 1));
        springTo(p, at, 0, v, SHEET_SPRING, y3d);
        fade(b, opacity, 1, 200);
      }
    };

    const ts = (e: TouchEvent) => (e.touches.length === 1 ? start(e.touches[0].clientX, e.touches[0].clientY, e.target as Element) : (g = null));
    const tm = (e: TouchEvent) => {
      if (e.touches.length === 1 && move(e.touches[0].clientX, e.touches[0].clientY, e.timeStamp) && e.cancelable) e.preventDefault();
    };
    const te = (e: TouchEvent) => end(e.timeStamp);
    // The mouse drags too (a narrow desktop window), from anything that is not a control.
    const pm = (e: PointerEvent) => void move(e.clientX, e.clientY, e.timeStamp);
    const pu = (e: PointerEvent) => {
      window.removeEventListener("pointermove", pm);
      window.removeEventListener("pointerup", pu);
      end(e.timeStamp);
    };
    const pd = (e: PointerEvent) => {
      const el = e.target as Element;
      if (e.pointerType !== "mouse" || e.button !== 0 || el.closest(CONTROL) || !(el === p || el.closest(MOUSE_HOLD))) return;
      e.preventDefault();
      start(e.clientX, e.clientY, e.target as Element);
      window.addEventListener("pointermove", pm);
      window.addEventListener("pointerup", pu);
    };
    p.addEventListener("touchstart", ts, { passive: true });
    p.addEventListener("touchmove", tm, { passive: false });
    p.addEventListener("touchend", te);
    p.addEventListener("touchcancel", te);
    p.addEventListener("pointerdown", pd);
    return () => {
      p.removeEventListener("touchstart", ts);
      p.removeEventListener("touchmove", tm);
      p.removeEventListener("touchend", te);
      p.removeEventListener("touchcancel", te);
      p.removeEventListener("pointerdown", pd);
      window.removeEventListener("pointermove", pm);
      window.removeEventListener("pointerup", pu);
    };
  }, [kind]);
}

/** The phone's bottom sheet: every sheet of the app. `dismiss` is the scrim's tap and the drag; a long press opens a sheet
 *  under the finger, so the scrim ignores taps for the first 500 ms. */
export function BottomSheet({ onClose, label, cls, scrimCls, role = "dialog", panelRef, children }: { onClose(): void; label: string; cls?: string; scrimCls?: string; role?: "dialog" | "alertdialog"; panelRef?: ElRef<HTMLDivElement>; children: ComponentChildren }) {
  const root = useRef<HTMLDivElement>(null);
  const back = useRef<HTMLDivElement>(null);
  const own = useRef<HTMLDivElement>(null);
  const panel = panelRef ?? own;
  const opened = useRef(Date.now());
  useSheetMotion(root, back, panel, "sheet", onClose);
  // V0.composer-focus-reach (2): the sheet itself takes the focus, not its first control: a field focused on open would raise the
  // phone's keyboard over the sheet before the owner touched it.
  useFocusTrap(panel, "panel");
  return (
    <div class={`m-scrim${scrimCls ? ` ${scrimCls}` : ""}`} ref={root}>
      <div class="sheet-back" ref={back} onClick={() => Date.now() - opened.current > 500 && onClose()} />
      <div class={`m-sheet${cls ? ` ${cls}` : ""}`} ref={panel} role={role} aria-modal="true" aria-label={label}>
        <div class="m-grab" />
        {children}
      </div>
    </div>
  );
}

/** The dialog of both fronts. At phone width with `onClose` it is a bottom sheet and drags like one (V0.32's reused
 *  dialogs); on the desktop it fades and settles, and the mouse has nothing to drag. */
export function DialogScrim({ onClose, onBack, label, cls, scrimCls, role = "dialog", panelRef, children }: { onClose?: () => void; onBack?: () => void; label: string; cls?: string; scrimCls?: string; role?: "dialog" | "alertdialog"; panelRef?: ElRef<HTMLDivElement>; children: ComponentChildren }) {
  const root = useRef<HTMLDivElement>(null);
  const back = useRef<HTMLDivElement>(null);
  const own = useRef<HTMLDivElement>(null);
  const panel = panelRef ?? own;
  const asSheet = useRef(matches(PHONE_QUERY)).current;
  useSheetMotion(root, back, panel, asSheet ? "sheet" : "dialog", onClose);
  // V0.composer-focus-reach (2): focus in, Tab kept inside, focus back on close. As a sheet the panel takes the focus, as
  // BottomSheet's does (no keyboard raised on open); on the desktop its first control does.
  useFocusTrap(panel, asSheet ? "panel" : "first");
  return (
    <div class={`scrim${scrimCls ? ` ${scrimCls}` : ""}`} ref={root}>
      <div class="sheet-back" ref={back} onMouseDown={onBack} />
      <div class={`dlg${cls ? ` ${cls}` : ""}`} ref={panel} role={role} aria-modal="true" aria-label={label}>
        {children}
      </div>
    </div>
  );
}

// ---- small things that come and go ----

const POPS = {
  toast: { from: { opacity: 0, translate: "0 16px", scale: 0.96 }, to: { opacity: 1, translate: "0 0", scale: 1 }, inMs: 320, outMs: 160 },
  menu: { from: { opacity: 0, scale: 0.96 }, to: { opacity: 1, scale: 1 }, inMs: 140, outMs: 90 },
};

export function useEnterExit(ref: ElRef, kind: keyof typeof POPS) {
  useLayoutEffect(() => {
    const el = ref.current!;
    const p = POPS[kind];
    if (reducedMotion() || !el.animate) return;
    el.animate([p.from, p.to], { duration: p.inMs, easing: EASE_OUT });
    return () => {
      if (!el.isConnected) return;
      const g = ghostOf(el);
      ghostHome(el).appendChild(g);
      const done = () => g.remove();
      g.animate([p.to, p.from], { duration: p.outMs, easing: "ease-in", fill: "forwards" }).finished.then(done, done);
    };
  }, []);
}

/** A div that animates in and out with `kind`. */
export function Pop({ kind, children, ...rest }: { kind: keyof typeof POPS; children: ComponentChildren; class?: string; role?: "status" }) {
  const ref = useRef<HTMLDivElement>(null);
  useEnterExit(ref, kind);
  return (
    <div ref={ref} {...rest}>
      {children}
    </div>
  );
}

/** Put first inside a row of tabs or segments: one mark that slides to the child with `.on`. `at` names the choice; the
 *  mark is measured when it changes or a size does, never on an ordinary render (a stream of updates must not force
 *  layout). `under` is a tab's underline, otherwise the mark fills the chosen segment. */
export function SlideMark({ at, under }: { at: string; under?: boolean }) {
  const ref = useRef<HTMLElement>(null);
  const ro = useRef<ResizeObserver | null>(null);
  const seen = useRef<Element | null>(null);
  const place = () => {
    const m = ref.current;
    if (!m?.parentElement) return;
    const on = m.parentElement.querySelector<HTMLElement>(":scope > .on");
    if (seen.current !== on) {
      if (seen.current) ro.current?.unobserve(seen.current);
      if (on) ro.current?.observe(on);
      seen.current = on;
    }
    m.style.opacity = on ? "1" : "0";
    if (!on) return;
    m.style.width = `${on.offsetWidth}px`;
    m.style.height = `${under ? 2 : on.offsetHeight}px`;
    m.style.translate = `${on.offsetLeft}px ${on.offsetTop + (under ? on.offsetHeight - 2 : 0)}px`;
  };
  useLayoutEffect(() => {
    if (!ro.current && typeof ResizeObserver === "function") {
      ro.current = new ResizeObserver(place);
      ro.current.observe(ref.current!.parentElement!);
    }
    place();
  }, [at]);
  useEffect(() => {
    const m = ref.current!;
    const raf = requestAnimationFrame(() => m.classList.add("ready")); // the first placement does not slide in
    void document.fonts?.ready.then(place);
    return () => (cancelAnimationFrame(raf), ro.current?.disconnect());
  }, []);
  return <i ref={ref} class={`slide${under ? " under" : ""}`} aria-hidden="true" />;
}

// ---- presses ----

const PRESSABLE = "button,a.btn,[role=button],[role=tab],[role=menuitem],summary,label.m-dchip,.rw,.one,.fpr,.pli,.cand,.tl,.al.click,.sec.tog,.m-ah";
const SCROLLS = ".m-list,.list,.m-sheet,.m-nbody,.m-abody,.convo,.chtab,.db";
const SCROLL_DELAY_MS = 70;
const MIN_SHOWN_MS = 90;
const SLOP_PX = 10;

/** One listener for the whole app. Unlike :active it shows a quick tap too, and a finger that starts a scroll lights
 *  nothing: inside a scroller the press waits a moment to see which it is, as native lists do. */
export function installPress(root: Document = document) {
  let cur: { el: Element; x: number; y: number; timer: ReturnType<typeof setTimeout> | null; shownAt: number } | null = null;
  const show = (c: NonNullable<typeof cur>) => {
    c.timer = null;
    c.shownAt = performance.now();
    c.el.setAttribute("data-pressed", "");
  };
  const clear = (el: Element) => el.removeAttribute("data-pressed");
  const cancel = () => {
    if (!cur) return;
    if (cur.timer) clearTimeout(cur.timer);
    clear(cur.el);
    cur = null;
  };
  root.addEventListener("pointerdown", (e) => {
    cancel();
    const pe = e as PointerEvent;
    if (pe.pointerType === "mouse" && pe.button !== 0) return;
    const t = e.target as Element | null;
    const hit = t?.closest?.(PRESSABLE);
    const el = hit?.closest(".m-mv") ?? hit;
    if (!el || el.matches(":disabled,[aria-disabled=true]")) return;
    const c: NonNullable<typeof cur> = { el, x: pe.clientX, y: pe.clientY, timer: null, shownAt: 0 };
    cur = c;
    if (pe.pointerType === "touch" && el.parentElement?.closest(SCROLLS)) c.timer = setTimeout(() => show(c), SCROLL_DELAY_MS);
    else show(c);
  }, { capture: true, passive: true });
  root.addEventListener("pointermove", (e) => {
    const pe = e as PointerEvent;
    if (cur && Math.hypot(pe.clientX - cur.x, pe.clientY - cur.y) > SLOP_PX) cancel();
  }, { capture: true, passive: true });
  root.addEventListener("pointerup", () => {
    if (!cur) return;
    const c = cur;
    cur = null;
    if (c.timer) {
      clearTimeout(c.timer);
      show(c);
    }
    setTimeout(() => clear(c.el), Math.max(0, MIN_SHOWN_MS - (performance.now() - c.shownAt)));
  }, { capture: true, passive: true });
  root.addEventListener("pointercancel", cancel, { capture: true, passive: true });
  root.addEventListener("scroll", cancel, { capture: true, passive: true });
}
