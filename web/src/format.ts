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
  return dayMonth(d);
}

/** V0.125: "08:12" today, "Mon 06:00" this week, "9 Nov 06:00" before that: a past time with its clock time. */
export function dayTime(iso: string | null | undefined, now: number): string {
  const hm = clockTime(iso);
  if (!hm) return "";
  const d = new Date(Date.parse(iso!));
  const diff = daysBetween(d, new Date(now));
  if (diff === 0) return hm;
  if (diff > 0 && diff < 7) return `${d.toLocaleDateString(undefined, { weekday: "short" })} ${hm}`;
  return `${dayMonth(d)} ${hm}`;
}

/** "14:20": the clock time of an instant in epoch ms, 24-hour with two digits, whatever the locale. */
export function clockAt(ms: number): string {
  return new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
}

/** "14:20" for an ISO time; "" for none or one that does not parse. */
export function clockTime(iso: string | null | undefined): string {
  if (!iso) return "";
  const t = Date.parse(iso);
  return Number.isNaN(t) ? "" : clockAt(t);
}

/** "12 Nov": the day and month of a date. */
export function dayMonth(d: Date): string {
  return d.toLocaleDateString(undefined, { day: "numeric", month: "short" });
}

/** "Fri": the short weekday of an ISO time; "" for none or one that does not parse. */
export function shortWeekday(iso: string | null | undefined): string {
  const t = iso ? Date.parse(iso) : NaN;
  return Number.isNaN(t) ? "" : new Date(t).toLocaleDateString(undefined, { weekday: "short" });
}

/** The grapheme clusters of a string (an emoji with its modifiers is one); code points where Intl.Segmenter is missing. */
export function graphemes(s: string): string[] {
  if (typeof Intl.Segmenter !== "function") return Array.from(s);
  return Array.from(new Intl.Segmenter(undefined, { granularity: "grapheme" }).segment(s), (x) => x.segment);
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
  return `${dayMonth(d)} ${hm}`;
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

// ---- V0.web-helpers: shortening text. One definition each; never a broken emoji (a surrogate pair stays whole). ----

/** The first `n` UTF-16 units of `s`, one fewer when the cut would split a surrogate pair. */
function head(s: string, n: number): string {
  const c = s.charCodeAt(n - 1);
  return s.slice(0, c >= 0xd800 && c <= 0xdbff ? n - 1 : n);
}

/** The last `n` UTF-16 units of `s`, one fewer when the cut would split a surrogate pair. */
function tail(s: string, n: number): string {
  if (n <= 0) return "";
  const at = s.length - n;
  const c = s.charCodeAt(at);
  return s.slice(c >= 0xdc00 && c <= 0xdfff ? at + 1 : at);
}

/** At most `max` characters, cut at the end with "…" (no space left before it). */
export function clip(s: string, max: number): string {
  return s.length > max ? head(s, max - 1).trimEnd() + "…" : s;
}

/** The text on one line (every run of white space one space, trimmed), clipped to `max`. */
export function oneLine(s: string, max = Infinity): string {
  return clip(s.replace(/\s+/g, " ").trim(), max);
}

/** The first line that is not blank, on one line and clipped to `max`. */
export function firstLine(s: string, max = Infinity): string {
  return oneLine(s.split("\n").find((x) => x.trim()) ?? "", max);
}

/** At most `max` characters, cut in the middle, keeping the start and the more telling end: "src/…/FilterSheet.tsx". */
export function middleTruncate(s: string, max = 40): string {
  if (s.length <= max) return s;
  const keep = max - 1;
  const end = Math.ceil(keep * 0.6);
  return head(s, keep - end) + "…" + tail(s, end);
}
