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
  state?: string; // OPEN | MERGED | CLOSED | MISSING (V0.72: GitHub says it does not exist)
  checks?: "pass" | "fail" | "pending" | "none" | string;
  review?: string;
  conflict?: boolean;
  /** V0.97: the head commit Fyr last read, and what the Ready rule reads */
  head?: string;
  draft?: boolean;
  mergeState?: string;
  autoMerge?: boolean;
  /** V0.101: when Fyr last read the PR, for "checked N s ago" */
  checkedAt?: string;
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
  /** V0.51: the name of the host that runs the thread */
  host?: string;
  /** V0.51: the host is unreachable since then (RFC 3339): the front shows its cached copy, read-only */
  hostOffline?: string;
  /** V0.149 (4): why a root run request cannot ask for the password on the thread's host; absent when it can */
  rootPrompt?: string;
  id: string;
  title: string;
  cwd: string;
  project: string;
  profile: string;
  model: string;
  /** V0.50: "" or missing is the provider's default */
  effort?: string;
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
  /** V0.59: the thread's change records */
  changes?: Change[];
  /** V0.60, #31: the thread's own actions and their last runs; a change and the live view hold their own */
  offers?: Offer[];
  runs?: OfferRun[];
  settledAt?: string | null;
  /** V0.104: when the owner pinned the thread; a pinned thread sorts above every other */
  pinnedAt?: string | null;
  archivedAt?: string | null;
  settleWhenDone?: boolean;
  settleWhenMerged?: boolean;
  /** V0.59 (7): settles when the change (an id or a key) reaches the phase */
  settleWhen?: { change: string; phase: string };
  source?: "fyr" | "t3" | string;
  /** the project this thread belongs to (V0.23); `project` stays the display name */
  projectId?: string;
  /** what "New project from this thread" starts with (V0.30, V0.55): the thread's folder; absent: a name only */
  newProject?: NewProjectDraft;
  /** V0.46: the end of the quiet hours of the thread's project, while they hold (RFC 3339) */
  quietUntil?: string;
  /** the thread whose agent started this one (V0.22) */
  parent?: string | null;
  /** The checkout branch, read after each run; empty when not a git checkout. */
  branch?: string | null;
  tasks?: TaskDTO[] | null;
  /** V0.37: the agent keeps a live view */
  liveView?: boolean;
  /** V0.45: a handover to another profile is going on */
  handover?: { to: string; model?: string; at: string } | null;
  /** V0.37: the live view; read it through liveViewOf (view.ts) */
  live?: { pageId?: string; state?: Record<string, unknown>; updatedAt?: string; checkedAt?: string; headline?: string; offers?: Offer[]; runs?: OfferRun[] } | null;
  /** V0.33: messages the host still holds (queued or sent, not yet read) */
  outbox?: { id: string; state: string }[] | null;
  /** V0.143: the live agent process takes nothing Fyr writes; since its last output, and why */
  unresponsive?: { since: string; at: string; reason: string } | null;
  /** V0.153: Fyr waits for a thrashing host instead of killing a slow agent */
  machineWait?: { since: string; why: string } | null;
  attention: Attention;
  view?: { seenAt?: string | null; snoozeUntil?: string | null } | null;
  unavailable?: Record<string, string> | null;
}

/**
 * #41: the client's thread, a ThreadDTO as decode.ts leaves it. Every list is present (the host omits empty
 * ones), so no screen needs `?? []`. The wire type stays ThreadDTO.
 */
export interface Thread extends ThreadDTO {
  decisions: Decision[];
  tasks: TaskDTO[];
  changes: Change[];
  offers: Offer[];
  runs: OfferRun[];
  outbox: { id: string; state: string }[];
  unavailable: Record<string, string>;
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
  /** V0.50: the models with the effort levels each takes */
  modelCaps?: { id: string; name?: string; efforts?: string[] }[];
  defaultEffort?: string;
  cli?: string;
  /** V0.50: why it cannot run now (no token or key yet); not offered in pickers */
  unavailable?: string;
}

/** What "New project from this thread" starts with (V0.55): a name and the thread's folder. */
export interface NewProjectDraft {
  name: string;
  folder: string;
}

export interface ProjectIconSpec {
  kind: "monogram" | "emoji" | "line" | string;
  /** Missing when empty: the host omits it. */
  value?: string;
}

/** A project: a name, a colour and one folder on its host (V0.55, decision 0017). Without a folder threads are filed
 *  in it by hand. */
export interface Project {
  id?: string;
  /** #42: when the host built this copy; the client keeps the higher of a snapshot's and a frame's */
  rev?: number;
  /** V0.51: the host the project lives on (a front stamps it) */
  host?: string;
  name: string;
  /** a palette name: slate blue sky teal green olive violet indigo rose brown */
  color?: string;
  icon?: ProjectIconSpec;
  /** the one folder on the project's host; threads there or below land in it (V0.55) */
  folder?: string;
  profile?: string;
  model?: string;
  threads?: number;
  lastUsed?: string;
  /** when the host made it; V0.121: projects the owner has not placed follow by this */
  createdAt?: string;
  /** V0.46: quiet hours, in the host's time zone: a list of windows (decision 0014 addendum); absent is off.
   *  A host from before the addendum sends one window as an object; quietList reads both. */
  quiet?: QuietHours[] | QuietHours;
  /** V0.46: the end of the quiet hours the project is in now (RFC 3339); absent when not quiet */
  quietUntil?: string;
  /** V0.58: only on the stream, `{id, deleted: true}` after project.delete */
  deleted?: boolean;
  /** V0.116: the version of the project's image icon (served at /api/projects/<id>/icon); absent draws the glyph */
  image?: string;
  /** V0.116: who set name, color and image: "owner", "agent", or "" for a placeholder; a key may be missing */
  by?: Partial<Record<SetupField, string>>;
  /** V0.116: the last project setup run: its thread, when, or why it could not start */
  setup?: { thread?: string; at: string; error?: string };
}

