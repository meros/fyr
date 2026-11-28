// V0.tooltips: one tooltip for the whole desktop app, in place of the browser's. Every element keeps its `title` (its
// accessible name or description, and what the tests and the code read); while the pointer rests on one, the layer
// holds the title back (so the browser's grey box does not open as well) and shows it in Fyr's own: in the theme,
// after a short wait and at once along a row, on keyboard focus too, with the key as a key cap, inside the window,
// following the title when it changes, and gone on a press, Escape, a scroll or a hover card.
//
// No touch: a tooltip never opens from a finger or a pen (no hover there); the phone states what a tooltip would say
// as text (CLAUDE.md, SPEC U26). No interruption: it never takes the focus or a key (Escape still reaches the dialog
// under it), and it shows only for the owner's own pointer or focus, never for something that arrives by itself.

import { anchorGone } from "./popover.ts";
import { CLOSE_GRACE, OPEN_DELAY, isWarm, placeTip, sameWords, splitTip } from "./tooltip.ts";

/** True when the element, or a part of it, is cut off: its text ends in an ellipsis or is clamped to fewer lines. */
function cutOff(el: Element): boolean {
  const over = (e: Element) => e.scrollWidth > e.clientWidth + 1 || e.scrollHeight > e.clientHeight + 1;
  if (over(el)) return true;
  const parts = el.querySelectorAll("*");
  for (let i = 0; i < parts.length && i < 40; i++) if (over(parts[i])) return true;
  return false;
}

/** The browser's own tooltip is kept for these: a frame's title names the frame and is not shown on hover. */
const SKIP = "iframe,frame,object,embed,link,style,title,abbr[title]";

interface Held {
  el: Element;
  /** the title as it is now; the element gets it back when the pointer leaves */
  text: string;
  /** an aria-label added while the title is held, because the title was the element's only name */
  label: boolean;
}

