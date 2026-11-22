// SPEC 0.5 V0.44: provider quota per profile, as words. Pure.
import type { HostId } from "./hostid.ts";
import type { Card, CardRow } from "./card.ts";
import type { WakeUp } from "./types.ts";
import { relTime, untilLabel } from "./format.ts";

interface QuotaWindow {
  name: string;
  usedPct: number;
  resetsAt?: string;
}
interface QuotaBalance {
  amount?: number;
  spent?: number;
  currency: string;
}
export interface Quota {
  profile: string;
  /** V0.127: the host of the provider (the front stamps it); the same id on two hosts is two quotas */
  host?: string;
  /** Slice 2b: the install id of the host it was read from (api.ts): its provider's name is that host's */
  hostId?: HostId;
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
const HOT = 80;

interface QuotaPart {
  text: string;
  hot: boolean;
}

function pctText(p: number): string {
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

/** V0.quota-in-thread: one short figure, for a place with room for one (the phone's header): the busiest window's
 *  share ("61 %"), else the balance ("¥12.40 left"). null without data. */
export function busiestFigure(q: Quota): QuotaPart | null {
  const ws = q.windows ?? [];
  if (!ws.length) return quotaParts(q)[0] ?? null;
  const top = Math.max(...ws.map((w) => w.usedPct));
  return { text: pctText(top), hot: top >= HOT };
}

/** "5 h" → "5 hours", "7 d" → "7 days"; any other window name as the provider gives it. */
export function windowName(n: string): string {
  const m = /^(\d+) ?(h|d)$/.exec(n.trim());
  if (!m) return n;
  const k = Number(m[1]);
  return `${k} ${m[2] === "h" ? (k === 1 ? "hour" : "hours") : k === 1 ? "day" : "days"}`;
}

/** V0.hover-cards (amends V0.44's detail lines, V0.77 (1), V0.quota-in-thread): one quota as a card, on hover on the
 *  desktop and as text on the phone. A verdict (Plenty left, Running low, Used up, or the error's word), then each window
 *  with its share and reset time, the balance, the problem and its next try once (V0.77 (1)); how old the reading is
 *  last. */
export function quotaCard(q: Quota, name: string, now: number): Card {
  const ws = q.windows ?? [];
  const top = ws.length ? ws.reduce((a, b) => (b.usedPct > a.usedPct ? b : a)) : null;
  const card: Card = { head: name, sections: [] };
  if (!hasData(q)) {
    const st = cardState(q);
    card.verdict = CARD_WORD[st];
    card.level = q.error ? "hot" : "calm";
    if (!q.error) card.summary = q.source === "session" ? "Usage shows after the next run on this account." : "Fyr has not read this account's usage yet.";
  } else if (top && top.usedPct >= 100) {
    card.verdict = "Used up";
    card.level = "hot";
    const r = untilLabel(top.resetsAt, now);
    card.summary = `New runs on this account wait${r ? ` until ${r}` : " until the window resets"}.`;
  } else if (top && top.usedPct >= HOT) {
    card.verdict = "Running low";
    card.level = "warm";
    const r = untilLabel(top.resetsAt, now);
    card.summary = `The ${windowName(top.name)} window is ${pctText(top.usedPct)} used${r ? `; it resets ${r}` : ""}.`;
  } else {
    card.verdict = ws.length ? "Plenty left" : "Ready";
    card.level = "calm";
  }
  const rows: CardRow[] = ws.map((w) => {
    const r = untilLabel(w.resetsAt, now);
    return { label: windowName(w.name), value: `${pctText(w.usedPct)} used`, note: r ? `resets ${r}` : undefined, hot: w.usedPct >= HOT };
  });
  const b = q.balance;
  if (b?.amount !== undefined) rows.push({ label: "Balance", value: `${money(b.amount, b.currency)} left` });
  else if (b?.spent !== undefined) rows.push({ label: "Spent", value: money(b.spent, b.currency), note: "the key has no limit" });
  if (q.error) rows.push({ label: "Problem", value: q.error, note: q.retryAt ? `next try ${untilLabel(q.retryAt, now)}` : undefined, hot: true });
  else if (q.retryAt) rows.push({ label: "Next try", value: untilLabel(q.retryAt, now) });
  if (rows.length) card.sections.push({ rows });
  if (q.updatedAt) {
    const age = q.source === "session" ? `From the last run, ${relTime(q.updatedAt, now)} ago` : `Read ${relTime(q.updatedAt, now)} ago`;
    card.source = q.stale ? `${age} · may be out of date` : age;
  } else if (q.stale) card.source = "May be out of date";
  return card;
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

/** V0.quota-reset-wake: one window's wake-on-reset control, shared by the desktop quota popover and phone Details. */
interface QuotaWakeOption {
  window: string;
  resetsAt?: string;
  when: string;
  armedRule?: string;
  disabledReason?: string;
}

/** Per-window wake controls for a provider's quota windows. Never invents a reset time: stale readings and windows
 *  without a future reset time (including token accounts read only from sessions) carry the reason they are off. */
export function quotaWakeOptions(q: Quota, wakes: WakeUp[] | undefined, now: number, unavailable?: string): QuotaWakeOption[] {
  const ws = q.windows ?? [];
  return ws.map((w) => {
    const armed = wakes?.find((x) => x.window === w.name && (!x.provider || x.provider === q.profile));
    const when = untilLabel(w.resetsAt, now);
    if (armed) return { window: w.name, resetsAt: w.resetsAt, when, armedRule: armed.rule };
    let disabledReason = unavailable;
    if (!disabledReason && q.stale) disabledReason = "Quota reading is stale; wait for a fresh reading";
    if (!disabledReason && !w.resetsAt) disabledReason = q.source === "session" ? "No reset time known (token account read only from sessions)" : "No reset time known";
    if (!disabledReason && w.resetsAt) {
      const ms = Date.parse(w.resetsAt);
      if (Number.isNaN(ms) || ms <= now) disabledReason = "No future reset time";
    }
    return { window: w.name, resetsAt: w.resetsAt, when, disabledReason };
  });
}
