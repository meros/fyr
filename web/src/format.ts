// Display helpers. Pure.

/** "0 s", "4 min", "3 h", "12 d": how long ago, or how far off, a span of ms is. Always rounded down; never negative. */
export function ago(ms: number): string {
  const sec = Math.floor(Math.max(0, ms) / 1000);
  if (sec < 60) return `${sec} s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min} min`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h} h`;
  return `${Math.floor(h / 24)} d`;
}

/** A duration as people say it: "40 s", "14 min", "1 h 5 min". Rounded down. */
export function span(ms: number): string {
  const sec = Math.floor(Math.max(0, ms) / 1000);
  if (sec < 60) return `${sec} s`;
  const m = Math.floor(sec / 60);
  if (m < 60) return `${m} min`;
  return `${Math.floor(m / 60)} h${m % 60 ? ` ${m % 60} min` : ""}`;
}

/** The minute a clock reading falls in: the memo key for a view that shows minutes. */
export function minuteOf(now: number): number {
  return Math.floor(now / 60_000);
}

/** "0 s", "4 min", "3 h", "12 d" relative time; `now` is the server-adjusted clock. */
export function relTime(iso: string | null | undefined, now: number): string {
  if (!iso) return "";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  return ago(now - t);
}

/** Calendar days from `a` to `b` in this device's time zone (DST-safe: rounds the day starts). */
export function daysBetween(a: Date, b: Date): number {
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  return Math.round((day(b) - day(a)) / 86400_000);
}

/** "14:20" today, "Mon 06:00" this week, "12 Nov" before that. */
export function whenLabel(iso: string | null | undefined, now: number): string {
  if (!iso) return "";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const d = new Date(t);
  const hm = clockTime(iso);
  const diff = daysBetween(d, new Date(now));
  if (diff === 0) return hm;
  if (diff > 0 && diff < 7) return `${d.toLocaleDateString(undefined, { weekday: "short" })} ${hm}`;
  return d.toLocaleDateString(undefined, { day: "numeric", month: "short" });
}

/** V0.125: "08:12" today, "Mon 06:00" this week, "9 Nov 06:00" before that: a past time with its clock time. */
export function dayTime(iso: string | null | undefined, now: number): string {
  const hm = clockTime(iso);
  if (!hm) return "";
  const d = new Date(Date.parse(iso!));
  const diff = daysBetween(d, new Date(now));
  if (diff === 0) return hm;
  if (diff > 0 && diff < 7) return `${d.toLocaleDateString(undefined, { weekday: "short" })} ${hm}`;
  return `${d.toLocaleDateString(undefined, { day: "numeric", month: "short" })} ${hm}`;
}

export function clockTime(iso: string | null | undefined): string {
  if (!iso) return "";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  return new Date(t).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
}

/** "Tomorrow 06:00", "Mon 06:00", "14:30" for a future time. */
export function untilLabel(iso: string | null | undefined, now: number): string {
  if (!iso) return "";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const d = new Date(t);
  const hm = clockTime(iso);
  const diff = daysBetween(new Date(now), d);
  if (diff === 0) return hm;
  if (diff === 1) return `tomorrow ${hm}`;
  if (diff < 7) return `${d.toLocaleDateString(undefined, { weekday: "short" })} ${hm}`;
  return `${d.toLocaleDateString(undefined, { day: "numeric", month: "short" })} ${hm}`;
}

export function duration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return "";
  if (ms < 1000) return `${(ms / 1000).toFixed(1)}s`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

export function count(n: number): string {
  return n > 9999 ? "9999+" : String(n);
}

/** "claude-sonnet-5-5" → "sonnet 5.5"; other names are kept. */
export function shortModel(m: string): string {
  const x = m.replace(/^claude-/, "");
  const r = /^([a-z]+)-(\d+)-(\d+)(.*)$/.exec(x);
  return r ? `${r[1]} ${r[2]}.${r[3]}${r[4].replace(/-/g, " ")}` : x;
}
