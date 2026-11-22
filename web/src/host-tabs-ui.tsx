// V0.host-tabs: one shared tabbed host switcher for desktop and phone. Used on every multi-host screen: Settings →
// Providers, Automation, Setup, Hosts (host sub-page), linked project editor, phone Host load sheet, and HostField
// pickers. Renders nothing with one host. Phone 44 px touch targets come from app.css / phone.css.

import { useEffect, useRef } from "preact/hooks";
import { HostMark } from "./host-mark-ui.tsx";
import { stepTabIndex, type HostTabItem } from "./host-tabs.ts";
import { SlideMark } from "./motion-ui.tsx";

interface HostTabsProps {
  items: HostTabItem[];
  active: string;
  onPick(id: string): void;
  label?: string;
  /** compact variant inside forms (HostField) */
  compact?: boolean;
  /** extra class on the tablist */
  class?: string;
}

export function HostTabs({ items, active, onPick, label = "Hosts", compact, class: cls }: HostTabsProps) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = ref.current?.querySelector<HTMLElement>(".htab.on");
    el?.scrollIntoView?.({ inline: "nearest", block: "nearest" });
  }, [active]);

  if (items.length <= 1) return null;

  const onKeyDown = (e: KeyboardEvent) => {
    const idx = items.findIndex((x) => x.id === active);
    const next = stepTabIndex(items.length, idx < 0 ? 0 : idx, e.key);
    if (next === null) return;
    e.preventDefault();
    const target = items[next];
    onPick(target.id);
    const btns = ref.current?.querySelectorAll<HTMLButtonElement>(".htab");
    btns?.[next]?.focus();
  };

  return (
    <div class={`htabs${compact ? " compact" : ""}${cls ? ` ${cls}` : ""}`} role="tablist" aria-label={label} ref={ref} onKeyDown={onKeyDown}>
      <SlideMark at={active} under={!compact} />
      {items.map((it) => {
        const on = it.id === active;
        return (
          <button
            key={it.id}
            type="button"
            role="tab"
            aria-selected={on}
            tabIndex={on || !items.some((x) => x.id === active) ? 0 : -1}
            class={`htab${on ? " on" : ""}${it.offline ? " off" : ""}`}
            data-host={it.name}
            data-tab-id={it.id}
            title={it.title ?? (it.offline ? `${it.label} · offline` : it.label)}
            onClick={() => onPick(it.id)}
          >
            <HostMark name={it.name} size={16} />
            <span class="htab-n mono">{it.label}</span>
            {it.meta && <span class={`htab-m mono${it.metaLevel && it.metaLevel !== "calm" ? ` ${it.metaLevel}` : ""}`}>{it.meta}</span>}
            {(it.badge ?? 0) > 0 && (
              <span class="htab-b" aria-label={`${it.badge} need attention`}>
                {it.badge}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
