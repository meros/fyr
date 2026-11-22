// The mark-seen rule (SPEC 6.4): send thread.markSeen only when the thread view has been focused
// for 1.5 s and the user has scrolled to the end. A Space peek never marks seen. After "mark unseen"
// the client stops marking that thread seen until the user leaves the view and opens it again.

import type { Thread } from "./types.ts";

export const SEEN_DELAY_MS = 1500;

type ViewMode = "open" | "peek";

export interface SeenTracker {
  threadId: string | null;
  mode: ViewMode | null;
  /** since when the view has been open and the window focused; null while the window is blurred */
  focusedSince: number | null;
  atEnd: boolean;
  suppressed: boolean;
  /** `${threadId}@${activity}` of the last markSeen sent, so one result is marked once */
  sentKey: string | null;
}

export function initialTracker(): SeenTracker {
  return { threadId: null, mode: null, focusedSince: null, atEnd: false, suppressed: false, sentKey: null };
}

export function openView(tr: SeenTracker, threadId: string | null, mode: ViewMode, now: number, windowFocused = true): SeenTracker {
  if (tr.threadId === threadId && tr.mode === mode) return tr;
  const same = tr.threadId === threadId;
  return {
    threadId,
    mode: threadId ? mode : null,
    // Upgrading a peek to open starts the clock now.
    focusedSince: threadId && windowFocused ? now : null,
    atEnd: same ? tr.atEnd : false,
    suppressed: same ? tr.suppressed : false,
    sentKey: same ? tr.sentKey : null,
  };
}

export function closeView(tr: SeenTracker): SeenTracker {
  return tr.threadId === null ? tr : initialTracker();
}

export function setAtEnd(tr: SeenTracker, atEnd: boolean): SeenTracker {
  return tr.atEnd === atEnd ? tr : { ...tr, atEnd };
}

export function setWindowFocus(tr: SeenTracker, focused: boolean, now: number): SeenTracker {
  if (focused) return tr.focusedSince !== null || !tr.threadId ? tr : { ...tr, focusedSince: now };
  return tr.focusedSince === null ? tr : { ...tr, focusedSince: null };
}

export function markedUnseen(tr: SeenTracker): SeenTracker {
  return { ...tr, suppressed: true };
}

function resultKey(t: Thread): string {
  return `${t.id}@${t.activityAt ?? t.updatedAt}`;
}

export function shouldMarkSeen(tr: SeenTracker, t: Thread | undefined, now: number): boolean {
  if (!t || tr.threadId !== t.id) return false;
  if (tr.mode !== "open" || tr.suppressed || !tr.atEnd || tr.focusedSince === null) return false;
  if (now - tr.focusedSince < SEEN_DELAY_MS) return false;
  if (t.attention.state !== "done") return false;
  if ("thread.markSeen" in t.unavailable) return false;
  return tr.sentKey !== resultKey(t);
}

export function markSent(tr: SeenTracker, t: Thread): SeenTracker {
  return { ...tr, sentKey: resultKey(t) };
}
