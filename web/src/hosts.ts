// SPEC 0.5 V0.51 (decision 0016): a front lists its hosts; a thread on a host the front cannot reach is shown
// from the front's cache, marked offline and read-only.
import { clockTime, dayTime } from "./format.ts";
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
  /** V0.125: the URL the front dials for it, as configured or as discovery found it; missing from an older front */
  url?: string;
  /** V0.170: what it runs on (laptop, desktop, server) and its OS ("linux", "darwin"); missing until it is seen */
  machine?: string;
  os?: string;
  /** decision 0030 Amendment 1: how the front reaches it: over the pipe, a peer link or the old whois link */
  link?: HostLink;
  /** V0.host-updates (#154): its update state; missing from a host without a release source or an older one */
  update?: HostUpdate;
}

/** V0.host-updates (#154): what a host runs, the release it wants, how far behind, its last update, a release it holds back. */
export interface HostUpdate {
  running?: string;
  build?: string;
  wanted?: string;
  /** commits the wanted release is ahead of the running one; -1 not known */
  behind: number;
  last?: { build: string; commit?: string; result: string; reason?: string; startedAt: string; endedAt?: string; local?: boolean };
  bad?: { commit: string; why: string; at?: string };
  paused?: boolean;
}

export interface HostLink {
  state: "up" | "connecting" | "down" | string;
  via: "pipe" | "peer" | "whois" | string;
  /** a peer link that is up: which side opened it */
  opened?: "here" | "them" | string;
  since?: string;
  error?: { kind: string; text: string };
}

const LINK_WHY: Record<string, string> = {
  timeout: "no answer in time (a firewall or an ACL drops silently)",
  refused: "its peer port refused the connection",
  "key-mismatch": "another install answered than the one in the roster",
  "not-a-member": "it does not have this install as a member yet",
  removed: "it was removed from the roster",
  "older-version": "it runs an older Fyr: followed one way until it is updated",
  "no-address": "no address for it yet; it may open the link itself",
  "no-route": "no route to it",
  closed: "the link closed",
};

/** The reason a link is down, in words: the front's text, else ours for the kind. */
export function linkWhy(l: Pick<HostLink, "error"> | undefined): string {
  if (!l?.error) return "";
  return l.error.text || LINK_WHY[l.error.kind] || l.error.kind;
}

/** "Link up, opened by it, since 14:20" / "Link down: no answer in time…" / "" for the pipe and for none. */
export function linkLine(l: HostLink | undefined, now: number): string {
  if (!l || l.via === "pipe") return "";
  if (l.via === "whois") return l.state === "up" ? "Old link (tailscaled's whois)" : `Old link down${l.error ? `: ${linkWhy(l)}` : ""}`;
  if (l.state === "up") {
    const who = l.opened === "them" ? "opened by it" : l.opened === "here" ? "opened from here" : "";
    const at = l.since ? dayTime(l.since, now) : "";
    return ["Link up", who, at ? `since ${at}` : ""].filter(Boolean).join(", ");
  }
  if (l.state === "connecting") return "Link connecting…";
  return `Link down${l.error ? `: ${linkWhy(l)}` : ""}`;
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
      ...(typeof h.url === "string" && h.url ? { url: h.url } : {}),
      ...(typeof h.machine === "string" && h.machine ? { machine: h.machine } : {}),
      ...(typeof h.os === "string" && h.os ? { os: h.os } : {}),
      // decision 0030 Amendment 1, 0031 2.6: how the front reaches it; the pipe is the host in the front's own process
      ...(h.link && typeof h.link === "object" && typeof (h.link as { state?: unknown }).state === "string" ? { link: h.link as HostLink } : {}),
      ...(h.update && typeof h.update === "object" ? { update: decodeUpdate(h.update as Record<string, unknown>) } : {}),
    }));
}

function decodeUpdate(u: Record<string, unknown>): HostUpdate {
  const str = (v: unknown) => (typeof v === "string" && v ? v : undefined);
  const last = u.last && typeof u.last === "object" && typeof (u.last as { result?: unknown }).result === "string" ? (u.last as HostUpdate["last"]) : undefined;
  const bad = u.bad && typeof u.bad === "object" && typeof (u.bad as { commit?: unknown }).commit === "string" ? (u.bad as HostUpdate["bad"]) : undefined;
  return {
    running: str(u.running),
    build: str(u.build),
    wanted: str(u.wanted),
    behind: typeof u.behind === "number" ? u.behind : -1,
    ...(last ? { last } : {}),
    ...(bad ? { bad } : {}),
    ...(u.paused === true ? { paused: true } : {}),
  };
}

