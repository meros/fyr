// SPEC 0.5 V0.80: the machine load in the desktop rail and a working thread's CPU in its row. One poll for the
// whole client, every 5 s while the page is visible; the host reads at most every 2 s.

import { useEffect, useState } from "preact/hooks";
import type { Api } from "./api.ts";
import { useApi } from "./api-context.ts";
import { loadDetail, MAX_PIPS, railMeter, railPips, railSlots, threadCPU, threadSlot, type HostLoad, type RailPips, type Slots } from "./load.ts";

const POLL_MS = 5_000;

let source: Pick<Api, "load"> | null = null;
let current: HostLoad[] = [];
let lastAt = 0;
let timer: ReturnType<typeof setInterval> | undefined;
const subs = new Set<(l: HostLoad[]) => void>();

async function poll(): Promise<void> {
  if (!source || (typeof document !== "undefined" && document.visibilityState === "hidden")) return;
  lastAt = Date.now();
  const l = await source.load().catch(() => current);
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
  return (
    <div class="rld">
      {list.map((h) => {
        const m = railMeter(h);
        const p = railPips(h);
        const sl = railSlots(h);
        const label = `Load ${m.text} on ${h.cores} cores${p ? `, ${p.held} of ${p.n} slots in use${p.waiting ? `, ${p.waiting} waiting` : ""}` : ""}`;
        return (
          <div class={`rldr ${m.level}`} title={loadDetail(h).join("\n")} role="img" aria-label={label}>
            <LoadRing share={m.share} text={m.text} />
            {p && (p.n <= MAX_PIPS ? <SlotPips p={p} /> : sl && <small class={`rlds${sl.hot ? " hot" : ""}`}>{`${p.held}/${p.n}`}</small>)}
          </div>
        );
      })}
    </div>
  );
}

const R = 12;
const C = 2 * Math.PI * R;

function LoadRing({ share, text }: { share: number; text: string }) {
  return (
    <span class="rring" aria-hidden="true">
      <svg viewBox="0 0 30 30" width="30" height="30">
        <circle class="trk" cx="15" cy="15" r={R} />
        {share > 0 && <circle class="arc" cx="15" cy="15" r={R} stroke-dasharray={`${(share * C).toFixed(2)} ${C.toFixed(2)}`} transform="rotate(-90 15 15)" />}
      </svg>
      <b>{text}</b>
    </span>
  );
}

/** Filled pips for the slots in use, empty for the free ones; "+2" in the hot colour while threads wait. */
function SlotPips({ p }: { p: RailPips }) {
  return (
    <span class={`rpips${p.waiting ? " hot" : ""}`} aria-hidden="true">
      {Array.from({ length: p.n }, (_, i) => (
        <i class={i < p.held ? "on" : undefined} />
      ))}
      {p.waiting > 0 && <em>+{p.waiting}</em>}
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

/** V0.86: " · waiting for a test slot (2 ahead)" or " · test slot" on a thread's row; nothing without one. */
export function ThreadSlot({ id }: { id: string }) {
  const s = threadSlot(useLoad(), id);
  if (!s) return null;
  return (
    <>
      {" · "}
      <span class="tslot" title="Heavy jobs (test suites, builds, e2e runs) take a slot; a few run at once across the threads">
        {s}
      </span>
    </>
  );
}
