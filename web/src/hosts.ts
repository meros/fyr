// SPEC 0.5 V0.51 (decision 0016): a front lists its hosts; a thread on a host the front cannot reach is shown
// from the front's cache, marked offline and read-only.
import { clockTime, dayTime } from "./format.ts";
import type { Profile } from "./types.ts";
import type { DeployProgress } from "./deploy.ts";
import { channelLabel } from "./updates.ts";
import { asHostId, type HostId } from "./hostid.ts";

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
  id?: HostId;
  /** V0.124: the id the front first saw under this name, when the host now reports another */
  idWas?: HostId;
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
interface HostUpdate {
  running?: string;
  build?: string;
  wanted?: string;
  /** commits the wanted release is ahead of the running one; -1 not known */
  behind: number;
  last?: { build: string; commit?: string; result: string; reason?: string; startedAt: string; endedAt?: string; local?: boolean };
  bad?: { commit: string; why: string; at?: string };
  paused?: boolean;
  /** V0.versioned-releases: the running build's vX.Y.Z (empty on main) and the channel it follows */
  version?: string;
  channel?: string;
  /** V0.update-now-fresh: when the release that waits out its quiet period installs by itself; missing when none waits */
  due?: string;
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
  "key-mismatch": "another host answered than the one that is paired",
  "not-a-member": "it does not have this host as a member yet",
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
      ...(asHostId(h.id) ? { id: asHostId(h.id) } : {}),
      ...(asHostId(h.idWas) ? { idWas: asHostId(h.idWas) } : {}),
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
    ...(str(u.version) ? { version: str(u.version) } : {}),
    ...(str(u.channel) ? { channel: str(u.channel) } : {}),
    ...(str(u.due) ? { due: str(u.due) } : {}),
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

/** V0.settings-ia: what is wrong with a host's update that the owner can press on, read ONE way by the count, the
 *  Hosts list's chip, Automation and the host's page. The host's own summary says it when the host sends one (it is
 *  live, with the state); a host that sends none is read from its settings (the updates view's `bad`). null: nothing. */
export function updateTrouble(h: Pick<HostInfo, "update"> | undefined, view?: { bad?: { commit: string; why: string } } | null): { chip: "Update held back" | "Update failed"; held?: { commit: string; why: string; at?: string } } | null {
  const u = h?.update;
  if (u) {
    if (u.bad) return { chip: "Update held back", held: u.bad };
    const lastBad = u.last ? LAST_BAD[u.last.result] : undefined;
    return lastBad && u.behind !== 0 ? { chip: "Update failed" } : null;
  }
  return view?.bad?.commit ? { chip: "Update held back", held: view.bad } : null;
}

/** V0.settings-ia: the one row of a host's Updates that asks for a press, first in the group: what is wrong or
 *  waiting, why, and the button's word. null when there is nothing to install, retry or go back to. A host that sends
 *  no summary cannot say how far behind it is, so the press is offered. `chip` is the amber word of a counted row
 *  (updateTrouble); the caller shows it and the button only while the host is online. */
export function updateAction(h: Pick<HostInfo, "update">, view: { bad?: { commit: string; why: string }; paused?: unknown } | null | undefined, now: number): { title: string; desc: string; button: "Try again" | "Update now" | "Back to release"; chip?: string } | null {
  const u = h.update;
  const t = updateTrouble(h, view);
  if (t?.held) {
    const at = t.held.at ? dayTime(t.held.at, now) : "";
    return { title: `Release ${short7(t.held.commit)} is held back`, desc: `${t.held.why ? `Reason: ${t.held.why}${at ? ` (${at})` : ""}. ` : ""}Fyr skips it until a newer release; Try again installs it now.`, button: "Try again", chip: t.chip };
  }
  if (t) return { title: `The ${LAST_BAD[u!.last!.result]}`, desc: `${u!.last!.reason ? `Reason: ${u!.last!.reason}. ` : ""}This host is still behind the newest release.`, button: "Update now", chip: t.chip };
  if (u ? u.paused : view?.paused) return { title: "A local build runs", desc: "Auto-update waits until a release contains it, or until you go back to the release.", button: "Back to release" };
  // V0.update-now-fresh: Update now reads the newest green main at once and installs it without the quiet period, so it
  // is offered while a release waits (with the time it would install by itself) and also when the host thinks it is up
  // to date: it reads `deployable` only hourly, so a release CI published since may not be known here yet.
  if (u) {
    if (u.behind === 0) return { title: "Up to date as of the last read", desc: "Fyr reads the newest release every hour; Update now reads it again and installs it at once if it is newer.", button: "Update now" };
    const due = u.due && Date.parse(u.due) > now ? clockTime(u.due) : "";
    return {
      title: u.behind > 0 ? `${u.behind} commit${u.behind === 1 ? "" : "s"} behind the newest release` : "A newer release may be ready",
      desc: due ? `With Auto-update on, Fyr installs it by itself at ${due}, once the release has stayed the newest for its quiet period; Update now does not wait.` : "Fyr installs it by itself when Auto-update is on; Update now does not wait.",
      button: "Update now",
    };
  }
  return view ? { title: "Install the newest release now", desc: "This host does not say how far behind it is.", button: "Update now" } : null;
}

const RESULT_WORDS: Record<string, string> = { inProgress: "in progress", installed: "installed", failed: "failed", rolledBack: "rolled back", replaced: "replaced" };

/** V0.host-updates: the rows of a host's Updates group on its page: what runs, the newest release, the last update. */
export function updateRows(u: HostUpdate, now: number): { title: string; desc: string; bad?: boolean }[] {
  const rows: { title: string; desc: string; bad?: boolean }[] = [];
  // V0.versioned-releases: the version first, the channel after the build.
  rows.push({ title: "Runs", desc: [u.version ?? "", u.running ? short7(u.running) : "", u.build && !u.build.startsWith(short7(u.running)) ? `build ${u.build}` : "", channelLabel(u.channel), u.paused ? "a local build; auto-update waits for a release that contains it" : ""].filter(Boolean).join(" · ") || "Not known" });
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

/** "host-a offline since 19:40 · read-only", or "" while the thread's host is reachable. */
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

type Route = "this machine" | "this host" | "tailnet" | "LAN" | "by address" | "";

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
  return `Another host answers as ${h.name}: id ${shortID(h.id)}, it was ${shortID(h.idWas)}.`;
}

/** V0.125: a host row's line in Settings → Hosts: "online since 08:12 · tailnet · id F7QK"; a changed id replaces it.
 *  `stale`: this device has no connection, so the state is "as last read". */
export function hostLine(h: HostInfo, now: number, stale = false): string {
  const changed = idChangedLine(h);
  if (changed) return changed;
  const route = hostRoute(h);
  // V0.host-ids: a host with no install id cannot be named by any call until it updates.
  return [stale ? "as last read" : hostState(h, now), updateLine(h).text, route, h.id ? `id ${shortID(h.id)}` : "needs an update"].filter(Boolean).join(" · ");
}

/** V0.125: the network a host's link runs over. Decision 0031 2.6: the host in the front's own process is the one whose
 *  link is the pipe, a fact about the link; no host is "this install". V0.settings-ia: that host, and one dialled on
 *  the loopback, is "this host" (the one this device is connected to); "this machine" read as the phone in the hand. */
export function hostRoute(h: Pick<HostInfo, "url" | "found" | "link">): Route {
  const r = h.link?.via === "pipe" ? "this machine" : routeOf(h.url, h.found);
  return r === "this machine" ? "this host" : r;
}

/** V0.125: how this browser reaches this Fyr, from the page's host name and the state's user (This device). */
export function howIn(hostname: string, user: string): string {
  const h = hostname.toLowerCase();
  if (h.endsWith(".ts.net")) return `Through Tailscale Serve, as ${user || "your login"}. Any device signed in to your tailnet with that login can open this Fyr.`;
  if (routeOf(`http://${h.includes(":") && !h.startsWith("[") ? `[${h}]` : h}/`) === "this machine") return "This device is the host itself (localhost): it is always allowed.";
  return `At ${hostname}${user ? `, as ${user}` : ""}. What stands in front of Fyr there decides who gets in.`;
}

/** V0.132: the hosts New project and New thread offer, in the owner's order; an offline one is named so. None when the
 *  front has one host (nothing to pick). Decision 0031 2.6: the same through every front. Slice 2b: a host is picked by
 *  its install id, and one without an id (older than V0.124) cannot be addressed, so it is not offered. */
export function hostChoices(hosts: readonly Pick<HostInfo, "id" | "name" | "online">[] | undefined): { id: HostId; name: string; label: string; online: boolean }[] {
  const hs = hosts ?? [];
  if (hs.length <= 1) return [];
  return hs.flatMap((h) => (h.id ? [{ id: h.id, name: h.name, label: h.online ? h.name : `${h.name} (offline)`, online: h.online }] : []));
}

/** Slice 2b: where a host picker starts: the only host when there is one (no picker shows), else the first choice in
 *  the owner's order, shown in the picker, so the call names the host the owner sees. None when no host can be named. */
export function pickerStart(hosts: readonly Pick<HostInfo, "id" | "name" | "online">[] | undefined): HostId | undefined {
  const hs = hosts ?? [];
  return hs.length === 1 ? hs[0].id : hostChoices(hs)[0]?.id;
}

/** V0.132: why nothing can be made on a picked host now; "" when it is online. Nothing falls back to another host:
 *  with no host picked, or one the front no longer lists, it says so. */
export function hostOfflineNote(id: HostId | undefined, hosts: readonly Pick<HostInfo, "id" | "name" | "online" | "since">[] | undefined, now: number): string {
  if (!id) return "Pick the host it starts on.";
  const h = hosts?.find((x) => x.id === id);
  if (!h) return "That host is no longer one of this Fyr's hosts; pick another.";
  if (h.online) return "";
  const at = dayTime(h.since, now);
  return `${h.name} is offline${at ? ` since ${at}` : ""}. Its folders cannot be read and nothing can be made on it until it is back; pick another host or wait.`;
}

/** Slice 2b: each host's own profiles, by install id (store.ts `profilesByHost`). A record's profile is looked up on
 *  its own host; there is no merged list. */
export type HostProfiles = Readonly<Record<string, Profile[]>>;

/** The profiles of a record's host; none when its host is not known. */
export function profilesOf(r: { hostId?: HostId }, byHost: HostProfiles): Profile[] {
  return (r.hostId && byHost[r.hostId]) || [];
}

/** V0.206: the provider a thread runs on, as its header and its inbox row show it. The id is looked up on the thread's
 *  host (the same id on another host is another provider); one no longer configured shows its id. */
export function threadProvider(t: { hostId?: HostId; profile: string }, byHost: HostProfiles): { name: string; color?: string; title: string } {
  const p = profilesOf(t, byHost).find((x) => x.id === t.profile);
  const name = p?.name ?? t.profile;
  return { name, color: p?.color || undefined, title: p?.unavailable ? `Provider ${name} · ${p.unavailable}` : `Provider ${name}` };
}

/** V0.206: the provider for an inbox row: null while every provider has the one name, as a row shows no host with one
 *  host. Names count, not ids: "Claude" on two hosts tells the rows nothing. */
export function rowProvider(t: { hostId?: HostId; profile: string }, byHost: HostProfiles): ReturnType<typeof threadProvider> | null {
  const me = threadProvider(t, byHost);
  if (!me.name) return null;
  const names = new Set([me.name, ...Object.values(byHost).flatMap((ps) => ps.map((x) => x.name))]);
  return names.size > 1 ? me : null;
}

/** V0.139: the read-only host chip of an existing project or thread, in the same place on every screen: its name, and
 *  "offline since 19:40 · read-only" when it is. null with one host (nothing to say). Slice 2b: the record's own host,
 *  by its install id; a record whose host the front does not list says so, it is not taken for the first host's. */
export function hostChip(id: HostId | undefined, hosts: readonly Pick<HostInfo, "id" | "name" | "online" | "since">[] | undefined, now: number): { name: string; line: string; offline: boolean } | null {
  if (!hosts || hosts.length < 2) return null;
  const h = id ? hosts.find((x) => x.id === id) : undefined;
  if (!h) return { name: "Unknown host", line: "not one of this Fyr's hosts · read-only", offline: true };
  const n = h.name;
  if (h.online) return { name: n, line: "", offline: false };
  const at = dayTime(h.since, now);
  return { name: n, line: `offline${at ? ` since ${at}` : ""} · read-only`, offline: true };
}
