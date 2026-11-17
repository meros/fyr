// SPEC V0.164 (design/rogue-agents/SPEC.md 8): undeclared heavy work on the row, the rail and the open thread. The
// texts are undeclared.ts's. One component serves the desktop and the phone unless a reason is written beside it.

import { useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { withHost } from "./host-settings.ts";
import { refreshLoad, useLoad } from "./load-ui.tsx";
import { UndIcon, useUnd } from "./undeclared-ctx.tsx";
import { findUndeclared, rowUndeclared, stripView } from "./undeclared.ts";

/** The command a thread's strip and Settings ask the host to run: the owner's dismiss of one episode. */
export function useDismiss(): (host: string, thread: string, on: boolean) => Promise<string | null> {
  const api = useApi();
  return async (host, thread, on) => {
    const r = await api.command("host.undeclaredDismiss", undefined, withHost({ thread, on }, host));
    refreshLoad();
    return r.ok ? null : (r.error ?? "That did not work");
  };
}

/** The start of a flagged thread's second line (SPEC 8.1): the mark, the word and the use, in the beam ink. Null for a
 *  thread that is not flagged or whose mark the owner dismissed. */
export function RowUndeclared({ id, now }: { id: string; now: number }) {
  const m = rowUndeclared(useLoad(), id, now);
  if (!m) return null;
  return (
    <span class="tund" title={m.title}>
      <UndIcon />
      {m.text}
    </span>
  );
}

/** True when the row shows the mark: the row then leaves out the thread's own CPU and job words, which the mark carries. */
export function useRowUndeclared(id: string, now: number): boolean {
  return !!rowUndeclared(useLoad(), id, now);
}

/** The strip under the header of an open thread (SPEC 8.3), on both fronts: the use, the command, what it counts against,
 *  when the agent was told. Dismiss hides it until the work stops; "Add as heavy job…" opens Settings → Automation at
 *  this command's suggestion. Paused, it turns harbour and has no buttons. The phone's buttons sit under the text
 *  (phone.css: touch, 44 px). Null when the thread is not flagged or dismissed. */
export function UndeclaredStrip({ id, now }: { id: string; now: number }) {
  const list = useLoad();
  const nav = useUnd();
  const dismiss = useDismiss();
  const [err, setErr] = useState<string | null>(null);
  const f = findUndeclared(list, id);
  if (!f || f.u.dismissed) return null;
  const { h, u } = f;
  const s = stripView(h, u, now);
  const add = () => {
    const sug = h.suggestions?.find((x) => x.command === u.command);
    nav.openSettings({ page: "automation", ...(sug ? { sub: sug.id } : {}) });
  };
  return (
    <div class={`undb${s.paused ? " paused" : ""}`} role="status">
      <UndIcon size={14} />
      <span class="undb-b">
        <b>{s.head}</b>
        {s.usage && ` · ${s.usage}`}
        {s.body && ` ${s.body}`}
        <br />
        <span class="mono">{u.command}</span> <span class="dim">· {s.note}</span>
        {err && <span class="err"> · {err}</span>}
      </span>
      {(s.add || s.dismiss) && (
        <span class="undb-a">
          {s.add && (
            <button type="button" class="btn" onClick={add}>
              Add as heavy job…
            </button>
          )}
          {s.dismiss && (
            <button type="button" class="btn" onClick={async () => setErr(await dismiss(h.host, u.thread, true))}>
              Dismiss
            </button>
          )}
        </span>
      )}
    </div>
  );
}
