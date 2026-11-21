// V0.one-action-list (#452): what a thread offers, written once. The desktop's right-click menu, the thread header's ⋯
// menu and the phone's action sheets all render from threadActions(); one dispatch (runAction) runs a pick on either
// front. Whether an entry can be used is the host's word: a disabled entry carries the reason the host returned in
// `thread.unavailable` (the one action vocabulary of the Solid core). Pure: the thread in, the entries out.
import { settleLabel } from "./gesture.ts";
import type { Thread } from "./types.ts";

export type Front = "desktop" | "phone";

/** Where the actions are asked for: a row in the list (the context menu, the phone's peek sheet), or the open thread
 *  (the header's ⋯ menu, the phone's thread sheet). */
export type Where = "row" | "thread";

export type ActionId =
  | "open"
  | "retry"
  | "pin"
  | "unpin"
  | "settle"
  | "unsettle"
  | "settleWhenDone"
  | "snooze"
  | "unsnooze"
  | "markSeen"
  | "markUnseen"
  | "archive"
  | "unarchive"
  | "project"
  | "liveView"
  | "handover"
  | "regenTitle"
  | "interrupt"
  | "dismissPR"
  | "details"
  | "select"
  | "copyLink";

/** A sheet the action opens: a dialog on the desktop, a sheet on the phone. */
export type SheetId = "snooze" | "project" | "handover" | "details";

export type ThreadAction = {
  id: ActionId;
  label: string;
  /** what the entry does, for its tooltip (desktop) or second line (phone) */
  hint?: string;
  /** the desktop's key for it; the phone has no keyboard and does not show it */
  key?: string;
  /** the host's reason it cannot be used now */
  disabled?: string;
  /** entries of one group sit together; a menu draws a line between groups */
  group: "open" | "state" | "thread" | "link";
  /** Offered on one front only, and why (CLAUDE.md: a difference between the fronts says its reason). */
  only?: { front: Front; why: string };
} & (
  | { kind: "command"; command: string; args?: Record<string, unknown> }
  | { kind: "sheet"; sheet: SheetId; command?: string }
  | { kind: "navigate" }
  | { kind: "copy" }
  | { kind: "select" }
);

/** V0.189: the one seen/unseen entry of a thread, on both fronts: Mark seen while it is unread (or in Needs you, as
 *  before), else Mark unseen. The host sends an unread thread in section "seen", so the state decides. */
export function seenToggle(t: Thread): { id: "markSeen" | "markUnseen"; action: "thread.markSeen" | "thread.markUnseen"; label: string; key?: string } {
  return t.attention.state === "done" || t.attention.section === "needs"
    ? { id: "markSeen", action: "thread.markSeen", label: "Mark seen" }
    : { id: "markUnseen", action: "thread.markUnseen", label: "Mark unseen", key: "u" };
}

/** V0.203: what "settle when done" waits for, in the words both fronts show. */
export const SETTLE_DONE_HINT = "Once no run is live, nothing waits for you and no PR is open";
export const SETTLE_DONE_TOAST = "Settles when done";

/** V0.203: the one "settle when done" entry of a thread, on both fronts: it arms the wait, or cancels it when armed. */
export function settleDoneEntry(t: Pick<Thread, "settleWhenDone">): { id: "settleWhenDone"; action: "thread.settleWhenDone" | "thread.unsettle"; label: string } {
  return t.settleWhenDone
    ? { id: "settleWhenDone", action: "thread.unsettle", label: "Cancel settle when done" }
    : { id: "settleWhenDone", action: "thread.settleWhenDone", label: "Settle when done" };
}

/** V0.203: the toast after a settle of one thread, on both fronts; undefined keeps the usual undo label. */
export function settleToast(t: Pick<Thread, "running"> | undefined): string | undefined {
  return t?.running ? "Settled; run stopped" : undefined;
}

/** The link Copy link copies. */
export function threadLink(origin: string, id: string): string {
  return `${origin}/?open=${id}`;
}

/** Every action the thread offers here, in menu order, each with the host's reason when it cannot be used. The
 *  entries offered on one front only say so in `only`; actionsFor() gives one front's list. */
