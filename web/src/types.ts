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

/** A subagent or background task of a thread (SPEC V0.16). */
export interface TaskDTO {
  id: string;
  description: string;
  type?: string;
  status: "running" | "completed" | "failed" | "stopped";
  activity?: string;
  summary?: string;
  toolUses?: number;
  tokens?: number;
  startedAt?: string;
  updatedAt?: string;
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
  /** the project this thread belongs to (V0.23); `project` stays the display name */
  projectId?: string;
  /** where the thread runs (V0.28) */
  workspaceId?: string;
  /** a project the thread could get, when it has none and its folder is a git clone (V0.28) */
  suggest?: ProjectSuggestion;
  /** what "New project from this thread" starts with (V0.30); absent: a name only */
  newProject?: ProjectSuggestion;
  /** the thread whose agent started this one (V0.22) */
  parent?: string | null;
  /** The checkout branch, read after each run; empty when not a git checkout. */
  branch?: string | null;
  tasks?: TaskDTO[] | null;
  /** V0.37: the agent keeps a live view */
  liveView?: boolean;
  /** V0.37: the live view; read it through liveViewOf (view.ts) */
  live?: { pageId?: string; state?: Record<string, unknown>; updatedAt?: string; checkedAt?: string; headline?: string } | null;
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

/** Where a project's work lives (V0.23). */
export type ProjectSource = { kind: "folder"; host: string; path: string; main?: boolean } | { kind: "repo"; url: string } | { kind: string; [k: string]: unknown };

/** A project made from a thread's workspace (V0.28): `project.createFrom {threadId}`. */
export interface ProjectSuggestion {
  name: string;
  sources: ProjectSource[];
}

export interface ProjectIconSpec {
  kind: "monogram" | "emoji" | "line" | string;
  /** Missing when empty: the host omits it. */
  value?: string;
}

/** A project: a host-independent identity with sources (V0.23). A project may have no source (a label). */
export interface Project {
  id?: string;
  name: string;
  /** a palette name: slate blue sky teal green olive violet indigo rose brown */
  color?: string;
  icon?: ProjectIconSpec;
  sources?: ProjectSource[];
  profile?: string;
  model?: string;
  threads?: number;
  lastUsed?: string;
  /** the project's main folder on this host, when it has one (V0.42) */
  cwd?: string;
  /** V0.42: the repos its folders' clones are matched by (from git origin, and repo sources made by hand); read-only */
  repos?: string[];
}

/** A folder in the host folder picker (V0.42). */
export interface FsDir {
  name: string;
  path: string;
  repo?: boolean;
}

/** GET /api/fs/list (V0.42): a folder and its subfolders; parent is "" at the home folder. */
export interface FsListing {
  path: string;
  parent: string;
  dirs: FsDir[];
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
  /** V0.39: where an update of Fyr is, while one runs or just ended */
  deploy?: import("./deploy.ts").DeployProgress | null;
}

export interface FyrEvent {
  seq: number;
  at: string;
  subject: string;
  type: string;
  data: any;
}

export interface CommandReply {
  ok: boolean;
  threadId?: string;
  error?: string;
  /** project.create: the new project's id; thread.send and thread.create: { messageId } (V0.33) */
  result?: { id?: string; messageId?: string; [k: string]: unknown };
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
