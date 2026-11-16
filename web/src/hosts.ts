// SPEC 0.5 V0.51 (decision 0016): a front lists its hosts; a thread on a host the front cannot reach is shown
// from the front's cache, marked offline and read-only.
import { clockTime } from "./format.ts";
import type { Profile } from "./types.ts";
import type { DeployProgress } from "./deploy.ts";

export interface HostInfo {
  name: string;
  online: boolean;
  /** online: since when; offline: since when it was unreachable (RFC 3339) */
  since?: string;
  error?: string;
  /** V0.54: this host's profiles (providers are per host, decision 0015); missing from a front older than V0.54 */
  profiles?: Profile[];
  /** V0.54: an update of Fyr on this host */
  deploy?: DeployProgress;
}

/** The host's name for the rail and the phone; without one in the state, "mock" or "local". */
export function hostLabel(host: string, mock: boolean): string {
  return host || (mock ? "mock" : "local");
}

export function decodeHosts(raw: unknown): HostInfo[] {
  if (!Array.isArray(raw)) return [];
  return raw
    .filter((h): h is Record<string, unknown> => !!h && typeof h === "object" && typeof (h as { name?: unknown }).name === "string")
    .map((h) => ({
      name: h.name as string,
      online: h.online === true,
      since: typeof h.since === "string" ? h.since : undefined,
      error: typeof h.error === "string" ? h.error : undefined,
      profiles: Array.isArray(h.profiles) ? (h.profiles as Profile[]) : undefined,
      deploy: h.deploy && typeof h.deploy === "object" && typeof (h.deploy as { phase?: unknown }).phase === "string" ? (h.deploy as DeployProgress) : undefined,
    }));
}

/** "meros-laptop offline since 19:40 · read-only", or "" while the thread's host is reachable. */
export function offlineLine(t: { host?: string; hostOffline?: string }): string {
  if (!t.hostOffline) return "";
  const at = clockTime(t.hostOffline);
  return `${t.host || "Its host"} offline${at ? ` since ${at}` : ""} · read-only`;
}

/** V0.54: the host shows on rows and headers only when the front has more than one. */
export function multiHost(hosts: HostInfo[] | undefined): boolean {
  return (hosts?.length ?? 0) > 1;
}

/** V0.54: the host name a thread row or header shows: "" with one host. */
export function hostTag(t: { host?: string }, hosts: HostInfo[] | undefined): string {
  return multiHost(hosts) && t.host ? t.host : "";
}

/** V0.54: the profiles a new thread on `host` can use: that host's, else the merged list (one host, or an older front). */
export function profilesOn(host: string | undefined, hosts: HostInfo[] | undefined, all: Profile[]): Profile[] {
  const h = host ? hosts?.find((x) => x.name === host) : undefined;
  return h?.profiles ?? all;
}

/** V0.54: the host a new thread starts on: its project's host, else the first host (the front's default). */
export function hostForProject(projectHost: string | undefined, hosts: HostInfo[] | undefined): string | undefined {
  return projectHost || hosts?.[0]?.name;
}

const BUSY = new Set(["build", "test", "drain", "switch", "client"]);

/** The status words of a host row in Settings → Hosts. */
export function hostStatus(h: HostInfo, self: string): string {
  const here = h.name === self ? "this machine · " : "";
  if (h.online && h.deploy && BUSY.has(h.deploy.phase)) return `${here}online · updating Fyr`;
  if (h.online) return `${here}online`;
  const at = clockTime(h.since);
  return `${here}offline${at ? ` since ${at}` : ""}`;
}
