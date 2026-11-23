// Pure state & labels for the Setup Wizard (decision 0037, #434).

type SetupStatus = "ok" | "fixable" | "missing";

interface SetupFix {
  id: string;
  label: string;
}

export interface SetupRepo {
  name: string;
  path: string;
  remote?: string;
  github?: boolean;
}

export interface GHAuthState {
  running: boolean;
  url?: string;
  code?: string;
  error?: string;
  done?: boolean;
}

export interface GHRepoItem {
  nameWithOwner: string;
  description?: string;
}

/** The receipt of the gh.login fix: the device-login state; null when the reply is not one. */
export function ghAuthOf(result: unknown): GHAuthState | null {
  const a = result as Partial<GHAuthState> | null;
  return a && typeof a === "object" && typeof a.running === "boolean" ? (a as GHAuthState) : null;
}

/** The receipt of the gh.repos fix: the repositories the account can see. */
export function ghReposOf(result: unknown): GHRepoItem[] {
  const r = (result as { repos?: unknown } | null)?.repos;
  return Array.isArray(r) ? (r as GHRepoItem[]) : [];
}

export interface SetupStep {
  id: string;
  title: string;
  status: SetupStatus;
  optional?: boolean;
  reason: string;
  fixes?: SetupFix[];
  detail?: Record<string, unknown>;
}

export interface SetupReport {
  os: string;
  host: string;
  done: number;
  total: number;
  ready: boolean;
  steps: SetupStep[];
  ghAuth?: GHAuthState;
}

/** One-line summary for Settings → Setup nav row. */
export function setupSummary(r: SetupReport | null): string {
  if (!r || !r.total) return "";
  if (r.done >= r.total) return "Ready";
  return `${r.done} of ${r.total}`;
}

/** Amber badge count on Settings → Setup: required steps that are not yet ok. */
export function setupBadge(r: SetupReport | null): number {
  if (!r) return 0;
  return r.steps.filter((s) => !s.optional && s.status !== "ok").length;
}

/** Whether the "Set up · X of 6" card sits atop the inbox (decision 0037). */
export function setupNeedsCard(r: SetupReport | null): boolean {
  if (!r || !r.total) return false;
  return !r.ready;
}

/** Label for the top-of-inbox setup card. */
export function setupCardLabel(r: SetupReport): string {
  return `Set up · ${r.done} of ${r.total}`;
}

/** Short human word for a step's status pill. */
export function stepPill(s: SetupStep): { text: string; cls: string } {
  if (s.status === "ok") return { text: "Ready", cls: "ok" };
  if (s.status === "fixable") return { text: "Fixable", cls: "fix" };
  return { text: s.optional ? "Optional" : "Missing", cls: s.optional ? "opt" : "miss" };
}