export function installTooltips(doc: Document = document) {
  const win = doc.defaultView;
  if (!win || (win as { __fyrTips?: boolean }).__fyrTips) return;
  (win as { __fyrTips?: boolean }).__fyrTips = true;

  const tip = doc.createElement("div");
  tip.className = "ftip";
  tip.setAttribute("aria-hidden", "true"); // the element's own title or label already says it to a screen reader
  const usePopover = typeof (tip as HTMLElement & { showPopover?: () => void }).showPopover === "function";
  if (usePopover) tip.setAttribute("popover", "manual"); // the top layer: above an open dialog too
  const body = doc.createElement("div");
  body.className = "ftip-in";
  tip.append(body);
  doc.body.append(tip);

  let held: Held | null = null; // the element under the pointer whose title is held back
  let shownFor: Element | null = null;
  let shownText = "";
  let openTimer: ReturnType<typeof setTimeout> | undefined;
  let closeTimer: ReturnType<typeof setTimeout> | undefined;
  let lastAt = 0; // when a tooltip was last shown or closed, for the warm start
  let pressedOn: Element | null = null; // a press closes the tooltip until the pointer leaves that element
  let pointer = "mouse";

  const titleOf = (el: Element): string => (held?.el === el ? held.text : (el.getAttribute("title") ?? ""));
  const anchorFrom = (t: EventTarget | null): Element | null => {
    let el = t instanceof win.Element ? t : null;
    while (el) {
      if (held?.el === el || (el.hasAttribute("title") && el.getAttribute("title")!.trim())) return el.matches(SKIP) ? null : el;
      el = el.parentElement;
    }
    return null;
  };

  const hold = (el: Element) => {
    if (held?.el === el) return;
    release();
    const text = el.getAttribute("title") ?? "";
    const needsLabel = !el.hasAttribute("aria-label") && !el.hasAttribute("aria-labelledby") && !(el.textContent ?? "").trim() && !/^(INPUT|SELECT|TEXTAREA)$/.test(el.tagName);
    held = { el, text, label: needsLabel };
    if (needsLabel) el.setAttribute("aria-label", text);
    el.removeAttribute("title");
    watch.observe(el, { attributes: true, attributeFilter: ["title"] });
  };
  const release = () => {
    if (!held) return;
    const h = held;
    held = null;
    watch.disconnect();
    if (h.el.isConnected && h.text) h.el.setAttribute("title", h.text);
    if (h.label && h.el.getAttribute("aria-label") === h.text) h.el.removeAttribute("aria-label");
  };
  // The app changes a title while it is held (a reason that changes, a count): take it up and show the new words.
  const watch = new MutationObserver(() => {
    if (!held) return;
    const v = held.el.getAttribute("title");
    if (v === null) return;
    held.text = v;
    if (held.label) held.el.setAttribute("aria-label", v);
    held.el.removeAttribute("title");
    if (!v.trim()) hide();
    else if (shownFor === held.el) render(v);
  });

  const render = (text: string) => {
    if (text === shownText) return;
    shownText = text;
    const { text: words, keys } = splitTip(text);
    body.textContent = "";
    const p = doc.createElement("span");
    p.className = "ftip-t";
    p.textContent = words;
    body.append(p);
    if (keys.length) {
      const k = doc.createElement("span");
      k.className = "ftip-k";
      keys.forEach((key, i) => {
        if (i) k.append(doc.createTextNode(" or "));
        const cap = doc.createElement("kbd");
        cap.textContent = key;
        k.append(cap);
      });
      body.append(k);
    }
    if (shownFor) place();
  };

  const place = () => {
    if (!shownFor) return;
    if (!shownFor.isConnected) return hide();
    const r = shownFor.getBoundingClientRect();
    const vv = win.visualViewport;
    const area = { left: vv?.offsetLeft ?? 0, top: vv?.offsetTop ?? 0, width: vv?.width ?? win.innerWidth, height: vv?.height ?? win.innerHeight };
    if (anchorGone(r, area)) return hide();
    const prefer = shownFor.closest(".rail") ? "right" : "above";
    const p = placeTip(r, { width: tip.offsetWidth, height: tip.offsetHeight }, area, prefer);
    tip.style.left = `${Math.round(p.left)}px`;
    tip.style.top = `${Math.round(p.top)}px`;
    tip.style.setProperty("--nub", `${Math.round(p.nub)}px`);
    tip.dataset.side = p.side;
  };

  const show = (el: Element) => {
    clearTimeout(openTimer);
    clearTimeout(closeTimer);
    const text = titleOf(el);
    if (!text.trim() || hoverCardOpen(el)) return;
    // a tooltip that says what is already in view is noise: it shows only while that text is cut off
    if (el instanceof win.HTMLElement && sameWords(text, el.innerText) && !cutOff(el)) return;
    const again = shownFor !== null;
    shownFor = el;
    shownText = "";
    tip.dataset.instant = again || isWarm(lastAt, performance.now()) ? "1" : "";
    if (!tip.classList.contains("on")) {
      tip.classList.add("on");
      if (usePopover) {
        try {
          (tip as HTMLElement & { showPopover(): void }).showPopover();
        } catch {
          /* already open */
        }
      }
    }
    render(text);
    place();
    lastAt = performance.now();
  };
  const hide = () => {
    clearTimeout(openTimer);
    clearTimeout(closeTimer);
    if (!shownFor) return;
    shownFor = null;
    shownText = "";
    tip.classList.remove("on");
    if (usePopover) {
      try {
        (tip as HTMLElement & { hidePopover(): void }).hidePopover();
      } catch {
        /* already closed */
      }
    }
    lastAt = performance.now();
  };
  // A hover card or a menu is open and the anchor is not in it: the card says more, and the tooltip would cover it.
  const hoverCardOpen = (el: Element | null = shownFor) => [...doc.querySelectorAll(".lcpop, [role=menu]")].some((p) => !el || !p.contains(el));

  doc.addEventListener("pointerdown", (e) => {
    pointer = (e as PointerEvent).pointerType || "mouse";
    pressedOn = anchorFrom(e.target);
    hide();
  }, { capture: true, passive: true });

  doc.addEventListener("pointerover", (e) => {
    const pe = e as PointerEvent;
    pointer = pe.pointerType || "mouse";
    if (pointer !== "mouse") return;
    const el = anchorFrom(pe.target);
    if (el !== pressedOn) pressedOn = null;
    if (!el) {
      release();
      if (shownFor) {
        clearTimeout(closeTimer);
        closeTimer = setTimeout(hide, CLOSE_GRACE);
      } else clearTimeout(openTimer);
      return;
    }
    hold(el);
    if (el === shownFor) {
      clearTimeout(closeTimer);
      return;
    }
    if (el === pressedOn) return;
    clearTimeout(openTimer);
    if (shownFor || isWarm(lastAt, performance.now())) show(el);
    else openTimer = setTimeout(() => held?.el === el && show(el), OPEN_DELAY);
  }, { capture: true, passive: true });

  doc.documentElement.addEventListener("pointerleave", () => {
    release();
    hide();
  });

  doc.addEventListener("focusin", (e) => {
    const el = e.target as Element | null;
    if (!el || pointer === "touch" || pointer === "pen" || !el.hasAttribute?.("title") || el.matches(SKIP)) return;
    let visible = false;
    try {
      visible = el.matches(":focus-visible");
    } catch {
      visible = false;
    }
    if (visible) show(el);
  });
  doc.addEventListener("focusout", (e) => {
    if (e.target === shownFor && held?.el !== shownFor) hide();
  });
  doc.addEventListener("keydown", (e) => {
    pointer = "key";
    if (!shownFor) return;
    // Escape closes the tooltip and still reaches what is under it (a dialog closes on the same press); any other key
    // acts, so the tooltip goes (Tab's focus shows the next one at once)
    if (!["Shift", "Control", "Alt", "Meta"].includes(e.key)) hide();
  }, true);
  // A scroll (the open thread following its run) moves the tooltip with its anchor; out of the view it closes.
  let frame = 0;
  const later = () => {
    if (shownFor && !frame) frame = win.requestAnimationFrame(() => ((frame = 0), place()));
  };
  win.addEventListener("scroll", later, { capture: true, passive: true });
  win.addEventListener("resize", later);
  win.addEventListener("blur", () => {
    release();
    hide();
  });
  // A hover card or a menu that opens closes the tooltip under it; an anchor that leaves the page takes its tooltip along.
  new MutationObserver(() => {
    if (!shownFor) return;
    if (!shownFor.isConnected || hoverCardOpen()) hide();
  }).observe(doc.body, { childList: true, subtree: true });
}
