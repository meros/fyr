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
    if (!wait) {
      // V0.thread-claims: every other wait says why inside the thread too (CI, a quota reset, a held or queued command,
      // a handover), as its inbox row does; until now only the desktop chip's tooltip had it. The agent's own watches
      // have their strip with a Cancel (WaitsStrip), so they are not said twice.
      // Running subagents have theirs too (AgentsStrip).
      const reason = t.attention.reason ?? "";
      const watch = !!t.waits?.length && reason.startsWith(t.waits[0].text);
      const agents = t.tasks.some((x) => x.status === "running");
      const why = t.attention.state === "waiting" && !watch && !agents ? reason : "";
      if (!why) return null;
      return (
        <div class={cls} role="status" data-wait>
          <span class="icw">
            <StateIcon state="waiting" size={14} />
          </span>
          <span class="ell" title={why}>
            {why}
          </span>
        </div>
      );
    }
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
      <button class={phone ? "m-bt" : "btn"} disabled={!isAvailable(t, "thread.restart")} title={why ?? "Restart the agent. It picks up where it left off and gets your unread messages again."} onClick={onRestart}>
        Restart agent
      </button>
    </div>
  );
}
