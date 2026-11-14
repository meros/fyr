// SPEC 0.5 V0.45, decision 0013: hand a thread over to another profile. Pure: the dialog's words, the divider's
// words, the default target. The host picks the rung; the client only says which one it will be.
import type { Profile, ThreadDTO } from "./types.ts";

export type Rung = "resume" | "adopt" | "brief";
export interface HandoverPlan {
  rung: Rung;
  note?: string;
}

/** What the dialog says before the owner confirms. */
export function rungText(r: string): string {
  switch (r) {
    case "resume":
      return "Full history: this conversation continues as it is";
    case "brief":
      return "The current agent writes a handover first";
    default:
      return "The new agent reads the old conversation";
  }
}

/** What the divider says afterwards. */
export function rungDone(r: string): string {
  switch (r) {
    case "resume":
      return "Full history";
    case "brief":
      return "The previous agent wrote a handover";
    default:
      return "The new agent read the old conversation";
  }
}

/** The profiles a thread can go to: every other one. */
export function handoverTargets(t: ThreadDTO, profiles: Profile[]): Profile[] {
  return profiles.filter((p) => p.id !== t.profile && !p.unavailable);
}

/** The model a target starts with: its default, else its first, else the CLI's own (""). */
export function targetModel(p: Profile | undefined): string {
  return p?.defaultModel ?? p?.models[0] ?? "";
}

/** The host's answer when it can be asked; a mock or an old host gets the likely rung. */
export function planGuess(t: ThreadDTO): HandoverPlan {
  return t.sessionId ? { rung: "resume" } : { rung: "adopt", note: "The thread has no session yet" };
}

export function parsePlan(x: unknown): HandoverPlan | null {
  const r = (x as { rung?: unknown } | null)?.rung;
  if (r !== "resume" && r !== "adopt" && r !== "brief") return null;
  const note = (x as { note?: unknown }).note;
  return { rung: r, ...(typeof note === "string" && note ? { note } : {}) };
}

function profileName(id: string, profiles: Profile[]): string {
  return profiles.find((p) => p.id === id)?.name ?? id;
}

/** The divider in the conversation: "Handed over from Claude to Claude (norban) · 17:20". */
export function dividerLine(d: { from?: string; to?: string }, profiles: Profile[], clock: string): string {
  return `Handed over from ${profileName(d.from ?? "", profiles)} to ${profileName(d.to ?? "", profiles)}${clock ? ` · ${clock}` : ""}`;
}
