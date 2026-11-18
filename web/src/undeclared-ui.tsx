// SPEC V0.164 (design/rogue-agents/SPEC.md 8): undeclared heavy work on the row, the rail and the open thread. The
// texts are undeclared.ts's. One component serves the desktop and the phone unless a reason is written beside it.

import { useLoad } from "./load-ui.tsx";
import { UndIcon } from "./undeclared-ctx.tsx";
import { findUndeclared, rowUndeclared, stripView } from "./undeclared.ts";

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

/** The strip under the header of an open thread (SPEC 8.3, V0.209), on both fronts: only while Fyr holds its work paused,
 *  and with nothing to press. Null otherwise. */
export function UndeclaredStrip({ id }: { id: string }) {
  const list = useLoad();
  const f = findUndeclared(list, id);
  const s = f && stripView(f.h, f.u);
  if (!s) return null;
  return (
    <div class="undb paused" role="status">
      <UndIcon size={14} />
      <span class="undb-b">
        <b>{s.head}</b> {s.body}
        <br />
        <span class="mono">{s.command}</span> <span class="dim">· {s.note}</span>
      </span>
    </div>
  );
}
