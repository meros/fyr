// V0.conversation-jumps: moving inside a long conversation. Pure: which entries are stops (the owner's own messages,
// the Run cards that wait for the owner), which stop a jump goes to from where the view stands, and how many messages
// arrived since the owner scrolled up. conversation-ui.tsx draws the controls, the same on the desktop and the phone.

import type { Entry, Item } from "./conversation.ts";

/** "own": a message the owner wrote (in the composer or the live view); "needs": a Run card that waits for him. */
export type StopKind = "own" | "needs";

export interface Stop {
  key: string;
  kind: StopKind;
}

/** What an item is a stop for, or null. A message from another thread is that thread's, not the owner's. */
export function stopOf(it: Item, waiting: ReadonlySet<string>): StopKind | null {
  if (it.kind === "user") return !it.from || it.from === "view" ? "own" : null;
  if (it.kind === "runcard" && waiting.has(it.action)) return "needs";
  return null;
}

/** The DOM key of a Run card that stands at the end because its place is not loaded (placeCards' `loose`). */
export const looseKey = (id: string) => `card-${id}`;

/**
 * The stops in the order the conversation draws them: the paced entries (a fold never holds a message or a card, so
 * it stays folded), the loose cards, then the messages the agent has not read. `waiting` are the ids of the run
 * requests not yet run.
 */
export function stopsOf(entries: Entry[], pending: Item[], loose: string[], waiting: ReadonlySet<string>): Stop[] {
  const out: Stop[] = [];
  // V0.run-cards-stay-put: a loose card stands before the loaded messages, so it is the first stop.
  for (const id of loose) if (waiting.has(id)) out.push({ key: looseKey(id), kind: "needs" });
  for (const en of entries) {
    if (en.kind !== "item") continue;
    const kind = stopOf(en.item, waiting);
    if (kind) out.push({ key: en.key, kind });
  }
  for (const it of pending) {
    const kind = stopOf(it, waiting);
    if (kind) out.push({ key: it.key, kind });
  }
  return out;
}

/** The desktop keys ([ ] w, app-keyboard.tsx) reach the open conversation as this window event. */
export const JUMP_EVENT = "fyr:jump";
export type JumpAsk = { kind: StopKind; dir: 1 | -1 };

/** A stop where it is drawn: its top and bottom in the scroller's content, in px. */
export interface Placed extends Stop {
  top: number;
  bottom: number;
}

/** A jump lands with the stop this far under the top of the view. */
export const LAND = 12;

/**
 * The stop a jump goes to, or null. `view` is the visible part of the content. From the stop the last jump landed on,
 * while it is still in view, the jump steps in order (at the end of the conversation the view cannot scroll a late stop
 * up to the top, so its position says nothing); otherwise it goes by position: next is the first stop a jump would scroll down to,
 * previous the last one it would scroll up to. `wrap` goes round from the last to the first.
 */
export function pickStop(stops: Placed[], kind: StopKind, dir: 1 | -1, view: { top: number; bottom: number }, from?: string | null, wrap = false): string | null {
  const list = stops.filter((s) => s.kind === kind);
  if (!list.length) return null;
  const round = () => (wrap ? list[dir > 0 ? 0 : list.length - 1].key : null);
  const i = from ? list.findIndex((s) => s.key === from) : -1;
  if (i >= 0 && list[i].bottom > view.top && list[i].top < view.bottom) return list[i + dir]?.key ?? round();
  // Where the view would stand after the jump: a stop that would not move it is not previous or next.
  const to = (s: Placed) => Math.max(0, s.top - LAND);
  const hit = dir > 0 ? list.find((s) => to(s) > view.top + 4) : list.findLast((s) => to(s) < view.top - 4);
  return hit?.key ?? round();
}

/** Whether an item counts as a new message on the end arrow: what the agent says or shows, a card, a message from
 *  another thread. Tool calls, run marks and the owner's own messages do not. */
export function counts(it: Item): boolean {
  switch (it.kind) {
    case "assistant":
      return !it.quiet;
    case "runcard":
    case "preview":
      return true;
    case "user":
      return !!it.from && it.from !== "view";
    default:
      return false;
  }
}

/** Where the owner left the end: the key of the last item that counts, or "" when none does yet. */
export function leftAt(items: Item[]): string {
  for (let i = items.length - 1; i >= 0; i--) if (counts(items[i])) return items[i].key;
  return "";
}

/** How many messages arrived after the mark `leftAt` took. Older messages loaded above it do not count; a mark that is
 *  no longer in the list (another thread, a rebuild) counts nothing rather than everything. */
export function newSince(items: Item[], mark: string): number {
  const from = mark === "" ? 0 : items.findIndex((it) => it.key === mark) + 1;
  if (from === 0 && mark !== "") return 0;
  let n = 0;
  for (let i = from; i < items.length; i++) if (counts(items[i])) n++;
  return n;
}
