// V0.waits-strip: what the agent waits for (its armed watches), above the composer, with a cancel for each. One component
// serves the desktop and the phone; the phone's button is a 44 px touch target (phone.css, as the leftover strip's). The phone
// shows the first wait and "+N more": the screen is short (360x640 shows the composer above the keyboard line), so every wait
// as a 44 px row would push the composer off it; a cancel reveals the next.
// V0.snooze-until-wake, V0.snooze-every-wait: every wait has "Snooze until then" too: the thread leaves the list until the
// wait ends (its time, or its watch firing or being cancelled, at most 24 h).

import { canSnoozeWait } from "./snooze.ts";
import type { Thread } from "./types.ts";

/** Null when the thread has no armed watch. A press on Cancel is the owner's thread.cancelWait {rule}; a press on
 *  Snooze is thread.snoozeUntilWait {rule}. */
export function WaitsStrip({ t, onCancel, onSnooze, phone }: { t: Pick<Thread, "waits" | "unavailable" | "attention">; onCancel(rule: string): void; onSnooze(rule: string): void; phone?: boolean }) {
  const all = t.waits ?? [];
  if (all.length === 0) return null;
  const ws = phone ? all.slice(0, 1) : all;
  const more = all.length - ws.length;
  const now = Date.now();
  return (
    <div class="wtsb" role="status">
      {ws.map((w) => (
        <div class="wtsb-r" key={w.rule}>
          <span class="wtsb-b">Waiting: {w.text}{more > 0 ? ` · +${more} more` : ""}</span>
          {canSnoozeWait(t, w, now) && (
            <button
              type="button"
              class="btn"
              title={w.at ? "Snooze this thread until the agent wakes. It leaves the list until then." : "Snooze this thread until this wait ends (at most 24 h). It leaves the list until then."}
              aria-label={`Snooze until then: ${w.text}`}
              onClick={() => onSnooze(w.rule)}
            >
              {/* The phone says "Snooze": two buttons and the wait's words share 360 px, and the full label leaves the words no room. */}
              {phone ? "Snooze" : "Snooze until then"}
            </button>
          )}
          <button type="button" class="btn" title="Stop waiting. The agent is told it will not wake for this." aria-label={`Cancel the wait: ${w.text}`} onClick={() => onCancel(w.rule)}>
            Cancel wait
          </button>
        </div>
      ))}
    </div>
  );
}
