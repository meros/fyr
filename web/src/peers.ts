// SPEC V0.159, decision 0030 3.6: Settings → Hosts, the installs this one is paired with, the tailnet nodes that are
// not (and why), and the owner's invite. Pure: the words; the panel is peers-ui.tsx.

import { linkLine, type HostLink } from "./hosts.ts";

export interface MemberRow {
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

export interface CandidateRow {
  name: string;
  online: boolean;
  kind: "tagged" | "other-user" | "shared" | "expired" | "waiting" | string;
}

export interface PeerState {
  members: MemberRow[];
  candidates: CandidateRow[];
  whoisLinkUses?: number;
  inviteUntil?: string;
  signedIn?: boolean;
}

export interface InviteInfo {
  code: string;
  until: string;
  link: string;
  svg?: string;
}

/** The member's link in a few words, "" for this install. */
export function memberLink(m: Pick<MemberRow, "self" | "link">, now: number): string {
  return m.self || !m.link ? "" : linkLine({ ...m.link, via: m.link.via || "peer" }, now);
}

/** How an install came into the roster, in a few words. */
export function viaWords(m: Pick<MemberRow, "self" | "via" | "by">): string {
  if (m.self) return "This install";
  return m.via === "vouched" ? `Vouched for by ${m.by ?? "a member"}` : "Paired";
}

/** Why a tailnet node is not paired, and what to do. */
export function candidateWords(c: Pick<CandidateRow, "kind" | "online">): string {
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
    default:
      return c.kind + off;
  }
}

/** Whether a candidate can be paired with an invite from here: the ones a tag or a missing Serve keep from pairing. */
export function needsInvite(c: Pick<CandidateRow, "kind">): boolean {
  return c.kind === "tagged";
}

/** The other installs only, in the order the roster lists them. */
export function others(s: PeerState | null): MemberRow[] {
  return (s?.members ?? []).filter((m) => !m.self);
}

/** The Hosts nav line's tail: how many installs are paired, "" when none. */
export function pairedSummary(s: PeerState | null): string {
  const n = others(s).length;
  return n ? `${n} paired` : "";
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
