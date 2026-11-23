// V0.waits-strip: what the agent waits for (its armed watches), above the composer, with a cancel for each. One component
// serves the desktop and the phone; the phone's button is a 44 px touch target (phone.css, as the leftover strip's). The phone
// shows the first wait and "+N more": the screen is short (360x640 shows the composer above the keyboard line), so every wait
// as a 44 px row would push the composer off it; a cancel reveals the next.

import type { Thread } from "./types.ts";

/** Null when the thread has no armed watch. A press on Cancel is the owner's thread.cancelWait {rule}. */
export function WaitsStrip({ t, onCancel, phone }: { t: Pick<Thread, "waits">; onCancel(rule: string): void; phone?: boolean }) {
  const all = t.waits ?? [];
  if (all.length === 0) return null;
  const ws = phone ? all.slice(0, 1) : all;
  const more = all.length - ws.length;
  return (
    <div class="wtsb" role="status">
      {ws.map((w) => (
        <div class="wtsb-r" key={w.rule}>
          <span class="wtsb-b">Waiting: {w.text}{more > 0 ? ` · +${more} more` : ""}</span>
          <button type="button" class="btn" title="Stop waiting. The agent is told it will not wake for this." aria-label={`Cancel the wait: ${w.text}`} onClick={() => onCancel(w.rule)}>
            Cancel wait
          </button>
        </div>
      ))}
    </div>
  );
}
