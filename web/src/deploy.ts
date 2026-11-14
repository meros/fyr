// SPEC 0.5 V0.39: where an update of Fyr is, in words, for the banner.
import type { ThreadDTO } from "./types.ts";

export interface DeployProgress {
  phase: string; // build, test, drain, switch, done, failed
  detail?: string;
  blocking?: string[];
  build?: string;
  startedAt?: string;
  at?: string;
}

export type DeployLine = { text: string; tone: "busy" | "ok" | "fail"; more?: string };

function minutes(fromIso: string | undefined, now: number): string {
  const t = fromIso ? Date.parse(fromIso) : NaN;
  if (Number.isNaN(t)) return "";
  const s = Math.max(0, Math.round((now - t) / 1000));
  return s < 60 ? `${s} s` : `${Math.round(s / 60)} min`;
}

export function deployLine(p: DeployProgress | null | undefined, threads: Record<string, ThreadDTO>, now: number, conn: string): DeployLine | null {
  if (!p || !p.phase) return null;
  const busy = p.phase === "build" || p.phase === "test" || p.phase === "drain" || p.phase === "switch";
  if (busy && conn !== "live") return { text: "Restarting Fyr · back in a few seconds", tone: "busy" };
  const took = minutes(p.startedAt, now);
  switch (p.phase) {
    case "build":
      return { text: `Updating Fyr · building the new version${took ? ` · ${took}` : ""}`, tone: "busy" };
    case "test":
      return { text: `Updating Fyr · running the tests${took ? ` · ${took}` : ""}`, tone: "busy" };
    case "drain": {
      const names = (p.blocking ?? []).map((id) => threads[id]?.title || "a thread");
      if (names.length === 1) return { text: `Updating Fyr · waiting for “${names[0]}” to finish its turn`, tone: "busy" };
      return { text: `Updating Fyr · waiting for ${names.length} threads to finish: ${names.join(", ")}`, tone: "busy" };
    }
    case "switch":
      return { text: "Restarting Fyr · this page reloads by itself", tone: "busy" };
    case "done":
      return { text: `Fyr is updated${p.build ? ` · build ${p.build}` : ""}`, tone: "ok" };
    case "failed": {
      const first = (p.detail ?? "").split("\n").find((l) => l.trim()) ?? "";
      return { text: `The update failed${first ? `: ${first}` : ""}`, tone: "fail", more: p.detail };
    }
  }
  return null;
}
