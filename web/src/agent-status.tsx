// SPEC 0.5 V0.143: the status line above the composer, one component on desktop and phone. While a run goes it
// spins with the live line; while the agent does not respond it says since when and why, and offers Restart.
import { StateIcon } from "./icons.tsx";
import { liveLine } from "./tasks.ts";
import { isAvailable, unavailableReason } from "./store.ts";
import type { Thread } from "./types.ts";

export function AgentStatus({ t, now, phone, onRestart }: { t: Thread; now: number; phone?: boolean; onRestart(): void }) {
  const stuck = t.unresponsive;
  if (!t.running && !stuck) return null;
  const cls = phone ? "sys m-status" : "sys live";
  if (!stuck) {
    return (
      <div class={cls}>
        <span class="spin icw">
          <StateIcon state="working" size={14} />
        </span>
        <span class="ell">{liveLine(t, now)}</span>
      </div>
    );
  }
  const why = unavailableReason(t, "thread.restart");
  // Phone: the button keeps the touch size of the phone's buttons (m-bt); the desktop uses its small .btn.
  return (
    <div class={`${cls} stuck`} role="status">
      <span class="icw">
        <StateIcon state="blocked" size={14} />
      </span>
      <span class="ell" title={stuck.reason}>
        {liveLine(t, now)}
      </span>
      <button class={phone ? "m-bt" : "btn"} disabled={!isAvailable(t, "thread.restart")} title={why ?? "End the process and resume the session; unread messages are delivered again"} onClick={onRestart}>
        Restart agent
      </button>
    </div>
  );
}
