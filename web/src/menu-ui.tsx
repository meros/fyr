// SPEC 0.5 V0.31: Fyr's right-click menu. Opens at the pointer, stays inside the
// window, works from the keyboard, closes on a click outside, a scroll that moves what it was
// opened on, or Escape.
import type { RefObject } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { anchorMoved, entryForKey, type MenuEntry, type MenuItem } from "./menu.ts";
import { useEnterExit } from "./motion-ui.tsx";

const ENTRY = "[role=menuitem]:not([aria-disabled=true])";

/** V0.menu-keeps-focus: an open menu keeps the keyboard's place when its entries change under it. An entry is drawn
 *  by its id, and a thread that changes while the menu is open swaps one for another (Mark seen becomes Mark unseen
 *  1.5 s after the thread opens, Stop run goes when the run ends): the focused button leaves the page, the focus
 *  falls to the body, and the menu's keys stop reaching it. So the menu remembers which entry had the focus and, after
 *  a render that left the focus on nothing, gives it to the entry now in that place. `ref` is the element that holds
 *  the entries; it may be empty while the menu is closed. Desktop only: the phone shows these actions in a sheet that
 *  is tapped, with no keyboard and so no focus to keep. */
export function useKeepMenuFocus(ref: RefObject<HTMLElement | null>) {
  const at = useRef(-1);
  useLayoutEffect(() => {
    const items = [...(ref.current?.querySelectorAll<HTMLElement>(ENTRY) ?? [])];
    const i = items.indexOf(document.activeElement as HTMLElement);
    if (i >= 0) at.current = i;
    else if (!items.length) at.current = -1;
    else if (at.current >= 0 && (!document.activeElement || document.activeElement === document.body)) items[Math.min(at.current, items.length - 1)].focus();
  });
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    // The arrows move the focus without a render, and a focus that goes elsewhere on purpose is not taken back.
    const on = (e: FocusEvent) => (at.current = [...el.querySelectorAll<HTMLElement>(ENTRY)].indexOf(e.target as HTMLElement));
    const off = (e: FocusEvent) => {
      if (e.relatedTarget && !el.contains(e.relatedTarget as Node)) at.current = -1;
    };
    el.addEventListener("focusin", on);
    el.addEventListener("focusout", off);
    return () => {
      el.removeEventListener("focusin", on);
      el.removeEventListener("focusout", off);
    };
  }, [ref]);
}

/** anchor: the element the menu was opened on; a scroll closes the menu only when it moves that element. */
export function ContextMenu({ x, y, items, anchor, onPick, onClose }: { x: number; y: number; items: MenuEntry[]; anchor?: Element | null; onPick(it: MenuItem): void; onClose(): void }) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ left: x, top: y });
  useEnterExit(ref, "menu");
  useKeepMenuFocus(ref);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const left = Math.max(4, Math.min(x, window.innerWidth - r.width - 4));
    const top = y + r.height > window.innerHeight - 4 ? Math.max(4, y - r.height) : y;
    setPos({ left, top });
    el.querySelector<HTMLElement>("[role=menuitem]:not([aria-disabled=true])")?.focus();
  }, [x, y]);
  /* eslint-disable react-hooks/exhaustive-deps -- the menu's listeners are set once, for the menu's life; the anchor's place when it opened (top0) is what a scroll is measured against */
  useEffect(() => {
    const away = (e: Event) => {
      if (!ref.current?.contains(e.target as Node)) onClose();
    };
    const close = () => onClose();
    const top = () => (anchor?.isConnected ? anchor.getBoundingClientRect().top : null);
    const top0 = anchor ? top() : null;
    const scrolled = () => {
      if (anchorMoved(top0, top())) onClose();
    };
    document.addEventListener("mousedown", away, true);
    window.addEventListener("scroll", scrolled, true);
    window.addEventListener("blur", close);
    window.addEventListener("resize", close);
    return () => {
      document.removeEventListener("mousedown", away, true);
      window.removeEventListener("scroll", scrolled, true);
      window.removeEventListener("blur", close);
      window.removeEventListener("resize", close);
    };
  }, []);
  /* eslint-enable react-hooks/exhaustive-deps */
  const onKey = (e: KeyboardEvent) => {
    e.stopPropagation(); // the inbox keys must not act while the menu has focus
    const list = [...(ref.current?.querySelectorAll<HTMLElement>("[role=menuitem]:not([aria-disabled=true])") ?? [])];
    const i = list.indexOf(document.activeElement as HTMLElement);
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const n = list.length;
      if (n) list[(i + (e.key === "ArrowDown" ? 1 : n - 1) + n) % n].focus();
    } else if (e.key === "Tab") {
      e.preventDefault();
    } else if (!e.metaKey && !e.ctrlKey && !e.altKey) {
      // A key shown beside an entry runs it: the menu shows no hint for a key that would do nothing here.
      const it = entryForKey(items, e.key);
      if (it) (e.preventDefault(), onPick(it));
    }
  };
  return (
    <div class="ctxm" ref={ref} role="menu" style={{ left: `${pos.left}px`, top: `${pos.top}px` }} onKeyDown={onKey} onContextMenu={(e) => e.preventDefault()}>
      <MenuRows items={items} onPick={onPick} />
    </div>
  );
}

/** The rows of a menu, one look for the row's right-click menu and the thread header's ⋯ menu: the label, the key at
 *  the right, a line between groups, and under an entry that is off the reason why (a tooltip alone is not read). */
export function MenuRows({ items, onPick }: { items: MenuEntry[]; onPick(it: MenuItem): void }) {
  return (
    <>
      {items.map((it, k) =>
        "sep" in it ? (
          <div key={k} class="ctxm-sep" role="separator" />
        ) : (
          <button key={it.id} class="ctxm-i" role="menuitem" aria-disabled={it.disabled ? true : undefined} title={it.disabled ? undefined : it.hint} onClick={() => !it.disabled && onPick(it)}>
            <span class="ctxm-t">
              <span class="ell">{it.label}</span>
              {it.disabled && <small class="ell">{it.disabled}</small>}
            </span>
            {it.key && <kbd>{it.key}</kbd>}
          </button>
        ),
      )}
    </>
  );
}
