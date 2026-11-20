import { type AnswerMove, arrivalText } from "./arrived.ts";

/** V0.arrived-from: the bar under a thread's header after an answer moved the owner here. The same on desktop and
 *  phone; the phone's buttons are 44 px for touch (the m- class). */
export function ArrivedBar({ a, onBack, onDismiss, phone }: { a: AnswerMove; onBack(): void; onDismiss(): void; phone?: boolean }) {
  const x = arrivalText(a);
  return (
    <div class={`arrived${phone ? " m" : ""}`} role="status" aria-live="polite">
      <span class="arr-ic" aria-hidden="true">
        ✓
      </span>
      <span class="arr-tx">
        <b class="ell">{x.done}</b>
        <span class="ell">{x.more}</span>
      </span>
      <button class="arr-back" onClick={onBack} title={phone ? undefined : "Back to the thread you answered (Alt+←)"}>
        ‹ Back
      </button>
      <button class="arr-x" onClick={onDismiss} aria-label="Hide">
        ×
      </button>
    </div>
  );
}