/** V0.116: the fields the setup agent fills. */
export type SetupField = "name" | "color" | "image";

/** One window of a project's quiet hours (V0.46): HH:MM; to not after from crosses midnight, from equal to to is
 *  all day; days are the start days, absent = every day. */
export interface QuietHours {
  from: string;
  to: string;
  days?: string[];
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
  /** V0.124: this install's key id ("F7QK-2M9X-…"); missing from a Fyr older than V0.124 */
  hostId?: string;
  user: string;
  threads: ThreadDTO[];
  actions: ActionInfo[];
  profiles: Profile[];
  projects: Project[];
  /** V0.39: where an update of Fyr is, while one runs or just ended */
  deploy?: import("./deploy.ts").DeployProgress | null;
  /** V0.51: the front's hosts; missing from a host older than the split */
  hosts?: import("./hosts.ts").HostInfo[];
  /** V0.121: the owner's project order, one list across a front's hosts; absent until set */
  projectOrder?: string[];
  /** V0.147: a host's newest stall of the last 24 h */
  stall?: unknown;
  /** V0.147: a front's hosts' stalls, each with its host */
  stalls?: unknown;
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

/** V0.59 (decision 0018): one change a thread makes or asks for. */
export type ChangePhase = "working" | "review" | "landed" | "live" | "abandoned" | "reverted";

/** V0.101 (decision 0024 section 5): why a change waits on the owner, as the host computed it. head is the commit the
 *  cause is about (a Merge button is bound to it); ask and action name an open ask. */
export interface ChangeNeed {
  cause: "ask" | "ready" | "failure" | "fixing" | "waiting";
  head?: string;
  text?: string;
  ask?: string;
  action?: string;
  muted?: boolean;
}

export interface Change {
  id: string;
  key: string;
  kind: string;
  title: string;
  targets?: string[];
  branch?: string;
  revisions?: { id: string; summary?: string; url?: string; by: string; at: string }[];
  phase: ChangePhase;
  label?: string;
  conditions?: ChangeCondition[];
  evidence?: { kind: string; url?: string; attachment?: string; title?: string }[];
  group?: string;
  after?: string[];
  /** V0.101: why it waits on the owner, computed on the host; the client only reads it. cause: ask, ready, failure,
   *  fixing (muted: a failure while the agent runs) or waiting (V0.136, muted: a merge ask that waits for CI) */
  need?: ChangeNeed;
  /** V0.97: the change's PR as Fyr last read it, and what it waits for from the owner (the host's Ready rule) */
  pr?: PullRequest;
  ready?: "Merge" | "Review and merge";
  phaseBy: "agent" | "observed" | "owner";
  /** V0.60, #31: the change's actions and their last runs */
  offers?: Offer[];
  runs?: OfferRun[];
  /** V0.66: the failed checks' annotations while the PR is open. The deploy watch is a Rule of the thread (#46). */
  annotations?: CiAnnotation[];
  /** #35, decision 0019: where its git facts are read */
  workspace?: { repo?: string; path?: string; branch?: string; base?: string; madeByFyr?: boolean };
  /** #35: the host record of the same branch this thread's change follows */
  observed?: string;
  /** #35: set on a host record (an observed branch): the threads that show it. It is one change on the board. */
  threads?: string[];
  /** V0.84: the owner's Not now and a fingerprint of what they saw; the host clears it when the change moves */
  parked?: { at: string; seen: string };
  /** V0.98: the change is finished and off the board; reason is the phase, "merged", "done" (the owner's Done) or
   *  "landed before v2" */
  done?: { at: string; by: "agent" | "observed" | "owner"; reason: string };
  /** V0.98 (4): when a fact of the change last moved */
  touchedAt?: string;
  /** V0.102: Fyr approved its merge by the repo's auto-merge setting, with no press */
  autoApproved?: { repo?: string; deploy?: boolean; head: string; at: string };
  createdAt: string;
  updatedAt: string;
}

/** V0.66 (7): a check run's annotation on the PR's head commit. */
export interface CiAnnotation {
  path: string;
  line: number;
  endLine?: number;
  level: "failure" | "warning" | "notice";
  message: string;
  title?: string;
  check: string;
}

export interface ChangeCondition {
  type: string;
  status: "ok" | "fail" | "pending";
  reason?: string;
  source: "agent" | "observed" | "owner";
  at: string;
}

/** V0.60: a button the owner presses, on a change, the thread or its live view. */
export interface Offer {
  id: string;
  on?: "thread" | "view";
  label: string;
  run: "fyr" | "script" | "agent";
  effect?: string;
  argv?: string[];
  cwd?: string;
  timeoutSec?: number;
  prompt?: string;
  in?: "this" | "new";
  when?: ChangePhase[];
  revision?: string;
  onOk?: ChangePhase;
  primary?: boolean;
  /** V0.149: a run request: the shell text the owner runs, why, and whether it runs as root through pkexec */
  request?: boolean;
  command?: string;
  why?: string;
  root?: boolean;
  by: "agent" | "observed";
  digest: string;
}

export interface OfferRun {
  action: string;
  label: string;
  by: string;
  at: string;
  running?: boolean;
  ok: boolean;
  exit?: number;
  output?: string;
  endedAt?: string;
}
