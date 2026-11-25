// DTOs of the v0 HTTP API (docs/API-v0.md). The client renders these; it never computes status.

import type { Usage } from "./usage.ts";
import type { HostId } from "./hostid.ts";
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

/** V0.row-group-code: the owner's move in Needs you, the host's code; the groups of Your move. */
export type NeedsCode = "answer" | "approve" | "unblock";

export interface Attention {
  state: AttentionState;
  section: Section;
  reason?: string;
  /** V0.row-group-code: set in Needs you only. Absent from a host before it (see rows.ts). */
  group?: NeedsCode;
  /** V0.row-group-code: the two parts of a reason "<word>: <subject>" or "<change title>: <need>" a row splits: the
   *  word goes in the state's place ("Run", "Settle", "Merge"), the subject on the line under it. */
  word?: string;
  subject?: string;
}

export interface Failure {
  class: string;
  message: string;
  at?: string;
}

interface QuestionOption {
  label: string;
  description?: string;
}

export interface Question {
  question: string;
  header?: string;
  multiSelect?: boolean;
  options: QuestionOption[];
}

interface Effect {
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
  /** V0.tool-approval-judge: the judge's short reason when it escalated a tool call to the owner. */
  judgeNote?: string;
  effect?: Effect;
  /** V0.ask-tool: asked with Fyr's ask tool; the agent is not waiting in a tool call. */
  async?: boolean;
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
  /** V0.195: a command the agent left running, and since when the thread has used almost no CPU beside it. */
  background?: boolean;
  tool?: string;
  /** V0.subagent-timeline: the Task/Agent call that started it */
  toolUseId?: string;
  quietSince?: string;
}

/** V0.wakeups-via-watch, V0.quota-reset-wake: one armed wake-up on a thread. */
export interface WakeUp {
  rule: string;
  at: string;
  note?: string;
  provider?: string;
  window?: string;
  by?: string;
}

/** Decision 0039, V0.trans-host-handover: link to the counterpart thread on another host. */
interface HandoverLink {
  hostId: string;
  hostName?: string;
  threadId: string;
}

export interface ThreadDTO {
  /** Increases on every change, also across host restarts: keep the higher (API v0). */
  rev?: number;
  /** V0.51: the name of the host that runs the thread */
  host?: string;
  /** Decision 0031: the install id of the host whose state or frame the thread came in; the client stamps it */
  hostId?: HostId;
  /** Decision 0031: the host's own id of the thread; `id` is the ref (`<hostId>/<rid>`) the client keys by */
  rid?: string;
  /** V0.51: the host is unreachable since then (RFC 3339): the front shows its cached copy, read-only */
  hostOffline?: string;
  /** V0.149 (4): why a root run request cannot ask for the password on the thread's host; absent when it can */
  rootPrompt?: string;
  /** V0.usage-ledger: the thread's tokens so far, from the host's usage ledger; absent before its first counted result */
  usage?: Usage;
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
  /** V0.project-archive: put away with its archived project; out of the inbox until new work brings it back */
  shelvedAt?: string | null;
  settleWhenDone?: boolean;
  settleWhenMerged?: boolean;
  /** V0.settle-request: the agent said its work is done, on a project that asks the owner; a Settle chip shows */
  settleAsk?: { at: string; why?: string } | null;
  /** V0.59 (7): settles when the change (an id or a key) reaches the phase */
  settleWhen?: { change: string; phase: string };
  /** V0.wakeups-via-watch, V0.quota-reset-wake: armed wake-ups (timed or quota reset), soonest first; note is the prompt it runs then */
  wakes?: WakeUp[];
  /** V0.waits-strip: every armed watch of the agent's (a time, a change's phase, a check, the run's end), soonest wake-up first */
  waits?: { rule: string; text: string; by?: string; /** V0.snooze-until-wake: when a wait on a time wakes */ at?: string }[];
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
  /** Decision 0039, V0.trans-host-handover: cross-host handover links */
  handedOverFrom?: HandoverLink | null;
  handedOverTo?: HandoverLink | null;
  /** V0.37: the live view; read it through liveViewOf (view.ts) */
  live?: { pageId?: string; state?: Record<string, unknown>; updatedAt?: string; checkedAt?: string; headline?: string; offers?: Offer[]; runs?: OfferRun[] } | null;
  /** V0.33: messages the host still holds (queued or sent, not yet read) */
  outbox?: { id: string; state: string }[] | null;
  /** V0.143: the live agent process takes nothing Fyr writes; since its last output, and why */
  unresponsive?: { since: string; at: string; reason: string } | null;
  /** V0.153: Fyr waits for a thrashing host instead of killing a slow agent */
  machineWait?: { since: string; why: string } | null;
  /** V0.210: a question Fyr read in the agent's text; the owner's message, Not a question or a settle clears it */
  attention: Attention;
  /** V0.185: snoozeWhenDone: the snooze lasts until work is done; snoozeUntil is then its maximum time */
  view?: { seenAt?: string | null; snoozeUntil?: string | null; snoozeWhenDone?: boolean } | null;
  unavailable?: Record<string, string> | null;
}

