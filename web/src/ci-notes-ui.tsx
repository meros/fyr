// CI on the diff lines (SPEC 0.5 V0.66 (8)): the thread's annotations reach the Changes tab's file rows and diffs
// through a context, so the tab itself needs only a count and one call on its lines.

import { createContext, type ComponentChildren } from "preact";
import { useContext, useMemo } from "preact/hooks";
import { ciNotes, countLabel, notesFor } from "./ci-notes.ts";
import type { Change, CiAnnotation } from "./types.ts";

export { placeNotes } from "./ci-notes.ts";

const Notes = createContext<CiAnnotation[]>([]);

/** Wraps the Changes tab with the thread's annotations. */
export function CiNotesScope({ changes, children }: { changes: Change[] | undefined; children: ComponentChildren }) {
  const notes = useMemo(() => ciNotes(changes), [changes]);
  return <Notes.Provider value={notes}>{children}</Notes.Provider>;
}

export function useCiNotes(path: string): CiAnnotation[] {
  const all = useContext(Notes);
  return useMemo(() => notesFor(all, path), [all, path]);
}

/** The number of annotations on a file, on its row. */
export function CiCount({ path }: { path: string }) {
  const n = useCiNotes(path);
  if (!n.length) return null;
  const label = countLabel(n.length);
  return (
    <span class={`chg-ci tnum${n.some((a) => a.level === "failure") ? " fail" : ""}`} title={label} aria-label={label}>
      {n.length}
    </span>
  );
}
