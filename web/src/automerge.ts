// SPEC V0.102: auto-merge per repo. The owner's setting (Settings → Auto-merge) and the line a change shows when
// Fyr approved its merge by that setting, with no press.

import type { Change } from "./types.ts";

/** One repo as the host lists it (GET /api/autoMerge): owner/name in lower case. */
/** V0.110: the owner's auto-deploy setting (GET /api/autoDeploy). */
export interface AutoDeploySetting {
  on: boolean;
}

export interface AutoMergeRepo {
  repo: string;
  on: boolean;
}

/** owner/name, the only form the host accepts; "" otherwise. */
export function repoKey(s: string): string {
  const t = s.trim();
  return /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(t) ? t.toLowerCase() : "";
}

/** "Merge auto-approved by the repo setting (acme/app, a1b2c3d)", V0.110 "Deploy auto-approved by the setting (a1b2c3d)",
 *  or null when the owner pressed it or nothing ran. */
export function autoMergeLine(c: Pick<Change, "autoApproved">): string | null {
  const a = c.autoApproved;
  if (!a) return null;
  if (a.deploy) return `Deploy auto-approved by the setting (${a.head.slice(0, 7)})`;
  return `Merge auto-approved by the repo setting (${a.repo}, ${a.head.slice(0, 7)})`;
}

/** The repos with the opted-in ones first, then by name. */
export function sortRepos(rs: AutoMergeRepo[]): AutoMergeRepo[] {
  return [...rs].sort((a, b) => (a.on !== b.on ? (a.on ? -1 : 1) : a.repo < b.repo ? -1 : a.repo > b.repo ? 1 : 0));
}
