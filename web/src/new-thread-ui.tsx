// V0.dialogs-review: the parts of New thread that the desktop dialog (dialogs.tsx) and the phone screen
// (phone-new.tsx) share as they are: the line about a draft above the prompt, and what stands beside Start.

import type { useNewThreadForm } from "./new-thread-form.ts";

type Form = ReturnType<typeof useNewThreadForm>;

/** The line above the prompt of a form that opened with a draft ("Draft from yesterday 16:40 · in acme/web"), until
 *  the owner changes the form or hides the line; and after a Discard, the way back ("Discarded · Undo") until the
 *  next change. The draft's line has one control, × (hide the line): Discard has one place, beside Start
 *  (`StartNote`), so it does not move when the line goes and never stands next to a × that could be taken for it. */
export function DraftLine({ n }: { n: Pick<Form, "draftLine" | "undoDiscard" | "hideLine"> }) {
  const l = n.draftLine;
  if (!l) return null;
  return (
    <div class="ntdraft" role="status">
      <span class="ntdt">{l.text}</span>
      {l.kind === "discarded" && (
        <button class="dfl" onClick={n.undoDiscard}>
          Undo
        </button>
      )}
      <button class="ntdx" aria-label="Hide this line" title="Hide this line" onClick={n.hideLine}>
        ×
      </button>
    </div>
  );
}

/** What stands beside New thread's Start on both fronts (the desktop's foot, the phone's bottom bar), always in
 *  view: the error of the last try (an alert), else that Enter must be pressed again for an untouched draft, else why
 *  Start is off; and Discard while the form holds something. */
export function StartNote({ n }: { n: Pick<Form, "err" | "block" | "enterNote" | "hasDraft" | "discard"> }) {
  return (
    <div class="dfn">
      {n.err ? (
        <span class="err" role="alert">
          {n.err}
        </span>
      ) : n.enterNote ? (
        <span class="dfe" role="status">
          {n.enterNote}
        </span>
      ) : (
        n.block && <span>{n.block}</span>
      )}
      {n.hasDraft && (
        <span>
          <button class="dfl" onClick={n.discard}>
            Discard
          </button>
        </span>
      )}
    </div>
  );
}
