// SPEC 0.5 V0.44: provider quota where a profile is chosen and, V0.quota-in-thread, in the header of the open thread. One poll for the whole client
// (every minute while the page is visible; the host reads providers at most every five minutes).

import { profilesOf, type HostProfiles } from "./hosts.ts";
import { quotasForHost } from "./host-tabs.ts";
import { useEffect, useRef, useState } from "preact/hooks";
import type { Profile, WakeUp } from "./types.ts";
import type { Api } from "./api.ts";
import { useApi } from "./api-context.ts";
import { hasData, quotaCard, quotaFor, quotaParts, quotaSummary, quotaWakeOptions, busiestFigure, type Quota } from "./quota.ts";
import type { Card } from "./card.ts";
import { CardView, HoverCard, useHover, usePopoverDismiss, type AnchorAt } from "./card-ui.tsx";

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
    setQ(current); // a poll that landed between this render and now reached the earlier subscribers only
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
  }, [api]);
  return q;
}

const nameOf = (profiles: Profile[], id: string) => profiles.find((p) => p.id === id)?.name ?? id;

/** "Claude 5 h 34 % · 7 d 61 %" for the option text of a profile picker; "" without data. */
export function quotaSuffix(list: Quota[], profile: string): string {
  const q = quotaFor(list, profile);
  return q && hasData(q) ? quotaParts(q).map((x) => x.text).join(" · ") : "";
}

/** One calm line; V0.hover-cards: hover shows its card (quotaCard), a tap opens the same card below (the phone's way:
 *  no hover there). Bare (inside a Settings row, both fronts): the card on hover only; the phone reaches the same card
 *  from the new-thread screen's quota list and the load sheet. */
export function QuotaLine(p: { q: Quota; name: string; color?: string; /** V0.119: the figures alone, no name and no button (inside a row that is a button) */ bare?: boolean }) {
  const [open, setOpen] = useState(false);
  const hv = useHover();
  const card = quotaCard(p.q, p.name, Date.now());
  const parts = quotaParts(p.q);
  if (p.bare)
    return (
      <span class={`qline bare${!hasData(p.q) && p.q.error ? " err" : ""}`} onMouseEnter={hv.bind.onMouseEnter} onMouseLeave={hv.bind.onMouseLeave}>
        {parts.length
          ? parts.map((x, i) => (
              <>
                {i > 0 && " · "}
                <span class={x.hot ? "hot" : undefined}>{x.text}</span>
              </>
            ))
          : (p.q.error ?? "Not read yet")}
        {hv.at && <QuotaHover c={card} at={hv.at} />}
      </span>
    );
  return (
    <div class={`qline${p.q.stale ? " stale" : ""}${!hasData(p.q) && p.q.error ? " err" : ""}`}>
      <button type="button" class="qsum" aria-expanded={open} {...hv.bind} onClick={() => (hv.close(), setOpen((o) => !o))}>
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
      {hv.at && !open && <QuotaHover c={card} at={hv.at} />}
      {open && (
        <div class="qdet">
          <CardView c={card} head={false} />
        </div>
      )}
    </div>
  );
}

/** The quota's hover card, under the line or chip it belongs to. */
function QuotaHover({ c, at }: { c: Card; at: AnchorAt }) {
  return (
    <HoverCard at={at} place="below">
      <CardView c={c} />
    </HoverCard>
  );
}

/** The quota of one profile (new-thread dialog, under the picker); nothing when the host has none. */
export function ProfileQuota(p: { profile: string; profiles: Profile[]; host?: string }) {
  const q = quotaFor(useQuotas(), p.profile, p.host);
  return q ? <QuotaLine q={q} name={nameOf(p.profiles, p.profile)} /> : null;
}

/** Every profile's quota (phone new-thread screen, the load sheet), each named by its own host's provider. With `host`,
 *  shows only that host's providers (V0.host-tabs). */
export function QuotaList(p: { profiles: HostProfiles; host?: string }) {
  const list = quotasForHost(useQuotas(), p.host);
  if (!list.length) return null;
  return (
    <div class="qlist" aria-label="Quota">
      {list.map((q) => (
        <QuotaLine q={q} name={nameOf(profilesOf(q, p.profiles), q.profile)} color={profilesOf(q, p.profiles).find((x) => x.id === q.profile)?.color} />
      ))}
    </div>
  );
}