/**
 * #41: the client's thread, a ThreadDTO as decode.ts leaves it. Every list is present (the host omits empty
 * ones), so no screen needs `?? []`. The wire type stays ThreadDTO.
 */
export interface Thread extends ThreadDTO {
  /** Slice 2b: the host whose state the thread is kept in; every thread the store holds has one */
  hostId: HostId;
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
  /** the project ids (on its host) it is limited to; none or empty is every project */
  projects?: string[];
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
  /** Decision 0031: the install id of the host the project came from; the client stamps it */
  hostId?: HostId;
  /** Decision 0031: the host's own id of the project; `id` is the ref */
  rid?: string;
  name: string;
  /** a palette name: slate blue sky teal green olive violet indigo rose brown */
  color?: string;
  icon?: ProjectIconSpec;
  /** the one folder on the project's host; threads there or below land in it (V0.55) */
  folder?: string;
  /** Decision 0034: records on several hosts with one link are one project */
  link?: string;
  /** Decision 0034: the folder's git origin, as the host read it */
  repo?: string;
  /** Decision 0034: when the values the members share were set; the newest wins */
  sharedAt?: string;
  /** V0.project-archive: when the owner archived it; absent is active. A linked project is archived when every member is */
  archivedAt?: string;
  /** Decision 0034: the client's: the member whose image a linked project shows */
  imageFrom?: { id: string; hostId: HostId };
  /** Decision 0034: the client's: every record of a linked project, the one shown first; absent on any other project */
  members?: Project[];
  profile?: string;
  model?: string;
  /** V0.model-choice: the default effort of a new thread here, when its model takes it */
  effort?: string;
  threads?: number;
  lastUsed?: string;
  /** when the host made it; V0.121: projects the owner has not placed follow by this */
  createdAt?: string;
  /** V0.46: quiet hours, in the host's time zone: a list of windows (decision 0014 addendum); absent is off.
   *  A host from before the addendum sends one window as an object; quietList reads both. */
  quiet?: QuietHours[] | QuietHours;
  /** V0.project-priority: how much of the host its threads get (the slot queue, CPU); absent is normal */
  priority?: "high" | "low";
  /** V0.settle-request: what an agent's settle_request does; absent is ask (a Settle chip) */
  agentSettle?: "auto";
  /** V0.tool-approval-judge: how agent tool approvals run; absent is ask (always ask the owner) */
  toolApproval?: "auto";
  /** V0.46: the end of the quiet hours the project is in now (RFC 3339); absent when not quiet */
  quietUntil?: string;
  /** V0.quiet-now: the end of a quiet stretch the owner started by hand (RFC 3339); a time passed is off */
  quietNow?: string;
  /** V0.58: only on the stream, `{id, deleted: true}` after project.delete */
  deleted?: boolean;
  /** V0.116: the version of the project's image icon (served at /api/projects/<id>/icon); absent draws the glyph */
  image?: string;
  /** V0.project-icons-on-demand: when the image was set; a linked project shows its newest member's */
  imageAt?: string;
  /** V0.116: who set name, color and image: "owner", "agent", or "" for a placeholder; a key may be missing */
  by?: Partial<Record<SetupField, string>>;
  /** V0.116: the last project setup run: its thread, when, or why it could not start */
  setup?: { thread?: string; at: string; error?: string };
  /** V0.project-playbook: what the project does around and after a pull request (playbook.ts) */
  playbook?: PlaybookEntry[];
  /** V0.project-playbook: when the playbook was last written; a linked project shows its newest member's */
  playbookAt?: string;
}

