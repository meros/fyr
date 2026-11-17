// SPEC V0.126 (decision 0016): from any front the owner configures every host it follows, with the controls they would
// have connected to that host directly. Providers (with System agents) and Automation show one group per host, this
// install first; every read and command of a group names its host, and the front sends it there. An offline host shows
// what the front last read from it, read-only. One install (or a front with one host) is one part without a heading,
// as before. Pure.
import { dayTime } from "./format.ts";
import type { HostInfo } from "./hosts.ts";
import type { AutoMergeRepo } from "./automerge.ts";

export interface SettingsHost {
  /** the host's name for ?host= and args.host; "" for the one part of a front with one host */
  name: string;
  /** the name shown */
  label: string;
  /** this install */
  self: boolean;
  online: boolean;
  /** online: since when; offline: since when it was unreachable */
  since?: string;
}

/** The parts of a per-host page: one without a host name when the front has one host or none, else one per host, in
 *  the owner's host order (V0.132; the front sends hosts[] in it). */
export function settingsHosts(hosts: Pick<HostInfo, "name" | "online" | "since">[] | undefined, self: string): SettingsHost[] {
  const hs = hosts ?? [];
  if (hs.length <= 1) return [{ name: "", label: hs[0]?.name || self, self: true, online: true }];
  return hs.map((h) => ({ name: h.name, label: h.name, self: h.name === self, online: h.online, ...(h.since ? { since: h.since } : {}) }));
}

/** The line under a group's heading: "This install", "Online since 08:31", "Offline since 19:40". */
export function groupLine(h: SettingsHost, now: number): string {
  if (h.self) return "This install";
  const at = dayTime(h.since, now);
  return `${h.online ? "Online" : "Offline"}${at ? ` since ${at}` : ""}`;
}

/** The note on an offline host's group: what is shown and that nothing changes; "" for a host that is online. readAt:
 *  when the front read what it shows (X-Fyr-Read-At), null when it has nothing. */
export function readOnlyNote(h: SettingsHost, readAt: string | null, now: number): string {
  if (h.online) return "";
  if (!readAt) return `${h.label} is offline, and Fyr has not read its settings yet. They show when it is back.`;
  return `${h.label} is offline. These are its settings as Fyr last read them at ${dayTime(readAt, now)}; nothing here can be changed until it is back.`;
}

/** A per-host read's path: ?host= names the host; the one part of a one-host front names none. */
export function settingsPath(path: string, host: string): string {
  return host ? `${path}?host=${encodeURIComponent(host)}` : path;
}

/** A per-host command's args: args.host names the host the front sends it to. */
export function withHost(args: Record<string, unknown>, host: string): Record<string, unknown> {
  return host ? { ...args, host } : args;
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** The Automation page's line over every host: "Deploys on · merges in 2 repos", "Deploys on 1 of 2 hosts · …"; ""
 *  until every host is read. A host to skip (offline, never read) is left out. */
export function automationSummaryHosts(parts: { deploy: boolean | null; repos: AutoMergeRepo[] | null; skip?: boolean }[]): string {
  const read = parts.filter((p) => !p.skip);
  if (!read.length || read.some((p) => p.deploy === null || p.repos === null)) return "";
  const on = read.filter((p) => p.deploy).length;
  const deploy = on === read.length ? "Deploys on" : on === 0 ? "Deploys off" : `Deploys on ${on} of ${plural(read.length, "host", "hosts")}`;
  const merges = read.reduce((n, p) => n + p.repos!.filter((r) => r.on).length, 0);
  return `${deploy} · ${merges ? `merges in ${plural(merges, "repo", "repos")}` : "merges off"}`;
}
