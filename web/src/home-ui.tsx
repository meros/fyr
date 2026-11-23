// V0.status-home: the status screen. One component for both fronts (CLAUDE.md): the desktop draws it in the thread
// pane while no thread is open, the phone as its own Status screen. The phone passes `phone` for its 44 px targets and
// one-column tiles (touch, a 360 px screen); the desktop adds its key hints (it has a keyboard).

import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { HOME_LOG, HOME_MOVES, homeDay, homeLog, hourTicks, openAt, type HomeDay, homeMoves, homeNeeds, homeRunning, homeSummary, lookWindow, runningLine, startOfDay, type HomeMove } from "./home.ts";
import { profilesOf, type HostInfo, type HostProfiles } from "./hosts.ts";
import { HostMeters, ThreadCPU, useLoad } from "./load-ui.tsx";
import { stripHosts } from "./host-load.ts";
import { ProjectIcon } from "./project-ui.tsx";
import { projectOf } from "./projects.ts";
import { QuotaLine, useQuotas } from "./quota-ui.tsx";
import { UsageBars } from "./usage-ui.tsx";
import { todayHours, todayRange, tokens, type HostUsage } from "./usage.ts";
import { SECTIONS, type Sections } from "./store.ts";
import { clockTime, minuteOf, relTime } from "./format.ts";
import { storedGet, storedSet } from "./prefs.ts";
import { hostVersions } from "./updates.ts";
import type { Project, Thread } from "./types.ts";

const LOOK_KEY = "fyr.home.lookedAt";

function readLook(): number | null {
  const v = storedGet(LOOK_KEY);
  return v ? Number(v) : null;
}

function writeLook(at: number): void {
  storedSet(LOOK_KEY, String(at));
}

interface HomeProps {
  /** what the inbox shows (its filters and quiet hours hold here too) */
  sec: Sections;
  projects: Project[];
  /** slice 2b: each host's profiles, for the quota lines' names */
  profiles: HostProfiles;
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
  // eslint-disable-next-line react-hooks/exhaustive-deps -- recomputed once a minute (the minute of now), not on each tick of now
  const mergedToday = useMemo(() => homeLog(byId, p.projects, startOfDay(p.now)).filter((e) => e.kind === "merged").length, [byId, p.projects, minuteOf(p.now)]);
  // eslint-disable-next-line react-hooks/exhaustive-deps -- recomputed once a minute (the minute of now), not on each tick of now
  const today = useMemo(() => homeDay(byId, p.projects, p.now), [byId, p.projects, minuteOf(p.now)]);
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

