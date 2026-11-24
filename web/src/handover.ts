// SPEC 0.5 V0.45, decision 0013; V0.trans-host-handover, decision 0039: hand a thread over to another profile or host.
// Pure: the dialog's words, the divider's words, target folder resolution, git limitations, and effective rung.
import type { HostId } from "./hostid.ts";
import type { Profile, Project, Thread } from "./types.ts";

type Rung = "resume" | "adopt" | "brief";

interface HandoverGit {
  repo?: string;
  branch?: string;
  default?: string;
  head?: string;
  ahead?: number;
  unpushed?: number;
  dirty?: number;
  worktree?: boolean;
}

export interface HandoverPlan {
  rung: Rung;
  note?: string;
  cli?: string;
  git?: HandoverGit;
}

export interface CrossHostHandoverArgs {
  toHost: HostId;
  profile: string;
  model?: string;
  effort?: string;
  cwd?: string;
  projectId?: string;
  useWorktree?: boolean;
}

interface TargetFolderSuggestion {
  cwd: string;
  projectId: string;
  reason: string;
}

/** What the dialog says before the owner confirms. */
export function rungText(r: string): string {
  switch (r) {
    case "resume":
      return "Full history: this conversation continues as it is";
    case "brief":
      return "The current agent writes a handover first";
    default:
      return "The new agent reads the old conversation";
  }
}

/** What the divider says afterwards. */
export function rungDone(r: string): string {
  switch (r) {
    case "resume":
      return "Full history";
    case "brief":
      return "The previous agent wrote a handover";
    default:
      return "The new agent read the old conversation";
  }
}

/** The profiles a thread can go to on its own host: every other one; V0.model-choice: one that cannot run is shown, not pickable. */
export function handoverTargets(t: Thread, profiles: Profile[]): Profile[] {
  return profiles.filter((p) => p.id !== t.profile);
}

/** Decision 0039: on the same host, every other provider; on another host, all providers on that target host. */
export function handoverTargetsForHost(t: Thread, targetHostId: HostId, profiles: Profile[]): Profile[] {
  return targetHostId === t.hostId ? handoverTargets(t, profiles) : profiles;
}

/** Adjust the source host's plan for a target host and provider (decision 0039). */
export function effectivePlan(sourcePlan: HandoverPlan, sourceHostId: HostId, targetHostId: HostId, targetProfile?: Profile): HandoverPlan {
  if (targetHostId === sourceHostId || sourcePlan.rung !== "resume") return sourcePlan;
  const srcCli = sourcePlan.cli || "claude";
  const dstCli = targetProfile?.cli || "claude";
  if (srcCli !== dstCli) {
    return { ...sourcePlan, rung: "adopt", note: "The target runs another CLI" };
  }
  if (srcCli !== "claude") {
    return { ...sourcePlan, rung: "adopt", note: "Session transfer is only supported for Claude sessions" };
  }
  return sourcePlan;
}

/** Decision 0039: pre-select the target folder from a linked project (0034) or a matching git origin repo on targetHostId. */
export function suggestTargetFolder(t: Pick<Thread, "projectId" | "hostId">, targetHostId: HostId, projects: readonly Project[], sourceGitRepo?: string): TargetFolderSuggestion {
  const all = flattenProjects(projects);
  const srcProj = t.projectId ? all.find((p) => p.id === t.projectId || p.rid === t.projectId) : undefined;
  const onTarget = all.filter((p) => p.hostId === targetHostId && !!p.folder);
  if (srcProj?.link) {
    const linked = onTarget.find((p) => p.link === srcProj.link);
    if (linked?.folder) {
      return { cwd: linked.folder, projectId: linked.rid ?? linked.id ?? "", reason: `Linked project “${linked.name}”` };
    }
  }
  const repo = sourceGitRepo || srcProj?.repo;
  if (repo) {
    const matched = onTarget.find((p) => p.repo === repo);
    if (matched?.folder) {
      return { cwd: matched.folder, projectId: matched.rid ?? matched.id ?? "", reason: `Matches repo ${repo}` };
    }
  }
  return { cwd: "", projectId: "", reason: "No matching folder on this host: pick one, or start in a fresh scratch folder." };
}

