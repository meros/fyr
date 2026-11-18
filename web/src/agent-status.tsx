// SPEC 0.5 V0.143: the status line above the composer, one component on desktop and phone. While a run goes it
// spins with the live line (V0.153: "Waiting for the machine (memory pressure)" while Fyr waits for the host); while the agent does not respond it says since when and why, and offers Restart.
// V0.195: after the turn, it names the background command the thread waits for, how long it has run, and that the thread went quiet.
import { StateIcon } from "./icons.tsx";
import { liveLine, waitLine } from "./tasks.ts";
import { isAvailable, unavailableReason } from "./store.ts";
import type { Thread } from "./types.ts";

export function AgentStatus({ t, now, phone, onRestart }: { t: Thread; now: number; phone?: boolean; onRestart(): void }) {
  const stuck = t.unresponsive;
  const cls = phone ? "sys m-status" : "sys live";
  if (!t.running && !stuck && !t.machineWait) {
    const wait = waitLine(t, now);
    if (!wait) return null;
    const quiet = t.tasks.some((x) => x.status === "running" && x.quietSince);
    return (
      <div class={`${cls}${quiet ? " stuck" : ""}`} role="status" data-bg-wait>
        <span class="icw">
          <StateIcon state="waiting" size={14} />
        </span>
        <span class="ell" title={wait}>
          {wait}
        </span>
      </div>
    );
  }
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
