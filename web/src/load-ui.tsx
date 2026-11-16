// SPEC 0.5 V0.80: the machine load in the desktop rail and a working thread's CPU in its row. One poll for the
// whole client, every 5 s while the page is visible; the host reads at most every 2 s.

import { useEffect, useState } from "preact/hooks";
import type { Api } from "./api.ts";
import { useApi } from "./api-context.ts";
import { loadDetail, railLoad, threadCPU, type HostLoad } from "./load.ts";

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

/** The rail (desktop): each host's 1-minute load under a "load" label; the averages, pressure and agents on hover. */
export function LoadRail() {
  const list = useLoad();
  if (!list.length) return null;
  return (
    <div class="rld" aria-label="Machine load">
      {list.map((h) => {
        const f = railLoad(h);
        return (
          <div class="rldr" title={loadDetail(h).join("\n")}>
            <span class={f.hot ? "hot" : undefined}>{f.text}</span>
            <small>load</small>
          </div>
        );
      })}
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
