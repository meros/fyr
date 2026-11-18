// SPEC 0.5 V0.44: provider quota where a profile is chosen and, V0.quota-in-thread, in the header of the open thread. One poll for the whole client
// (every minute while the page is visible; the host reads providers at most every five minutes).

import { useEffect, useState } from "preact/hooks";
import type { Profile } from "./types.ts";
import type { Api } from "./api.ts";
import { useApi } from "./api-context.ts";
import { hasData, quotaDetail, quotaFor, quotaParts, quotaSummary, busiestFigure, type Quota } from "./quota.ts";

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

/** Every line about one quota: the windows with their reset times, the freshness and the notes; an error without data
 *  comes first (quotaDetail names an error only beside data). */
function quotaLines(q: Quota, name: string): string[] {
  const d = quotaDetail(q, name, Date.now());
  return hasData(q) || !q.error ? d : [`${name}: ${q.error}`, ...d];
}

/** V0.quota-in-thread: the quota of the provider a thread runs on, as a chip in the desktop thread's header, after the
 *  model and the effort: every window, the reset times on hover. Nothing without a reading or an error. */
export function ThreadQuotaChip(p: { profile: string; host?: string; name: string }) {
  const q = quotaFor(useQuotas(), p.profile, p.host);
  if (!q || !(hasData(q) || q.error)) return null;
  const parts = quotaParts(q);
  const tip = quotaLines(q, p.name).join("\n");
  return (
    <span class={`chip m qchip${q.stale ? " stale" : ""}`} title={tip} aria-label={`Quota: ${quotaSummary(q, p.name)}`}>
      {parts.length ? (
        parts.map((x, i) => (
          <>
            {i > 0 && <span class="qsep"> · </span>}
            <span class={x.hot ? "hot" : undefined}>{x.text}</span>
          </>
        ))
      ) : (
        <span>No quota reading</span>
      )}
    </span>
  );
}

/** V0.quota-in-thread: the same quota on the phone, in the thread's title bar before the ⋯ button: one figure, the
 *  busiest window's share ("61 %"), 44 px high for the finger; a tap opens the thread's Details, which lists every
 *  window (`useThreadQuotaLines`). Not a chip with every window as on the desktop: the phone's chip row is one line of
 *  at most 360 px that the project, the state, the host, the branch, the provider and the PR already fill, and a phone
 *  has no hover. Nothing without data. */
export function ThreadQuotaFigure(p: { profile: string; host?: string; name: string; onOpen(): void }) {
  const q = quotaFor(useQuotas(), p.profile, p.host);
  const f = q && busiestFigure(q);
  if (!q || !f) return null;
  return (
    <button type="button" class={`qfig${f.hot ? " hot" : ""}${q.stale ? " stale" : ""}`} aria-label={`Quota: ${quotaSummary(q, p.name)}. Details`} onClick={p.onOpen}>
      {f.text}
    </button>
  );
}

/** V0.quota-in-thread: the same quota as lines (the phone's thread Details, where there is no hover): each window with
 *  its reset time, then the freshness and the notes. null without a reading or an error. */
export function useThreadQuotaLines(profile: string, host: string | undefined, name: string): string[] | null {
  const q = quotaFor(useQuotas(), profile, host);
  if (!q || !(hasData(q) || q.error)) return null;
  return quotaLines(q, name).map((l) => (l.startsWith(`${name} `) ? l.slice(name.length + 1) : l.startsWith(`${name}: `) ? l.slice(name.length + 2) : l));
}
