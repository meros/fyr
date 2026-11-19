// V0.status-home: the status screen. One component for both fronts (CLAUDE.md): the desktop draws it in the thread
// pane while no thread is open, the phone as its own Status screen. The phone passes `phone` for its 44 px targets and
// one-column tiles (touch, a 360 px screen); the desktop adds its key hints (it has a keyboard).

import { useEffect, useMemo, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { HOME_LOG, HOME_MOVES, homeLog, homeMoves, homeNeeds, homeRunning, homeSummary, lookWindow, runningLine, startOfDay, type HomeMove } from "./home.ts";
import type { HostInfo } from "./hosts.ts";
import { HostMeters, ThreadCPU, useLoad } from "./load-ui.tsx";
import { stripHosts } from "./load.ts";
import { ProjectIcon } from "./project-ui.tsx";
import { projectOf } from "./projects.ts";
import { QuotaLine, useQuotas } from "./quota-ui.tsx";
import { SECTIONS, type Sections } from "./store.ts";
import { clockTime, relTime } from "./format.ts";
import type { Profile, Project, Thread } from "./types.ts";

const LOOK_KEY = "fyr.home.lookedAt";

function readLook(): number | null {
  try {
    const v = localStorage.getItem(LOOK_KEY);
    return v ? Number(v) : null;
  } catch {
    return null;
  }
}

function writeLook(at: number): void {
  try {
    localStorage.setItem(LOOK_KEY, String(at));
  } catch {
    /* storage blocked: the next visit covers a day */
  }
}

export interface HomeProps {
  /** what the inbox shows (its filters and quiet hours hold here too) */
  sec: Sections;
  projects: Project[];
  profiles: Profile[];
  hosts: HostInfo[];
  /** the server-adjusted clock */
  now: number;
  loaded: boolean;
  phone?: boolean;
  onOpen(id: string): void;
  /** go to Your move in the list */
  onAllNeeds(): void;
  /** the desktop's key hints; the phone has none */
  keys?: [string, string][];
}

export function HomeScreen(p: HomeProps) {
  const listed = useMemo(() => [...p.sec.pinned, ...SECTIONS.flatMap((s) => p.sec[s.key])], [p.sec]);
  const byId = useMemo(() => Object.fromEntries(listed.map((t) => [t.id, t])), [listed]);
  const needs = useMemo(() => homeNeeds(listed), [listed]);
  const moves = useMemo(() => homeMoves(needs.slice(0, HOME_MOVES), p.projects), [needs, p.projects]);
  const running = useMemo(() => homeRunning(listed), [listed]);
  // The window is fixed while the screen shows, so an entry does not leave under the owner's eyes; leaving the screen
  // (or the page going to the background) is the look.
  const [since] = useState(() => lookWindow(readLook(), Date.now()));
  useEffect(() => {
    const hide = () => document.visibilityState === "hidden" && writeLook(Date.now());
    document.addEventListener("visibilitychange", hide);
    return () => {
      document.removeEventListener("visibilitychange", hide);
      writeLook(Date.now());
    };
  }, []);
  const log = useMemo(() => homeLog(byId, p.projects, since), [byId, p.projects, since]);
  const mergedToday = useMemo(() => homeLog(byId, p.projects, startOfDay(p.now)).filter((e) => e.kind === "merged").length, [byId, p.projects, Math.floor(p.now / 60_000)]);
  const day = new Date(p.now).toLocaleDateString(undefined, { weekday: "long" });

  if (!p.loaded) return null;
  return (
    <div class={`home${p.phone ? " phone" : ""}`} aria-label="Status">
      <div class="st-body">
        <header class="st-head">
          <h1 class="st-day">
            {day} <span class="tnum">{clockTime(new Date(p.now).toISOString())}</span>
          </h1>
          <p class={`st-sum${needs.length ? " att" : ""}`}>{homeSummary({ running: running.filter((t) => t.running).length, needs: needs.length, mergedToday })}</p>
        </header>

        {moves.length > 0 && (
          <section class="st-blk" aria-label="Your move">
            <div class="st-bh">
              <span>Your move</span>
              {needs.length > moves.length && (
                <button class="st-more" onClick={p.onAllNeeds}>
                  All {needs.length} →
                </button>
              )}
            </div>
            {moves.map((m) => (
              <MoveRow key={m.thread.id} m={m} p={p} />
            ))}
          </section>
        )}

        {running.length > 0 && (
          <section class="st-blk" aria-label="Moving without you">
            <div class="st-bh">
              <span>Moving without you</span>
            </div>
            <div class="st-tiles">
              {running.map((t) => (
                <RunTile key={t.id} t={t} p={p} />
              ))}
            </div>
          </section>
        )}

        {!moves.length && !running.length && <div class="st-calm">All quiet. Start a thread{p.phone ? " with +" : " with c"}.</div>}

        {log.length > 0 && (
          <section class="st-blk" aria-label="Since you looked">
            <div class="st-bh">
              <span>Since you looked · {clockTime(new Date(since).toISOString())}</span>
            </div>
            <ul class="st-log">
              {log.slice(0, HOME_LOG).map((e) => (
                <li key={`${e.kind}${e.thread}${e.at}`}>
                  <button class="st-ev" onClick={() => p.onOpen(e.thread)}>
                    <time class="tnum">{clockTime(e.at)}</time>
                    <span class="ell">
                      <b class={`st-k ${e.kind}`}>{e.word}</b> {e.text}
                    </span>
                  </button>
                </li>
              ))}
              {log.length > HOME_LOG && <li class="st-lmore">and {log.length - HOME_LOG} more</li>}
            </ul>
          </section>
        )}
      </div>
      <SystemStrip hosts={p.hosts} profiles={p.profiles} keys={p.keys} />
    </div>
  );
}

function MoveRow({ m, p }: { m: HomeMove; p: HomeProps }) {
  const api = useApi();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const t = m.thread;
  const press = async () => {
    if (!m.merge) return p.onOpen(t.id);
    setBusy(true);
    setErr("");
    try {
      const r = await api.command(m.merge.host, "action.run", m.merge.thread, m.merge.args);
      if (!r.ok) setErr(r.error ?? "Not merged");
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div class="st-mv">
      <button class="st-mvt" onClick={() => p.onOpen(t.id)}>
        <span class="st-tt ell">
          <ProjectIcon project={projectOf(t, p.projects)} name={t.project} size={14} />
          {t.title}
        </span>
        <small class="ell">
          <b>{m.state}</b>
          {m.meta ? ` · ${m.meta}` : ""}
        </small>
        {err && <small class="st-err">{err}</small>}
      </button>
      <button class="btn p st-act" disabled={busy} onClick={() => void press()}>
        {busy ? "Merging…" : m.verb}
      </button>
    </div>
  );
}

function RunTile({ t, p }: { t: Thread; p: HomeProps }) {
  const since = t.running && t.runStartedAt ? relTime(t.runStartedAt, p.now) : "";
  return (
    <button class={`st-tile${t.running ? " on" : ""}`} onClick={() => p.onOpen(t.id)}>
      <span class="st-tt ell">
        <ProjectIcon project={projectOf(t, p.projects)} name={t.project} size={14} />
        {t.title}
      </span>
      <span class="st-line ell">
        {t.running && <i class="st-dot" aria-hidden="true" />}
        {runningLine(t)}
      </span>
      <span class="st-meta tnum ell">
        {[p.hosts.length > 1 ? t.host : "", since].filter(Boolean).join(" · ")}
        {t.running && <ThreadCPU id={t.id} />}
      </span>
    </button>
  );
}

/** The machines and the quotas, at the foot: each host's two meters (as on the rail and the phone's strip) and every
 *  provider's quota line; the desktop adds its keys. */
function SystemStrip({ hosts, profiles, keys }: { hosts: HostInfo[]; profiles: Profile[]; keys?: [string, string][] }) {
  const strip = stripHosts(useLoad(), hosts);
  const quotas = useQuotas();
  if (!strip.length && !quotas.length && !keys?.length) return null;
  // One host is not named, as on the rail (V0.140).
  const several = strip.length > 1;
  return (
    <footer class="st-sys" aria-label="Hosts and quotas">
      {strip.map((h) => (
        <span class="st-host">
          <HostMeters h={h.load} offline={!h.load} name={several ? h.name : undefined} full={h.name} />
        </span>
      ))}
      {quotas.map((q) => (
        <span class="st-q">
          <QuotaLine q={q} name={profiles.find((x) => x.id === q.profile)?.name ?? q.profile} bare={false} />
        </span>
      ))}
      {keys && keys.length > 0 && (
        <span class="st-keys">
          {keys.map(([k, l]) => (
            <span>
              <kbd>{k}</kbd> {l}
            </span>
          ))}
        </span>
      )}
    </footer>
  );
}
