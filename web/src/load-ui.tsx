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
import { loadDetail, normalizeLoad, railBudget, railLabel, railMeter, shortHostNames, slotTip, STRIP_MAX, undeclaredTip, type StripHost, threadCPU, threadJob, threadSlot, type HostLoad, type RailBudget, type Slots } from "./load.ts";

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

/** The rail (desktop), V0.99: each host's 1-minute load as a ring (the share of the cores, coloured calm, warm or
 *  hot) with the figure inside, and the slots for heavy jobs as pips under it; the exact values on hover. */
export function LoadRail() {
  const list = useLoad();
  if (!list.length) return null;
  const several = list.length > 1;
  const short = shortHostNames(list.map((h) => h.host));
  const und = useUnd();
  return (
    <div class="rld">
      {list.map((h) => {
        const m = railMeter(h);
        const bud = railBudget(h);
        const label = railLabel(h, several); // V0.140: with several hosts each ring names its host
        return (
          <div class="rldw">
          <div class={`rldr ${m.level}`} title={loadDetail(h).join("\n")} role="img" aria-label={label}>
            <LoadRing share={m.share} text={m.text} />
            {several && (
              <small class="rldh mono" title={h.host}>
                {short.get(h.host)}
              </small>
            )}
            {bud && <SlotBar b={bud} tip={slotTip(h, { lookup: und.lookup, now: und.now() })} />}
          </div>
          <RailUndeclared h={h} />
          </div>
        );
      })}
    </div>
  );
}

/** V0.166: the phone's load strip, in the inbox header: per host the rail's ring (the load as a share of the cores),
 *  its short name with several hosts, and the heavy jobs' use as a percentage (V0.163) in the hot colour while jobs
 *  wait; an offline host as an empty ring and "off". One button: a tap opens the sheet. The same pieces as the rail. */
export function LoadStrip({ hosts, onOpen }: { hosts: StripHost[]; onOpen(): void }) {
  if (!hosts.length) return null;
  const several = hosts.length > 1;
  const short = shortHostNames(hosts.map((h) => h.name));
  // the hosts with a load first (in the owner's order), so an offline host never hides a live one behind "+n"
  const shown = [...hosts.filter((h) => h.load), ...hosts.filter((h) => !h.load)].slice(0, STRIP_MAX);
  const label = hosts.map((h) => (h.load ? railLabel(h.load, true) : `${h.name}: offline`)).join("; ");
  return (
    <button type="button" class="m-load" onClick={onOpen} aria-label={`Host load: ${label}. Show details`}>
      {shown.map((h) => {
        if (!h.load) {
          return (
            <span class="mlh off">
              <LoadRing share={0} text="–" />
              <span class="mlc">
                {several && <small class="mono">{short.get(h.name)}</small>}
                <small class="mono">off</small>
              </span>
            </span>
          );
        }
        const m = railMeter(h.load);
        const bud = railBudget(h.load);
        return (
          <span class={`mlh rldr ${m.level}`}>
            <LoadRing share={m.share} text={m.text} />
            <span class="mlc">
              {several && <small class="mono">{short.get(h.name)}</small>}
              {bud && <small class={`mono${bud.hot ? " hot" : ""}`}>{bud.text.replace(" ", "")}{bud.waiting > 0 && <em>+{bud.waiting}</em>}</small>}
            </span>
          </span>
        );
      })}
      {hosts.length > shown.length && <span class="mlmore mono">+{hosts.length - shown.length}</span>}
    </button>
  );
}

/** V0.166: the phone's load sheet: per host the ring, the full name and state, the heavy jobs' bar, and the rail's
 *  tooltip lines as text (the load, the signals, CPU and memory in use and their budget, the running jobs with their
 *  share, who waits and why, undeclared work); then the provider quotas. */
export function LoadSheetBody({ hosts, quotas }: { hosts: StripHost[]; quotas: ComponentChildren }) {
  const und = useUnd();
  return (
    <div class="m-lsheet">
      {hosts.map((h) => {
        const m = h.load && railMeter(h.load);
        const bud = h.load && railBudget(h.load);
        const lines = h.load ? loadDetail(h.load) : [];
        const ut = h.load ? undeclaredTip(h.load, und.lookup, und.now()) : "";
        return (
          <section class={`mls${h.load ? "" : " off"}`} aria-label={h.name}>
            <div class={`mlsh rldr ${m ? m.level : ""}`}>
              <LoadRing share={m ? m.share : 0} text={m ? m.text : "–"} />
              <span class="mlsn">
                <b class="mono ell">{h.name}</b>
                <small>{h.load ? `Load ${m!.text} of the cores` : `Offline${h.offline && clockTime(h.offline) ? ` since ${clockTime(h.offline)}` : ""}; no load until it is back`}</small>
              </span>
              {bud && <SlotBar b={bud} tip="" />}
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

const R = 12;
const C = 2 * Math.PI * R;

/** V0.152: the figure is a share of the cores; its "%" is set smaller so three digits fit the ring. */
function LoadRing({ share, text }: { share: number; text: string }) {
  const pct = text.endsWith("%");
  return (
    <span class="rring" aria-hidden="true">
      <svg viewBox="0 0 30 30" width="30" height="30">
        <circle class="trk" cx="15" cy="15" r={R} />
        {share > 0 && <circle class="arc" cx="15" cy="15" r={R} stroke-dasharray={`${(share * C).toFixed(2)} ${C.toFixed(2)}`} transform="rotate(-90 15 15)" />}
      </svg>
      <b class={text.length > 3 ? "w3" : undefined}>
        {pct ? text.slice(0, -1) : text}
        {pct && <small>%</small>}
      </b>
    </span>
  );
}

/** Decision 0029 S2, V0.152, V0.163: heavy jobs' use of their budget as a thin bar with its percentage under it (the
 *  tighter of CPU and memory), "+2" in the hot colour while jobs wait; both resources and every job on hover. No count. */
function SlotBar({ b, tip }: { b: RailBudget; tip: string }) {
  return (
    <span class={`rbar${b.hot ? " hot" : ""}`} title={tip}>
      <span class="trk">
        <i style={{ width: `${Math.round(b.share * 100)}%` }} />
        {/* V0.164: the use Fyr did not admit, hatched, after the admitted part */}
        {!!b.und && <i class="und" style={{ width: `${Math.round(b.und * 100)}%` }} />}
      </span>
      <small class="mono">
        {b.text}
        {b.waiting > 0 && <em>+{b.waiting}</em>}
      </small>
    </span>
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

