// SPEC 0.5 V0.31: what Fyr's right-click menu holds. Pure: the target in, the
// entries out. The menu component and the app wire the ids to actions.
import type { Thread } from "./types.ts";

export type MenuItem = { id: string; label: string; key?: string; disabled?: string };
export type MenuEntry = MenuItem | { sep: true };

export type MenuTarget =
  | { kind: "thread"; t: Thread }
  | { kind: "text"; selection: string }
  | { kind: "link"; href: string; selection?: string }
  | { kind: "image"; src: string }
  | { kind: "editable" }
  | { kind: "none" };

const SEP: MenuEntry = { sep: true };

function item(t: Thread, id: string, action: string, label: string, key?: string): MenuItem {
  const why = t.unavailable[action];
  return { id, label, key, ...(why ? { disabled: why } : {}) };
}

/** V0.189: the one seen/unseen entry of a thread, on both fronts: Mark seen while it is unread (or in Needs you, as
 *  before), else Mark unseen. The host sends an unread thread in section "seen", so the state decides. */
export function seenToggle(t: Thread): { id: "markSeen" | "markUnseen"; action: "thread.markSeen" | "thread.markUnseen"; label: string; key?: string } {
  return t.attention.state === "done" || t.attention.section === "needs"
    ? { id: "markSeen", action: "thread.markSeen", label: "Mark seen" }
    : { id: "markUnseen", action: "thread.markUnseen", label: "Mark unseen", key: "u" };
}

export function threadMenu(t: Thread): MenuEntry[] {
  const settled = !!t.settledAt || t.attention.state === "settled";
  const archived = !!t.archivedAt || t.attention.state === "archived";
  const snoozed = t.attention.state === "snoozed";
  const mark = seenToggle(t);
  const out: MenuEntry[] = [{ id: "open", label: "Open", key: "↵" }, SEP];
  out.push(t.pinnedAt ? item(t, "unpin", "thread.unpin", "Unpin") : item(t, "pin", "thread.pin", "Pin")); // V0.104
  out.push(settled ? item(t, "unsettle", "thread.unsettle", "Unsettle") : item(t, "settle", "thread.settle", "Settle", "e"));
  out.push(snoozed ? item(t, "unsnooze", "thread.unsnooze", "Unsnooze") : item(t, "snooze", "thread.snooze", "Snooze…", "s"));
  out.push(item(t, mark.id, mark.action, mark.label, mark.key));
  out.push(archived ? item(t, "unarchive", "thread.unarchive", "Unarchive") : item(t, "archive", "thread.archive", "Archive", "#"));
  out.push(SEP, item(t, "project", "thread.setProject", "Project…", "p"));
  out.push(item(t, "liveView", "thread.setView", t.liveView ? "Turn off live view" : "Turn on live view"));
  out.push(item(t, "handover", "thread.handover", "Hand over…"));
  out.push(item(t, "regenTitle", "thread.regenerateTitle", "Regenerate title")); // V0.108
  if (t.running) out.push(item(t, "interrupt", "thread.interrupt", "Stop run", "Esc"));
  if (t.pr && (t.pr.state === "MERGED" || t.pr.state === "CLOSED" || t.pr.state === "MISSING")) out.push(item(t, "dismissPR", "thread.dismissPR", "Dismiss PR"));
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
