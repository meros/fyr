import type { AttentionState, Section, ThreadDTO } from "../src/types.ts";

export function thread(id: string, state: AttentionState, section: Section, extra: Partial<ThreadDTO> = {}): ThreadDTO {
  return {
    id,
    title: id,
    cwd: "/x",
    project: "p",
    profile: "claude",
    model: "m",
    createdAt: "2026-11-14T08:00:00Z",
    updatedAt: "2026-11-14T08:00:00Z",
    running: false,
    attention: { state, section, reason: "" },
    ...extra,
  };
}
