// SPEC V0.176: leftover processes on the open thread; the rail's and the phone sheet's count are in load-ui.tsx,
// Settings' lists in settings-ui.tsx. The texts are leftovers.ts's. One component serves the desktop and the phone unless a reason is
// written beside it.

import { useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { hostIdByName } from "./hosts.ts";
import { useHosts } from "./host-pick-ui.tsx";
import { refreshLoad, useLoad } from "./load-ui.tsx";
import { findLeftovers, stripText } from "./leftovers.ts";

/** A host command on a leftover (End, Keep, Remove), then a fresh read of the load; the error text or null. */
export function useLeftoverCommand(): (host: string, action: string, args: Record<string, unknown>) => Promise<string | null> {
  const api = useApi();
  const { hosts } = useHosts();
  return async (host, action, args) => {
    const r = await api.command(hostIdByName(hosts, host), action, undefined, args);
    refreshLoad();
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
    for (const l of f.items) e = (await run(f.host, action, { id: l.id, ...args })) ?? e;
    setErr(e);
    setBusy(false);
  };
  const keepable = f.items.some((l) => !l.kept);
  return (
    <div class="lftb" role="status">
      <span class="lftb-b">
        {stripText(f.items, now)}
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
