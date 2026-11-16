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
  /** V0.123: the front found it on the tailnet (decision 0025); else it is configured */
  found?: boolean;
  /** V0.124 (decision 0026): the host's install key id ("F7QK-2M9X-…"); missing from a host older than V0.124 */
  id?: string;
  /** V0.124: the id the front first saw under this name, when the host now reports another */
  idWas?: string;
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
      ...(h.found === true ? { found: true } : {}),
      ...(typeof h.id === "string" && h.id ? { id: h.id } : {}),
      ...(typeof h.idWas === "string" && h.idWas ? { idWas: h.idWas } : {}),
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
  const found = h.found ? " · found on the tailnet" : "";
  if (h.online && h.deploy && BUSY.has(h.deploy.phase)) return `${here}online · updating Fyr${found}`;
  if (h.online) return `${here}online${found}`;
  const at = clockTime(h.since);
  return `${here}offline${at ? ` since ${at}` : ""}${found}`;
}

/** V0.124: the short form of an install id, its first group ("F7QK"). */
export function shortID(id: string | undefined): string {
  return (id ?? "").split("-")[0];
}

/** V0.124: the status line of a host row with its short id: "online · id F7QK". */
export function hostLine(h: HostInfo, self: string): string {
  const s = hostStatus(h, self);
  return h.id ? `${s} · id ${shortID(h.id)}` : s;
}

/** V0.124: a host now reports another install id than the one the front first saw under its name: what the row says, or "". */
export function idChangedLine(h: HostInfo): string {
  if (!h.idWas || !h.id) return "";
  return `Another install answers as ${h.name}: id ${shortID(h.id)}, it was ${shortID(h.idWas)}. Trust it if you reinstalled or replaced this machine.`;
}
