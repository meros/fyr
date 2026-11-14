// Quiet hours per project (SPEC 0.5 V0.46, decision 0014). The host decides when a project is quiet and sends
// quietUntil on the project and on each of its threads; the client only hides and re-renders on time.
import type { Project, QuietHours, ThreadDTO } from "./types.ts";

export const WEEK: { key: string; label: string }[] = [
  { key: "mon", label: "Mon" },
  { key: "tue", label: "Tue" },
  { key: "wed", label: "Wed" },
  { key: "thu", label: "Thu" },
  { key: "fri", label: "Fri" },
  { key: "sat", label: "Sat" },
  { key: "sun", label: "Sun" },
];

/** The default window when quiet hours are turned on: the owner's example. */
export const DEFAULT_QUIET: QuietHours = { from: "17:00", to: "06:00" };

function until(x: { quietUntil?: string } | undefined): number {
  const t = x?.quietUntil ? Date.parse(x.quietUntil) : NaN;
  return Number.isFinite(t) ? t : 0;
}

/** Whether a thread (or project) is in its quiet hours at now. */
export function isQuiet(x: { quietUntil?: string } | undefined, now: number): boolean {
  return until(x) > now;
}

/** The inbox's threads: quiet ones left out, unless show (the project filter picks a project, or a search is typed). */
export function hideQuiet(threads: Record<string, ThreadDTO>, show: boolean, now: number): Record<string, ThreadDTO> {
  if (show) return threads;
  let any = false;
  for (const t of Object.values(threads)) if (isQuiet(t, now)) any = true;
  if (!any) return threads;
  const out: Record<string, ThreadDTO> = {};
  for (const t of Object.values(threads)) if (!isQuiet(t, now)) out[t.id] = t;
  return out;
}

/** When the client must look again: the nearest quietUntil still ahead, or null. One timer, no polling. */
export function nextQuietEnd(threads: Record<string, ThreadDTO>, projects: Project[], now: number): number | null {
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

function dayText(days: string[] | undefined): string {
  const idx = (days ?? []).map((d) => WEEK.findIndex((w) => w.key === d)).filter((i) => i >= 0).sort((a, b) => a - b);
  if (!idx.length || idx.length === 7) return "";
  const run = idx.every((v, i) => i === 0 || v === idx[i - 1] + 1);
  if (run && idx.length > 1) return `, ${WEEK[idx[0]].label}–${WEEK[idx[idx.length - 1]].label}`;
  return `, ${idx.map((i) => WEEK[i].label).join(", ")}`;
}

/** The project screen's one line. */
export function quietLine(q: QuietHours): string {
  const when = q.from === q.to ? "all day" : `from ${q.from} to ${q.to}`;
  return `Threads hide and send no notifications ${when}${dayText(q.days)}.`;
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