        <Today day={today} phone={p.phone} onOpen={p.onOpen} />
        <TodayTokens now={p.now} phone={p.phone} />

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

const hhmm = (at: number) => clockTime(new Date(at).toISOString());

/** V0.usage-page: today's tokens on every host, one bar per local hour from midnight to now (the hour now drawn
 *  lighter: it is still filling), read again each minute. One series, one hue, no legend; a bar names its hour and
 *  count on hover, and Settings → Usage has the table. The same block on both fronts; none on a front that cannot read
 *  usage, or before a host counted anything today. */
function TodayTokens({ now, phone }: { now: number; phone?: boolean }) {
  const api = useApi();
  const apiRef = useRef(api);
  apiRef.current = api;
  const [hosts, setHosts] = useState<HostUsage[] | null>(null);
  const minute = minuteOf(now);
  useEffect(() => {
    const a = apiRef.current;
    if (!a.usage) return;
    let live = true;
    a.usage({ ...todayRange(Date.now()), group: ["hour"] }).then((r) => live && setHosts(r.hosts)).catch(() => {});
    return () => {
      live = false;
    };
  }, [minute]);
  if (!hosts) return null;
  const hours = todayHours(hosts, now);
  const total = hours.reduce((n, h) => n + h.total, 0);
  if (!total) return null;
  const ticks = (phone ? [0, 12] : [0, 6, 12, 18]).filter((h) => h < hours.length);
  return (
    <section class="st-blk st-tokens" aria-label="Today's tokens">
      <div class="st-bh">
        <span>Today · tokens</span>
      </div>
      <div class="st-nums">
        <div class="st-num">
          <b class="tnum">{tokens(total)}</b>
          <span>tokens so far</span>
          <small>busiest {hours.reduce((a, b) => (b.total > a.total ? b : a)).at}</small>
        </div>
      </div>
      <UsageBars bars={hours} label={`Tokens per hour today: ${tokens(total)} so far`} ticks={ticks} cls="st-ug" />
    </section>
  );
}

/** V0.day-chart: the day so far: four numbers, then the open changes from midnight to now as a step area, with a dot
 *  on the axis for each merge. Hover (or a touch) shows the time and the open count; a dot near the pointer names the
 *  merge, and a click on it opens its thread. The phone's chart is shorter and its numbers sit two by two (a 360 px
 *  screen). */
function Today({ day, phone, onOpen }: { day: HomeDay; phone?: boolean; onOpen(id: string): void }) {
  const box = useRef<HTMLDivElement>(null);
  const [w, setW] = useState(0);
  const [hx, setHx] = useState<number | null>(null);
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setW(el.clientWidth));
    ro.observe(el);
    setW(el.clientWidth);
    return () => ro.disconnect();
  }, []);
  const quiet = day.steps.length === 1 && day.steps[0].open === 0 && !day.started;
  const H = phone ? 96 : 120;
  const pad = { l: 26, r: 8, t: 10, b: 20 };
  const top = Math.max(2, day.peak);
  const span = Math.max(1, day.to - day.from);
  const x = (at: number) => pad.l + ((at - day.from) / span) * Math.max(0, w - pad.l - pad.r);
  const y = (n: number) => pad.t + (1 - n / top) * (H - pad.t - pad.b);
  const base = y(0);
  let line = "";
  day.steps.forEach((s, i) => {
    const sx = x(s.at);
    line += i === 0 ? `M${sx},${y(s.open)}` : `H${sx}V${y(s.open)}`;
  });
  line += `H${x(day.to)}`;
  const area = `${line}V${base}H${x(day.from)}Z`;
  const at = hx === null ? null : day.from + ((hx - pad.l) / Math.max(1, w - pad.l - pad.r)) * span;
  const near = hx === null ? undefined : day.merges.find((m) => Math.abs(x(m.at) - hx) <= 8);
  const point = (e: PointerEvent) => {
    const r = (e.currentTarget as SVGElement).getBoundingClientRect();
    const px = e.clientX - r.left;
    setHx(px < pad.l || px > w - pad.r ? null : px);
  };
  const tipAt = near ? near.at : at;
  const tipLeft = hx === null ? 0 : Math.min(Math.max(hx, 70), w - 70);
  return (
    <section class="st-blk st-today" aria-label="Today">
      <div class="st-bh">
        <span>Today · open changes</span>
      </div>
      <div class="st-nums">
        <Num n={day.finished} label="finished" sub={day.merges.length ? `${day.merges.length} merged` : ""} />
        <Num n={day.opened} label="opened" />
        <Num n={day.open} label="open now" sub={day.peak > day.open ? `peak ${day.peak}` : ""} />
        <Num n={day.started} label={day.started === 1 ? "thread started" : "threads started"} />
      </div>
      {!quiet && (
        <div class="st-chart" ref={box} style={{ height: `${H}px` }}>
          {w > 0 && (
            <svg width={w} height={H} role="img" aria-label={`Open changes today: ${day.steps[0].open} at midnight, ${day.open} now, peak ${day.peak}; ${day.merges.length} merged`} onPointerMove={point} onPointerDown={point} onPointerLeave={() => setHx(null)} onClick={() => near && onOpen(near.thread)} style={{ cursor: near ? "pointer" : "default" }}>
              {[0, top].map((n) => (
                <g key={n}>
                  <line class="st-grid" x1={pad.l} x2={w - pad.r} y1={y(n)} y2={y(n)} />
                  <text class="st-ax" x={pad.l - 6} y={y(n) + 3} text-anchor="end">
                    {n}
                  </text>
                </g>
              ))}
              {hourTicks(day.from, day.to, phone ? 4 : 6).filter((t) => x(t) < w - pad.r - 16).map((t) => (
                <text key={t} class="st-ax" x={x(t)} y={H - 5} text-anchor="middle">
                  {hhmm(t)}
                </text>
              ))}
              <path class="st-area" d={area} />
              <path class="st-ln" d={line} />
              <circle class="st-now" cx={x(day.to)} cy={y(day.open)} r={3.5} />
              {day.merges.map((m) => (
                <circle key={`${m.thread}${m.at}`} class={`st-mg${near === m ? " hot" : ""}`} cx={x(m.at)} cy={base} r={near === m ? 5 : 4} />
              ))}
              {hx !== null && <line class="st-cross" x1={near ? x(near.at) : hx} x2={near ? x(near.at) : hx} y1={pad.t} y2={base} />}
            </svg>
          )}
          {hx !== null && tipAt !== null && (
            <div class="st-tip" style={{ left: `${tipLeft}px` }}>
              <span class="tnum">{hhmm(tipAt)}</span> · <b class="tnum">{openAt(day, tipAt)}</b> open
              {near && (
                <div class="ell">
                  <b class="st-k merged">Merged</b> {near.text}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </section>
  );
}

function Num({ n, label, sub }: { n: number; label: string; sub?: string }) {
  return (
    <div class="st-num">
      <b class="tnum">{n}</b>
      <span>{label}</span>
      {sub && <small>{sub}</small>}
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
function SystemStrip({ hosts, profiles, keys }: { hosts: HostInfo[]; profiles: HostProfiles; keys?: [string, string][] }) {
  const strip = stripHosts(useLoad(), hosts);
  const quotas = useQuotas();
  const versions = hostVersions(hosts); // V0.versioned-releases
  if (!strip.length && !quotas.length && !keys?.length && !versions.length) return null;
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
          <QuotaLine q={q} name={profilesOf(q, profiles).find((x) => x.id === q.profile)?.name ?? q.profile} bare={false} />
        </span>
      ))}
      {versions.map((v) => (
        <span class="st-ver" key={v}>
          {v}
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