/** V0.project-playbook: one entry of a project's playbook: a stage a merged change reaches, or one of the notes. */
export interface PlaybookEntry {
  slot: "stage" | "verify" | "promote" | "issue" | "run" | "rollback" | "lesson";
  /** a stage's environment: dev, prod; a lesson's short name (V0.playbook-lessons) */
  name?: string;
  how?: string;
  /** a stage: where it is looked at */
  url?: string;
  /** a stage: the shell command Fyr runs after a merge ({sha} is the merge commit; exit 0: the stage holds it) */
  probe?: string;
  /** the owner allowed the probe */
  probeOk?: boolean;
  /** a stage: reaching it makes a change live */
  live?: boolean;
  /** V0.playbook-moments, a stage: Fyr asks the owner to look at each change that gets here */
  ownerChecks?: boolean;
  /** V0.playbook-moments, a stage: the command that puts a change here by hand; Fyr shows it on a Run card */
  deployCmd?: string;
  /** V0.playbook-moments: the merges in a row the probe did not see here within a day */
  missed?: number;
  /** V0.playbook-probe-health: what the probe last answered on its host, and since when: it holds the commit
   *  (reached), not yet (waiting), it was stopped at the 30 s limit (timeout), or it cannot run (error) */
  last?: { state: "reached" | "waiting" | "timeout" | "error"; exit?: number; note?: string; at?: string };
  /** V0.playbook-moments: what an agent found that differs from the owner's entry */
  suggest?: Pick<PlaybookEntry, "how" | "url" | "probe" | "deployCmd" | "live" | "ownerChecks" | "evidence" | "thread" | "at">;
  by: string;
  /** the thread it was learned in */
  thread?: string;
  evidence?: string;
  at?: string;
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
  /** V0.171: the owner's host marks by install id ({id: {icon, color}}); a front's, missing when none is set */
  hostMarks?: Record<string, { icon?: string; color?: string }>;
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

/** V0.59 (decision 0018): one change a thread makes or asks for. */
export type ChangePhase = "working" | "review" | "landed" | "live" | "abandoned" | "reverted";

/** V0.101 (decision 0024 section 5): why a change waits on the owner, as the host computed it. head is the commit the
 *  cause is about (a Merge button is bound to it); ask and action name an open ask. */
export interface ChangeNeed {
  cause: "ask" | "ready" | "failure" | "fixing" | "waiting" | "automerge" | "merging"; // V0.205: automerge is muted, Fyr merges it itself; V0.merge-in-flight: so is a merge that was started
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
  /** V0.36: the owner dismissed this merged or closed PR */
  dismissed?: boolean;
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
  /** V0.sensitive-pr: why its merge is sensitive (the agent's reason, or the GitHub label "sensitive"); never auto-merged. */
  sensitive?: string;
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

interface ChangeCondition {
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
  /** V0.next-step-chips: a run request nothing waits on: a chip under the newest message, never in the dock */
  optional?: boolean;
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
