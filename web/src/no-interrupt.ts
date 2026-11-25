// V0.no-interrupt: what the host or the clock does never takes over what the owner is doing. The rules that need no
// DOM are here; the composer's is in compose.ts (`answerTakesComposer`).

/** (3) How long a card that just arrived shows before the keys act on it: a key pressed for something else (a digit,
 *  Enter) in the moment a question or an approval comes is not an answer to it. */
export const ARRIVAL_GRACE_MS = 1000;

/** (3) The keys act on the open thread's decision only once it has shown for ARRIVAL_GRACE_MS. `shownAt` 0: it was
 *  there when the owner opened the thread, so it takes keys at once. */
export function cardTakesKeys(shownAt: number | null, now: number): boolean {
  return shownAt !== null && (shownAt === 0 || now - shownAt >= ARRIVAL_GRACE_MS);
}

/** (4) How long after the owner's last key the page counts as being typed in. */
export const TYPING_QUIET_MS = 10_000;

/** (4) A reload for a new build waits while the owner is busy: a dialog or sheet is open, a field they are in holds
 *  text, or they typed in the last TYPING_QUIET_MS. A hidden page is never busy. */
export function ownerBusy(s: { hidden: boolean; dialogOpen: boolean; fieldText: string | null; lastKeyAt: number; now: number }): boolean {
  if (s.hidden) return false;
  return s.dialogOpen || !!s.fieldText?.trim() || s.now - s.lastKeyAt < TYPING_QUIET_MS;
}

/** V0.no-interrupt-rest (1): the tab a thread shows when nobody picked one: a live view that was there when the thread opened makes it
 *  View; one that appears while the thread is open adds the View tab but leaves the owner on what they read. */
export function unpickedTab(hasView: boolean, hadViewWhenOpened: boolean): "view" | "conv" {
  return hasView && hadViewWhenOpened ? "view" : "conv";
}

/** V0.no-interrupt-rest (3): the desktop opens the next thread after an answer only if the owner is still on the answered thread and has
 *  pressed no key since the answer: the host's yes can come seconds late. */
export function moveOnAfterAnswer(s: { stillOpen: boolean; keyAfter: boolean }): boolean {
  return s.stillOpen && !s.keyAfter;
}

/** V0.no-interrupt-rest (2): the page a live view's frame shows: a republished page replaces the shown one at once, unless the owner is in
 *  the frame (it has the focus): then the old page stays and the header offers the new one. */
export function framePage(shown: string, latest: string, inFrame: boolean): string {
  return shown === latest || inFrame ? shown : latest;
}

/** (5) A field's value pushed by the host replaces the text only while the owner has not changed it. */
export function pushedValue(text: string, before: string, after: string): string {
  return text === before ? after : text;
}
