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
  kind: string;
  windows?: QuotaWindow[];
  balance?: QuotaBalance;
  updatedAt?: string;
  stale?: boolean;
  error?: string;
  retryAt?: string;
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
  return q.error ? `${name}: ${q.error}` : `${name}: not read yet`;
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
  if (q.updatedAt) lines.push(`Updated ${relTime(q.updatedAt, now)} ago`);
  if (q.stale) lines.push("Stale: showing the last data read");
  if (q.error) lines.push(q.error);
  if (q.retryAt) lines.push(`Next try ${untilLabel(q.retryAt, now)}`);
  return lines;
}

export function quotaFor(list: Quota[], profile: string): Quota | undefined {
  return list.find((q) => q.profile === profile);
}
