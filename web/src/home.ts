// V0.status-home: the status screen, what the desktop shows with no thread open and the phone's Status screen. Pure: the
// headline, Your move with the one action each row offers, what moves now, and what happened since the owner last
// looked. home-ui.tsx draws it on both fronts.

import { boardItems, finishedSince, greenMerges, type GreenMerge } from "./board.ts";
import { needsGroup, needsGroupRank, rowMeta, rowState } from "./rows.ts";
import type { Project, Thread } from "./types.ts";

/** Your move shows this many rows; the rest are a count that opens the list. */
export const HOME_MOVES = 5;
/** Since you looked shows this many entries. */
export const HOME_LOG = 8;
/** Without a last look (a first visit, cleared storage), Since you looked covers this long. */
export const HOME_LOOK_DEFAULT_MS = 24 * 3600_000;
/** A last look older than this is cut to it, so a week away is not a wall of entries. */
export const HOME_LOOK_MAX_MS = 7 * 24 * 3600_000;

export interface HomeMove {
  thread: Thread;
  /** the bold word, as the inbox row says it */
  state: string;
  /** the line after it */
  meta: string;
  /** the button: Merge when the thread has one green PR to merge, else what the owner does in the thread */
  verb: string;
  /** set when the button merges here, without opening the thread */
  merge?: GreenMerge;
}

/** The threads whose move it is, in Your move's order: the group (Answer, Approve, Unblock, Questions in text), then
 *  the newest activity. Pinned ones included: a pin does not make a question go away. */
export function homeNeeds(threads: Thread[]): Thread[] {
  const seen = new Set<string>();
  const out: Thread[] = [];
  for (const t of threads) {
    if (t.attention.section !== "needs" || seen.has(t.id)) continue;
    seen.add(t.id);
    out.push(t);
  }
  const at = (t: Thread) => Date.parse(t.activityAt ?? t.updatedAt) || 0;
  return out.sort((a, b) => needsGroupRank(a) - needsGroupRank(b) || at(b) - at(a));
}

/** Your move's rows. A thread with exactly one green PR merges from here (the board's Merge, bound to the head shown);
 *  every other row opens the thread, where its card is. */
export function homeMoves(needs: Thread[], projects: Project[]): HomeMove[] {
  return needs.map((t) => {
    const g = needsGroup(t);
    const greens = greenMerges(boardItems({ [t.id]: t }, projects));
    const merge = g === "approve" && greens.length === 1 ? greens[0] : undefined;
    const verb = merge ? "Merge" : g === "answer" || g === "text" ? "Answer" : g === "approve" ? "Review" : "Open";
    return { thread: t, state: rowState(t), meta: rowMeta(t), verb, ...(merge ? { merge } : {}) };
  });
}

/** What moves without the owner: the working threads, running ones first, then the longest running. */
export function homeRunning(threads: Thread[]): Thread[] {
  const seen = new Set<string>();
  const out = threads.filter((t) => t.attention.section === "working" && !seen.has(t.id) && seen.add(t.id));
  const since = (t: Thread) => Date.parse(t.runStartedAt ?? t.updatedAt) || 0;
  return out.sort((a, b) => Number(b.running) - Number(a.running) || since(a) - since(b));
}

/** The line a running tile shows: the live activity, else why it waits (Waiting for CI), else its state. */
export function runningLine(t: Thread): string {
  return (t.running && t.activity) || rowState(t);
}

export type LogKind = "merged" | "done" | "settled" | "failed";

export interface LogEntry {
  at: string;
  kind: LogKind;
  /** the word before the text */
  word: string;
  text: string;
  thread: string;
}

const WORD: Record<LogKind, string> = { merged: "Merged", done: "Done", settled: "Settled", failed: "Failed" };

/** Since you looked: changes finished (merged or done), threads settled and threads that failed after `since`, newest
 *  first. A thread archived since is left out, as on the board. */
export function homeLog(threads: Record<string, Thread>, projects: Project[], since: number): LogEntry[] {
  const after = (iso: string | null | undefined) => !!iso && Date.parse(iso) > since;
  const out: LogEntry[] = [];
  for (const i of finishedSince(threads, projects, new Date(since).toISOString())) {
    const kind: LogKind = i.change.done!.reason === "merged" ? "merged" : "done";
    const pr = i.change.pr?.number ? `#${i.change.pr.number} ` : "";
    out.push({ at: i.change.done!.at, kind, word: WORD[kind], text: `${pr}${i.change.title}`, thread: i.thread.id });
  }
  for (const t of Object.values(threads)) {
    if (t.archivedAt) continue;
    if (after(t.settledAt)) out.push({ at: t.settledAt!, kind: "settled", word: WORD.settled, text: t.title, thread: t.id });
    if (t.attention.state === "failed" && t.failure && after(t.failure.at ?? t.updatedAt)) {
      out.push({ at: t.failure.at ?? t.updatedAt, kind: "failed", word: WORD.failed, text: `${t.title}${t.failure.message ? `: ${t.failure.message}` : ""}`, thread: t.id });
    }
  }
  return out.sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
}

/** The start of the window Since you looked covers: the last look, else a day; never more than a week. */
export function lookWindow(lastLook: number | null, now: number): number {
  if (lastLook === null || Number.isNaN(lastLook) || lastLook > now) return now - HOME_LOOK_DEFAULT_MS;
  return Math.max(lastLook, now - HOME_LOOK_MAX_MS);
}

/** The line under the clock: "3 running · 2 need you · 5 merged today", or the calm line when nothing goes on. */
export function homeSummary(o: { running: number; needs: number; mergedToday: number }): string {
  const parts: string[] = [];
  if (o.running) parts.push(`${o.running} running`);
  if (o.needs) parts.push(`${o.needs} need${o.needs === 1 ? "s" : ""} you`);
  if (!parts.length) parts.push("Nothing running · nothing needs you");
  if (o.mergedToday) parts.push(`${o.mergedToday} merged today`);
  return parts.join(" · ");
}

/** Midnight of `now`'s day, local time. */
export function startOfDay(now: number): number {
  const d = new Date(now);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}
