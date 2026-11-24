// V0.207: the browser side of still-list.ts, used once in app.tsx for both fronts: the scroll correction that keeps one
// row at its height on the screen when rows change above it. The correction is by hand because Safari, so the
// phone, has no `overflow-anchor`.
// V0.list-motion: then the list moves as one layout on one clock. Every element that takes space in it has a key
// (`data-mk`: bands, group labels, rows, "Show all", the filter's empty note); each is measured before the change and
// after it, and list-motion.ts plans what moves, what arrives, what leaves and which counts roll. This file plays the
// plan: every move on the one list spring, scaled by its own distance, so a band and the rows under it land together.
import { useLayoutEffect, useRef } from "preact/hooks";
import { ghostOf, reducedMotion } from "./motion-ui.tsx";
import { frameMs, LIST_SPRING, unitPath } from "./motion.ts";
import { ENTER_MS, EXIT_MS, FILTER_EXIT_MS, planLayout, type Plan, type Snap } from "./list-motion.ts";
import { anchorRow } from "./still-list.ts";

const LIST = ".list, .m-list";
const EASE = "cubic-bezier(.2,.8,.2,1)";
const UNIT = unitPath(LIST_SPRING);
const LIST_MS = frameMs(UNIT);
/** how long a row that changed section keeps its wash after it lands */
const WASH_MS = 1100;
const MOVE = "lm-move";

// ---- a filter (V0.list-motion (7)) ----

let filterAt = 0;
/** The chip, host and search handlers, a fold and "Show all" call this before they change what is shown: the next
 *  change of the list is played as one change of who is in it (V0.list-motion (7)), not as threads that arrived and left. */
export function markListFilter(): void {
  filterAt = Date.now();
}
const FILTER_WINDOW_MS = 1000;

interface Box {
  left: number;
  width: number;
  el: HTMLElement;
  /** a band's fill and its title's ink, which fade to their new colours on the list's clock */
  bg?: string;
  ink?: string;
}

interface Mark {
  id: string | null;
  top: number;
  list: HTMLElement;
  snaps: Map<string, Snap>;
  boxes: Map<string, Box>;
  filter: boolean;
}

const keyed = (list: HTMLElement) => [...list.querySelectorAll<HTMLElement>("[data-mk]")];
const rowsOf = (list: HTMLElement) => [...list.querySelectorAll<HTMLElement>("[data-id]")];
/** the number of a count, without the old one that may still be rolling out beside it */
const countText = (el: HTMLElement) => (el.firstElementChild ?? el).textContent ?? "";

function snapAll(list: HTMLElement, withBoxes: boolean): { snaps: Map<string, Snap>; boxes: Map<string, Box> } {
  const snaps = new Map<string, Snap>();
  const boxes = new Map<string, Box>();
  for (const el of keyed(list)) {
    const k = el.dataset.mk!;
    if (snaps.has(k)) continue;
    const r = el.getBoundingClientRect();
    snaps.set(k, { top: r.top, height: r.height, group: el.dataset.sec, text: k.startsWith("n:") ? countText(el) : undefined });
    if (!withBoxes) {
      boxes.set(k, { left: r.left, width: r.width, el });
      continue;
    }
    const box: Box = { left: r.left, width: r.width, el };
    if (k.startsWith("sec:")) {
      box.bg = getComputedStyle(el).backgroundColor;
      const t = el.querySelector(".sti");
      if (t) box.ink = getComputedStyle(t).color;
    }
    boxes.set(k, box);
  }
  return { snaps, boxes };
}

function measure(open: string | null): Mark | null {
  const list = document.querySelector<HTMLElement>(LIST);
  if (!list) return null;
  const box = list.getBoundingClientRect();
  const rows = rowsOf(list);
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
  const filter = Date.now() - filterAt < FILTER_WINDOW_MS;
  if (filter) filterAt = 0;
  // Positions as the owner sees them, a move in flight included, so a change during a move goes on from there.
  return { id: el ? id : null, top: el ? el.getBoundingClientRect().top : 0, list, ...snapAll(list, true), filter };
}

// ---- playing the plan ----

const raised = () => getComputedStyle(document.documentElement).getPropertyValue("--raised").trim() || "#fff";
const wash = () => getComputedStyle(document.documentElement).getPropertyValue("--beam-wash").trim() || "rgba(232,150,15,.15)";

