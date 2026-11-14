// Display helpers. Pure.

/** "now", "4 min", "3 h", "Mon", "12 Nov" style relative time; `now` is the server-adjusted clock. */
export function relTime(iso: string | null | undefined, now: number): string {
  if (!iso) return "";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const d = Math.max(0, now - t);
  const min = Math.floor(d / 60_000);
  if (min < 1) return "now";
  if (min < 60) return `${min} min`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h} h`;
  const date = new Date(t);
  if (h < 24 * 6) return date.toLocaleDateString(undefined, { weekday: "short" });
  return date.toLocaleDateString(undefined, { day: "numeric", month: "short" });
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
  const n = new Date(now);
  const hm = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((day(d) - day(n)) / 86400_000);
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

export function shortModel(m: string): string {
  return m.replace(/^claude-/, "");
}
