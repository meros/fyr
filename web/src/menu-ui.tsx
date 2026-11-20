// SPEC 0.5 V0.31: Fyr's right-click menu. Opens at the pointer, stays inside the
// window, works from the keyboard, closes on a click outside, a scroll that moves what it was
// opened on, or Escape.
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { anchorMoved, type MenuEntry, type MenuItem } from "./menu.ts";
import { useEnterExit } from "./motion-ui.tsx";

/** anchor: the element the menu was opened on; a scroll closes the menu only when it moves that element. */
export function ContextMenu({ x, y, items, anchor, onPick, onClose }: { x: number; y: number; items: MenuEntry[]; anchor?: Element | null; onPick(it: MenuItem): void; onClose(): void }) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ left: x, top: y });
  useEnterExit(ref, "menu");
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const left = Math.max(4, Math.min(x, window.innerWidth - r.width - 4));
    const top = y + r.height > window.innerHeight - 4 ? Math.max(4, y - r.height) : y;
    setPos({ left, top });
    el.querySelector<HTMLElement>("[role=menuitem]:not([aria-disabled=true])")?.focus();
  }, [x, y]);
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
    }
  };
  return (
    <div class="ctxm" ref={ref} role="menu" style={{ left: `${pos.left}px`, top: `${pos.top}px` }} onKeyDown={onKey} onContextMenu={(e) => e.preventDefault()}>
      {items.map((it, k) =>
        "sep" in it ? (
          <div key={k} class="ctxm-sep" role="separator" />
        ) : (
          <button
            key={it.id}
            class="ctxm-i"
            role="menuitem"
            aria-disabled={it.disabled ? true : undefined}
            title={it.disabled ?? it.hint}
            onClick={() => !it.disabled && onPick(it)}
          >
            <span class="ell">{it.label}</span>
            {it.key && <kbd>{it.key}</kbd>}
          </button>
        ),
      )}
    </div>
  );
}
