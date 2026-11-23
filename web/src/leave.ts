// V0.app-shell-tidy: the one rule for a command that takes threads out of the list (settle, archive, snooze), on both
// fronts. The screen lets the threads go only once the host took the command: a refused or failed one leaves the
// owner where they were, with the error toast. Where the screen goes is the front's own (the desktop opens the next
// row, `afterSettle`; the phone goes back to the inbox, `leaveOpenThread`), and each checks there that the owner is
// still on a thread that left.

/** Runs `leave` with the threads the host took (`ThreadCommands.took`, or the ids a bulk settle took), when it took
 *  any. Resolves to whether the screen left. */
export async function leaveAfterHost(took: Promise<string[]>, leave: (ids: string[]) => void): Promise<boolean> {
  const ids = await took;
  if (!ids.length) return false;
  leave(ids);
  return true;
}
