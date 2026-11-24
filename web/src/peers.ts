// SPEC V0.159, decision 0030 3.6: Settings → Hosts, the installs this one is paired with, the tailnet nodes that are
// not (and why), and the owner's invite. Pure: the words; the panel is peers-ui.tsx.

import { linkLine, type HostLink } from "./hosts.ts";

interface MemberRow {
  id: string;
  name: string;
  self?: boolean;
  port?: number;
  /** this install may give this host commands (the host's own switch, 3.7) */
  drive: boolean;
  /** "paired": this install signed its add; "vouched": a member did */
  via: "paired" | "vouched";
  by?: string;
  /** the state of the link to it (decision 0030 Amendment 1); missing for this install */
  link?: HostLink & { opened?: string };
}

interface CandidateRow {
  name: string;
  online: boolean;
  kind: "tagged" | "other-user" | "shared" | "expired" | "waiting" | "lan" | string;
  /** #259, a LAN candidate: the id it claims, where it was heard (every address), who found it */
  id?: string;
  addr?: string;
  addrs?: string[];
  dup?: boolean;
  source?: "mdns" | "hand" | string;
  /** "address:<name>:<id>" (heard at a member's address) or "name:<name>:<id>" (goes by its name) */
  spoof?: string;
}

/** #259: a found install the owner ignored on this host; addr while it is heard. */
export interface IgnoredRow {
  id: string;
  at: string;
  addr?: string;
}

export interface PeerState {
  members: MemberRow[];
  candidates: CandidateRow[];
  ignored?: IgnoredRow[];
  whoisLinkUses?: number;
  inviteUntil?: string;
  signedIn?: boolean;
  /** V0.180: set while /etc/fyr/config.json differs from what fyrd read at start */
  configChanged?: string;
}

export interface InviteInfo {
  code: string;
  until: string;
  link: string;
  svg?: string;
}

/** V0.180 (#264): the config file changed since fyrd read it; null while it has not. */
export function configNote(s: Pick<PeerState, "configChanged"> | null): { title: string; how: string } | null {
  if (!s?.configChanged) return null;
  const t = s.configChanged;
  return {
    title: t.charAt(0).toUpperCase() + t.slice(1),
    how: "Fyr reads its config file at start. On NixOS, systemctl reload fyr restarts Fyr on the new file and the agents keep running.",
  };
}

/** The member's link in a few words, "" for this install. */
export function memberLink(m: Pick<MemberRow, "self" | "link">, now: number): string {
  return m.self || !m.link ? "" : linkLine({ ...m.link, via: m.link.via || "peer" }, now);
}

/** How an install came into the roster, in a few words. */
export function viaWords(m: Pick<MemberRow, "self" | "via" | "by">): string {
  if (m.self) return "This host";
  return m.via === "vouched" ? `Vouched for by ${m.by ?? "a member"}` : "Paired";
}

/** Why a tailnet node is not paired, and what to do. */
export function candidateWords(c: Pick<CandidateRow, "kind" | "online" | "id" | "addr" | "addrs" | "source">): string {
  const off = c.online ? "" : " · offline";
  switch (c.kind) {
    case "tagged":
      return `A tagged node: pair it with an invite${off}`;
    case "other-user":
      return `Another tailnet user's${off}`;
    case "shared":
      return `Shared in from another tailnet${off}`;
    case "expired":
      return `Its key has expired${off}`;
    case "waiting":
      return `Pairs by itself once its Serve and peer port are up${off}`;
    case "lan":
      return [c.id ?? "", lanAddrs(c), foundBy(c.source)].filter(Boolean).join(" · ") + off;
    default:
      return c.kind + off;
  }
}

function lanAddrs(c: Pick<CandidateRow, "addr" | "addrs">): string {
  return (c.addrs?.length ? c.addrs : c.addr ? [c.addr] : []).join(", ");
}

/** #259: who found a LAN candidate (tailnet discovery was retired, #360). */
export function foundBy(source: string | undefined): string {
  if (source === "hand") return "added by hand";
  return source === "mdns" || !source ? "found by mDNS" : `found by ${source}`;
}

/** #259: the warning line of a candidate that claims a paired install's address or name, or one id
 * heard at two addresses; "" for none. Pair asks for the other install's code either way. */
export function candidateWarn(c: Pick<CandidateRow, "spoof" | "dup" | "addrs">): string {
  const [how, name, id] = (c.spoof ?? "").split(":");
  const either = "Pair asks for its code either way.";
  if (how === "address") return `This address is ${name}'s (${(id ?? "").slice(0, 4)}). Pair it only if you reinstalled ${name}. ${either}`;
  if (how === "name") return `You already have a host named ${name} with another id (${(id ?? "").slice(0, 4)}). ${either}`;
  if (c.dup) return `One id at ${c.addrs?.length ?? 2} addresses: one host on two networks, or two hosts with one id. ${either}`;
  return "";
}

/** #259: Pair on a spoof is never one press: it asks first. */
export function pairAsks(c: Pick<CandidateRow, "spoof">): boolean {
  return !!c.spoof;
}

/** #259: the install id an invite names ("" for no invite); an invite carries its maker's id. */
export function inviteId(code: string): string {
  const m = /^FYR1-([0-9A-Z]{4}-[0-9A-Z]{4}-[0-9A-Z]{4}-[0-9A-Z]{4})-/.exec(typedInvite(code));
  return m ? m[1] : "";
}

/** Whether a candidate can be paired with an invite from here: the ones a tag or a missing Serve keep from pairing. */
export function needsInvite(c: Pick<CandidateRow, "kind">): boolean {
  return c.kind === "tagged";
}

/** The other installs only, in the order the roster lists them. */
export function others(s: PeerState | null): MemberRow[] {
  return (s?.members ?? []).filter((m) => !m.self);
}

/** An invite as typed: spaces out, upper case. */
export function typedInvite(s: string): string {
  return s.replace(/\s+/g, "").toUpperCase();
}

/** The invite in a page's address (`?pair=fyr1-…`), or "". */
export function inviteFromUrl(search: string): string {
  const v = new URLSearchParams(search).get("pair") ?? "";
  return /^fyr1-/i.test(v) ? typedInvite(v) : "";
}
