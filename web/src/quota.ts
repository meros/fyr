// SPEC 0.5 V0.44: provider quota per profile, as words. Pure.
import { relTime, untilLabel } from "./format.ts";

export interface QuotaWindow {
  name: string;
  usedPct: number;
  resetsAt?: string;
}
export interface QuotaBalance {
  amount?: number;
  spent?: number;
  currency: string;
}
export interface Quota {
  profile: string;
  /** V0.127: the host of the provider (the front stamps it); the same id on two hosts is two quotas */
  host?: string;
  kind: string;
  windows?: QuotaWindow[];
  balance?: QuotaBalance;
  updatedAt?: string;
  stale?: boolean;
  error?: string;
  retryAt?: string;
  /** V0.122: "session" when the windows come only from the last session's rate_limit_event (a token account). */
  source?: "session";
}

/** A window at or above this share is coloured; nothing else is. */
export const HOT = 80;

export interface QuotaPart {
  text: string;
  hot: boolean;
}

export function pctText(p: number): string {
  return `${Math.round(p)} %`;
}

const SYMBOL: Record<string, string> = { USD: "$", CNY: "¥", EUR: "€", GBP: "£" };

/** "$12.40", "¥12.40", "12.40 SEK". */
export function money(amount: number, currency: string): string {
  const n = amount.toFixed(2);
  const s = SYMBOL[currency.toUpperCase()];
  return s ? `${s}${n}` : `${n} ${currency}`.trim();
}

export function hasData(q: Quota): boolean {
  return !!(q.windows?.length || q.balance);
}

/** The pieces after the profile name: "5 h 34 %", "7 d 61 %", or "¥12.40 left". */
export function quotaParts(q: Quota): QuotaPart[] {
  const out: QuotaPart[] = (q.windows ?? []).map((w) => ({ text: `${w.name} ${pctText(w.usedPct)}`, hot: w.usedPct >= HOT }));
  const b = q.balance;
  if (b?.amount !== undefined) out.push({ text: `${money(b.amount, b.currency)} left`, hot: false });
  else if (b?.spent !== undefined) out.push({ text: `${money(b.spent, b.currency)} used`, hot: false });
  return out;
}

/** "Claude 5 h 34 % · 7 d 61 %"; without data, the error ("DeepSeek: No API key…"). */
export function quotaSummary(q: Quota, name: string): string {
  const parts = quotaParts(q);
  if (parts.length) return `${name} ${parts.map((p) => p.text).join(" · ")}`;
  if (q.error) return `${name}: ${q.error}`;
  return q.source === "session" ? `${name}: usage shows after the next run` : `${name}: not read yet`;
}

/** The rail's one short figure: the busiest window ("61%") or the balance ("¥12"). */
export function railFigure(q: Quota): QuotaPart | null {
  const ws = q.windows ?? [];
  if (ws.length) {
    const top = Math.max(...ws.map((w) => w.usedPct));
    return { text: `${Math.round(top)}%`, hot: top >= HOT };
  }
  const b = q.balance;
  if (b?.amount !== undefined) return { text: money(Math.floor(b.amount), b.currency).replace(/\.00( |$)/, "$1"), hot: false };
  return null;
}

/** V0.152: each provider's initial on the rail: the first letter of its name, upper case. Where two different names
 *  share it, the first letter of a later word that no other name starts with ("Claude", "Claude (work account)" → "C",
 *  "W"), else the shortest start of the name that tells it apart ("Codex", "Claude" → "Co", "Cl"). The same name keeps
 *  the same initial (one provider on two hosts). */
export function railInitials(names: string[]): string[] {
  const words = (n: string) => n.match(/[\p{L}\p{N}]+/gu) ?? ["?"];
  const up = (w: string) => w[0].toUpperCase() + w.slice(1).toLowerCase();
  const lead = (n: string) => up(words(n)[0][0]);
  const distinct = [...new Set(names)];
  return names.map((n) => {
    const others = distinct.filter((m) => m !== n && lead(m) === lead(n));
    if (!others.length) return lead(n);
    const taken = new Set(distinct.filter((m) => m !== n).map(lead));
    const later = words(n).slice(1).find((w) => !taken.has(up(w[0])));
    if (later) return up(later[0]);
    const w = words(n)[0];
    for (let k = 2; k <= w.length; k++) if (others.every((m) => !words(m)[0].toLowerCase().startsWith(w.slice(0, k).toLowerCase()))) return up(w.slice(0, k));
    return lead(n);
  });
}

/** The detail lines on hover or tap: windows with reset times in local time, then freshness and notes. */
export function quotaDetail(q: Quota, name: string, now: number): string[] {
  const lines: string[] = [];
  for (const w of q.windows ?? []) {
    const r = untilLabel(w.resetsAt, now);
    lines.push(`${name} ${w.name}: ${pctText(w.usedPct)} used${r ? `, resets ${r}` : ""}`);
  }
  const b = q.balance;
  if (b?.amount !== undefined) lines.push(`${name}: ${money(b.amount, b.currency)} left`);
  else if (b?.spent !== undefined) lines.push(`${name}: ${money(b.spent, b.currency)} used (no limit on the key)`);
  if (q.updatedAt) lines.push(q.source === "session" ? `From the last session, ${relTime(q.updatedAt, now)} ago` : `Updated ${relTime(q.updatedAt, now)} ago`);
  // V0.77 (1), #65: the error and its next try appear once in the detail, never again in the line above it.
  if (q.stale) lines.push("Stale: showing the last data read");
  if (q.error && hasData(q)) lines.push(q.error);
  if (q.retryAt) lines.push(`Next try ${untilLabel(q.retryAt, now)}`);
  return lines;
}

/** V0.77 (1), #65: what the card's badge says. "Ready" means nothing is wrong, so a limited, expired, stale or unread
 *  account never shows Ready beside its own error. */
/** V0.122: the badge names the error it has; only a rate limit says "Rate limited", and a token account waiting for
 *  its first session says so. */
export type CardState = "ready" | "limited" | "expired" | "notoken" | "error" | "stale" | "waiting" | "unknown";
export function cardState(q: Quota): CardState {
  if (q.error) {
    if (/rate limit/i.test(q.error)) return "limited";
    if (/no token/i.test(q.error)) return "notoken";
    if (/expired|login/i.test(q.error)) return "expired";
    return "error";
  }
  if (q.stale) return "stale";
  if (hasData(q)) return "ready";
  return q.source === "session" ? "waiting" : "unknown";
}

export const CARD_WORD: Record<CardState, string> = {
  ready: "Ready",
  limited: "Rate limited",
  expired: "Login expired",
  notoken: "No token",
  error: "Can't read",
  stale: "Stale",
  waiting: "After a run",
  unknown: "Not read yet",
};

export function quotaFor(list: Quota[], profile: string, host?: string): Quota | undefined {
  if (host) return list.find((q) => q.profile === profile && (!q.host || q.host === host)); // V0.127
  return list.find((q) => q.profile === profile);
}
