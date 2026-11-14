// The one place where host JSON becomes client types (SPEC 0.5 V0.24). The host
// omits empty fields (Go omitempty), so every field a screen reads without a
// check gets its empty value here, and nothing past this point needs `?? ""`.
import type { Attention, Project, StateDTO, ThreadDTO } from "./types.ts";

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const arr = <T>(v: unknown): T[] => (Array.isArray(v) ? (v as T[]) : []);
const obj = (v: unknown): Record<string, unknown> => (v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {});

const NO_ATTENTION: Attention = { state: "seen", section: "seen", reason: "" } as Attention;

export function decodeThread(raw: unknown): ThreadDTO {
  const t = obj(raw) as unknown as ThreadDTO;
  const a = obj(t.attention);
  return {
    ...t,
    id: str(t.id),
    title: str(t.title),
    cwd: str(t.cwd),
    project: str(t.project),
    profile: str(t.profile),
    model: str(t.model),
    createdAt: str(t.createdAt),
    updatedAt: str(t.updatedAt),
    running: t.running === true,
    decisions: arr(t.decisions),
    tasks: arr(t.tasks),
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
    sources: arr(p.sources),
    cwd: str(p.cwd),
  };
}

export function decodeState(raw: unknown): StateDTO {
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
  };
}
