// V0.wakeups-via-watch: the agent's armed wake-up as a chip, one component for the desktop header, the phone header and
// the inbox row (row-ui.tsx, both fronts), so a thread that waits on a timer reads apart from a finished one.

import { wakeChip } from "./offers.ts";
import type { Thread } from "./types.ts";

const CLOCK = (
  <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <circle cx="12" cy="12" r="9" />
    <path d="M12 7v5l3 2" />
  </svg>
);

/** The chip in the class of its slot ("chip" on the desktop header, "m-chip" on the phone's, "pr" on a row). With onCancel it
 *  is a button: a press cancels the wake-up (thread.cancelWake), as Cancel settle when done does for that wait. */
export function WakeChip({ t, now, cls, onCancel, button }: { t: Pick<Thread, "wakes">; now: number; cls: string; onCancel?: (rule: string) => void; button?: string }) {
  const w = wakeChip(t, now);
  if (!w) return null;
  if (!onCancel)
    return (
      <span class={`${cls} wake`} title={w.title}>
        {CLOCK}
        <span>{w.text}</span>
      </span>
    );
  const label = `${w.text}. Cancel this wake-up`;
  // button: the phone wraps the chip in a 44 px tap target, as its project chip does
  if (button)
    return (
      <button type="button" class={button} title={`${w.title}. Press to cancel it.`} aria-label={label} onClick={(e) => (e.stopPropagation(), onCancel(w.rule))}>
        <span class={`${cls} wake`}>
          {CLOCK}
          <span>{w.text}</span>
        </span>
      </button>
    );
  return (
    <button type="button" class={`${cls} wake`} title={`${w.title}. Press to cancel it.`} aria-label={label} onClick={(e) => (e.stopPropagation(), onCancel(w.rule))}>
      {CLOCK}
      <span>{w.text}</span>
    </button>
  );
}