function playMove(el: HTMLElement, dy: number, delay: number): Animation {
  return el.animate(
    UNIT.map((p) => ({ translate: `0 ${dy * p}px` })),
    { id: MOVE, duration: LIST_MS, delay, easing: "linear", fill: "backwards" },
  );
}

/** A row that changed section: above the bands it crosses while it travels, then a wash where it lands. */
function lift(el: HTMLElement, delay: number): void {
  const base = getComputedStyle(el).backgroundColor;
  el.classList.add("lm-lift");
  const a = el.animate([{ boxShadow: "0 1px 2px rgba(0,0,0,.06)" }, { boxShadow: "0 8px 22px rgba(0,0,0,.14)", offset: 0.3 }, { boxShadow: "0 0 0 rgba(0,0,0,0)" }], { id: "lm-lift", duration: LIST_MS, delay, easing: "ease-out" });
  const done = () => el.classList.remove("lm-lift");
  a.finished.then(done, done);
  const w = wash();
  el.animate([{ backgroundColor: base }, { backgroundColor: w, offset: 0.12 }, { backgroundColor: w, offset: 0.3 }, { backgroundColor: base }], { id: "lm-wash", duration: WASH_MS, delay: delay + LIST_MS * 0.45, easing: "linear" });
}

/** The leaving element's copy, where it was on the screen, in the list's own layer. */
function ghost(list: HTMLElement, b: Box, s: Snap): HTMLElement {
  const g = ghostOf(b.el);
  // the copy is no row: nothing finds it by its id or measures it as part of the list
  for (const n of [g, ...g.querySelectorAll<HTMLElement>("[data-mk],[data-id]")]) n.removeAttribute("data-mk"), n.removeAttribute("data-id");
  g.getAnimations?.().forEach((a) => a.cancel());
  const L = list.getBoundingClientRect();
  Object.assign(g.style, {
    position: "absolute",
    top: `${s.top - L.top - list.clientTop + list.scrollTop}px`,
    left: `${b.left - L.left - list.clientLeft + list.scrollLeft}px`,
    width: `${b.width}px`,
    height: `${s.height}px`,
    margin: "0",
    translate: "",
    zIndex: "1",
    pointerEvents: "none",
  });
  g.classList.add("lm-ghost");
  list.appendChild(g);
  return g;
}

function roll(el: HTMLElement, from: string, up: boolean, delay: number): void {
  el.querySelectorAll("[data-roll]").forEach((n) => n.remove());
  const cur = el.firstElementChild as HTMLElement | null;
  if (!cur) return;
  const old = document.createElement("span");
  old.dataset.roll = "";
  old.setAttribute("aria-hidden", "true");
  old.textContent = from;
  el.appendChild(old);
  const o: KeyframeAnimationOptions = { duration: LIST_MS * 0.8, delay, easing: EASE, fill: "both" };
  const d = up ? 1 : -1;
  cur.animate([{ translate: `0 ${d}em`, opacity: 0 }, { translate: "0 0", opacity: 1 }], o);
  const gone = () => old.remove();
  old.animate([{ translate: "0 0", opacity: 1 }, { translate: `0 ${-d}em`, opacity: 0 }], o).finished.then(gone, gone);
  setTimeout(gone, delay + LIST_MS + 200);
}

