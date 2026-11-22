// SPEC V0.157, decision 0030 3.4: Settings → Devices. The signed-in devices of this install, the ones waiting for
// approval, and the mode of the browser way in. Pure: the words and the counts; the page is devices-ui.tsx.
import { relTime } from "./format.ts";

export interface DeviceRow {
  id: string;
  name: string;
  how: "local" | "tailnet" | "link" | "code" | string;
  created: string;
  lastSeen: string;
  node?: string;
  /** the device that added or approved it */
  by?: string;
  pending?: boolean;
  /** the approval code of a waiting device */
  code?: string;
  codeUntil?: string;
}

export interface ModeRow {
  way: "browser" | "peer";
  mode: "observe" | "enforce";
  /** set back to observe by hand: it stays until the owner enforces again */
  hand: boolean;
  since: string;
  refused: number;
  success: Record<string, number> | null;
  ready: boolean;
  why?: string;
  by?: string;
}

interface AuditRow {
  at: string;
  event: string;
  by: string;
  detail?: string;
}

export interface AuthState {
  devices: DeviceRow[];
  modes: ModeRow[];
  /** null from a host before V0.devices-empty-audit whose trail is empty */
  audit: AuditRow[] | null;
  /** requests a host served from a front of an older build (decision 0030 slice 1) */
  legacyFronts?: number;
  /** the device that asks, when it is signed in */
  you?: string;
}

const HOW: Record<string, string> = {
  local: "Link from this machine",
  tailnet: "Tailscale",
  link: "One-time link",
  code: "Approval code",
};

/** How a device got in. */
export function howWords(how: string): string {
  return HOW[how] ?? how;
}

/** "ABCD1234" → "ABCD-1234", as it is shown and typed. */
export function codeShow(code: string | undefined): string {
  return code ? `${code.slice(0, 4)}-${code.slice(4)}` : "";
}

/** The line under a device's name: how it got in, and when it was last used. */
export function deviceLine(d: DeviceRow, now: number): string {
  const used = now - Date.parse(d.lastSeen) < 60_000 ? "now" : relTime(d.lastSeen, now);
  return `${howWords(d.how)} · last used ${used === "now" ? "now" : `${used} ago`}`;
}

/** The signed-in devices and the ones that wait, apart. */
export function splitDevices(s: AuthState | null): { signedIn: DeviceRow[]; waiting: DeviceRow[] } {
  const all = s?.devices ?? [];
  return { signedIn: all.filter((d) => !d.pending), waiting: all.filter((d) => d.pending) };
}

/** The nav's line: "3 devices", "3 devices · 1 waiting"; "" until it is read. */
export function devicesSummary(s: AuthState | null): string {
  if (!s) return "";
  const { signedIn, waiting } = splitDevices(s);
  const base = signedIn.length === 1 ? "1 device" : `${signedIn.length} devices`;
  return waiting.length ? `${base} · ${waiting.length} waiting` : base;
}

/** The amber count beside Devices: the devices that wait for the owner. */
export function devicesNeed(s: AuthState | null): number {
  return splitDevices(s).waiting.length;
}

/** The browser way in, as one line. */
export function modeLine(m: ModeRow | undefined, now: number): string {
  if (!m) return "";
  if (m.mode === "enforce") return "Every browser has to be signed in. Nothing else gets the API.";
  const days = Math.max(0, Math.floor((now - Date.parse(m.since)) / 86_400_000));
  const seen = m.refused === 0 ? "nothing would have been refused" : `${m.refused} request${m.refused === 1 ? "" : "s"} without a session would have been refused`;
  const tail = m.hand ? " Set back by hand: it stays until you turn it on." : m.ready ? " It turns itself on at the next check." : m.why ? ` Not yet: ${m.why}.` : "";
  return `Browsers without a session still get in. In ${days === 1 ? "1 day" : `${days} days`}, ${seen}.${tail}`;
}

/** The notice for the owner when the install enforced a way in by itself in the last 7 days; null otherwise. */
export function autoNotice(audit: AuditRow[] | null, now: number): string | null {
  for (const a of [...(audit ?? [])].reverse()) {
    if (a.event === "mode" && a.by === "auto" && now - Date.parse(a.at) < 7 * 86_400_000) return `Fyr now asks every browser to sign in. ${a.detail?.includes("fyr auth observe") ? "Run fyr auth observe on the machine that runs Fyr to go back." : ""}`.trim();
  }
  return null;
}

/** An approval code as typed: letters and digits only, 8 at most, upper case. */
export function typedCode(s: string): string {
  return s.toUpperCase().replace(/[^A-Z0-9]/g, "").slice(0, 8);
}
