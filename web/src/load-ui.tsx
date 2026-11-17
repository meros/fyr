// SPEC 0.5 V0.80: the machine load in the desktop rail and a working thread's CPU in its row. One poll for the
// whole client, every 5 s while the page is visible; the host reads at most every 2 s.

import type { ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { Api } from "./api.ts";
import { useApi } from "./api-context.ts";
import { clockTime } from "./format.ts";
import { ContextMenu } from "./menu-ui.tsx";
import { UndIcon, useUnd } from "./undeclared-ctx.tsx";
import { railUndeclared } from "./undeclared.ts";
import { loadDetail, normalizeLoad, railBudget, railLabel, railMeter, shortHostNames, slotTip, STRIP_MAX, undeclaredTip, type StripHost, threadCPU, threadJob, threadSlot, type HostLoad, type Slots } from "./load.ts";

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

/** V0.167: what the two meters are, first in every host's tooltip. */
export const METERS_LEGEND = "ld: the load, % of the cores (1 min) · job: heavy jobs' use of their budget (the tighter of CPU and memory)";

/** V0.167: one host's load and heavy jobs as two labelled meters, the same on the desktop rail, the phone's strip and
 *  its sheet: the host's name (when given), "ld" with a bar in the load colour (V0.99's and decision 0029's rule) and
 *  the load as a share of the cores (V0.152), "job" with a blue bar and the heavy jobs' use (V0.163; V0.164's
 *  undeclared part hatched; red with "+n" while jobs wait). An offline host: its name and "offline". */
export function HostMeters({ h, name, full, jobsTip, offline }: { h?: HostLoad; name?: string; full?: string; jobsTip?: string; offline?: boolean }) {
  const m = h && railMeter(h);
  const bud = h && railBudget(h);
  return (
    <span class={`hm${m ? ` ${m.level}` : ""}${offline ? " off" : ""}`}>
      {name && (
        <span class="hmn mono" title={full}>
          {name}
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
            <span class={`hmr job${bud.hot ? " hot" : ""}`} title={jobsTip}>
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

/** The rail (desktop), V0.99, V0.167: per host its two meters (HostMeters), stacked; the exact values on hover. */
export function LoadRail() {
  const list = useLoad();
  if (!list.length) return null;
  const several = list.length > 1;
  const short = shortHostNames(list.map((h) => h.host));
  const und = useUnd();
  return (
    <div class="rld">
      {list.map((h) => (
        <div class="rldw">
          <div class={`rldr ${railMeter(h).level}`} title={[METERS_LEGEND, ...loadDetail(h)].join("\n")} role="img" aria-label={railLabel(h, several)}>
            <HostMeters h={h} name={several ? short.get(h.host) : undefined} full={h.host} jobsTip={slotTip(h, { lookup: und.lookup, now: und.now() })} />
          </div>
          <RailUndeclared h={h} />
        </div>
      ))}
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
          <HostMeters h={h.load} offline={!h.load} name={several ? short.get(h.name) : undefined} />
        </span>
      ))}
      {hosts.length > shown.length && <span class="mlmore mono">+{hosts.length - shown.length}</span>}
    </button>
  );
}

/** V0.166, V0.167: the phone's load sheet: per host its full name and state, its two meters, and the rail's tooltip
 *  lines as text (the load, the signals, CPU and memory in use and their budget, the running jobs with their share,
 *  who waits and why, undeclared work); then the provider quotas. */
export function LoadSheetBody({ hosts, quotas }: { hosts: StripHost[]; quotas: ComponentChildren }) {
  const und = useUnd();
  return (
    <div class="m-lsheet">
      <p class="mlsk">{METERS_LEGEND}</p>
      {hosts.map((h) => {
        const lines = h.load ? loadDetail(h.load) : [];
        const ut = h.load ? undeclaredTip(h.load, und.lookup, und.now()) : "";
        return (
          <section class={`mls${h.load ? "" : " off"}`} aria-label={h.name}>
            <div class="mlsh">
              <span class="mlsn">
                <b class="mono ell">{h.name}</b>
                {!h.load && <small>{`Offline${h.offline && clockTime(h.offline) ? ` since ${clockTime(h.offline)}` : ""}; no load until it is back`}</small>}
              </span>
              {h.load && <HostMeters h={h.load} />}
            </div>
            {lines.length > 0 && (
              <ul class="mlsl">
                {lines.slice(1).map((l) => (
                  <li>{l}</li>
                ))}
                {ut && <li class="und">{ut}</li>}
              </ul>
            )}
          </section>
        );
      })}
      <section class="mls" aria-label="Quota">
        <div class="mlsq">Provider quotas</div>
        {quotas}
      </section>
    </div>
  );
}

/** " · 2.6 cores" after a working thread's line; nothing before the host's second read. */
export function ThreadCPU({ id }: { id: string }) {
  const c = threadCPU(useLoad(), id);
  if (!c) return null;
  return (
    <>
      {" · "}
      <span class={`tcpu${c.hot ? " hot" : ""}`} title="CPU of this thread and everything it started, over the last seconds">
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
      <span class="tslot" title="Heavy jobs (test suites, builds, e2e runs) run when their share of the host (CPU and memory) fits">
        {s}
      </span>
    </>
  );
}

/** Decision 0029 S1: the job a thread's slot runs ("yarn build · 3.0 GB"), or null; the row puts it first on its line. */
export function useThreadJob(id: string): string | null {
  return threadJob(useLoad(), id);
}

export function ThreadJob({ text }: { text: string }) {
  return (
    <span class="tslot" title="The heavy job this thread runs, and its share of the host (the larger of CPU and memory)">
      {text}
    </span>
  );
}

/** The rail's button under a host's meter (SPEC 8.2). Desktop only: the phone shows no host load (V0.99 (3)) and has no
 *  rail. One thread: a press opens it. Several: a menu of them, each with its use. Null when none is counted. */
function RailUndeclared({ h }: { h: HostLoad }) {
  const nav = useUnd();
  const [at, setAt] = useState<{ x: number; y: number } | null>(null);
  const r = railUndeclared(h, nav.lookup);
  if (!r) return null;
  return (
    <>
      <button
        type="button"
        class="rund"
        title={r.title}
        aria-label={r.aria}
        aria-haspopup={r.only ? undefined : "menu"}
        onClick={(e) => {
          if (r.only) return nav.openThread(r.only);
          const b = (e.currentTarget as HTMLElement).getBoundingClientRect();
          setAt({ x: b.right + 6, y: b.top });
        }}
      >
        <UndIcon size={9} />
        {r.count}
      </button>
      {at && <ContextMenu x={at.x} y={at.y} items={r.entries.map((x) => ({ id: x.id, label: x.label }))} onPick={(it) => (setAt(null), nav.openThread(it.id))} onClose={() => setAt(null)} />}
    </>
  );
}

