// SPEC 0.5 V0.44: provider quota where a profile is chosen and in the rail. One poll for the whole client
// (every minute while the page is visible; the host reads providers at most every five minutes).

import { useEffect, useState } from "preact/hooks";
import type { Profile } from "./types.ts";
import type { Api } from "./api.ts";
import { useApi } from "./api-context.ts";
import { hasData, quotaDetail, quotaFor, quotaParts, quotaSummary, railFigure, type Quota } from "./quota.ts";

const POLL_MS = 60_000;

/** The Api of the first subscriber (#41: from ApiContext; there is one per app). */
let source: Pick<Api, "quota"> | null = null;
let current: Quota[] = [];
let lastAt = 0;
let timer: ReturnType<typeof setInterval> | undefined;
const subs = new Set<(q: Quota[]) => void>();

async function poll(): Promise<void> {
  if (!source || (typeof document !== "undefined" && document.visibilityState === "hidden")) return;
  lastAt = Date.now();
  const q = await source.quota().catch(() => current);
  current = q;
  for (const s of subs) s(q);
}

function onVisible(): void {
  if (document.visibilityState === "visible" && Date.now() - lastAt > POLL_MS) void poll();
}

export function useQuotas(): Quota[] {
  const api = useApi();
  const [q, setQ] = useState(current);
  useEffect(() => {
    subs.add(setQ);
    if (subs.size === 1) {
      source = api;
      if (Date.now() - lastAt > POLL_MS / 2) void poll();
      timer = setInterval(() => void poll(), POLL_MS);
      document.addEventListener("visibilitychange", onVisible);
    }
    return () => {
      subs.delete(setQ);
      if (subs.size === 0) {
        clearInterval(timer);
        document.removeEventListener("visibilitychange", onVisible);
      }
    };
  }, []);
  return q;
}

const nameOf = (profiles: Profile[], id: string) => profiles.find((p) => p.id === id)?.name ?? id;

/** "Claude 5 h 34 % · 7 d 61 %" for the option text of a profile picker; "" without data. */
export function quotaSuffix(list: Quota[], profile: string): string {
  const q = quotaFor(list, profile);
  return q && hasData(q) ? quotaParts(q).map((x) => x.text).join(" · ") : "";
}

/** One calm line; hover shows the details, a tap opens them below. */
export function QuotaLine(p: { q: Quota; name: string; color?: string; /** V0.119: the figures alone, no name and no button (inside a row that is a button) */ bare?: boolean }) {
  const [open, setOpen] = useState(false);
  const now = Date.now();
  const detail = quotaDetail(p.q, p.name, now);
  const parts = quotaParts(p.q);
  if (p.bare)
    return (
      <span class={`qline bare${!hasData(p.q) && p.q.error ? " err" : ""}`} title={detail.join("\n")}>
        {parts.length
          ? parts.map((x, i) => (
              <>
                {i > 0 && " · "}
                <span class={x.hot ? "hot" : undefined}>{x.text}</span>
              </>
            ))
          : (p.q.error ?? "Not read yet")}
      </span>
    );
  return (
    <div class={`qline${p.q.stale ? " stale" : ""}${!hasData(p.q) && p.q.error ? " err" : ""}`}>
      <button type="button" class="qsum" title={detail.join("\n")} aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        {p.color && <i style={{ background: p.color }} />}
        {parts.length ? (
          <span class="ell">
            {p.name}{" "}
            {parts.map((x, i) => (
              <>
                {i > 0 && " · "}
                <span class={x.hot ? "hot" : undefined}>{x.text}</span>
              </>
            ))}
          </span>
        ) : (
          <span class="ell">{quotaSummary(p.q, p.name)}</span>
        )}
      </button>
      {open && (
        <ul class="qdet">
          {detail.map((l) => (
            <li>{l}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** The quota of one profile (new-thread dialog, under the picker); nothing when the host has none. */
export function ProfileQuota(p: { profile: string; profiles: Profile[] }) {
  const q = quotaFor(useQuotas(), p.profile);
  return q ? <QuotaLine q={q} name={nameOf(p.profiles, p.profile)} /> : null;
}

/** Every profile's quota (phone new-thread screen). */
export function QuotaList(p: { profiles: Profile[] }) {
  const list = useQuotas();
  if (!list.length) return null;
  return (
    <div class="qlist" aria-label="Quota">
      {list.map((q) => (
        <QuotaLine q={q} name={nameOf(p.profiles, q.profile)} color={p.profiles.find((x) => x.id === q.profile)?.color} />
      ))}
    </div>
  );
}

/** The rail footer (desktop): each profile's colour and its busiest window or balance; details on hover. */
export function QuotaRail(p: { profiles: Profile[] }) {
  const list = useQuotas();
  const now = Date.now();
  const rows = list.map((q) => ({ q, f: railFigure(q) })).filter((r) => r.f || r.q.error);
  if (!rows.length) return null;
  return (
    <div class="rq" aria-label="Quota">
      {rows.map(({ q, f }) => {
        const name = nameOf(p.profiles, q.profile);
        const tip = [quotaSummary(q, name), ...quotaDetail(q, name, now).filter((l) => !l.startsWith(`${name} `))].join("\n");
        return (
          <div class={`rqr${q.stale ? " stale" : ""}`} title={tip}>
            <i style={{ background: p.profiles.find((x) => x.id === q.profile)?.color ?? "var(--ink-faint)" }} />
            <span class={f?.hot ? "hot" : undefined}>{f ? f.text : "!"}</span>
          </div>
        );
      })}
    </div>
  );
}
