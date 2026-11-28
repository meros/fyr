// SPEC V0.176: leftover processes on the open thread; the rail's and the phone sheet's count are in load-ui.tsx,
// Settings' lists in settings-ui.tsx. The texts are leftovers.ts's. One component serves the desktop and the phone unless a reason is
// written beside it.

import type { HostId } from "./hostid.ts";
import { useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { refreshLoad, useLoad } from "./load-ui.tsx";
import { findLeftovers, stripText } from "./leftovers.ts";

/** A host command on a leftover (End, Keep, Remove); the error text or null. Caller calls refreshLoad() once done. */
function useLeftoverCommand(): (host: HostId | undefined, action: string, args: Record<string, unknown>) => Promise<string | null> {
  const api = useApi();
  return async (host, action, args) => {
    if (!host) return "This host reports no install id, so nothing can be sent to it until it updates";
    const r = await api.command(host, action, undefined, args);
    return r.ok ? null : (r.error ?? "That did not work");
  };
}

/** The strip under an open thread's header: what the thread left running, and when Fyr ends it. End ends them now,
 *  Keep holds them for a day (through a settle too). The phone's buttons sit under the text (phone.css: touch, 44 px).
 *  Null when the thread left nothing. */
export function LeftoverStrip({ id, now }: { id: string; now: number }) {
  const f = findLeftovers(useLoad(), id);
  const run = useLeftoverCommand();
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  if (!f) return null;
  const all = async (action: string, args: Record<string, unknown> = {}) => {
    setBusy(true);
    let e: string | null = null;
    for (const l of f.items) e = (await run(f.hostId, action, { id: l.id, ...args })) ?? e;
    refreshLoad();
    setErr(e);
    setBusy(false);
  };
  const keepable = f.items.some((l) => !l.kept);
  const text = stripText(f.items, now);
  return (
    <div class="lftb" role="status">
      <span class="lftb-b" title={err ? `${text} · ${err}` : text}>
        {text}
        {err && <span class="err"> · {err}</span>}
      </span>
      <span class="lftb-a">
        {keepable && (
          <button type="button" class="btn" disabled={busy} onClick={() => void all("host.leftoverKeep", { hours: 24 })} title="Keep them for a day, through a settle too">
            Keep
          </button>
        )}
        <button type="button" class="btn" disabled={busy} onClick={() => void all("host.leftoverEnd")} title="End them now (SIGTERM, then SIGKILL after 5 s)">
          End
        </button>
      </span>
    </div>
  );
}
