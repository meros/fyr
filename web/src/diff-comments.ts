// V0.diff-line-comments (#641): the owner picks a line or a range in a file's diff on the Changes tab, writes a
// comment under it, and sends every pending comment as one message to the thread. Pure logic: picking, the comment's
// location, the message format and its parse (the conversation folds the quotes), and the per-thread drafts.

import type { DiffLine } from "./changes.ts";
import { browserStorage } from "./prefs.ts";
import type { KV } from "./drafts.ts";

/** A line the owner can pick: an added, removed or context line (never a hunk header or a note). */
export type PickLine = DiffLine & { kind: "add" | "del" | "ctx" };

export function pickable(l: DiffLine): l is PickLine {
  return l.kind === "add" || l.kind === "del" || l.kind === "ctx";
}

/** A line's key in its file's diff: the same line has the same key after a reload of the same diff. */
export function lineKey(l: DiffLine): string {
  return `${l.kind}:${l.old ?? ""}:${l.new ?? ""}`;
}

export interface DiffComment {
  id: string;
  path: string;
  /** the diff it was written on: the whole branch, or new since the owner's last look */
  mode: "whole" | "since";
  /** the head the line numbers refer to (short sha), when the host named it */
  head?: string;
  /** "since <sha>": the seen head the since diff starts from */
  since?: string;
  /** the picked lines, in diff order */
  lines: { kind: PickLine["kind"]; old?: number; new?: number; text: string }[];
  text: string;
}

/** The picked lines between two picks (either order), the hunk headers and notes between them left out. */
export function pickRange(lines: DiffLine[], a: number, b: number): PickLine[] {
  const [lo, hi] = a <= b ? [a, b] : [b, a];
  return lines.slice(lo, hi + 1).filter(pickable);
}

const span = (ns: number[]): string => {
  if (!ns.length) return "";
  const lo = Math.min(...ns);
  const hi = Math.max(...ns);
  return lo === hi ? `${lo}` : `${lo}-${hi}`;
};

/** "web/src/x.ts:42-45 (new)", "…:40 (old)", or "…:42-45 (new), 40-41 (old)" for a range over both sides. */
export function location(c: Pick<DiffComment, "path" | "lines">): string {
  const hasNew = c.lines.some((l) => l.kind !== "del");
  const hasDel = c.lines.some((l) => l.kind === "del");
  const news = span(c.lines.filter((l) => l.kind !== "del").map((l) => l.new!).filter((n) => n !== undefined));
  const olds = span(c.lines.filter((l) => l.kind === "del").map((l) => l.old!).filter((n) => n !== undefined));
  if (hasNew && hasDel) return `${c.path}:${news} (new), ${olds} (old)`;
  if (hasDel) return `${c.path}:${olds} (old)`;
  return `${c.path}:${news} (new)`;
}

/** Which diff the line numbers refer to: "whole branch at abc1234" or "since abc1234, at def5678". */
export function diffName(c: Pick<DiffComment, "mode" | "head" | "since">): string {
  const at = c.head ? `at ${c.head}` : "";
  if (c.mode === "since" && c.since) return [`since ${c.since}`, at].filter(Boolean).join(", ");
  return ["whole branch", at].filter(Boolean).join(" ");
}

const SIGN: Record<PickLine["kind"], string> = { add: "+", del: "-", ctx: " " };

/** The first line of a message of diff comments; the conversation recognises it. */
export const HEADER = "Comments on the diff";

/** The fence around the quoted lines: longer than any run of backticks in them. */
function fence(lines: string[]): string {
  let n = 3;
  for (const l of lines) for (const m of l.match(/`+/g) ?? []) n = Math.max(n, m.length + 1);
  return "`".repeat(n);
}

/**
 * One message for every pending comment, in the order written:
 *
 *     Comments on the diff (2):
 *
 *     web/src/x.ts:42-45 (new) · since abc1234, at def5678
 *     ```diff
 *     + the quoted line
 *     ```
 *     The comment.
 */
export function formatComments(cs: DiffComment[]): string {
  const parts = [`${HEADER} (${cs.length}):`];
  for (const c of cs) {
    const quoted = c.lines.map((l) => SIGN[l.kind] + l.text);
    const f = fence(quoted);
    parts.push([`${location(c)} · ${diffName(c)}`, f + "diff", ...quoted, f, c.text.trim()].join("\n"));
  }
  return parts.join("\n\n");
}

export interface ParsedComment {
  where: string;
  quote: string[];
  text: string;
}

const LOC = /^(\S.*?:\d+(?:-\d+)? \((?:new|old)\)(?:, \d+(?:-\d+)? \(old\))?) · (.+)$/;

/** Reads a message formatted by formatComments back into its comments; null for any other message. */
export function parseComments(text: string): ParsedComment[] | null {
  const lines = text.split("\n");
  if (!/^Comments on the diff \(\d+\):$/.test(lines[0] ?? "")) return null;
  const out: ParsedComment[] = [];
  let i = 1;
  while (i < lines.length) {
    if (lines[i] === "") {
      i++;
      continue;
    }
    const m = LOC.exec(lines[i]);
    const open = /^(`{3,})diff$/.exec(lines[i + 1] ?? "");
    if (!m || !open) return null;
    i += 2;
    const quote: string[] = [];
    while (i < lines.length && lines[i] !== open[1]) quote.push(lines[i++]);
    if (i >= lines.length) return null;
    i++;
    const body: string[] = [];
    // The comment runs to the next location line followed by a fence, or the end.
    while (i < lines.length && !(LOC.test(lines[i]) && /^`{3,}diff$/.test(lines[i + 1] ?? "") && body.length && body[body.length - 1] === "")) body.push(lines[i++]);
    while (body.length && body[body.length - 1] === "") body.pop();
    out.push({ where: `${m[1]} · ${m[2]}`, quote, text: body.join("\n") });
  }
  return out.length ? out : null;
}

// ---- drafts (V0.29's rule: kept per thread, survive a reload and a screen change; Send clears them) ----

const PREFIX = "fyr.diffComments.";

function valid(x: unknown): x is DiffComment {
  if (!x || typeof x !== "object") return false;
  const c = x as DiffComment;
  return typeof c.id === "string" && typeof c.path === "string" && (c.mode === "whole" || c.mode === "since") && Array.isArray(c.lines) && c.lines.length > 0 && typeof c.text === "string";
}

export function commentStore(storage: KV | null) {
  const mem = new Map<string, DiffComment[]>();
  const subs = new Set<(threadId: string) => void>();
  return {
    get(threadId: string): DiffComment[] {
      if (mem.has(threadId)) return mem.get(threadId)!;
      try {
        const raw = storage?.getItem(PREFIX + threadId);
        const v: unknown = raw ? JSON.parse(raw) : [];
        const list = Array.isArray(v) ? v.filter(valid) : [];
        mem.set(threadId, list);
        return list;
      } catch {
        return [];
      }
    },
    set(threadId: string, list: DiffComment[]) {
      if (list.length) mem.set(threadId, list);
      else mem.set(threadId, []);
      try {
        if (list.length) storage?.setItem(PREFIX + threadId, JSON.stringify(list));
        else storage?.removeItem(PREFIX + threadId);
      } catch {
        /* private mode or full: the memory copy keeps them this session */
      }
      for (const f of subs) f(threadId);
    },
    subscribe(f: (threadId: string) => void): () => void {
      subs.add(f);
      return () => subs.delete(f);
    },
  };
}

export const diffComments = commentStore(browserStorage());

let seq = 0;
export function newCommentId(): string {
  return `dc_${Date.now().toString(36)}_${(seq++).toString(36)}`;
}
