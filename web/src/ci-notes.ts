// CI on the diff lines (SPEC 0.5 V0.66 (7), (8)). Pure logic: which annotations the Changes tab shows, and where
// they go in a file's diff.

import type { DiffLine } from "./changes.ts";
import type { Change, CiAnnotation } from "./types.ts";

const RANK: Record<string, number> = { failure: 0, warning: 1, notice: 2 };
const MARK: Record<string, string> = { failure: "✖", warning: "▲", notice: "ℹ" };

/** The annotations of the changes whose PR is in review (the host clears them when the checks stop failing). */
export function ciNotes(changes: Change[] | undefined): CiAnnotation[] {
  return (changes ?? []).filter((c) => c.phase === "review").flatMap((c) => c.annotations ?? []);
}

export function notesFor(notes: CiAnnotation[], path: string): CiAnnotation[] {
  return notes.filter((n) => n.path === path);
}

/** "✖ lint · unused: x is unused", the message's first line; withLine adds " (line N)". */
export function noteText(a: CiAnnotation, withLine = false): string {
  const msg = (a.message ?? "").split("\n")[0].trim().slice(0, 300);
  const head = a.title ? `${a.check} · ${a.title}` : a.check;
  return `${MARK[a.level] ?? MARK.notice} ${head}: ${msg}${withLine ? ` (line ${a.line})` : ""}`;
}

/** The diff lines with a note line under each new-side line an annotation names; annotations on lines the diff does
 *  not show come first, with their line number. Failures before warnings and notices. */
export function placeNotes(lines: DiffLine[], notes: CiAnnotation[]): DiffLine[] {
  if (!notes.length) return lines;
  const sorted = [...notes].sort((a, b) => (RANK[a.level] ?? 2) - (RANK[b.level] ?? 2));
  const shown = new Set<number>();
  for (const l of lines) if (l.new != null) shown.add(l.new);
  const at = new Map<number, CiAnnotation[]>();
  const out: DiffLine[] = [];
  for (const n of sorted) {
    if (shown.has(n.line)) at.set(n.line, [...(at.get(n.line) ?? []), n]);
    else out.push({ kind: "note", text: noteText(n, true) });
  }
  for (const l of lines) {
    out.push(l);
    if (l.new != null) for (const n of at.get(l.new) ?? []) out.push({ kind: "note", text: noteText(n) });
  }
  return out;
}

/** "2 CI annotations" */
export function countLabel(n: number): string {
  return `${n} CI ${n === 1 ? "annotation" : "annotations"}`;
}