/** V0.quota-reset-wake: wake-on-reset buttons under a quota card, shared by the desktop quota popover and phone Details. */
export function QuotaWakeActions(p: {
  q: Quota;
  wakes?: WakeUp[];
  wakeDisabled?: string;
  onWake(window: string): void;
  onCancelWake?(rule: string): void;
}) {
  const opts = quotaWakeOptions(p.q, p.wakes, Date.now(), p.wakeDisabled);
  if (!opts.length) return null;
  return (
    <div class="qwake">
      {opts.map((o) =>
        o.armedRule ? (
          <button
            key={o.window}
            type="button"
            class="btn sm"
            title={`Wakes when the ${o.window} window resets. Press to cancel.`}
            onClick={() => p.onCancelWake?.(o.armedRule!)}
          >
            Cancel {o.window} wake
          </button>
        ) : (
          <button
            key={o.window}
            type="button"
            class="btn sm"
            disabled={!!o.disabledReason}
            title={o.disabledReason ?? `Continue this thread when the ${o.window} window resets${o.when ? ` (${o.when})` : ""}`}
            onClick={() => p.onWake(o.window)}
          >
            Wake when {o.window} resets{o.when ? ` (${o.when})` : ""}
          </button>
        ),
      )}
    </div>
  );
}

/** V0.quota-in-thread, V0.quota-reset-wake: the quota of the provider a thread runs on, as a chip in the desktop
 *  thread's header, after the model and the effort: every window, the reset times on hover, and click to arm or cancel
 *  a wake when a window resets. Nothing without a reading or an error. */
export function ThreadQuotaChip(p: {
  profile: string;
  host?: string;
  name: string;
  wakes?: WakeUp[];
  wakeDisabled?: string;
  onWake?(window: string): void;
  onCancelWake?(rule: string): void;
}) {
  const q = quotaFor(useQuotas(), p.profile, p.host);
  const hv = useHover();
  const [open, setOpen] = useState(false);
  const chipRef = useRef<HTMLSpanElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  usePopoverDismiss(open, [chipRef, popRef], () => setOpen(false));
  if (!q || !(hasData(q) || q.error)) return null;
  const parts = quotaParts(q);
  const canOpen = !!p.onWake && !!q.windows?.length;
  const toggle = () => {
    if (!canOpen || !chipRef.current) return;
    hv.close();
    setOpen((o) => !o);
  };
  return (
    <span
      ref={chipRef}
      class={`chip m qchip${q.stale ? " stale" : ""}${canOpen ? " click" : ""}`}
      role={canOpen ? "button" : undefined}
      aria-expanded={canOpen ? open : undefined}
      tabIndex={0}
      aria-label={`Quota: ${quotaSummary(q, p.name)}`}
      {...hv.bind}
      onClick={canOpen ? toggle : undefined}
      onKeyDown={canOpen ? (e: KeyboardEvent) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), toggle()) : undefined}
    >
      {open ? (
        <HoverCard at={() => chipRef.current?.getBoundingClientRect() ?? null} place="below" interactive popRef={popRef} onGone={() => setOpen(false)}>
          <CardView c={quotaCard(q, p.name, Date.now())} />
          <QuotaWakeActions
            q={q}
            wakes={p.wakes}
            wakeDisabled={p.wakeDisabled}
            onWake={(w) => (p.onWake!(w), setOpen(false))}
            onCancelWake={(r) => (p.onCancelWake?.(r), setOpen(false))}
          />
        </HoverCard>
      ) : (
        hv.at && <QuotaHover c={quotaCard(q, p.name, Date.now())} at={hv.at} />
      )}
      {parts.length ? (
        <span class="qtxt">
          {parts.map((x, i) => (
            <>
              {i > 0 && <span class="qsep"> · </span>}
              <span class={x.hot ? "hot" : undefined}>{x.text}</span>
            </>
          ))}
        </span>
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

/** V0.quota-in-thread, V0.hover-cards: the same quota card as the desktop chip's hover (the phone's thread Details,
 *  where there is no hover). null without a reading or an error. */
export function useThreadQuotaCard(profile: string, host: string | undefined, name: string): Card | null {
  const q = quotaFor(useQuotas(), profile, host);
  if (!q || !(hasData(q) || q.error)) return null;
  return quotaCard(q, name, Date.now());
}

/** V0.quota-reset-wake: the raw Quota for a thread's provider, for phone Details' wake controls. */
export function useThreadQuota(profile: string, host: string | undefined): Quota | undefined {
  return quotaFor(useQuotas(), profile, host);
}
