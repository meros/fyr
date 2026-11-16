// Quiet hours per project (SPEC 0.5 V0.46, decision 0014). The host decides when a project is quiet and sends
// quietUntil on the project and on each of its threads; the client only hides and re-renders on time.
import type { Project, QuietHours, Thread } from "./types.ts";

export const WEEK: { key: string; label: string }[] = [
  { key: "mon", label: "Mon" },
  { key: "tue", label: "Tue" },
  { key: "wed", label: "Wed" },
  { key: "thu", label: "Thu" },
  { key: "fri", label: "Fri" },
  { key: "sat", label: "Sat" },
  { key: "sun", label: "Sun" },
];

/** The default window "+ Add quiet hours" adds: the owner's example. */
export const DEFAULT_QUIET: QuietHours = { from: "17:00", to: "06:00" };

/** Decision 0014 addendum: one-tap windows. Weeknights start Sunday to Friday, so with All weekend the quiet runs
 *  from Friday 17:00 to Monday 06:00 without a gap (days are the start days of a window). */
export const QUIET_PRESETS: { key: string; label: string; hint: string; window: QuietHours }[] = [
  { key: "weeknights", label: "Weeknights", hint: "17:00–06:00", window: { from: "17:00", to: "06:00", days: ["mon", "tue", "wed", "thu", "fri", "sun"] } },
  { key: "weekend", label: "All weekend", hint: "Sat–Sun", window: { from: "00:00", to: "00:00", days: ["sat", "sun"] } },
];

/** A project's quiet hours as a list: the list, the single object a host from before the addendum sends, or nothing. */
export function quietList(q: unknown): QuietHours[] {
  const ok = (w: unknown): w is QuietHours => !!w && typeof w === "object" && typeof (w as QuietHours).from === "string" && typeof (w as QuietHours).to === "string";
  if (Array.isArray(q)) return q.filter(ok);
  return ok(q) ? [q] : [];
}

/** A window in the shape the host stores: days left out when every day. */
export function cleanWindow(w: QuietHours): QuietHours {
  return { from: w.from, to: w.to, ...(w.days?.length && w.days.length < 7 ? { days: WEEK.map((d) => d.key).filter((k) => w.days!.includes(k)) } : {}) };
}

/** All day is from equal to to; the toggle writes 00:00–00:00 and, turned off, the default evening. */
export const isAllDay = (w: QuietHours) => w.from === w.to;
export function setAllDay(w: QuietHours, on: boolean): QuietHours {
  return on ? { ...w, from: "00:00", to: "00:00" } : { ...w, from: DEFAULT_QUIET.from, to: DEFAULT_QUIET.to };
}

/** Whether the list already holds a preset's window, so its chip can step aside. */
export function hasWindow(list: QuietHours[], w: QuietHours): boolean {
  const k = JSON.stringify(cleanWindow(w));
  return list.some((x) => JSON.stringify(cleanWindow(x)) === k);
}

function until(x: { quietUntil?: string } | undefined): number {
  const t = x?.quietUntil ? Date.parse(x.quietUntil) : NaN;
  return Number.isFinite(t) ? t : 0;
}

/** Whether a thread (or project) is in its quiet hours at now. */
export function isQuiet(x: { quietUntil?: string } | undefined, now: number): boolean {
  return until(x) > now;
}

/** The inbox's threads: quiet ones left out, unless show (the project filter picks a project, or a search is typed). */
export function hideQuiet(threads: Record<string, Thread>, show: boolean, now: number): Record<string, Thread> {
  if (show) return threads;
  let any = false;
  for (const t of Object.values(threads)) if (isQuiet(t, now)) any = true;
  if (!any) return threads;
  const out: Record<string, Thread> = {};
  for (const t of Object.values(threads)) if (!isQuiet(t, now)) out[t.id] = t;
  return out;
}

/** When the client must look again: the nearest quietUntil still ahead, or null. One timer, no polling. */
export function nextQuietEnd(threads: Record<string, Thread>, projects: Project[], now: number): number | null {
  let next: number | null = null;
  for (const x of [...Object.values(threads), ...projects]) {
    const u = until(x);
    if (u > now && (next === null || u < next)) next = u;
  }
  return next;
}

