// SPEC V0.126 (#151): the update record (GET /api/updates): every attempt to update Fyr on this host, why it ran,
// how it ended and the logs kept with its build; whether a local build pauses auto-update.

export interface UpdatePhase {
  name: string;
  at: string;
}

export interface UpdateRecord {
  build: string;
  commit?: string;
  trigger: string; // deployable, agent, owner
  local?: boolean;
  branch?: string;
  thread?: string;
  result: string; // inProgress, installed, failed, rolledBack
  reason?: string;
  startedAt: string;
  endedAt?: string;
  phases?: UpdatePhase[];
  logs?: string[];
}

export interface UpdatesView {
  updates: UpdateRecord[];
  paused?: { build: string; commit?: string; branch?: string; at: string } | null;
  bad?: { commit: string; why: string };
  live?: string;
  github?: GitHubView; // V0.173
}

const RESULT: Record<string, string> = { inProgress: "in progress", installed: "installed", failed: "failed", rolledBack: "rolled back" };
const TRIGGER: Record<string, string> = { deployable: "new release", agent: "an agent asked", owner: "you pressed Update now" };

function short(s: string | undefined): string {
  return s ? s.slice(0, 7) : "";
}

function took(a: string, b: string | undefined): string {
  const t = b ? Date.parse(b) - Date.parse(a) : NaN;
  if (Number.isNaN(t) || t < 0) return "";
  const s = Math.round(t / 1000);
  return s < 60 ? `${s} s` : `${Math.round(s / 60)} min`;
}

function when(iso: string, now: number): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const d = new Date(t);
  const sameDay = new Date(now).toDateString() === d.toDateString();
  const hm = `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  return sameDay ? hm : `${d.getDate()}/${d.getMonth() + 1} ${hm}`;
}

/** One record as a row: "Release 00702c2 · live" or "Local build of v0.124-key · failed", and what, when, how long. */
export function updateRow(u: UpdateRecord, now: number): { title: string; desc: string; fail: boolean } {
  const what = u.local ? `Local build${u.branch ? ` of ${u.branch}` : ""}${u.commit ? ` (${short(u.commit)})` : ""}` : `Release ${short(u.commit) || u.build}`;
  const result = RESULT[u.result] ?? u.result;
  const bits = [TRIGGER[u.trigger] ?? u.trigger, when(u.startedAt, now), took(u.startedAt, u.endedAt)].filter(Boolean);
  const fail = u.result === "failed" || u.result === "rolledBack";
  return { title: `${what} · ${result}`, desc: fail && u.reason ? `${bits.join(" · ")} · ${u.reason}` : bits.join(" · "), fail };
}

/** The line over the list: what runs, and whether auto-update waits for a local build. */
export function updateState(v: UpdatesView): string {
  if (v.paused) {
    return `A local build${v.paused.branch ? ` of ${v.paused.branch}` : ""} runs (${v.paused.build}). Auto-update waits until a release contains it, or until you go back to the release.`;
  }
  return v.live ? `Fyr runs ${v.live}.` : "";
}

// SPEC V0.173: what this host's fyrd spent of the account's GitHub API limit, and what the account has left (shared by
// every caller of the login: fyrd and agents on every host, and the owner's own gh).
export interface GitHubUse {
  kind: string;
  repo?: string;
  calls: number;
  free?: number;
}

export interface GitHubView {
  rate?: { limit: number; remaining: number; reset: string; graphqlRemaining: number; at: string } | null;
  since: string;
  uses: GitHubUse[] | null;
  calls: number;
}

function hhmm(iso: string): string {
  const d = new Date(iso);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

/** The line over the group: the account's REST calls left and when they reset; low under a tenth. */
export function githubState(v: GitHubView): { title: string; desc: string; low: boolean } {
  const desc = `This host's Fyr made ${v.calls} counted REST call${v.calls === 1 ? "" : "s"} since ${hhmm(v.since)}.`;
  const r = v.rate;
  if (!r || !r.limit) return { title: "Account limit not read yet", desc, low: false };
  return { title: `${r.remaining} of ${r.limit} REST calls left · resets ${hhmm(r.reset)}`, desc, low: r.remaining < r.limit / 10 };
}

/** One row per kind of call, most first, at most n. */
export function githubRows(v: GitHubView, n = 6): { title: string; desc: string }[] {
  return (v.uses ?? []).slice(0, n).map((u) => ({
    title: `${u.kind}${u.repo ? ` · ${u.repo}` : ""}`,
    desc: `${u.calls} call${u.calls === 1 ? "" : "s"}${u.free ? ` (${u.free} answered 304, free)` : ""}`,
  }));
}
