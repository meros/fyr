// The Changes tab (SPEC 0.5 V0.25): the branch diff against its base. Pure logic: the DTOs of
// GET /api/threads/{id}/changes, unified-diff parsing, path shortening, sorting, the summary line.
// V0.65: the look, Viewed marks, the since line, range-diff rows and the agent's notes.

import { relTime } from "./format.ts";
import type { Change } from "./types.ts";

export interface ChangedFile {
  path: string;
  oldPath?: string;
  status: "added" | "modified" | "deleted" | "renamed";
  additions: number;
  deletions: number;
  binary: boolean;
  /** V0.65: the content's blob id now; absent when deleted */
  blob?: string;
  /** V0.65: the owner marked this content Viewed */
  viewed?: boolean;
}

export interface ChangesDTO {
  git: boolean;
  root?: string;
  branch?: string;
  base?: string;
  mergeBase?: string;
  commits?: number;
  /** V0.65 */
  head?: string;
  files?: ChangedFile[];
  truncated?: boolean;
  /** V0.65: new since the owner's last look */
  since?: SinceDTO;
}

export interface FilePatchDTO {
  path: string;
  patch: string;
  binary: boolean;
  truncated: boolean;
}

export interface DiffLine {
  /** meta: file headers; hunk: @@ lines; note: "\ No newline at end of file" */
  kind: "meta" | "hunk" | "ctx" | "add" | "del" | "note";
  text: string;
  old?: number;
  new?: number;
}

/** The host lists at most this many files (then `truncated`). */
export const MAX_FILES = 1000;

const HUNK = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

/** Splits a unified diff (one or more files) into drawable lines with old and new line numbers. */
export function parsePatch(patch: string): DiffLine[] {
  if (!patch) return [];
  const raw = patch.split("\n");
  if (raw[raw.length - 1] === "") raw.pop();
  const lines: DiffLine[] = [];
  let inHunk = false;
  let o = 0;
  let n = 0;
  for (const l of raw) {
    const h = HUNK.exec(l);
    if (h) {
      inHunk = true;
      o = Number(h[1]);
      n = Number(h[2]);
      lines.push({ kind: "hunk", text: l });
    } else if (!inHunk || l.startsWith("diff --git ")) {
      inHunk = false;
      lines.push({ kind: "meta", text: l });
    } else if (l.startsWith("+")) {
      lines.push({ kind: "add", text: l.slice(1), new: n++ });
    } else if (l.startsWith("-")) {
      lines.push({ kind: "del", text: l.slice(1), old: o++ });
    } else if (l.startsWith("\\")) {
      lines.push({ kind: "note", text: l.slice(1).trim() });
    } else {
      lines.push({ kind: "ctx", text: l.startsWith(" ") ? l.slice(1) : l, old: o++, new: n++ });
    }
  }
  return lines;
}

/** Shortens a path in the middle to at most `max` characters; the file name stays whole when it fits. */
export function shortenPath(path: string, max: number): string {
  if (path.length <= max) return path;
  const cut = path.lastIndexOf("/");
  const name = path.slice(cut + 1);
  if (name.length + 2 > max) return "…" + path.slice(-(max - 1));
  let prefix = "";
  for (const seg of path.slice(0, Math.max(cut, 0)).split("/")) {
    const next = `${prefix}${seg}/`;
    if (next.length + 2 + name.length > max) break;
    prefix = next;
  }
  return `${prefix}…/${name}`;
}

/** Files by path, as the host sends them (a copy). */
export function sortFiles(files: ChangedFile[]): ChangedFile[] {
  return [...files].sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
}

export function statusMark(s: string): string {
  return s === "added" ? "A" : s === "deleted" ? "D" : s === "renamed" ? "R" : "M";
}

export function statusWord(s: string): string {
  return s === "added" ? "Added" : s === "deleted" ? "Deleted" : s === "renamed" ? "Renamed" : "Modified";
}

/** "4 files · +120 −18 · against origin/main · 3 commits" */
export function summaryLine(c: ChangesDTO): string {
  const files = c.files ?? [];
  let add = 0;
  let del = 0;
  for (const f of files) {
    add += f.additions;
    del += f.deletions;
  }
  const count = c.truncated ? `${MAX_FILES.toLocaleString("en-US")}+ files` : `${files.length} ${files.length === 1 ? "file" : "files"}`;
  const parts = [count, `+${add} −${del}`];
  if (c.base) {
    parts.push(`against ${c.base}`);
    if (c.commits) parts.push(`${c.commits} ${c.commits === 1 ? "commit" : "commits"}`);
  } else parts.push("uncommitted work");
  return parts.join(" · ");
}

