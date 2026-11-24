// The PR tab's data and pure logic (SPEC 0.5 V0.43): merge status words shared with the PR chip, check grouping,
// and the text of the "Ask the agent" messages.

import { duration, oneLine } from "./format.ts";
import type { PullRequest } from "./types.ts";

export interface PrCheck {
  name: string;
  workflow?: string;
  state: "fail" | "running" | "pass" | "skipped";
  status?: string;
  conclusion?: string;
  startedAt?: string;
  completedAt?: string;
  url?: string;
}

interface PrComment {
  author: string;
  body: string;
  at?: string;
  url?: string;
}

export interface PrThread {
  path: string;
  line?: number;
  outdated: boolean;
  url?: string;
  comments: PrComment[];
}

interface PrReview {
  author: string;
  state: string;
  at?: string;
}

/** GET /api/threads/{id}/pr (docs/API-v0.md). */
export interface PrDetail {
  url: string;
  repo: string;
  number: number;
  title: string;
  body?: string;
  state: string;
  draft?: boolean;
  author?: string;
  head?: string;
  base?: string;
  headSha?: string;
  additions?: number;
  deletions?: number;
  changedFiles?: number;
  labels?: { name: string; color?: string }[];
  mergeable?: string;
  mergeState?: string;
  reviewDecision?: string;
  reviews?: PrReview[];
  checks: PrCheck[];
  /** V0.112: the checks summed up on the host; pending also when no check has reported yet on a repo with CI */
  checked?: "pass" | "fail" | "pending" | "none";
  threads: PrThread[];
  threadsError?: string;
  fetchedAt?: string;
}

export type PrReply = PrDetail | { error: string };

type Tone = "ok" | "warn" | "fail" | "busy" | "muted";

export interface MergeStatus {
  text: string;
  tone: Tone;
  canMerge: boolean;
  /** why Merge is disabled; absent when it is not */
  reason?: string;
}

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

function blocked(text: string, tone: Tone = "fail"): MergeStatus {
  return { text, tone, canMerge: false, reason: text };
}

/** The merge status line of the tab, important first (V0.43). The host's own refusal (MergeBlock) uses the same words. */
export function mergeStatus(d: PrDetail): MergeStatus {
  if (d.state === "MERGED") return { text: "Merged", tone: "ok", canMerge: false, reason: "Already merged" };
  if (d.state === "CLOSED") return { text: "Closed", tone: "muted", canMerge: false, reason: "The PR is closed" };
  const base = d.base || "the base branch";
  if (d.draft || d.mergeState === "DRAFT") return blocked("Draft: not ready for review", "muted");
  if (d.mergeable === "CONFLICTING" || d.mergeState === "DIRTY") return blocked(`Conflicts with ${base}`);
  const g = groupChecks(d.checks ?? []);
  const fails = g.failed.length;
  if (fails && d.mergeState !== "UNSTABLE") return blocked(`Blocked: ${plural(fails, "failing check")}`);
  if (d.reviewDecision === "CHANGES_REQUESTED") return blocked("Changes requested");
  if (d.reviewDecision === "REVIEW_REQUIRED") return blocked("Review required", "warn");
  if (d.mergeState === "BEHIND") return blocked(`Behind ${base}`, "warn");
  // V0.112: a merge waits for CI; no check reported on a repo with CI, or a check running, is not green.
  if (g.running.length) return blocked(`Checks running: ${g.running.length}`, "busy");
  if (!d.checks?.length && d.checked === "pending") return blocked("No check has reported yet", "busy");
  if (d.mergeState === "BLOCKED") return blocked("Blocked by branch rules");
  if (fails) return { text: `Ready to merge · ${plural(fails, "failing check")} ${fails === 1 ? "is" : "are"} not required`, tone: "warn", canMerge: true };
  return { text: "Ready to merge", tone: "ok", canMerge: true };
}

