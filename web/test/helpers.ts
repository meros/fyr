import { decodeThread } from "../src/decode.ts";
import type { AttentionState, Section, Thread, ThreadDTO } from "../src/types.ts";

export function thread(id: string, state: AttentionState, section: Section, extra: Partial<ThreadDTO> = {}): Thread {
  // #41: the client works on decoded threads; the fixture goes through the same decoder.
  return decodeThread({
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
  });
}