export function threadActions(t: Thread, where: Where): ThreadAction[] {
  const why = (command: string) => t.unavailable?.[command];
  const cmd = (id: ActionId, command: string, label: string, group: ThreadAction["group"], o: { key?: string; hint?: string; args?: Record<string, unknown> } = {}): ThreadAction => {
    const d = why(command);
    return { id, kind: "command", command, label, group, ...(o.args ? { args: o.args } : {}), ...(o.key ? { key: o.key } : {}), ...(o.hint ? { hint: o.hint } : {}), ...(d ? { disabled: d } : {}) };
  };
  const settled = !!t.settledAt || t.attention.state === "settled";
  const archived = !!t.archivedAt || t.attention.state === "archived";
  const snoozed = t.attention.state === "snoozed";
  const mark = seenToggle(t);
  const done = settleDoneEntry(t);
  const out: ThreadAction[] = [];
  // The open thread is already open.
  if (where === "row") out.push({ id: "open", kind: "navigate", label: "Open", key: "↵", group: "open" });
  if (t.attention.state === "failed") out.push(cmd("retry", "thread.retry", "Retry", "state", { key: "r" }));
  out.push(t.pinnedAt ? cmd("unpin", "thread.unpin", "Unpin", "state") : cmd("pin", "thread.pin", "Pin", "state", { hint: "Keep it on top of the list" })); // V0.104
  if (settled) out.push(cmd("unsettle", "thread.unsettle", "Unsettle", "state"));
  else {
    out.push(cmd("settle", "thread.settle", settleLabel(t), "state", { key: "e", hint: t.running ? "Settle now and stop the run" : "Settle now" }));
    out.push(cmd("settleWhenDone", done.action, done.label, "state", t.settleWhenDone ? {} : { hint: SETTLE_DONE_HINT }));
  }
  if (snoozed) out.push(cmd("unsnooze", "thread.unsnooze", "Unsnooze", "state"));
  else {
    const d = why("thread.snooze");
    out.push({ id: "snooze", kind: "sheet", sheet: "snooze", command: "thread.snooze", label: "Snooze…", key: "s", group: "state", ...(d ? { disabled: d } : {}) });
  }
  out.push(cmd(mark.id, mark.action, mark.label, "state", mark.key ? { key: mark.key } : {}));
  out.push(archived ? cmd("unarchive", "thread.unarchive", "Unarchive", "state") : cmd("archive", "thread.archive", "Archive", "state", { key: "#" }));
  out.push({ id: "project", kind: "sheet", sheet: "project", command: "thread.setProject", label: "Project…", key: "p", group: "thread", ...(why("thread.setProject") ? { disabled: why("thread.setProject") } : {}) });
  out.push(cmd("liveView", "thread.setView", t.liveView ? "Turn off live view" : "Turn on live view", "thread", { args: { on: !t.liveView }, hint: "The agent keeps its state pinned on top" }));
  {
    const d = why("thread.handover");
    out.push({ id: "handover", kind: "sheet", sheet: "handover", command: "thread.handover", label: "Hand over…", hint: "Continue this thread on another provider or host", group: "thread", ...(d ? { disabled: d } : {}) });
  }
  out.push(cmd("regenTitle", "thread.regenerateTitle", "Regenerate title", "thread", { hint: "Name the thread from its conversation so far" })); // V0.108
  if (t.running) out.push(cmd("interrupt", "thread.interrupt", "Stop run", "thread", { key: "Esc" }));
  if (t.pr && (t.pr.state === "MERGED" || t.pr.state === "CLOSED" || t.pr.state === "MISSING")) out.push(cmd("dismissPR", "thread.dismissPR", "Dismiss PR", "thread"));
  if (where === "thread")
    out.push({ id: "details", kind: "sheet", sheet: "details", label: "Details", group: "thread", only: { front: "phone", why: "the desktop's thread header shows the details (state, project, host, provider) at all times; the phone's header has no room for them" } });
  if (where === "row")
    out.push({ id: "select", kind: "select", label: "Select", group: "thread", only: { front: "phone", why: "the desktop picks rows with x or a ⌘/⇧-click; the phone has no keyboard or modifier keys, so its sheet starts a selection" } });
  out.push({ id: "copyLink", kind: "copy", label: "Copy link", group: "link" });
  return out;
}

/** The actions one front shows here. */
export function actionsFor(front: Front, t: Thread, where: Where): ThreadAction[] {
  return threadActions(t, where).filter((a) => !a.only || a.only.front === front);
}

/** The desktop thread header shows these as buttons beside its ⋯ menu (thread.tsx); the ⋯ menu leaves them out. */
export const DESK_HEADER_BUTTONS: readonly ActionId[] = ["retry", "pin", "unpin", "snooze", "settle"];

/** The desktop header's ⋯ menu: the open thread's actions that are not header buttons. The thread's own group comes
 *  first (Project… is the entry the menu opens on, V0.26): the state group's main entries are the buttons beside it. */
export function deskThreadMenu(t: Thread): ThreadAction[] {
  const rest = actionsFor("desktop", t, "thread").filter((a) => !DESK_HEADER_BUTTONS.includes(a.id));
  return [...rest.filter((a) => a.group === "thread"), ...rest.filter((a) => a.group !== "thread")];
}

/** How a front carries out a pick. `own` takes the actions whose front does more than send the command (a settle
 *  leaves the thread, a mark unseen keeps the open thread unread, a settle names a stopped run in its toast). */
export interface ActionHandlers {
  command(command: string, id: string, args?: Record<string, unknown>): void;
  sheet(sheet: SheetId, id: string): void;
  navigate(id: string): void;
  copy(text: string): void;
  select?(id: string): void;
  own?: Partial<Record<ActionId, (id: string) => void>>;
}

/** The one dispatch of a thread action, on both fronts. A disabled entry does nothing. */
export function runAction(a: ThreadAction, id: string, h: ActionHandlers, origin = ""): void {
  if (a.disabled) return;
  const own = h.own?.[a.id];
  if (own) return own(id);
  switch (a.kind) {
    case "command":
      return h.command(a.command, id, a.args);
    case "sheet":
      return h.sheet(a.sheet, id);
    case "navigate":
      return h.navigate(id);
    case "copy":
      return h.copy(threadLink(origin, id));
    case "select":
      return h.select?.(id);
  }
}