const hm = (d: Date) => `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;

/** "Quiet until 06:00" for the filter chip and line; the day is named when the end is more than a day away. */
export function quietNote(p: Pick<Project, "quietUntil"> | undefined, now: number): string | null {
  if (!isQuiet(p, now)) return null;
  const end = new Date(until(p));
  const day = end.getTime() - now > 24 * 3600_000 ? `${WEEK[(end.getDay() + 6) % 7].label} ` : "";
  return `Quiet until ${day}${hm(end)}`;
}

/** The chosen days in week order (Monday first) as indexes; [] when every day. */
function dayIdx(days: string[] | undefined): number[] {
  const idx = [...new Set((days ?? []).map((d) => WEEK.findIndex((w) => w.key === d)).filter((i) => i >= 0))].sort((a, b) => a - b);
  return idx.length === 7 ? [] : idx;
}

const same = (idx: number[], keys: string[]) => idx.length === keys.length && keys.every((k) => idx.includes(WEEK.findIndex((w) => w.key === k)));

/** "Mon–Fri", "Sun–Fri" (a run may wrap past Sunday), "Mon, Wed"; "" for every day. */
export function dayText(days: string[] | undefined): string {
  const idx = dayIdx(days);
  if (!idx.length) return "";
  if (idx.length > 1) {
    const set = new Set(idx);
    const start = idx.find((i) => !set.has((i + 6) % 7));
    if (start !== undefined && idx.every((_, k) => set.has((start + k) % 7)))
      return `${WEEK[start].label}–${WEEK[(start + idx.length - 1) % 7].label}`;
  }
  return idx.map((i) => WEEK[i].label).join(", ");
}

/** One window in plain words: "weeknights 17:00–06:00", "all weekend", "Mon, Wed 09:00–12:00". alone: the only
 *  window, which keeps the words of before the addendum ("from 17:00 to 06:00"). */
export function windowWords(w: QuietHours, alone = false): string {
  const idx = dayIdx(w.days);
  const days = dayText(w.days);
  if (isAllDay(w)) {
    if (same(idx, ["sat", "sun"])) return "all weekend";
    return days ? `all day ${days}` : "all day";
  }
  const span = `${w.from}–${w.to}`;
  const overnight = w.to <= w.from;
  if (!days) return alone ? `from ${w.from} to ${w.to}` : `daily ${span}`;
  if (overnight && [["mon", "tue", "wed", "thu", "fri"], ["sun", "mon", "tue", "wed", "thu", "fri"], ["sun", "mon", "tue", "wed", "thu"]].some((k) => same(idx, k))) return `weeknights ${span}`;
  if (same(idx, ["sat", "sun"])) return `weekends ${span}`;
  return `${days} ${span}`;
}

/** The project screen's one line, every window in plain words. */
export function quietLine(q: QuietHours[] | QuietHours): string {
  const list = quietList(q);
  if (!list.length) return "Off: threads show and notify as usual.";
  const words = list.map((w) => windowWords(w, list.length === 1));
  const all = words.length > 1 ? `${words.slice(0, -1).join(", ")} and ${words[words.length - 1]}` : words[0];
  return `Threads hide and send no notifications ${all}.`;
}

/** Toggle one day; days absent means every day. All seven become absent again; the last day cannot be turned off. */
export function toggleDay(days: string[] | undefined, day: string): string[] | undefined {
  const on = new Set(days?.length ? days : WEEK.map((w) => w.key));
  if (on.has(day)) {
    if (on.size === 1) return [...on];
    on.delete(day);
  } else on.add(day);
  const out = WEEK.map((w) => w.key).filter((k) => on.has(k));
  return out.length === 7 ? undefined : out;
}

// ---- V0.74: New thread leaves quiet projects out; a 30 s hold shows them for 30 minutes on this device ----

/** How long the owner holds the row, and how long quiet projects then show. The hold is slow on purpose. */
export const QUIET_HOLD_MS = 30_000;
export const QUIET_SHOW_MS = 30 * 60_000;
const SHOW_KEY = "fyr.quietShowUntil";

/** The projects New thread offers: quiet ones left out unless showUntil is ahead; keep (the picked one) stays. */
export function createProjects(projects: Project[], now: number, showUntil: number, keep?: string): Project[] {
  if (showUntil > now) return projects;
  return projects.filter((p) => !isQuiet(p, now) || (!!keep && p.id === keep));
}

type KV = Pick<Storage, "getItem" | "setItem" | "removeItem">;

/** The end of the unlock on this device, or 0. */
export function loadQuietShow(kv: KV | null): number {
  try {
    const t = Number(kv?.getItem(SHOW_KEY));
    return Number.isFinite(t) && t > 0 ? t : 0;
  } catch {
    return 0;
  }
}

/** from: start an unlock of QUIET_SHOW_MS (returns its end); null: end it (returns 0). */
export function saveQuietShow(kv: KV | null, from: number | null): number {
  const end = from === null ? 0 : from + QUIET_SHOW_MS;
  try {
    if (end) kv?.setItem(SHOW_KEY, String(end));
    else kv?.removeItem(SHOW_KEY);
  } catch {
    // private window or blocked storage: the unlock holds until the form closes
  }
  return end;
}

// ---- V0.114: every way into a quiet project goes through the same 30 s hold; one unlock for all of them ----

/** Whether x (a thread or a project) is quiet and the device has no unlock running: the paths into it are shut. */
export function quietLocked(x: { quietUntil?: string } | undefined, now: number, showUntil: number): boolean {
  return isQuiet(x, now) && !(showUntil > now);
}

/** The thread the screen may show. A thread opened while its project was not quiet, or while the unlock ran, stays
 *  open when the window starts or the unlock ends (V0.46 (6)): `prev` is that thread. Any other way to a quiet
 *  thread (a link, back and forward, a notice, a board card, ?open=, a push) shows the gate instead. A thread that
 *  is not here yet is not admitted, so the gate decides when it arrives. */
export function admitThread(prev: string | null, id: string | null, t: { quietUntil?: string } | undefined, now: number, showUntil: number): { admitted: string | null; gated: boolean } {
  if (!id) return { admitted: null, gated: false };
  if (id === prev) return { admitted: id, gated: false };
  if (!t) return { admitted: null, gated: false };
  if (quietLocked(t, now, showUntil)) return { admitted: null, gated: true };
  return { admitted: id, gated: false };
}

/** The threads search and the project filter may reach: quiet ones only while the unlock runs. */
export function reachable(threads: Record<string, Thread>, now: number, showUntil: number): Record<string, Thread> {
  return hideQuiet(threads, showUntil > now, now);
}

// The unlock is one value per device (browser storage), shared by New thread, the filter, search and the gate; a
// change in one place (or another tab) reaches every listener at once.
const listeners = new Set<(end: number) => void>();

export function subscribeQuietShow(fn: (end: number) => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** Start (from) or end (null) the unlock and tell every listener. */
export function setQuietShow(kv: KV | null, from: number | null): number {
  const end = saveQuietShow(kv, from);
  for (const fn of listeners) fn(end);
  return end;
}
