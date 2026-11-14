// The Changes tab (SPEC 0.5 V0.25): the branch diff against its base. Pure logic: the DTOs of
// GET /api/threads/{id}/changes, unified-diff parsing, path shortening, sorting, the summary line.

export interface ChangedFile {
  path: string;
  oldPath?: string;
  status: "added" | "modified" | "deleted" | "renamed";
  additions: number;
  deletions: number;
  binary: boolean;
}

export interface ChangesDTO {
  git: boolean;
  root?: string;
  branch?: string;
  base?: string;
  mergeBase?: string;
  commits?: number;
  files?: ChangedFile[];
  truncated?: boolean;
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