/** The same words from the poller's snapshot in the thread (no counts, no merge state). */
export function chipStatus(pr: PullRequest): MergeStatus {
  if (pr.state === "MERGED") return { text: "Merged", tone: "ok", canMerge: false };
  if (pr.state === "CLOSED") return { text: "Closed", tone: "muted", canMerge: false };
  if (pr.state === "MISSING") return { text: "Does not exist on GitHub", tone: "muted", canMerge: false };
  if (pr.conflict) return blocked("Conflicts with the base branch");
  if (pr.checks === "fail") return blocked("Checks failing");
  if (pr.review === "CHANGES_REQUESTED") return blocked("Changes requested");
  if (pr.review === "REVIEW_REQUIRED") return blocked("Review required", "warn");
  if (pr.checks === "pending") return blocked("Checks running", "busy"); // V0.112
  return { text: "Ready to merge", tone: "ok", canMerge: true };
}

/** The PR chip on inbox rows and in the thread header: a short label, the status line as its title. */
export function prChip(pr: PullRequest, short = false): { text: string; cls: string; title: string } {
  const s = chipStatus(pr);
  const n = `#${pr.number}`;
  const word =
    pr.state === "MERGED" ? "merged" : pr.state === "CLOSED" ? "closed" : pr.state === "MISSING" ? "missing" : pr.conflict ? "conflicts" : pr.checks === "fail" ? "failing" : pr.review === "CHANGES_REQUESTED" || pr.review === "REVIEW_REQUIRED" ? "review" : pr.checks === "pending" ? "running" : "ready";
  const cls = word === "merged" || word === "closed" || word === "missing" ? "merged" : s.tone === "fail" ? "fail" : s.tone === "ok" ? "ok" : "";
  const text = short && (word === "merged" || word === "closed" || word === "missing") ? word : `${n} ${word}`;
  return { text, cls, title: [n, s.text, pr.title].filter(Boolean).join(" · ") };
}

/** V0.multi-pr: urgency order for picking the inbox row's headline PR: failing > conflicts > changes requested >
 *  review required > ready > running > merged/closed/missing. */
export function prUrgency(pr: PullRequest): number {
  const st = pr.state ?? "OPEN";
  if (st !== "OPEN") return 6;
  if (pr.checks === "fail") return 0;
  if (pr.conflict) return 1;
  if (pr.review === "CHANGES_REQUESTED") return 2;
  if (pr.review === "REVIEW_REQUIRED") return 3;
  if (pr.checks === "pending") return 5;
  return 4;
}

/** V0.multi-pr: one inbox-row chip for one or more PRs: the most urgent PR, with "+N" when more exist, and every
 *  PR's status line in the tooltip. */
export function rowPrChip(prs: PullRequest[], short = false): { text: string; cls: string; title: string } | null {
  if (!prs.length) return null;
  const sorted = [...prs].sort((a, b) => prUrgency(a) - prUrgency(b));
  const top = prChip(sorted[0], short);
  if (sorted.length === 1) return top;
  return {
    text: `${top.text} +${sorted.length - 1}`,
    cls: top.cls,
    title: sorted.map((p) => prChip(p).title).join("\n"),
  };
}

/** Short repository name ("acme/api" → "api"), shown on multi-repo PR chips and switcher pills. */
export function shortRepo(repo?: string): string {
  if (!repo) return "";
  const i = repo.lastIndexOf("/");
  return i >= 0 ? repo.slice(i + 1) : repo;
}

/** True when prs span more than one repository. */
export function hasMultipleRepos(prs: PullRequest[]): boolean {
  const repos = new Set(prs.map((p) => (p.repo ?? "").toLowerCase()).filter(Boolean));
  return repos.size > 1;
}

/** V0.multi-pr: picks which PR the PR tab displays: an explicit selection if still present, else preferred if open,
 *  else the first PR in list order (newest open, else newest non-dismissed). */
export function pickPr(prs: PullRequest[], selected?: string, preferred?: string): PullRequest | null {
  if (!prs.length) return null;
  const key = (u: string) => (/^(.*?\/pull\/\d+)/i.exec(u)?.[1] ?? u).toLowerCase();
  if (selected) {
    const k = key(selected);
    const hit = prs.find((p) => key(p.url) === k);
    if (hit) return hit;
  }
  if (preferred) {
    const k = key(preferred);
    const hit = prs.find((p) => key(p.url) === k && (p.state ?? "OPEN") === "OPEN");
    if (hit) return hit;
  }
  return prs[0];
}

