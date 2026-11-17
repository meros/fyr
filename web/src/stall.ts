// SPEC 0.5 V0.147: a stall the host's watchdog saw (a lock held or a request running past 10 s), or the exit after
// the engine's lock stayed stuck past 10 min (V0.181; it was 2 min). One line, in words, with the dump's file under Details.

export interface StallInfo {
  /** the host it happened on; a host's own state leaves it out */
  host?: string;
  at: string;
  /** the longest holder, as the watchdog wrote it */
  what: string;
  /** the goroutine dump on that host */
  file: string;
  /** fyrd ended after this dump and the launcher started it again */
  exiting: boolean;
}

/** A stall from the wire, or null when it is not one. */
export function decodeStall(raw: unknown, host?: string): StallInfo | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  if (typeof r.at !== "string") return null;
  const h = typeof r.host === "string" && r.host ? r.host : host;
  return { ...(h ? { host: h } : {}), at: r.at, what: typeof r.what === "string" ? r.what : "", file: typeof r.file === "string" ? r.file : "", exiting: r.exiting === true };
}

/** The state's stalls: a front's `stalls`, else a host's own `stall` named by the host. */
export function stallsOf(dto: { stall?: unknown; stalls?: unknown; host?: string }): StallInfo[] {
  if (Array.isArray(dto.stalls)) return dto.stalls.map((x) => decodeStall(x)).filter((x): x is StallInfo => x !== null);
  const one = decodeStall(dto.stall, dto.host || undefined);
  return one ? [one] : [];
}

/** A stall from the stream replaces its host's. */
export function upsertStall(list: StallInfo[], s: StallInfo): StallInfo[] {
  return [...list.filter((x) => (x.host ?? "") !== (s.host ?? "")), s];
}

/** The newest stall not dismissed (by its time and host), or null. */
export function shownStall(list: StallInfo[], dismissed: string | null): StallInfo | null {
  let best: StallInfo | null = null;
  for (const s of list) if (stallKey(s) !== dismissed && (!best || Date.parse(s.at) > Date.parse(best.at))) best = s;
  return best;
}

export function stallKey(s: StallInfo): string {
  return `${s.host ?? ""}@${s.at}`;
}

function hhmm(iso: string): string {
  const d = new Date(iso);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

/** The banner's sentence and its details. */
export function stallLine(s: StallInfo, multiHost: boolean): { text: string; more: string } {
  const where = multiHost && s.host ? ` on ${s.host}` : "";
  const text = s.exiting
    ? `Fyr was stuck and restarted itself at ${hhmm(s.at)}${where}; no turn was lost`
    : `Fyr stalled at ${hhmm(s.at)}${where}: a step took over 10 s`;
  const more = [s.what && `Held longest: ${s.what}`, s.file && `Goroutine dump: ${s.file}`, "Hand the dump to an agent to find the cause."].filter(Boolean).join("\n");
  return { text, more };
}
