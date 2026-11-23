// V0.waits-strip: what the agent waits for (its armed watches), above the composer, with a cancel for each. One component
// serves the desktop and the phone; the phone's button is a 44 px touch target (phone.css, as the leftover strip's).

import type { Thread } from "./types.ts";

/** Null when the thread has no armed watch. A press on Cancel is the owner's thread.cancelWait {rule}. */
export function WaitsStrip({ t, onCancel }: { t: Pick<Thread, "waits">; onCancel(rule: string): void }) {
  const ws = t.waits ?? [];
  if (ws.length === 0) return null;
  return (
    <div class="wtsb" role="status">
      {ws.map((w) => (
        <div class="wtsb-r" key={w.rule}>
          <span class="wtsb-b">Waiting: {w.text}</span>
          <button type="button" class="btn" title="Stop waiting. The agent is told it will not wake for this." aria-label={`Cancel the wait: ${w.text}`} onClick={() => onCancel(w.rule)}>
            Cancel wait
          </button>
        </div>
      ))}
    </div>
  );
}
