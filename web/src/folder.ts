// V0.folder-chip (#26): the folder a thread works in, as the header's folder chip and its card show it. Pure: every git
// fact (the branch, a worktree Fyr made, its repo) is the host's; this only picks and words them.

import type { Card, CardRow } from "./card.ts";
import { middleTruncate } from "./format.ts";
import { ended, isDone } from "./offers.ts";
import { baseName } from "./paths.ts";
import type { Change, Thread } from "./types.ts";

export interface FolderView {
  /** the chip's first part: the folder's last path part, or the repo's name for a worktree Fyr made */
  name: string;
  /** the chip's second part: the checkout's branch; absent for a folder that is not a git checkout */
  branch?: string;
  /** the full path, what Copy path copies */
  path: string;
  /** a worktree Fyr made for a change (V0.change-worktree): the repo it belongs to and the change */
  worktree?: { repo: string; change: string };
  /** the thread's own folder, when the chip shows a worktree elsewhere */
  started?: string;
}

const NAME_MAX = 28;
const BRANCH_MAX = 32;

/** A repo's name: "github.com/acme/web" → "web"; a root folder → its last part. */
function repoName(repo: string): string {
  return baseName(repo.replace(/\/+$/, "").replace(/\.git$/, "")) || repo;
}

/** The newest open code change that works in a worktree Fyr made. */
function fyrWorktree(changes: Change[] | undefined): Change | undefined {
  const at = (c: Change) => Date.parse(c.touchedAt ?? c.updatedAt) || 0;
  return (changes ?? []).filter((c) => c.workspace?.madeByFyr && c.workspace.path && !ended(c.phase) && !isDone(c)).sort((a, b) => at(b) - at(a))[0];
}

/** Where the thread works: the worktree Fyr made for its open change, else its folder. A folder that is not a git
 *  checkout (a scratch folder) has no branch. */
export function threadFolder(t: Pick<Thread, "cwd"> & Partial<Pick<Thread, "branch" | "changes">>): FolderView {
  const c = fyrWorktree(t.changes);
  if (c) {
    const ws = c.workspace!;
    const path = ws.path!;
    const repo = ws.repo ? repoName(ws.repo) : baseName(path);
    const branch = ws.branch || c.branch;
    return { name: repo, branch: branch || undefined, path, worktree: { repo: ws.repo || repo, change: c.title }, started: t.cwd && t.cwd !== path ? t.cwd : undefined };
  }
  const path = t.cwd || "";
  return { name: baseName(path.replace(/\/+$/, "")) || path || "No folder", branch: t.branch || undefined, path };
}

/** The chip's two parts, each cut in the middle when long; branch is "" without git. */
export function folderChipText(v: FolderView): { name: string; branch: string } {
  return { name: middleTruncate(v.name, NAME_MAX), branch: v.branch ? middleTruncate(v.branch, BRANCH_MAX) : "" };
}

/** The chip's words for a screen reader and its title. */
export function folderLabel(v: FolderView): string {
  return v.branch ? `Folder ${v.name}, branch ${v.branch}` : `Folder ${v.name}`;
}

/** The card a click on the chip opens: the full path, the branch when git, the worktree's repo and change, the
 *  thread's own folder when it differs, and the host. */
export function folderCard(v: FolderView, host?: string): Card {
  const rows: CardRow[] = [{ label: "Path", value: v.path }];
  if (v.branch) rows.push({ label: "Branch", value: v.branch });
  if (v.worktree) rows.push({ label: "Worktree", value: v.worktree.repo, note: `Fyr made it for “${v.worktree.change}”` });
  if (v.started) rows.push({ label: "Thread", value: v.started, note: "the thread's own folder" });
  if (host) rows.push({ label: "Host", value: host });
  return { head: v.name, headMono: true, sections: [{ rows }] };
}