function flattenProjects(projects: readonly Project[]): Project[] {
  const out: Project[] = [];
  for (const p of projects) {
    if (p.members?.length) out.push(...p.members);
    else out.push(p);
  }
  return out;
}

/** Decision 0039: clear limitations text shown in the dialog when handing over to another host. */
export function gitLimitationNote(git: HandoverGit | undefined, fromHost: string, toHost: string): string {
  const from = fromHost || "the source host";
  const to = toHost || "the target host";
  if (git?.branch) {
    const stayed: string[] = [];
    if (git.dirty && git.dirty > 0) stayed.push(git.dirty === 1 ? "1 uncommitted file" : `${git.dirty} uncommitted files`);
    if (git.unpushed && git.unpushed > 0) stayed.push(git.unpushed === 1 ? "1 unpushed commit" : `${git.unpushed} unpushed commits`);
    if (stayed.length) {
      return `${stayed.join(" and ")} on ${from} (${git.branch}) will stay there: Fyr will not commit or push them. Local files and running processes do not move; the agent on ${to} will be told about the host move.`;
    }
    return `Branch ${git.branch} is clean and pushed. Local files outside git and running processes stay on ${from}; the agent on ${to} will be told about the host move.`;
  }
  return `Local files and running processes stay on ${from}; the agent on ${to} will be told about the host move.`;
}

/** The host's answer when it can be asked; a mock or an old host gets the likely rung. */
export function planGuess(t: Thread): HandoverPlan {
  return t.sessionId ? { rung: "resume" } : { rung: "adopt", note: "The thread has no session yet" };
}

export function parsePlan(x: unknown): HandoverPlan | null {
  if (!x || typeof x !== "object") return null;
  const obj = x as { rung?: unknown; note?: unknown; cli?: unknown; git?: unknown };
  const r = obj.rung;
  if (r !== "resume" && r !== "adopt" && r !== "brief") return null;
  const note = obj.note;
  const cli = obj.cli;
  const rawGit = obj.git && typeof obj.git === "object" ? (obj.git as Record<string, unknown>) : null;
  let git: HandoverGit | undefined;
  if (rawGit && typeof rawGit.branch === "string" && rawGit.branch) {
    git = {
      ...(typeof rawGit.repo === "string" && rawGit.repo ? { repo: rawGit.repo } : {}),
      branch: rawGit.branch,
      ...(typeof rawGit.default === "string" && rawGit.default ? { default: rawGit.default } : {}),
      ...(typeof rawGit.head === "string" && rawGit.head ? { head: rawGit.head } : {}),
      ...(typeof rawGit.ahead === "number" && rawGit.ahead > 0 ? { ahead: rawGit.ahead } : {}),
      ...(typeof rawGit.unpushed === "number" && rawGit.unpushed > 0 ? { unpushed: rawGit.unpushed } : {}),
      ...(typeof rawGit.dirty === "number" && rawGit.dirty > 0 ? { dirty: rawGit.dirty } : {}),
      ...(rawGit.worktree === true ? { worktree: true } : {}),
    };
  }
  return {
    rung: r,
    ...(typeof note === "string" && note ? { note } : {}),
    ...(typeof cli === "string" && cli ? { cli } : {}),
    ...(git ? { git } : {}),
  };
}

function profileName(id: string, profiles: Profile[]): string {
  return profiles.find((p) => p.id === id)?.name ?? id;
}

/** The divider in the conversation: "Handed over from Claude to Claude (acme) · 17:20", or with host names across hosts. */
export function dividerLine(d: { from?: string; to?: string; fromHost?: string; toHost?: string }, profiles: Profile[], clock: string): string {
  const from = profileName(d.from ?? "", profiles);
  const to = profileName(d.to ?? "", profiles);
  const suffix = clock ? ` · ${clock}` : "";
  if (d.fromHost && d.toHost && d.fromHost !== d.toHost) {
    return `Handed over from ${from} on ${d.fromHost} to ${to} on ${d.toHost}${suffix}`;
  }
  return `Handed over from ${from} to ${to}${suffix}`;
}
