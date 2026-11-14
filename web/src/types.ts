// DTOs of the v0 HTTP API (docs/API-v0.md). The client renders these; it never computes status.

export type AttentionState =
  | "asking"
  | "failed"
  | "blocked"
  | "done"
  | "working"
  | "waiting"
  | "seen"
  | "snoozed"
  | "settled"
  | "archived";

export type Section = "needs" | "working" | "seen" | "snoozed" | "settled" | "archived";

export interface Attention {
  state: AttentionState;
  section: Section;
  reason?: string;
}

export interface Failure {
  class: string;
  message: string;
  at?: string;
}

export interface QuestionOption {
  label: string;
  description?: string;
}

export interface Question {
  question: string;
  header?: string;
  multiSelect?: boolean;
  options: QuestionOption[];
}

export interface Effect {
  action: string;
  args?: Record<string, unknown>;
  reason?: string;
}

export interface Decision {
  id: string;
  kind: "ask" | "approval" | "effect";
  createdAt?: string;
  agent?: string;
  questions?: Question[];
  tool?: string;
  input?: Record<string, unknown>;
  effect?: Effect;
}

export interface PullRequest {
  url: string;
  repo?: string;
  number: number;
  title?: string;
  state?: string; // OPEN | MERGED | CLOSED
  checks?: "pass" | "fail" | "pending" | "none" | string;
  review?: string;
  conflict?: boolean;
}

export interface ThreadDTO {
  /** Increases on every change, also across host restarts: keep the higher (API v0). */
  rev?: number;
  id: string;
  title: string;
  cwd: string;
  project: string;
  profile: string;
  model: string;
  sessionId?: string;
  createdAt: string;
  updatedAt: string;
  running: boolean;
  runStartedAt?: string | null;
  activity?: string;
  subagents?: number;
  activityAt?: string | null;
  summary?: string;
  failure?: Failure | null;
  decisions?: Decision[] | null;
  pr?: PullRequest | null;
  settledAt?: string | null;
  archivedAt?: string | null;
  settleWhenDone?: boolean;
  settleWhenMerged?: boolean;
  source?: "fyr" | "t3" | string;
  /** The checkout branch, read after each run; empty when not a git checkout. */
  branch?: string | null;
  attention: Attention;
  view?: { seenAt?: string | null; snoozeUntil?: string | null } | null;
  unavailable?: Record<string, string> | null;
}

export interface ActionInfo {
  name: string;
  label: string;
  key?: string;
  bulk?: boolean;
}

export interface Profile {
  id: string;
  name: string;
  color: string;
  models: string[];
  defaultModel?: string;
}

export interface Project {
  cwd: string;
  name: string;
  lastUsed?: string;
}

export interface StateDTO {
  now: string;
  /** SPEC V0.13: a different buildId after a reconnect reloads the page */
  buildId?: string;
  /** The host name for the rail's bottom indicator. */
  host?: string;
  user: string;
  threads: ThreadDTO[];
  actions: ActionInfo[];
  profiles: Profile[];
  projects: Project[];
}

export interface FyrEvent {
  seq: number;
  at: string;
  subject: string;
  type: string;
  data: any;
}

export interface ImageArg {
  mediaType: string;
  data: string; // base64, no data: prefix
}

export interface CommandReply {
  ok: boolean;
  threadId?: string;
  error?: string;
}

export interface T3Candidate {
  t3Id: string;
  title: string;
  cwd: string;
  profile?: string;
  model?: string;
  sessionId?: string;
  prUrl?: string;
  updatedAt?: string;
  hasTranscript?: boolean;
}
