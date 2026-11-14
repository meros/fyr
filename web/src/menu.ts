// SPEC 0.5 V0.31: what Fyr's right-click menu holds. Pure: the target in, the
// entries out. The menu component and the app wire the ids to actions.
import type { ThreadDTO } from "./types.ts";

export type MenuItem = { id: string; label: string; key?: string; disabled?: string };
export type MenuEntry = MenuItem | { sep: true };

export type MenuTarget =
  | { kind: "thread"; t: ThreadDTO }
  | { kind: "text"; selection: string }
  | { kind: "link"; href: string; selection?: string }
  | { kind: "image"; src: string }
  | { kind: "editable" }
  | { kind: "none" };

const SEP: MenuEntry = { sep: true };

function item(t: ThreadDTO, id: string, action: string, label: string, key?: string): MenuItem {
  const why = t.unavailable?.[action];
  return { id, label, key, ...(why ? { disabled: why } : {}) };
}

export function threadMenu(t: ThreadDTO): MenuEntry[] {
  const settled = !!t.settledAt || t.attention.state === "settled";
  const archived = !!t.archivedAt || t.attention.state === "archived";
  const snoozed = t.attention.state === "snoozed";
  const unseen = t.attention.section === "needs";
  const out: MenuEntry[] = [{ id: "open", label: "Open", key: "↵" }, SEP];
  out.push(settled ? item(t, "unsettle", "thread.unsettle", "Unsettle") : item(t, "settle", "thread.settle", "Settle", "e"));
  out.push(snoozed ? item(t, "unsnooze", "thread.unsnooze", "Unsnooze") : item(t, "snooze", "thread.snooze", "Snooze…", "s"));
  out.push(unseen ? item(t, "markSeen", "thread.markSeen", "Mark seen") : item(t, "markUnseen", "thread.markUnseen", "Mark unseen", "u"));
  out.push(archived ? item(t, "unarchive", "thread.unarchive", "Unarchive") : item(t, "archive", "thread.archive", "Archive", "#"));
  out.push(SEP, item(t, "project", "thread.setProject", "Project…", "p"));
  if (t.running) out.push(item(t, "interrupt", "thread.interrupt", "Stop run", "Esc"));
  out.push(SEP, { id: "copyLink", label: "Copy link" });
  return out;
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

/** Where a right-click landed, from the DOM (desktop and phone). */
export function targetAt(el: Element | null, selection: string, threads: Record<string, ThreadDTO>): MenuTarget {
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
