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

// ---- V0.74, V0.114: quiet projects stay out of New thread, the filter, search and the paths into a thread ----
// V0.quiet-lift: the only way in is to lift the quiet on the project's page, on the host, so every device, every
// host of a linked project and every path follow at once; a lifted project has no quietUntil.

/** How long the owner holds a lift choice. The hold is slow on purpose. */
export const QUIET_HOLD_MS = 30_000;

/** The projects New thread offers: quiet ones left out; `keep` (the picked one, and the one a restored draft names)
 *  stays. */
export function createProjects(projects: Project[], now: number, ...keep: (string | undefined)[]): Project[] {
  return projects.filter((p) => !isQuiet(p, now) || (!!p.id && keep.includes(p.id)));
}

/** The thread the screen may show. A thread opened while its project was not quiet stays open when the quiet starts
 *  (V0.46 (6)): `prev` is that thread. Any other way to a quiet thread (a link, back and forward, a notice, a board
 *  card, ?open=, a push) shows the gate instead. A thread that is not here yet is not admitted, so the gate decides
 *  when it arrives. */
export function admitThread(prev: string | null, id: string | null, t: { quietUntil?: string } | undefined, now: number): { admitted: string | null; gated: boolean } {
  if (!id) return { admitted: null, gated: false };
  if (id === prev) return { admitted: id, gated: false };
  if (!t) return { admitted: null, gated: false };
  if (isQuiet(t, now)) return { admitted: null, gated: true };
  return { admitted: id, gated: false };
}

/** The threads search and the project filter may reach: none of a quiet project. */
export function reachable(threads: Record<string, Thread>, now: number): Record<string, Thread> {
  return hideQuiet(threads, false, now);
}

// ---- V0.119: the project page saves quiet hours 600 ms after the last change, and at once when it closes ----

export const QUIET_SAVE_MS = 600;

/** A saver that sends only the last value pushed, `ms` after the last push; flush sends a waiting value at once. */
export function quietSaver<T>(send: (v: T) => void, ms = QUIET_SAVE_MS): { push(v: T): void; flush(): void } {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let waiting: { v: T } | null = null;
  const flush = () => {
    clearTimeout(timer);
    timer = undefined;
    const w = waiting;
    waiting = null;
    if (w) send(w.v);
  };
  return {
    push(v: T) {
      waiting = { v };
      clearTimeout(timer);
      timer = setTimeout(flush, ms);
    },
    flush,
  };
}

/** V0.quiet-now: the end of the quiet stretch the owner started by hand, or 0 when none holds at now. */
export function manualUntil(p: { quietNow?: string } | undefined, now: number): number {
  const t = p?.quietNow ? Date.parse(p.quietNow) : NaN;
  return Number.isFinite(t) && t > now ? t : 0;
}

/** V0.quiet-now: the next time the clock reads HH:MM after now (today, else tomorrow), in this browser's time zone. */
export function nextClock(hhmm: string, now: number): number {
  const m = /^(\d{1,2}):(\d\d)$/.exec(hhmm);
  const d = new Date(now);
  const at = new Date(d.getFullYear(), d.getMonth(), d.getDate(), m ? +m[1] : 0, m ? +m[2] : 0).getTime();
  return at > now ? at : new Date(d.getFullYear(), d.getMonth(), d.getDate() + 1, m ? +m[1] : 0, m ? +m[2] : 0).getTime();
}

/** "15:30", "tomorrow 06:00" or "Mon 06:00" (past a day): when a choice ends, as the snooze list says it. */
export function endWords(end: number, now: number): string {
  const d = new Date(end);
  const day = end - now > 24 * 3600_000 ? `${WEEK[(d.getDay() + 6) % 7].label} ` : new Date(now).getDate() !== d.getDate() ? "tomorrow " : "";
  return `${day}${hm(d)}`;
}

export interface QuietChoice {
  label: string;
  end: number;
  /** when it ends, in words */
  when: string;
}

/** V0.quiet-now: the one-tap choices of Quiet now: for an hour, for three, and until the next 08:00. */
export function quietNowChoices(now: number): QuietChoice[] {
  const morning = nextClock("08:00", now);
  const sameDay = new Date(morning).getDate() === new Date(now).getDate();
  return [
    { label: "1 hour", end: now + 3600_000 },
    { label: "3 hours", end: now + 3 * 3600_000 },
    { label: sameDay ? "Until 08:00" : "Until tomorrow 08:00", end: morning },
  ].map((c) => ({ ...c, when: endWords(c.end, now) }));
}

/** V0.quiet-lift: how far ahead a lift may end (core.MaxQuietLift). */
export const QUIET_LIFT_MAX_MS = 24 * 3600_000;

/** V0.quiet-lift: the list of times a lift runs for, as snooze offers them: 30 minutes (the old unlock), an hour,
 *  three, and until the quiet ends when that is further and at most a day ahead. quietEnd: the project's quietUntil. */
export function quietLiftChoices(now: number, quietEnd: number): QuietChoice[] {
  const out = [
    { label: "30 minutes", end: now + 30 * 60_000 },
    { label: "1 hour", end: now + 3600_000 },
    { label: "3 hours", end: now + 3 * 3600_000 },
  ];
  if (quietEnd > now + 3 * 3600_000 && quietEnd - now <= QUIET_LIFT_MAX_MS) out.push({ label: "Until the quiet ends", end: quietEnd });
  return out.filter((c) => c.end <= quietEnd || c.label === "30 minutes").map((c) => ({ ...c, when: endWords(c.end, now) }));
}

/** V0.quiet-lift: the end of the lift of the quiet, or 0 when none holds at now. */
export function liftUntil(p: { quietLift?: string } | undefined, now: number): number {
  const t = p?.quietLift ? Date.parse(p.quietLift) : NaN;
  return Number.isFinite(t) && t > now ? t : 0;
}

/** V0.quiet-now: what the Quiet now row says: "Quiet by hand until 15:30." (the day named past a day), or what it does. */
export function quietNowLine(p: { quietNow?: string } | undefined, now: number): string {
  const end = manualUntil(p, now);
  if (!end) return "Hide its threads and hold notifications from now, once.";
  return `Quiet by hand until ${endWords(end, now)}.`;
}
