// SPEC 0.5 V0.57: the View tab's chat can be dismissed; it stays hidden until a new message is typed there.

const KEY = "fyr.viewChatDismissed";

/** Which message the chat shows: its id, else its render key. */
export function chatKey(mine: { id?: string; key: string }): string {
  return mine.id ?? mine.key;
}

/** Hidden when the owner dismissed exactly this message in this thread. */
export function chatHidden(dismissed: Record<string, string>, threadId: string, key: string): boolean {
  return dismissed[threadId] === key;
}

export function loadDismissed(): Record<string, string> {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? "{}") as unknown;
    return v && typeof v === "object" ? (v as Record<string, string>) : {};
  } catch {
    return {};
  }
}

export function saveDismissed(d: Record<string, string>): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(d));
  } catch {}
}
