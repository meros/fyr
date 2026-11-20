// Pure state & labels for the Setup Wizard (decision 0037, #434).

export type SetupStatus = "ok" | "fixable" | "missing";

export interface SetupFix {
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
