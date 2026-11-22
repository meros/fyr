// SPEC 0.5 V0.80: the machine load in the desktop rail and a working thread's CPU in its row. One poll for the
// whole client, every 5 s while the page is visible; the host reads at most every 2 s.

import { HostMark } from "./host-mark-ui.tsx";
import { activeTabId, type HostTabItem } from "./host-tabs.ts";
import { HostTabs } from "./host-tabs-ui.tsx";
import type { ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import { CardView, HoverCard, useHover } from "./card-ui.tsx";
import type { Api } from "./api.ts";
import { useApi } from "./api-context.ts";
import { clockTime } from "./format.ts";
import { ContextMenu } from "./menu-ui.tsx";
import { UndIcon, useUnd } from "./undeclared-ctx.tsx";
import { railUndeclared } from "./undeclared.ts";
import { hostLeftovers } from "./leftovers.ts";
import { loadCard, normalizeLoad, railBudget, railLabel, railMeter, shortHostNames, STRIP_MAX, type LoadCard, type StripHost, threadCPU, threadJob, threadService, threadSlot, type HostLoad, type Slots } from "./host-load.ts";

const POLL_MS = 5_000;

let source: Pick<Api, "load"> | null = null;
let current: HostLoad[] = [];
let lastAt = 0;
let timer: ReturnType<typeof setInterval> | undefined;
const subs = new Set<(l: HostLoad[]) => void>();

async function poll(): Promise<void> {
  if (!source || (typeof document !== "undefined" && document.visibilityState === "hidden")) return;
  lastAt = Date.now();
  // V0.163: a host's `heavy` (an older one's `slots`) read as one field.
  const l = await source.load().then(normalizeLoad).catch(() => current);
  current = l;
  for (const s of subs) s(l);
}

function onVisible(): void {
  if (document.visibilityState === "visible" && Date.now() - lastAt > POLL_MS) void poll();
}

/** V0.137: the pool of one host as a reorder's receipt or the page's own move gives it, shown at once (the next poll
 *  confirms it); `refresh` reads the load now. */
export function patchSlots(host: string, slots: Slots | ((s: Slots) => Slots)): void {
  current = current.map((h) => (h.host === host && h.slots ? { ...h, slots: typeof slots === "function" ? slots(h.slots) : slots } : h));
  for (const s of subs) s(current);
}
export function refreshLoad(): void {
  void poll();
}

export function useLoad(): HostLoad[] {
  const api = useApi();
  const [l, setL] = useState(current);
  useEffect(() => {
    subs.add(setL);
    if (subs.size === 1) {
      source = api;
      if (Date.now() - lastAt > POLL_MS / 2) void poll();
      timer = setInterval(() => void poll(), POLL_MS);
      document.addEventListener("visibilitychange", onVisible);
    }
    return () => {
      subs.delete(setL);
      if (subs.size === 0) {
        clearInterval(timer);
        document.removeEventListener("visibilitychange", onVisible);
      }
    };
  }, []);
  return l;
}

/** V0.167: what the two meters are; the foot of every host's card. */
const METERS_LEGEND = "ld: the load as a share of the cores · job: heavy jobs' use of their budget";

/** V0.167: one host's load and heavy jobs as two labelled meters, the same on the desktop rail, the phone's strip and
 *  its sheet: the host's name (when given), "ld" with a bar in the load colour (V0.99's and decision 0029's rule) and
 *  the load as a share of the cores (V0.152), "job" with a blue bar and the heavy jobs' use (V0.163; V0.164's
 *  undeclared part hatched; red with "+n" while jobs wait). An offline host: its name and "offline". */
export function HostMeters({ h, name, full, offline }: { h?: HostLoad; name?: string; full?: string; offline?: boolean }) {
  const m = h && railMeter(h);
  const bud = h && railBudget(h);
  return (
    <span class={`hm${m ? ` ${m.level}` : ""}${offline ? " off" : ""}`}>
      {name && (
        // V0.170: the host's mark before its short name.
        <span class="hmn mono" title={full}>
          <HostMark name={full ?? name} size={11} />
          <span class="ell">{name}</span>
        </span>
      )}
      {offline || !m ? (
        <span class="hmo mono">offline</span>
      ) : (
        <>
          <span class={`hmr ld${m.level === "hot" ? " hot" : ""}`}>
            <span class="hmk">ld</span>
            <span class="hmb">
              <i style={{ width: `${Math.round(m.share * 100)}%` }} />
            </span>
            <span class="hmv">{m.text}</span>
          </span>
          {bud && (
            <span class={`hmr job${bud.hot ? " hot" : ""}`}>
              <span class="hmk">job</span>
              <span class="hmb">
                <i style={{ width: `${Math.round(bud.share * 100)}%` }} />
                {!!bud.und && <i class="und" style={{ width: `${Math.round(bud.und * 100)}%` }} />}
              </span>
              <span class="hmv">
                {bud.text.replace(" ", "")}
                {bud.waiting > 0 && <em>+{bud.waiting}</em>}
              </span>
            </span>
          )}
        </>
      )}
    </span>
  );
}

/** V0.load-card: one host's load as a card (CardView): the same on the rail's hover card and the phone's sheet. */
function LoadCardView({ c, head }: { c: LoadCard; head?: boolean }) {
  return <CardView c={c} head={!!head} />;
}

/** The rail (desktop), V0.99, V0.167: per host its two meters (HostMeters), stacked; the card (V0.load-card) on hover or
 *  keyboard focus. */
export function LoadRail() {
  const list = useLoad();
  const und = useUnd();
  const [open, setOpen] = useState<{ host: string; at: DOMRect } | null>(null);
  if (!list.length) return null;
  const several = list.length > 1;
  const short = shortHostNames(list.map((h) => h.host));
  const shown = open && list.find((h) => h.host === open.host);
  const show = (host: string) => (e: Event) => setOpen({ host, at: (e.currentTarget as HTMLElement).getBoundingClientRect() });
  const hide = () => setOpen(null);
  return (
    <div class="rld">
      {list.map((h) => (
        <div class="rldw">
          <div class={`rldr ${railMeter(h).level}`} tabIndex={0} role="img" aria-label={railLabel(h, several)} onMouseEnter={show(h.host)} onMouseLeave={hide} onFocus={show(h.host)} onBlur={hide}>
            <HostMeters h={h} name={several ? short.get(h.host) : undefined} full={h.host} />
          </div>
          <RailUndeclared h={h} />
          <RailLeftovers h={h} />
        </div>
      ))}
      {open && shown && (
        <HoverCard at={open.at}>
          <LoadCardView c={loadCard(shown, { lookup: und.lookup, now: und.now() })} head />
          <p class="lcf">{METERS_LEGEND}</p>
        </HoverCard>
      )}
    </div>
  );
}

/** V0.166, V0.167: the phone's load strip, in the inbox header: per host the rail's two meters (HostMeters) with its
 *  short name when there are several; an offline host as its name and "offline". One button: a tap opens the sheet. */
export function LoadStrip({ hosts, onOpen }: { hosts: StripHost[]; onOpen(): void }) {
  if (!hosts.length) return null;
  const several = hosts.length > 1;
  const short = shortHostNames(hosts.map((h) => h.name));
  // the hosts with a load first (in the owner's order), so an offline host never hides a live one behind "+n"
  const shown = [...hosts.filter((h) => h.load), ...hosts.filter((h) => !h.load)].slice(0, STRIP_MAX);
  const label = hosts.map((h) => (h.load ? railLabel(h.load, true) : `${h.name}: offline`)).join("; ");
  return (
    <button type="button" class="m-load" onClick={onOpen} aria-label={`Host load: ${label}. Show details`}>
      {shown.map((h) => (
        <span class={`mlh${h.load ? "" : " off"}`}>
          <HostMeters h={h.load} offline={!h.load} name={several ? short.get(h.name) : undefined} full={h.name} />
        </span>
      ))}
      {hosts.length > shown.length && <span class="mlmore mono">+{hosts.length - shown.length}</span>}
    </button>
  );
}

/** V0.166, V0.167, V0.load-card, V0.host-tabs: the phone's load sheet. With several hosts, HostTabs at the top picks one
 *  host at a time (each tab carries its live load % or offline state) so the sheet never stacks several full load cards;
 *  below it are that host's meters, card, leftovers and provider quotas. With one host there are no tabs. */
export function LoadSheetBody({ hosts, renderQuotas }: { hosts: StripHost[]; renderQuotas?: (host: string | undefined) => ComponentChildren }) {
  const und = useUnd();
  const [picked, setPicked] = useState<string | null>(null);
  const several = hosts.length > 1;
  const tabItems: HostTabItem[] = hosts.map((h) => {
    const m = h.load && railMeter(h.load);
    return {
      id: h.name,
      name: h.name,
      label: h.name,
      offline: !h.load,
      meta: m ? m.text : undefined,
      metaLevel: m ? m.level : undefined,
      title: h.load ? railLabel(h.load, true) : `${h.name}: offline`,
    };
  });
  const activeId = activeTabId(tabItems, picked);
  const shown = several ? hosts.filter((h) => h.name === activeId) : hosts;
  const quotaBody = renderQuotas?.(several ? activeId : undefined);
  return (
    <div class="m-lsheet">
      {several && <HostTabs items={tabItems} active={activeId} onPick={setPicked} label="Hosts" />}
      {shown.map((h) => {
        const c = h.load && loadCard(h.load, { lookup: und.lookup, now: und.now() });
        return (
          <section key={h.name} class={`mls${h.load ? "" : " off"}`} role={several ? "tabpanel" : undefined} data-host={h.name} aria-label={h.name}>
            <div class="mlsh">
              <span class="mlsn">
                <b class="mono ell">{h.name}</b>
                {!h.load && <small>{`Offline${h.offline && clockTime(h.offline) ? ` since ${clockTime(h.offline)}` : ""}; no load until it is back`}</small>}
              </span>
              {h.load && <HostMeters h={h.load} />}
            </div>
            {c && <LoadCardView c={c} />}
            {h.load && (
              <ul class="mlsl">
                <SheetLeftovers h={h.load} />
              </ul>
            )}
          </section>
        );
      })}
      <p class="mlsk">{METERS_LEGEND}</p>
      {quotaBody && (
        <section class="mls" aria-label="Quota">
          <div class="mlsq">Provider quotas</div>
          {quotaBody}
        </section>
      )}
    </div>
  );
}

/** " · 2.6 cores" after a working thread's line; nothing before the host's second read. */
export function ThreadCPU({ id, chip }: { id: string; chip?: boolean }) {
  const c = threadCPU(useLoad(), id);
  if (!c) return null;
  // V0.row-calm: in an inbox row the reading sits with the chips, where the cut at the line's end cannot take it.
  if (chip) return <span class={`tcpu${c.hot ? " hot" : ""}`} title="CPU this thread and what it started use right now">{c.text}</span>;
  return (
    <>
      {" · "}
      <span class={`tcpu${c.hot ? " hot" : ""}`} title="CPU this thread and what it started use right now">
        {c.text}
      </span>
    </>
  );
}

/** V0.86, V0.163: " · waiting (2 ahead) · needs ≈ 30 %" or " · build job ≈ 45 %" on a thread's row; nothing without one. */
export function ThreadSlot({ id }: { id: string }) {
  const s = threadSlot(useLoad(), id);
  if (!s) return null;
  return (
    <>
      {" · "}
      <span class="tslot" title="Heavy jobs such as tests and builds wait until the host has room">
        {s}
      </span>
    </>
  );
}

/** A row's load in one subscription: whether the thread has a CPU reading, its job and its services (a hook, so one
 *  per row: each useLoad re-renders its row on every poll). */
export function useRowLoad(id: string): { hasCPU: boolean; job: string | null; service: string | null } {
  const l = useLoad();
  return { hasCPU: !!threadCPU(l, id), job: threadJob(l, id), service: threadService(l, id) };
}

/** V0.services-not-jobs: the thread's running services ("service · 3.2 GB") after the rest of its line; nothing when it
 *  runs none. Desktop and phone share the row, so both show it. */
export function ThreadService({ text: s }: { text: string | null }) {
  if (!s) return null;
  return (
    <>
      {" · "}
      <span class="tslot" title="Services this thread runs (a dev stack, a dev server, containers). Fyr reserves their memory before it admits heavy jobs and never pauses them">
        {s}
      </span>
    </>
  );
}

export function ThreadJob({ text }: { text: string }) {
  return (
    <span class="tslot" title="The heavy job this thread runs, and how much of the host it takes">
      {text}
    </span>
  );
}

/** The rail's button under a host's meter (SPEC 8.2). Desktop only: the phone shows no host load (V0.99 (3)) and has no
 *  rail; its load sheet's card lists the same threads. One thread: a press opens it. Several: a menu of them, each with
 *  its use. V0.hover-cards: hover or focus shows the threads as a card. Null when none is counted. */
function RailUndeclared({ h }: { h: HostLoad }) {
  const nav = useUnd();
  const hv = useHover(".rail");
  const [at, setAt] = useState<{ x: number; y: number } | null>(null);
  const r = railUndeclared(h, nav.lookup, nav.now());
  if (!r) return null;
  return (
    <>
      <button
        type="button"
        class="rund"
        aria-label={r.aria}
        aria-haspopup={r.only ? undefined : "menu"}
        {...hv.bind}
        onClick={(e) => {
          hv.close();
          if (r.only) return nav.openThread(r.only);
          const b = (e.currentTarget as HTMLElement).getBoundingClientRect();
          setAt({ x: b.right + 6, y: b.top });
        }}
      >
        <UndIcon size={9} />
        {r.count}
      </button>
      {hv.at && !at && (
        <HoverCard at={hv.at}>
          <CardView c={r.card} />
        </HoverCard>
      )}
      {at && <ContextMenu x={at.x} y={at.y} items={r.entries.map((x) => ({ id: x.id, label: x.label }))} onPick={(it) => (setAt(null), nav.openThread(it.id))} onClose={() => setAt(null)} />}
    </>
  );
}

/** The rail's count under a host's meters (desktop: the phone has no rail and shows SheetLeftovers in its load sheet).
 *  A press opens Settings → Automation, where each one has End and Keep; hover or focus shows them as a card
 *  (V0.hover-cards). Null without any. */
function RailLeftovers({ h }: { h: HostLoad }) {
  const nav = useUnd();
  const hv = useHover(".rail");
  const r = hostLeftovers(h);
  if (!r) return null;
  return (
    <>
      <button type="button" class="rlft" aria-label={`${h.host}: ${r.text}. Show them`} {...hv.bind} onClick={() => (hv.close(), nav.openSettings({ page: "automation", host: h.host, sub: "leftovers" }))}>
        {r.count} left
      </button>
      {hv.at && (
        <HoverCard at={hv.at}>
          <CardView c={r.card} />
        </HoverCard>
      )}
    </>
  );
}

/** The phone's load sheet: the same card as the rail's hover card, as text (no hover on a phone). */
function SheetLeftovers({ h }: { h: HostLoad }) {
  const r = hostLeftovers(h);
  if (!r) return null;
  return (
    <li class="lft">
      <CardView c={r.card} />
    </li>
  );
}