const short7 = (s: string | undefined) => (s ? s.slice(0, 7) : "");
const LAST_BAD: Record<string, string> = { failed: "last update failed", rolledBack: "last update rolled back" };

/** V0.host-updates: how far a host is from the newest release, for its row: "3 commits behind · last update failed",
 *  "holds back 1a2b3c4", "local build", "up to date"; "" when it says nothing (no state, nothing read yet). */
export function updateLine(h: Pick<HostInfo, "update">): { text: string; bad: boolean } {
  const u = h.update;
  if (!u) return { text: "", bad: false };
  const bits: string[] = [];
  if (u.paused) bits.push("local build");
  else if (u.behind > 0) bits.push(`${u.behind} commit${u.behind === 1 ? "" : "s"} behind`);
  else if (u.behind === 0) bits.push("up to date");
  else if (u.wanted && u.running && !u.wanted.startsWith(u.running) && !u.running.startsWith(u.wanted)) bits.push("behind");
  const lastBad = u.last ? LAST_BAD[u.last.result] : undefined;
  if (u.bad) bits.push(`holds back ${short7(u.bad.commit)}`);
  else if (lastBad && u.behind !== 0) bits.push(lastBad);
  return { text: bits.join(" · "), bad: !!u.bad || (!!lastBad && u.behind !== 0) };
}

const RESULT_WORDS: Record<string, string> = { inProgress: "in progress", installed: "installed", failed: "failed", rolledBack: "rolled back", replaced: "replaced" };

/** V0.host-updates: the rows of a host's Updates group on its page: what runs, the newest release, the last update. */
export function updateRows(u: HostUpdate, now: number): { title: string; desc: string; bad?: boolean }[] {
  const rows: { title: string; desc: string; bad?: boolean }[] = [];
  rows.push({ title: "Runs", desc: [u.running ? short7(u.running) : "", u.build && !u.build.startsWith(short7(u.running)) ? `build ${u.build}` : "", u.paused ? "a local build; auto-update waits for a release that contains it" : ""].filter(Boolean).join(" · ") || "Not known" });
  if (u.wanted) {
    const how = u.behind > 0 ? `${u.behind} commit${u.behind === 1 ? "" : "s"} ahead of what runs` : u.behind === 0 ? "runs here" : "";
    rows.push({ title: "Newest release", desc: [short7(u.wanted), how].filter(Boolean).join(" · ") });
  }
  if (u.last) {
    const l = u.last;
    const at = dayTime(l.endedAt || l.startedAt, now);
    const what = l.local ? `local build ${l.build}` : `release ${short7(l.commit) || l.build}`;
    const bad = l.result === "failed" || l.result === "rolledBack";
    rows.push({ title: "Last update", desc: [`${what} ${RESULT_WORDS[l.result] ?? l.result}`, at, bad ? l.reason : ""].filter(Boolean).join(" · "), bad });
  }
  return rows;
}

/** V0.host-updates: the line of a release held back: "Release 1a2b3c4 is held back here since 14:02: failed its smoke
 *  test. Fyr skips it until a newer release." */
export function badLine(b: NonNullable<HostUpdate["bad"]>, now: number): string {
  const at = b.at ? dayTime(b.at, now) : "";
  return `Release ${short7(b.commit)} is held back here${at ? ` since ${at}` : ""}${b.why ? `: ${b.why}` : ""}. Fyr skips it until a newer release.`;
}

/** "host-a offline since 19:40 · read-only", or "" while the thread's host is reachable. */
export function offlineLine(t: { host?: string; hostOffline?: string }): string {
  if (!t.hostOffline) return "";
  const at = clockTime(t.hostOffline);
  return `${t.host || "Its host"} offline${at ? ` since ${at}` : ""} · read-only`;
}

/** The install id a host's name stands for in a picker (decision 0031: a call names the id). No name is the first host in
 *  the owner's order: the default *choice* of a picker, which the call then names. "" when the host has no id. */