function play(m: Mark, after: Map<string, Snap>, els: Map<string, Box>, plan: Plan): void {
  const { list } = m;
  if (plan.whole) {
    list.animate([{ opacity: 0.4 }, { opacity: 1 }], { duration: 180, easing: "ease-out" });
    return;
  }
  for (const mv of plan.moves) {
    const el = els.get(mv.key)?.el;
    if (!el) continue;
    playMove(el, mv.dy, mv.delay);
    if (mv.lift) lift(el, mv.delay);
  }
  for (const e of plan.enters) {
    const el = els.get(e.key)?.el;
    if (!el) continue;
    const frames = m.filter ? [{ opacity: 0, translate: "0 -4px" }, { opacity: 1, translate: "0 0" }] : [{ opacity: 0, scale: 0.97 }, { opacity: 1, scale: 1 }];
    el.animate(frames, { id: "lm-enter", duration: ENTER_MS, delay: e.delay, easing: EASE, fill: "backwards" });
  }
  for (const x of plan.exits) {
    const b = m.boxes.get(x.key);
    const s = m.snaps.get(x.key);
    if (!b || !s) continue;
    const g = ghost(list, b, s);
    let a: Animation;
    if (x.kind === "tuck") {
      g.style.background = raised();
      g.style.boxShadow = "0 6px 18px rgba(0,0,0,.12)";
      const dy = x.dy ?? 0;
      a = g.animate(
        UNIT.map((p) => {
          const q = 1 - p;
          return { translate: `0 ${dy * q}px`, scale: String(1 - 0.35 * q), opacity: Math.max(0, 1 - Math.max(0, (q - 0.55) / 0.45)) };
        }),
        { duration: LIST_MS, easing: "linear", fill: "forwards" },
      );
    } else if (x.kind === "fade") a = g.animate([{ opacity: 1 }, { opacity: 0 }], { duration: FILTER_EXIT_MS, easing: "ease-out", fill: "forwards" });
    else a = g.animate([{ opacity: 1, scale: 1 }, { opacity: 0, scale: 0.97 }], { duration: EXIT_MS, easing: "ease-in", fill: "forwards" });
    const done = () => g.remove();
    a.finished.then(done, done);
    setTimeout(done, LIST_MS + 400);
  }
  for (const r of plan.rolls) {
    const el = els.get(r.key)?.el;
    if (el) roll(el, r.from, r.up, r.delay);
  }
  // a band that went from none to some, or back, fades its fill and its title's ink
  for (const [k, b] of m.boxes) {
    if (!k.startsWith("sec:")) continue;
    const el = els.get(k)?.el;
    if (!el || !after.has(k)) continue;
    const bg = getComputedStyle(el).backgroundColor;
    if (b.bg && bg !== b.bg) el.animate([{ backgroundColor: b.bg }, { backgroundColor: bg }], { duration: LIST_MS, easing: EASE });
    const t = el.querySelector<HTMLElement>(".sti");
    const ink = t && getComputedStyle(t).color;
    if (t && b.ink && ink && ink !== b.ink) t.animate([{ color: b.ink }, { color: ink }], { duration: LIST_MS, easing: EASE });
  }
}

/** With reduced motion nothing moves: what arrived or changed place fades in, briefly. */
function playStill(before: Map<string, Snap>, after: Map<string, Snap>, els: Map<string, Box>): void {
  for (const [k, a] of after) {
    const b = before.get(k);
    if (k.startsWith("n:") || (b && Math.abs(b.top - a.top) < 1)) continue;
    els.get(k)?.el.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 120 });
  }
}

/** Keeps the anchor row at its height when the list changes, then plays the change. `sig` is what the list draws (the
 *  rows, the open sections, the counts) as one string; `sectionOf` says where a thread is now, for a row that leaves
 *  into a folded section. */
export function useListAnchor(sig: string, open: string | null, sectionOf?: (id: string) => string | null): void {
  const last = useRef(sig);
  // Read during the render, while the screen still shows the old list.
  const mark = useRef<Mark | null>(null);
  if (sig !== last.current) mark.current = measure(open);
  const where = useRef(sectionOf);
  where.current = sectionOf;
  useLayoutEffect(() => {
    last.current = sig;
    const m = mark.current;
    mark.current = null;
    if (!m || !m.list.isConnected) return;
    // A move still in flight ends where it is going: the new places are measured without it.
    for (const el of keyed(m.list)) for (const a of el.getAnimations()) if (a.id === MOVE) a.cancel();
    const el = m.id ? rowsOf(m.list).find((r) => r.dataset.id === m.id) : undefined;
    if (el) m.list.scrollTop += el.getBoundingClientRect().top - m.top;
    const { snaps: after, boxes: els } = snapAll(m.list, false);
    if (reducedMotion()) return playStill(m.snaps, after, els);
    const L = m.list.getBoundingClientRect();
    const plan = planLayout(m.snaps, after, {
      filter: m.filter,
      view: { top: L.top, bottom: L.bottom },
      moveMs: LIST_MS,
      tuckTo: (k) => {
        if (!k.startsWith("row:")) return null;
        const s = where.current?.(k.slice(4));
        return s ? `sec:${s}` : null;
      },
    });
    play(m, after, els, plan);
  }, [sig]);
}
