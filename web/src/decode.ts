// The one place where host JSON becomes client types (SPEC 0.5 V0.24). The host
// omits empty fields (Go omitempty), so every field a screen reads without a
// check gets its empty value here, and nothing past this point needs `?? ""`.
import { decodeHosts } from "./hosts.ts";
import type { HostId } from "./hostid.ts";
import type { Attention, Change, ChangePhase, Decision, NewProjectDraft, Offer, OfferRun, Project, StateDTO, Thread } from "./types.ts";

const str = (v: unknown): string => (typeof v === "string" ? v : "");
export const arr = <T>(v: unknown): T[] => (Array.isArray(v) ? (v as T[]) : []);
export const obj = (v: unknown): Record<string, unknown> => (v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {});

const NO_ATTENTION: Attention = { state: "seen", section: "seen", reason: "" } as Attention;

/** A prefill without a name is none (V0.30, V0.55). */
function decodeDraft(raw: unknown): NewProjectDraft | undefined {
  const s = obj(raw);
  const name = str(s.name).trim();
  return name ? { name, folder: str(s.folder) } : undefined;
}

const PHASES: readonly string[] = ["working", "review", "landed", "live", "abandoned", "reverted"] satisfies ChangePhase[];
const OWNERS = ["agent", "observed", "owner"] as const;

/** The records of a list: what is not an object is none. */
const records = (v: unknown): Record<string, unknown>[] => arr<unknown>(v).filter((x): x is Record<string, unknown> => !!x && typeof x === "object" && !Array.isArray(x));

/** A change with the fields the screens read without a check (`updatedAt` sorts the list, `phase` picks the chip). */
export function decodeChange(raw: Record<string, unknown>): Change {
  const c = raw as unknown as Change;
  return {
    ...c,
    id: str(c.id),
    key: str(c.key),
    kind: str(c.kind),
    title: str(c.title),
    phase: (PHASES.includes(c.phase) ? c.phase : "working") as ChangePhase,
    phaseBy: (OWNERS as readonly unknown[]).includes(c.phaseBy) ? c.phaseBy : "observed",
    createdAt: str(c.createdAt),
    updatedAt: str(c.updatedAt),
    ...(c.offers !== undefined ? { offers: records(c.offers).map(decodeOffer) } : {}),
    ...(c.runs !== undefined ? { runs: records(c.runs).map(decodeRun) } : {}),
  };
}

export function decodeOffer(raw: Record<string, unknown>): Offer {
  const o = raw as unknown as Offer;
  return { ...o, id: str(o.id), label: str(o.label), digest: str(o.digest), by: o.by === "agent" ? "agent" : "observed" };
}

export function decodeRun(raw: Record<string, unknown>): OfferRun {
  const r = raw as unknown as OfferRun;
  return { ...r, action: str(r.action), label: str(r.label), by: str(r.by), at: str(r.at), ok: r.ok === true };
}

/** Slice 2b: a thread is decoded into the state of the host it came in, so its host is that one whatever it says. */
export function decodeThread(raw: unknown, host: HostId): Thread {
  const t = obj(raw) as unknown as Thread;
  const a = obj(t.attention);
  return {
    ...t,
    hostId: host,
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
    changes: records(t.changes).map(decodeChange),
    offers: records(t.offers).map(decodeOffer),
    runs: records(t.runs).map(decodeRun),
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
type DecodedState = Omit<StateDTO, "threads"> & { threads: Thread[] };

/** One host's state (slice 2b), its threads that host's. */
export function decodeState(raw: unknown, host: HostId): DecodedState {
  const s = obj(raw) as unknown as StateDTO;
  return {
    ...s,
    now: str(s.now),
    user: str(s.user),
    threads: arr(s.threads).map((t) => decodeThread(t, host)).filter((t) => t.id),
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
