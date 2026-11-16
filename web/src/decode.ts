// The one place where host JSON becomes client types (SPEC 0.5 V0.24). The host
// omits empty fields (Go omitempty), so every field a screen reads without a
// check gets its empty value here, and nothing past this point needs `?? ""`.
import { decodeHosts } from "./hosts.ts";
import type { Attention, Decision, NewProjectDraft, Project, StateDTO, Thread } from "./types.ts";

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const arr = <T>(v: unknown): T[] => (Array.isArray(v) ? (v as T[]) : []);
const obj = (v: unknown): Record<string, unknown> => (v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {});

const NO_ATTENTION: Attention = { state: "seen", section: "seen", reason: "" } as Attention;

/** A prefill without a name is none (V0.30, V0.55). */
function decodeDraft(raw: unknown): NewProjectDraft | undefined {
  const s = obj(raw);
  const name = str(s.name).trim();
  return name ? { name, folder: str(s.folder) } : undefined;
}

export function decodeThread(raw: unknown): Thread {
  const t = obj(raw) as unknown as Thread;
  const a = obj(t.attention);
  return {
    ...t,
    id: str(t.id),
    title: str(t.title),
    cwd: str(t.cwd),
    project: str(t.project),
    newProject: decodeDraft(t.newProject),
    profile: str(t.profile),
    model: str(t.model),
    createdAt: str(t.createdAt),
    updatedAt: str(t.updatedAt),
    running: t.running === true,
    decisions: arr(t.decisions),
    tasks: arr(t.tasks),
    changes: arr(t.changes),
    offers: arr(t.offers),
    runs: arr(t.runs),
    outbox: arr(t.outbox),
    unavailable: obj(t.unavailable) as Record<string, string>,
    attention: typeof a.section === "string" && typeof a.state === "string" ? ({ ...NO_ATTENTION, ...a } as Attention) : NO_ATTENTION,
  };
}

export function decodeProject(raw: unknown): Project {
  const p = obj(raw) as unknown as Project;
  const icon = obj(p.icon);
  return {
    ...p,
    name: str(p.name),
    color: str(p.color),
    icon: { kind: str(icon.kind) || "monogram", value: str(icon.value) },
    folder: str(p.folder),
  };
}

/** The state with its threads decoded (#41). */
export type DecodedState = Omit<StateDTO, "threads"> & { threads: Thread[] };

export function decodeState(raw: unknown): DecodedState {
  const s = obj(raw) as unknown as StateDTO;
  return {
    ...s,
    now: str(s.now),
    user: str(s.user),
    host: str(s.host),
    threads: arr(s.threads).map(decodeThread).filter((t) => t.id),
    projects: arr(s.projects).map(decodeProject),
    actions: arr(s.actions),
    profiles: arr(s.profiles),
    hosts: decodeHosts(s.hosts),
    projectOrder: Array.isArray(s.projectOrder) ? s.projectOrder.filter((x): x is string => typeof x === "string") : undefined,
    deploy: s.deploy && typeof s.deploy === "object" && typeof (s.deploy as { phase?: unknown }).phase === "string" ? s.deploy : null,
  };
}

/** #41: the decision the thread waits on (the host sends the oldest first), or null. */
export function currentDecision(t: Pick<Thread, "decisions">): Decision | null {
  return t.decisions[0] ?? null;
}
