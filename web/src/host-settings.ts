// SPEC V0.126 (decision 0016): from any front the owner configures every host it follows, with the controls they would
// have connected to that host directly. Providers (with System agents) and Automation show one group per host, in the
// owner's host order (decision 0031 2.6: the same through every front); every read and command of a group names its host, and the front sends it there. An offline host shows
// what the front last read from it, read-only. One install (or a front with one host) is one part without a heading,
// as before. Pure.
import { dayTime } from "./format.ts";
import type { HostInfo } from "./hosts.ts";
import type { HostId } from "./hostid.ts";
import type { AutoMergeRepo } from "./automerge.ts";

export interface SettingsHost {
  /** the host's install id, which every call names (decision 0031); none for a host that reports none */
  id?: HostId;
  /** the host's name, the key of its part; "" for the one part of a front with one host */
  name: string;
  /** the name shown */
  label: string;
  online: boolean;
  /** online: since when; offline: since when it was unreachable */
  since?: string;
  /** V0.host-ids (decision 0031 2.1): the host reports no install id (older than V0.124). It is listed, says it needs an
   *  update, and nothing is read from it or sent to it: no call can name it. */
  noId?: true;
}

/** The parts of a per-host page: one without a host name when the front has one host or none, else one per host, in
 *  the owner's host order (V0.132; the front sends hosts[] in it). */
export function settingsHosts(hosts: Pick<HostInfo, "id" | "name" | "online" | "since">[] | undefined): SettingsHost[] {
  const hs = hosts ?? [];
  if (hs.length <= 1) return [{ ...(hs[0]?.id ? { id: hs[0].id } : {}), name: "", label: hs[0]?.name ?? "", online: true }];
  return hs.map((h) => ({ ...(h.id ? { id: h.id } : {}), name: h.name, label: h.name, online: h.online, ...(h.since ? { since: h.since } : {}), ...(h.id ? {} : { noId: true as const }) }));
}

/** The line under a group's heading: "Online since 08:31", "Offline since 19:40", "Needs an update". */
export function groupLine(h: SettingsHost, now: number): string {
  if (h.noId) return "Needs an update";
  const at = dayTime(h.since, now);
  return `${h.online ? "Online" : "Offline"}${at ? ` since ${at}` : ""}`;
}

/** The note on an offline host's group: what is shown and that nothing changes; "" for a host that is online. readAt:
 *  when the front read what it shows (X-Fyr-Read-At), null when it has nothing. */
export function readOnlyNote(h: SettingsHost, readAt: string | null, now: number): string {
  if (h.noId) return `${h.label} runs a Fyr from before install ids, so its settings cannot be read or changed from here. They show once it has updated.`;
  if (h.online) return "";
  if (!readAt) return `${h.label} is offline, and Fyr has not read its settings yet. They show when it is back.`;
  return `${h.label} is offline. These are its settings as Fyr last read them at ${dayTime(readAt, now)}; nothing here can be changed until it is back.`;
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
