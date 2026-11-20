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
  // V0.update-changelog: the commits this update brings over what ran before it (from), newest first, and how many
  // there are in all (the record keeps the newest few).
  from?: string;
  changes?: UpdateChange[];
  changesAll?: number;
}

export interface UpdateChange {
  commit: string;
  title: string;
}

export interface UpdatesView {
  updates: UpdateRecord[];
  paused?: { build: string; commit?: string; branch?: string; at: string } | null;
  bad?: { commit: string; why: string };
  live?: string;
  github?: GitHubView; // V0.173
  repo?: string; // V0.update-changelog: owner/name the releases come from, for the changelog's links
  key?: ReleaseKey; // V0.release-without-gh: the key releases are verified with
  version?: string; // V0.versioned-releases: the running build's vX.Y.Z; empty on main
  channel?: string; // V0.versioned-releases: main, stable or a pinned vX.Y.Z; missing from an older host
}

/** V0.versioned-releases: the channel in words: "stable channel", "main channel", "pinned to v1.2.3". */
export function channelLabel(channel: string | undefined): string {
  if (!channel) return "";
  return channel === "main" || channel === "stable" ? `${channel} channel` : `pinned to ${channel}`;
}

/** V0.versioned-releases: what a host runs, short: its version, else its build on main, with the channel:
 *  "v1.0.0 · stable channel", "c705ef536566 · main channel". Empty when the host says neither. */
export function versionLabel(v: { version?: string; build?: string; channel?: string }): string {
  const what = v.version || v.build || "";
  const ch = channelLabel(v.channel);
  return [what, ch].filter(Boolean).join(" · ");
}

/** V0.release-without-gh: the key a host verifies releases with (config updateKey, else the built-in one). */
export interface ReleaseKey {
  fingerprint?: string;
  builtIn?: boolean;
  error?: string;
}

/** V0.release-without-gh: the Release source row of Settings → Updates: the repo and the key's fingerprint. */
export function releaseSource(v: UpdatesView): { title: string; desc: string; bad: boolean } | null {
  if (!v.repo && !v.key) return null;
  const k = v.key;
  const desc = !k ? "" : k.error ? k.error : k.fingerprint ? `signed with ${k.fingerprint} (${k.builtIn ? "built in" : "updateKey"})` : "";
  return { title: v.repo || "meros/fyr", desc, bad: !!k?.error };
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

/** V0.update-changelog: the toggle of a record's changelog, "3 changes", or "" when the record has none. */
export function changesLabel(u: UpdateRecord): string {
  const n = Math.max(u.changesAll ?? 0, u.changes?.length ?? 0);
  return n ? `${n} change${n === 1 ? "" : "s"}` : "";
}

/** V0.update-changelog: a record's changelog as lines: the commit's title without its "(#N)", the PR it landed in, and
 *  links to both on GitHub; the last line says how many older ones the record does not keep. */
export function changeLines(u: UpdateRecord, repo = "meros/fyr"): { title: string; url: string; pr?: number; prUrl?: string; sha: string }[] {
  const base = `https://github.com/${repo}`;
  const out: { title: string; url: string; pr?: number; prUrl?: string; sha: string }[] = (u.changes ?? []).map((c) => {
    const m = /\s*\(#(\d+)\)\s*$/.exec(c.title);
    const pr = m ? Number(m[1]) : undefined;
    return { title: m ? c.title.slice(0, m.index) : c.title, url: `${base}/commit/${c.commit}`, sha: short(c.commit), ...(pr ? { pr, prUrl: `${base}/pull/${pr}` } : {}) };
  });
  const older = (u.changesAll ?? 0) - out.length;
  if (older > 0 && u.from && u.commit) out.push({ title: `and ${older} older`, url: `${base}/compare/${u.from}...${u.commit}`, sha: "" });
  return out;
}

/** The line over the list: what runs, and whether auto-update waits for a local build. */
export function updateState(v: UpdatesView): string {
  if (v.paused) {
    return `A local build${v.paused.branch ? ` of ${v.paused.branch}` : ""} runs (${v.paused.build}). Auto-update waits until a release contains it, or until you go back to the release.`;
  }
  if (!v.live) return "";
  // V0.versioned-releases: the version and the channel, when the host says them.
  const runs = v.version ? `${v.version} (build ${v.live})` : v.live;
  const ch = channelLabel(v.channel);
  return `Fyr runs ${runs}${ch ? ` · ${ch}` : ""}.`;
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
  /** V0.178: the budget holds this host's polls back until `until`. */
  paused?: { resource: string; until: string; remaining: number; why: string; deferred: number } | null;
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

/** V0.178: the row that says this host's polls wait; null while they run. */
export function githubPause(v: GitHubView): { title: string; desc: string } | null {
  const p = v.paused;
  if (!p) return null;
  const what = p.resource === "graphql" ? "PR reads" : "Polls";
  return { title: `${what} wait until ${hhmm(p.until)}`, desc: `${p.why[0].toUpperCase()}${p.why.slice(1)}. Merges and updates still run.` };
}

/** V0.versioned-releases: the status screen's version line, one per host that says its update state: "Fyr v1.0.0 ·
 *  stable channel" for one host, each host's name for several (as the strip names hosts only when there are several). */
export function hostVersions(hosts: { name: string; update?: { version?: string; build?: string; channel?: string } }[]): string[] {
  const with_ = hosts.filter((h) => h.update && versionLabel(h.update));
  return with_.map((h) => `${with_.length > 1 ? h.name : "Fyr"} ${versionLabel(h.update!)}`);
}