interface CheckGroups {
  failed: PrCheck[];
  running: PrCheck[];
  passed: PrCheck[];
  skipped: PrCheck[];
}

const byName = (a: PrCheck, b: PrCheck) => a.name.localeCompare(b.name) || (a.workflow ?? "").localeCompare(b.workflow ?? "");

/** Failed first, then running; passed and skipped fold into one line. */
export function groupChecks(checks: PrCheck[]): CheckGroups {
  const g: CheckGroups = { failed: [], running: [], passed: [], skipped: [] };
  for (const c of checks) {
    if (c.state === "fail") g.failed.push(c);
    else if (c.state === "running") g.running.push(c);
    else if (c.state === "skipped") g.skipped.push(c);
    else g.passed.push(c);
  }
  g.failed.sort(byName);
  g.running.sort(byName);
  g.passed.sort(byName);
  g.skipped.sort(byName);
  return g;
}

/** V0.70 (3): on a merged or closed PR the checks are history: muted, under "Checks at merge", with no ask. */
export function checksSection(state: string | undefined): { heading: string; history: boolean } {
  const history = (state ?? "OPEN") !== "OPEN";
  return { heading: history ? "Checks at merge" : "Checks", history };
}

export function passedLabel(g: CheckGroups): string {
  const parts = [];
  if (g.passed.length) parts.push(`${g.passed.length} passed`);
  if (g.skipped.length) parts.push(`${g.skipped.length} skipped`);
  return parts.join(", ");
}

export function checkDuration(c: PrCheck, now: number): string {
  const s = c.startedAt ? Date.parse(c.startedAt) : NaN;
  if (Number.isNaN(s)) return "";
  if (c.state === "running") return `running ${duration(Math.max(0, now - s))}`;
  const e = c.completedAt ? Date.parse(c.completedAt) : NaN;
  return Number.isNaN(e) ? "" : duration(Math.max(0, e - s));
}

/** "Ask the agent to fix this check". */
export function fixCheckMessage(c: PrCheck): string {
  return `The check ${c.name} failed${c.url ? `: ${c.url}` : ""}. Find the cause and fix it.`;
}

const COMMENT_MAX = 300;

function threadPlace(t: PrThread): string {
  return `${t.path}${t.line ? `:${t.line}` : ""}`;
}

/** "Ask the agent to address N review comments": each thread as path:line and its first comment. */
export function reviewMessage(threads: PrThread[]): string {
  const lines = threads.map((t, i) => {
    const c = t.comments[0];
    return `${i + 1}. ${threadPlace(t)}${t.outdated ? ", outdated" : ""}${c ? ` (${c.author}): ${oneLine(c.body, COMMENT_MAX)}` : ""}`;
  });
  return [`Address these ${plural(threads.length, "unresolved review comment")}:`, "", ...lines, "", "Fix what they ask for, or tell me why not."].join("\n");
}

export function reviewAskLabel(n: number): string {
  return `Ask the agent to address ${plural(n, "review comment")}`;
}

