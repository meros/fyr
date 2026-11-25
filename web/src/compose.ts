// V0.composer-focus-reach (1): what the composer of both fronts decides without a DOM: what a key does and what the field says.
// The hook and the two views are in composer.tsx.

/** What a key in the field does (V0.composer-focus-reach (1)). Enter sends and Shift+Enter is a new line where Enter sends
 *  (`enterSends`, the desktop); in the answer field Enter answers and ⌘/Ctrl+Enter queues the text as a message
 *  instead. "none": the key is the field's own (a letter, a new line, an IME composing). */
export function composerKey(
  e: { key: string; shiftKey: boolean; metaKey: boolean; ctrlKey: boolean; isComposing: boolean; repeat: boolean },
  o: { enterSends: boolean; answering: boolean },
): "send" | "answer" | "none" {
  if (!o.enterSends || e.key !== "Enter" || e.shiftKey || e.isComposing || e.repeat) return "none";
  return o.answering && !(e.metaKey || e.ctrlKey) ? "answer" : "send";
}

/** V0.no-interrupt (1): whether an open question's answer field takes the desktop composer. A question that arrives
 *  while the owner has a message in the field does not take it: the draft stays, and Enter sends it. The field becomes
 *  the answer field once it is empty, at once when the owner chooses "Other…" (0), or when the answer already has
 *  text. */
export function answerTakesComposer(draft: string, answer: { text: string; chosen: boolean } | null): boolean {
  if (!answer) return false;
  return !draft.trim() || answer.chosen || answer.text !== "";
}

/** The placeholder: the question to answer, why sending is off, or what a message does now. */
export function composerPlaceholder(t: { running: boolean }, sendReason: string | undefined, answer: { index: number } | null): string {
  if (answer) return `Answer question ${answer.index + 1} in your own words…`;
  if (sendReason) return sendReason;
  return t.running ? "Steer the run…" : "Message…";
}
