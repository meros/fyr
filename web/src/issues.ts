// V0.thread-issues: the GitHub issues a thread links, as chips in its header beside its PRs (desktop and phone).
import type { Issue, PullRequest } from "./types.ts";
import { shortRepo } from "./pr.ts";

/** The linked issues as the header shows them: open (or not read yet) first, then closed, each group oldest first. */
export function threadIssues(t: { issues?: Issue[] | null }): Issue[] {
  const all = [...(t.issues ?? [])];
  const closed = (i: Issue) => (i.state === "CLOSED" ? 1 : 0);
  return all.sort((a, b) => closed(a) - closed(b) || a.at.localeCompare(b.at));
}

const VIA: Record<Issue["via"], string> = {
  pr: "a PR of this thread closes it",
  created: "the agent filed it",
  owner: "you gave it",
  agent: "the agent linked it",
};

/** One issue's chip: "issue #12", "issue #12 closed"; with prefix the repo's short name goes first. */
export function issueChip(i: Issue, prefix = ""): { text: string; cls: string; title: string } {
  const closed = i.state === "CLOSED";
  const text = `${prefix ? `${prefix} ` : ""}issue #${i.number}${closed ? " closed" : ""}`;
  const state = closed ? "closed" : i.state === "OPEN" ? "open" : "";
  const title = [`${i.repo}#${i.number}`, state, i.title, VIA[i.via]].filter(Boolean).join(" · ");
  return { text, cls: closed ? "merged" : "", title };
}

/** True when the header's PRs and issues span more than one repository, so each chip names its repo. */
export function issuesNeedRepo(issues: Issue[], prs: PullRequest[]): boolean {
  const repos = new Set([...issues.map((i) => i.repo), ...prs.map((p) => p.repo ?? "")].map((r) => r.toLowerCase()).filter(Boolean));
  return repos.size > 1;
}

/** The repo prefix of an issue chip, when the header needs one. */
export function issuePrefix(i: Issue, need: boolean): string {
  return need ? shortRepo(i.repo) : "";
}