export function updatedLabel(iso: string | undefined, now: number): string {
  if (!iso) return "";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const s = Math.max(0, Math.floor((now - t) / 1000));
  if (s < 60) return `updated ${s} s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `updated ${m} min ago`;
  return `updated ${Math.floor(m / 60)} h ago`;
}

export const MERGE_METHODS = [
  { id: "squash", label: "Squash and merge", hint: "One commit on the base branch" },
  { id: "merge", label: "Merge commit", hint: "All commits, plus a merge commit" },
  { id: "rebase", label: "Rebase and merge", hint: "All commits, rebased onto the base branch" },
] as const;

export type MergeMethod = (typeof MERGE_METHODS)[number]["id"];

/** V0.dialogs-review (27): the method each repository was merged with last on this device, so the Merge dialog starts
 *  on it instead of always on Squash. */
export const MERGE_METHOD_KEY = "fyr.mergeMethod";

function mergeMethods(kv: Pick<Storage, "getItem"> | null): Record<string, unknown> {
  try {
    const j: unknown = JSON.parse(kv?.getItem(MERGE_METHOD_KEY) ?? "{}");
    return j && typeof j === "object" && !Array.isArray(j) ? (j as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

/** The method to start on for `repo`: the one used last there, else squash. */
export function rememberedMergeMethod(kv: Pick<Storage, "getItem"> | null, repo: string): MergeMethod {
  const m = mergeMethods(kv)[repo];
  return MERGE_METHODS.find((x) => x.id === m)?.id ?? "squash";
}

export function rememberMergeMethod(kv: Pick<Storage, "getItem" | "setItem"> | null, repo: string, method: MergeMethod): void {
  if (!repo) return;
  try {
    kv?.setItem(MERGE_METHOD_KEY, JSON.stringify({ ...mergeMethods(kv), [repo]: method }));
  } catch {
    /* storage blocked: the dialog starts on squash again */
  }
}

/** The dialog's confirm button names the result. */
/** V0.97: the short form of a head commit, as the owner sees it on a Merge button. */
export const shortHead = (sha?: string) => (sha ?? "").slice(0, 7);

/** V0.97: a Merge button names the head it merges ("Merge a1b2c3d"). */
export function headLabel(label: string, head?: string): string {
  return head ? `${label} ${shortHead(head)}` : label;
}

export function mergeButton(method: MergeMethod, n: number): string {
  return method === "squash" ? `Squash and merge #${n}` : method === "rebase" ? `Rebase and merge #${n}` : `Merge #${n}`;
}

/** V0.dialogs-review: one merge, as the Merge dialog asks about it, wherever it starts (the PR tab, a change's card,
 *  the agent's merge ask). */
export interface MergeAsk {
  /** what is merged: the PR's number, title and repository (the method remembered per repository) */
  number?: number;
  title?: string;
  repo?: string;
  /** the branches, when known */
  head?: string;
  base?: string;
  /** V0.97: the commit the merge is bound to */
  sha?: string;
  /** V0.sensitive-pr: why this merge needs a deliberate press; the dialog is then red and starts on Cancel */
  sensitive?: string;
  /** The host takes the method on this path (the PR tab's `pr.merge`): the dialog offers the choice. Without it the
   *  merge runs with the method its change or ask names (`action.run` and `thread.answer` take none), and the dialog
   *  says so instead of offering a choice it could not keep. */
  choose?: boolean;
  /** Start the merge; resolves to the host's error, or null when it started. */
  run(method: MergeMethod | undefined): Promise<string | null>;
}

/** The Merge dialog's title: it names what is merged. */
export function mergeTitle(a: Pick<MergeAsk, "number" | "sensitive">): string {
  const what = a.number ? `#${a.number}` : "this PR";
  return a.sensitive ? `Merge ${what}, a sensitive PR` : `Merge ${what}`;
}

/** The Merge dialog's primary button: the method when one is chosen there, else what it merges. */
export function mergeConfirm(a: Pick<MergeAsk, "number" | "choose" | "sha">, method: MergeMethod): string {
  if (a.choose && a.number) return mergeButton(method, a.number);
  return a.number ? `Merge #${a.number}` : headLabel("Merge", a.sha);
}

/** What the dialog says in place of the method choice when the host takes none on this path. */
export const MERGE_OWN_METHOD = "With the method this change asks for (squash unless the agent named another). The PR tab lets you pick one.";

/** The host's refusal, as the dialog prints it on a line of its own. */
export const notMerged = (err: string) => `Not merged: ${err}`;

/** The threads grouped per file, in the order they came. */
export function threadsByFile(threads: PrThread[]): { path: string; threads: PrThread[] }[] {
  const m = new Map<string, PrThread[]>();
  for (const t of threads) {
    const l = m.get(t.path);
    if (l) l.push(t);
    else m.set(t.path, [t]);
  }
  return [...m].map(([path, threads]) => ({ path, threads }));
}

export function reviewWord(state: string): string {
  switch (state) {
    case "APPROVED":
      return "approved";
    case "CHANGES_REQUESTED":
      return "requested changes";
    case "COMMENTED":
      return "commented";
    case "DISMISSED":
      return "dismissed";
    case "PENDING":
      return "review pending";
    default:
      return state.toLowerCase().replace(/_/g, " ");
  }
}
