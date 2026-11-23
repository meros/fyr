// V0.ai-search: the AI part of a search, one component for the desktop list and the phone's Search screen. The typed
// search stays instant and free; this runs only on the owner's Enter or press, since each run is a model call.

import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { askPerHost, mergeHits, type AiHit } from "./ai-search.ts";
import { bare, ref } from "./hostfeed.ts";
import { LINE_ICONS } from "./lucide.ts";
import type { Thread } from "./types.ts";

type Phase = { name: "idle" } | { name: "running" } | { name: "done"; hits: AiHit[]; missed: string } | { name: "failed"; why: string };

interface AiSearch {
  query: string;
  phase: Phase;
  byId: Map<string, Thread>;
  run(): void;
  cancel(): void;
}

export function useAiSearch(rawQuery: string, threads: readonly Thread[]): AiSearch {
  const api = useApi();
  const query = rawQuery.trim();
  const [phase, setPhase] = useState<Phase>({ name: "idle" });
  const abort = useRef<AbortController | null>(null);
  // By `<hostId>/<the host's id>`, what mergeHits gives, whatever form the client's own id has.
  const byId = useMemo(() => new Map(threads.flatMap((t) => (t.hostId ? [[ref(t.hostId, t.rid ?? bare(t.hostId, t.id)), t] as const] : []))), [threads]);
  const latest = useRef(threads);
  latest.current = threads;

  const cancel = () => {
    abort.current?.abort();
    abort.current = null;
    setPhase({ name: "idle" });
  };
  // A changed query makes the answer stale, and leaving the search ends the call.
  useEffect(() => cancel, [query]);

  const run = () => {
    if (!query || abort.current) return;
    const ask = askPerHost(latest.current);
    if (!ask.size) {
      setPhase({ name: "failed", why: "No host that is online has threads to search." });
      return;
    }
    const ctl = new AbortController();
    abort.current = ctl;
    setPhase({ name: "running" });
    const hosts = [...ask.keys()];
    void Promise.all(hosts.map((h) => api.searchThreads(h, query, ask.get(h)!, ctl.signal))).then((rs) => {
      if (ctl.signal.aborted) return;
      abort.current = null;
      const answers = rs.flatMap((r, i) => (r.ok ? [{ host: hosts[i], hits: r.data }] : []));
      const errors = rs.flatMap((r) => (r.ok ? [] : [r.error]));
      if (!answers.length) setPhase({ name: "failed", why: errors[0] ?? "The search failed." });
      else setPhase({ name: "done", hits: mergeHits(answers), missed: errors.length ? `${errors.length} of ${hosts.length} hosts did not answer: ${errors[0]}` : "" });
    });
  };
  return { query, phase, byId, run, cancel };
}

function Sparkles({ spin }: { spin?: boolean }) {
  return <svg class={spin ? "spin" : undefined} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" dangerouslySetInnerHTML={{ __html: LINE_ICONS.sparkles }} />;
}

export function AiSearchPanel({ ai, onOpen }: { ai: AiSearch; onOpen(id: string): void }) {
  const { phase, query } = ai;
  if (!query) return null;
  // A thread that left the reach of search since the answer (a quiet hour began, it was archived) is not offered.
  const hits = phase.name === "done" ? phase.hits.flatMap((h) => (ai.byId.has(h.id) ? [{ ...h, t: ai.byId.get(h.id)! }] : [])) : [];
  return (
    <section class="ais" aria-label="AI search" aria-live="polite">
      {phase.name === "idle" && (
        <button type="button" class="ais-ask" onClick={ai.run}>
          <Sparkles />
          <span>Ask AI to find “{query}”</span>
        </button>
      )}
      {phase.name === "running" && (
        <div class="ais-st">
          <Sparkles spin />
          <span class="ais-note">Reading your threads…</span>
          <button type="button" class="ais-x" onClick={ai.cancel}>Cancel</button>
        </div>
      )}
      {phase.name === "failed" && (
        <div class="ais-st">
          <Sparkles />
          <span class="ais-note">AI search failed: {phase.why}</span>
          <button type="button" class="ais-x" onClick={ai.run}>Try again</button>
        </div>
      )}
      {phase.name === "done" && (
        <>
          <div class="ais-st">
            <Sparkles />
            <span class="ais-note">{hits.length ? `AI found ${hits.length === 1 ? "1 thread" : `${hits.length} threads`}` : "AI found no thread that fits."}</span>
          </div>
          {hits.map((h) => (
            <button type="button" class="ais-hit" key={h.id} onClick={() => onOpen(h.t.id)}>
              <span class="ais-t">{h.t.title || "Untitled"}</span>
              <span class="ais-w">{[h.t.project, h.why].filter(Boolean).join(" · ")}</span>
            </button>
          ))}
          {phase.missed && <div class="ais-st"><span class="ais-note">{phase.missed}</span></div>}
        </>
      )}
    </section>
  );
}
