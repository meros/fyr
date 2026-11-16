// SPEC 0.5 V0.39: where an update of Fyr is, in words, for the banner.
import type { Thread } from "./types.ts";
import type { Ask } from "./confirm.ts";

export interface DeployProgress {
  phase: string; // build, test, drain, switch or (V0.81) client, done, failed
  detail?: string;
  blocking?: string[];
  build?: string;
  startedAt?: string;
  at?: string;
  /** V0.118 (3): the update changed only the web client (V0.81); its end needs no status beyond the reload */
  client?: boolean;
}

export type DeployLine = { text: string; tone: "busy" | "ok" | "fail"; more?: string };

function minutes(fromIso: string | undefined, now: number): string {
  const t = fromIso ? Date.parse(fromIso) : NaN;
  if (Number.isNaN(t)) return "";
  const s = Math.max(0, Math.round((now - t) / 1000));
  return s < 60 ? `${s} s` : `${Math.round(s / 60)} min`;
}

export function deployLine(p: DeployProgress | null | undefined, threads: Record<string, Thread>, now: number, conn: string): DeployLine | null {
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
    case "client":
      // V0.81: only the web client changes; nothing restarts, the page reloads.
      return { text: "Updating the web client · this page reloads by itself", tone: "busy" };
    case "done":
      return { text: `Fyr is updated${p.build ? ` · build ${p.build}` : ""}`, tone: "ok" };
    case "failed": {
      const first = (p.detail ?? "").split("\n").find((l) => l.trim()) ?? "";
      return { text: `The update failed${first ? `: ${first}` : ""}`, tone: "fail", more: p.detail };
    }
  }
  return null;
}

/** V0.83: Restart now shows only while the update waits for at least one running thread and the host is live. */
export function canRestartNow(p: DeployProgress | null | undefined, conn: string): boolean {
  return !!p && p.phase === "drain" && (p.blocking?.length ?? 0) > 0 && conn === "live";
}

/** V0.83: the confirmation before Restart now names the threads whose turns end and the "continue" each gets. */
export function restartNowAsk(p: DeployProgress, threads: Record<string, Thread>): Ask {
  const names = (p.blocking ?? []).map((id) => threads[id]?.title || "a thread");
  const cut = names.length === 1 ? `“${names[0]}” ends its turn now.` : `These ${names.length} threads end their turns now: ${names.join(", ")}.`;
  const after = names.length === 1 ? "It gets the message “continue” after the restart, so it picks the work up again." : "Each gets the message “continue” after the restart, so it picks the work up again.";
  return { title: "Restart Fyr now?", text: `${cut} ${after} A tool call in flight is lost.`, confirm: "Restart now", danger: true };
}

/** SPEC 0.5 V0.118 (2): a success shows this long from when the page first sees it, then goes by itself. */
export const OK_SHOW_MS = 5000;
/** V0.118 (2): a success older than this (a page opened later, within the host's two minutes) shows nothing. */
export const OK_FRESH_MS = 60_000;

/** SPEC 0.5 V0.118 (2): whether the inline status shows. Busy and failed show (a failure until it is dismissed);
 *  a client-only update shows nothing, the reload is its sign; a success shows OK_SHOW_MS from seenAt while fresh. */
export function deployShown(p: DeployProgress | null | undefined, line: DeployLine | null, seenAt: number | null, now: number): boolean {
  if (!p || !line) return false;
  if (p.phase === "client") return false;
  if (line.tone !== "ok") return true;
  if (p.client) return false;
  const at = p.at ? Date.parse(p.at) : NaN;
  if (!Number.isNaN(at) && now - at > OK_FRESH_MS) return false;
  return seenAt === null || now - seenAt < OK_SHOW_MS;
}