/** Splits a path into its folder (with the trailing slash) and its file name, for a row that keeps the name visible. */
export function splitPath(path: string): { dir: string; name: string } {
  const i = path.lastIndexOf("/");
  return i < 0 ? { dir: "", name: path } : { dir: path.slice(0, i + 1), name: path.slice(i + 1) };
}

// ---- V0.65: what changed since you looked ----

/** The owner's look as the host compares it: the head and every listed file's blob. */
export interface Look {
  head: string;
  files: Record<string, string>;
}

/** What the reply says is new since the look. */
export interface SinceDTO {
  head: string;
  at: string;
  mode: "ancestor" | "rebased" | "missing";
  commits: number;
  files: ChangedFile[];
  rangeDiff?: string;
  rangeDiffTruncated?: boolean;
}

/** The look to record for what the tab showed; null outside git. */
export function lookOf(c: ChangesDTO): Look | null {
  if (!c.git) return null;
  const files: Record<string, string> = {};
  for (const f of c.files ?? []) files[f.path] = f.blob ?? "";
  return { head: c.head ?? "", files };
}

/** Local Viewed marks since the last load: path → the blob ticked, or null for unticked. */
export type Marks = Record<string, string | null>;

/** A file is viewed while its mark holds its current content; a local tick wins over the host's answer. */
export function isViewed(f: ChangedFile, marks: Marks): boolean {
  if (f.path in marks) {
    const m = marks[f.path];
    return m !== null && m === (f.blob ?? "");
  }
  return !!f.viewed;
}

/** "3 of 7 viewed"; empty for an empty list. */
export function viewedLine(files: ChangedFile[], marks: Marks): string {
  if (!files.length) return "";
  return `${files.filter((f) => isViewed(f, marks)).length} of ${files.length} viewed`;
}

/** The tab opens on the new files only when there are some. */
export function defaultMode(c: ChangesDTO): "since" | "whole" {
  return c.since && c.since.files.length > 0 ? "since" : "whole";
}

export function sinceLabel(s: SinceDTO): string {
  return `New since you looked · ${s.files.length}`;
}

/** One line on how HEAD moved since the look. */
export function sinceLine(s: SinceDTO, now: number): string {
  const ago = relTime(s.at, now);
  if (s.mode === "missing") return `The commit you saw is gone; new by content since ${ago} ago`;
  if (s.mode === "rebased") return `Rebased since you looked ${ago} ago`;
  if (!s.commits) return `Uncommitted edits since you looked ${ago} ago`;
  return `${s.commits} ${s.commits === 1 ? "commit" : "commits"} since you looked ${ago} ago`;
}

/** One commit of `git range-diff`: kept as is, changed (with its interdiff), added or dropped. */
export interface RangeRow {
  kind: "same" | "changed" | "added" | "dropped";
  old?: string;
  new?: string;
  subject: string;
  body: string;
}

const RANGE = /^\s*(?:\d+|-):\s+([0-9a-f]+|-+)\s+([=!<>])\s+(?:\d+|-):\s+([0-9a-f]+|-+)\s?(.*)$/;
const RANGE_KIND: Record<string, RangeRow["kind"]> = { "=": "same", "!": "changed", ">": "added", "<": "dropped" };

/** Splits range-diff output into one row per commit; a changed commit keeps its indented interdiff. */
export function rangeDiffRows(text: string): RangeRow[] {
  const rows: RangeRow[] = [];
  const bodies: string[][] = [];
  for (const l of text.split("\n")) {
    const m = RANGE.exec(l);
    if (m) {
      const sha = (s: string) => (/^-+$/.test(s) ? undefined : s);
      rows.push({ kind: RANGE_KIND[m[2]], old: sha(m[1]), new: sha(m[3]), subject: m[4], body: "" });
      bodies.push([]);
    } else if (rows.length && l.trim() !== "") bodies[bodies.length - 1].push(l.startsWith("    ") ? l.slice(4) : l);
  }
  rows.forEach((r, i) => (r.body = bodies[i].join("\n")));
  return rows;
}

/** The agent's notes for this version: the code change of this branch, its 3 newest revisions with a summary. */
export function codeNotes(changes: Change[] | undefined, branch: string | undefined): { title: string; revisions: { id: string; summary: string; at: string }[] } | null {
  if (!changes || !branch) return null;
  const c = changes.filter((x) => x.kind === "code" && x.branch === branch).sort((a, b) => (a.updatedAt < b.updatedAt ? 1 : a.updatedAt > b.updatedAt ? -1 : 0))[0];
  if (!c) return null;
  const revs = (c.revisions ?? [])
    .filter((r) => r.summary)
    .sort((a, b) => (a.at < b.at ? 1 : a.at > b.at ? -1 : 0))
    .slice(0, 3)
    .map((r) => ({ id: r.id, summary: r.summary!, at: r.at }));
  return revs.length ? { title: c.title, revisions: revs } : null;
}