export function hostIdByName(hosts: HostInfo[] | undefined, name?: string): string {
  const hs = hosts ?? [];
  return (name ? hs.find((h) => h.name === name) : hs[0])?.id ?? "";
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

/** V0.54: an update of Fyr is running on the host. */
export function updating(h: HostInfo): boolean {
  return h.online && !!h.deploy && BUSY.has(h.deploy.phase);
}

const UPDATE_WORDS: Record<string, string> = { build: "building the new version", test: "running the tests", drain: "waiting for its threads to finish their turn", switch: "restarting", client: "updating the web client" };

/** V0.125: where an update of Fyr on the host is, in the deploy row's words ("running the tests"), or "". */
export function updateWords(h: HostInfo): string {
  return updating(h) ? (UPDATE_WORDS[h.deploy!.phase] ?? "") : "";
}

/** V0.125: "online since 08:12", "online since 08:12 · updating Fyr", "offline since Mon 19:40". */
export function hostState(h: HostInfo, now: number): string {
  const at = dayTime(h.since, now);
  const s = `${h.online ? "online" : "offline"}${at ? ` since ${at}` : ""}`;
  return updating(h) ? `${s} · updating Fyr` : s;
}

export type Route = "this machine" | "tailnet" | "LAN" | "by address" | "";

function ipv4(host: string): number[] | null {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(host);
  if (!m) return null;
  const b = m.slice(1).map(Number);
  return b.every((x) => x <= 255) ? b : null;
}

/** V0.125: the network that carries a link, from the URL the front dials. A host without a URL (a front older than
 *  V0.125) has a route only when the front found it, on the tailnet. */
export function routeOf(url: string | undefined, found = false): Route {
  if (!url) return found ? "tailnet" : "";
  let host: string;
  try {
    host = new URL(url).hostname.toLowerCase();
  } catch {
    return "";
  }
  if (!host) return "";
  const v6 = host.startsWith("[") ? host.slice(1, -1) : null;
  if (v6 !== null) {
    if (v6 === "::1") return "this machine";
    if (/^f[cd]/.test(v6) || /^fe[89ab]/.test(v6)) return "LAN";
    return "by address";
  }
  const b = ipv4(host);
  if (b) {
    if (b[0] === 127) return "this machine";
    if (b[0] === 100 && b[1] >= 64 && b[1] <= 127) return "tailnet";
    if (b[0] === 10 || (b[0] === 172 && b[1] >= 16 && b[1] <= 31) || (b[0] === 192 && b[1] === 168) || (b[0] === 169 && b[1] === 254)) return "LAN";
    return "by address";
  }
  if (host === "localhost" || host.endsWith(".localhost")) return "this machine";
  if (host.endsWith(".ts.net")) return "tailnet";
  if (host.endsWith(".local") || host.endsWith(".lan") || host.endsWith(".home.arpa") || !host.includes(".")) return "LAN";
  return "by address";
}

/** V0.124: the short form of an install id, its first group ("F7QK"). */
export function shortID(id: string | undefined): string {
  return (id ?? "").split("-")[0];
}

/** V0.124, V0.125: a host now reports another install id than the one the front first saw under its name: the line
 *  that says so, or "". What to do about it is on the host's page. */
export function idChangedLine(h: HostInfo): string {
  if (!h.idWas || !h.id) return "";
  return `Another install answers as ${h.name}: id ${shortID(h.id)}, it was ${shortID(h.idWas)}.`;
}

/** V0.125: a host row's line in Settings → Hosts: "online since 08:12 · tailnet · id F7QK"; a changed id replaces it. */
export function hostLine(h: HostInfo, now: number): string {
  const changed = idChangedLine(h);
  if (changed) return changed;
  const route = hostRoute(h);
  return [hostState(h, now), updateLine(h).text, route, h.id ? `id ${shortID(h.id)}` : "no id"].filter(Boolean).join(" · ");
}

/** V0.125: the network a host's link runs over. Decision 0031 2.6: the host in the front's own process is the one whose
 *  link is the pipe, a fact about the link; no host is "this install". */
export function hostRoute(h: Pick<HostInfo, "url" | "found" | "link">): Route {
  return h.link?.via === "pipe" ? "this machine" : routeOf(h.url, h.found);
}

/** V0.125: how this browser reaches this Fyr, from the page's host name and the state's user (This device). */
export function howIn(hostname: string, user: string): string {
  const h = hostname.toLowerCase();
  if (h.endsWith(".ts.net")) return `Through Tailscale Serve, as ${user || "your login"}. Any browser signed in to your tailnet with that login can open this Fyr.`;
  if (routeOf(`http://${h.includes(":") && !h.startsWith("[") ? `[${h}]` : h}/`) === "this machine") return "On this machine itself (localhost). A browser on this machine is always allowed.";
  return `At ${hostname}${user ? `, as ${user}` : ""}. What stands in front of Fyr there decides who gets in.`;
}

/** V0.132: the hosts New project and New thread offer, in the owner's order (the first is the default); an offline one
 *  is named so. None when the front has one host (nothing to pick). Decision 0031 2.6: the same through every front. */
export function hostChoices(hosts: Pick<HostInfo, "name" | "online">[] | undefined): { name: string; label: string; online: boolean }[] {
  const hs = hosts ?? [];
  if (hs.length <= 1) return [];
  return hs.map((h) => ({ name: h.name, label: h.online ? h.name : `${h.name} (offline)`, online: h.online }));
}

/** V0.132: why nothing can be made on a picked host now; "" when it is online (or none is picked). Nothing falls back
 *  to another host. */
export function hostOfflineNote(name: string | undefined, hosts: Pick<HostInfo, "name" | "online" | "since">[] | undefined, now: number): string {
  const h = name ? hosts?.find((x) => x.name === name) : undefined;
  if (!h || h.online) return "";
  const at = dayTime(h.since, now);
  return `${h.name} is offline${at ? ` since ${at}` : ""}. Its folders cannot be read and nothing can be made on it until it is back; pick another host or wait.`;
}

/** V0.139: the host a project or thread lives on: its own, else the first in the owner's order. */
export function hostOf(r: { host?: string }, hosts: HostInfo[] | undefined): string | undefined {
  return hostForProject(r.host, hosts);
}

/** V0.139: the providers of a host (a project's, a thread's): that host's, else the merged list (one host). */
export const profilesOfHost = profilesOn;

/** V0.206: the provider a thread runs on, as its header and its inbox row show it. The id is looked up on the thread's
 *  host (the same id on another host is another provider); one no longer configured shows its id. */
export function threadProvider(t: { host?: string; profile: string }, hosts: HostInfo[] | undefined, all: Profile[]): { name: string; color?: string; title: string } {
  const p = profilesOfHost(hostOf(t, hosts), hosts, all).find((x) => x.id === t.profile);
  const name = p?.name ?? t.profile;
  return { name, color: p?.color || undefined, title: p?.unavailable ? `Provider ${name} · ${p.unavailable}` : `Provider ${name}` };
}

/** V0.206: the provider for an inbox row: null while every provider has the one name, as a row shows no host with one
 *  host. Names count, not ids: "Claude" on two hosts tells the rows nothing. */
export function rowProvider(t: { host?: string; profile: string }, hosts: HostInfo[] | undefined, all: Profile[]): ReturnType<typeof threadProvider> | null {
  const me = threadProvider(t, hosts, all);
  if (!me.name) return null;
  const names = new Set([me.name, ...all.map((x) => x.name), ...(hosts ?? []).flatMap((h) => (h.profiles ?? []).map((x) => x.name))]);
  return names.size > 1 ? me : null;
}

/** V0.139: the read-only host chip of an existing project or thread, in the same place on every screen: its name, and
 *  "offline since 19:40 · read-only" when it is. null with one host (nothing to say). */
export function hostChip(name: string | undefined, hosts: Pick<HostInfo, "name" | "online" | "since">[] | undefined, now: number): { name: string; line: string; offline: boolean } | null {
  if (!hosts || hosts.length < 2) return null;
  const n = name || hosts[0].name;
  const h = hosts.find((x) => x.name === n);
  if (!h || h.online) return { name: n, line: "", offline: false };
  const at = dayTime(h.since, now);
  return { name: n, line: `offline${at ? ` since ${at}` : ""} · read-only`, offline: true };
}
