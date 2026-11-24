// SPEC 0.5 V0.31: what Fyr's right-click menu holds. Pure: the target in, the
// entries out. The menu component and the app wire the ids to actions.
import { actionGroups, actionsFor, menuKey, type ThreadAction } from "./actions.ts";
import type { Thread } from "./types.ts";

export { SETTLE_DONE_HINT, SETTLE_DONE_TOAST, seenToggle, settleDoneEntry } from "./actions.ts";

export type MenuItem = { id: string; label: string; key?: string; disabled?: string; hint?: string };
export type MenuEntry = MenuItem | { sep: true };

export type MenuTarget =
  | { kind: "thread"; t: Thread }
  | { kind: "text"; selection: string }
  | { kind: "link"; href: string; selection?: string }
  | { kind: "image"; src: string }
  | { kind: "editable" }
  | { kind: "none" };

const SEP: MenuEntry = { sep: true };

/** Actions as menu entries, a line between groups (actions.ts `actionGroups`). The key beside an entry is one the
 *  menu acts on while it is open (`menuKey`); the desktop's two menus, the row's and the header's ⋯, both come from
 *  here, so they have one order, one look and the same keys. */
export function menuEntries(acts: ThreadAction[]): MenuEntry[] {
  const out: MenuEntry[] = [];
  for (const g of actionGroups(acts)) {
    if (out.length) out.push(SEP);
    for (const a of g) {
      const key = menuKey(a);
      out.push({ id: a.id, label: a.label, ...(key ? { key } : {}), ...(a.hint ? { hint: a.hint } : {}), ...(a.disabled ? { disabled: a.disabled } : {}) });
    }
  }
  return out;
}

/** The entry a key typed in an open menu runs: the one that shows the key and is not off. */
export function entryForKey(items: MenuEntry[], key: string): MenuItem | undefined {
  return items.find((it): it is MenuItem => !("sep" in it) && !it.disabled && it.key === key);
}

/** V0.one-action-list: a row's right-click menu is the desktop's list of the thread's actions (actions.ts). */
export function threadMenu(t: Thread): MenuEntry[] {
  return menuEntries(actionsFor("desktop", t, "row"));
}

/** null: let the browser's own menu open (text fields). [] : open nothing. */
export function menuFor(target: MenuTarget): MenuEntry[] | null {
  switch (target.kind) {
    case "editable":
      return null;
    case "thread":
      return threadMenu(target.t);
    case "text":
      return [{ id: "copy", label: "Copy" }];
    case "link":
      return [...(target.selection ? [{ id: "copy", label: "Copy" }, SEP] : []), { id: "openLink", label: "Open link" }, { id: "copyLink", label: "Copy link" }];
    case "image":
      return [{ id: "openImage", label: "Open image" }];
    default:
      return [];
  }
}

/** Whether a scroll moved what the menu was opened on, so the menu must close (V0.31): its top on screen before and
 *  after, null when it is unknown (no anchor, or the anchor left the page). A scroll that leaves it in place, such as
 *  the open thread following a run's output, or the browser's scroll anchoring in the list, keeps the menu open:
 *  closing the menu there made a Settle picked from a row's menu land on the row instead. */
export function anchorMoved(before: number | null, after: number | null): boolean {
  if (before === null) return true;
  if (after === null) return false; // the row re-rendered elsewhere; the menu acts on the thread id, not the element
  return Math.abs(after - before) > 2;
}

/** Where a right-click landed, from the DOM (desktop and phone). */
export function targetAt(el: Element | null, selection: string, threads: Record<string, Thread>): MenuTarget {
  if (!el) return { kind: "none" };
  if (el.closest("input, textarea, select, [contenteditable=''], [contenteditable=true]")) return { kind: "editable" };
  const a = el.closest("a[href]") as HTMLAnchorElement | null;
  if (a) return { kind: "link", href: a.href, selection: selection || undefined };
  if (selection) return { kind: "text", selection };
  const img = el.closest("img") as HTMLImageElement | null;
  if (img && img.src && !img.closest("[data-id]")) return { kind: "image", src: img.src };
  const row = el.closest("[data-id]") as HTMLElement | null;
  const t = row ? threads[row.dataset.id ?? ""] : undefined;
  if (t) return { kind: "thread", t };
  return { kind: "none" };
}
