// Quiet projects in the UI (V0.74, V0.114, V0.46): the hold that shows them, the one unlock, the gate, and the
// quiet-hours group of the project page.
import { useEffect, useRef, useState } from "preact/hooks";
import type { Project, QuietHours } from "./types.ts";
import { DEFAULT_QUIET, QUIET_HOLD_MS, QUIET_PRESETS, WEEK, hasWindow, isAllDay, loadQuietShow, quietLine, quietNote, setAllDay, setQuietShow, subscribeQuietShow, toggleDay } from "./quiet.ts";
import { browserStorage } from "./prefs.ts";
import { Group, Switch, useSet } from "./settings-rows.tsx";
import { clockAt } from "./format.ts";

/** V0.74: the row under New thread's project list while a project is quiet. Holding it (finger or mouse button) for
 *  30 s shows quiet projects for 30 minutes; lifting, moving off or closing the list before then starts over. No key
 *  and no tap, on purpose. */
export function QuietHold({ count, showUntil, show, hide, text }: { count: number; showUntil: number; show(): void; hide(): void; text?: string }) {
  const [start, setStart] = useState<number | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const done = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const stop = () => {
    clearTimeout(done.current);
    setStart(null);
  };
  useEffect(() => () => clearTimeout(done.current), []);
  useEffect(() => {
    if (start === null) return;
    const id = setInterval(() => setNow(Date.now()), 200);
    return () => clearInterval(id);
  }, [start]);
  if (!count) return null;
  const n = count === 1 ? "1 quiet project" : `${count} quiet projects`;
  if (showUntil) {
    const hm = clockAt(showUntil);
    return (
      <div class="qhold on">
        <span class="qht">Quiet projects show until {hm}</span>
        <button type="button" class="btn sm" onClick={hide}>
          Hide
        </button>
      </div>
    );
  }
  const begin = (e: PointerEvent) => {
    if (e.button !== 0) return;
    e.preventDefault();
    clearTimeout(done.current);
    const t = Date.now();
    setStart(t);
    setNow(t);
    done.current = setTimeout(() => {
      setStart(null);
      show();
    }, QUIET_HOLD_MS);
  };
  const part = start === null ? 0 : Math.min(1, (now - start) / QUIET_HOLD_MS);
  const left = Math.max(1, Math.ceil((QUIET_HOLD_MS - (now - (start ?? now))) / 1000));
  return (
    <div
      class={`qhold${start !== null ? " held" : ""}`}
      onPointerDown={begin}
      onPointerUp={stop}
      onPointerLeave={stop}
      onPointerCancel={stop}
      onContextMenu={(e) => {
        e.preventDefault();
        e.stopPropagation();
      }}
    >
      <span class="qhf" style={{ width: `${part * 100}%` }} />
      <span class="qht">{start === null ? (text ?? `${n} hidden. Hold here for 30 s to show them for 30 minutes.`) : `Keep holding · ${left} s`}</span>
    </div>
  );
}

export interface QuietUnlock {
  /** the end of the unlock on this device on the host's clock, or 0 when none runs */
  showUntil: number;
  show(): void;
  hide(): void;
}

/** V0.114: the one unlock of quiet projects on this device. New thread, the project filter, search and the gate on a
 *  quiet thread all read it, so one hold opens every path and Hide shuts every path; another tab follows through
 *  the storage event. It re-renders when the 30 minutes end. clockOffset: the host's clock minus this browser's. */
export function useQuietUnlock(clockOffset = 0): QuietUnlock {
  const [end, setEnd] = useState(() => loadQuietShow(browserStorage()));
  useEffect(() => {
    const off = subscribeQuietShow(setEnd);
    const onStorage = (e: StorageEvent) => e.key === "fyr.quietShowUntil" && setEnd(loadQuietShow(browserStorage()));
    window.addEventListener("storage", onStorage);
    return () => (off(), window.removeEventListener("storage", onStorage));
  }, []);
  const [, setTick] = useState(0);
  useEffect(() => {
    const left = end - (Date.now() + clockOffset);
    if (left <= 0) return;
    const t = setTimeout(() => setTick((n) => n + 1), Math.min(left + 250, 2 ** 31 - 1));
    return () => clearTimeout(t);
  }, [end, clockOffset]);
  return {
    showUntil: end > Date.now() + clockOffset ? end : 0,
    show: () => void setQuietShow(browserStorage(), Date.now() + clockOffset),
    hide: () => void setQuietShow(browserStorage(), null),
  };
}

/** V0.114: what shows instead of a quiet project's threads (the project filter, search) or a quiet thread (a link, back
 *  and forward, a notice, a board card, ?open=, a push): the project is quiet until HH:MM, and the 30 s hold. */
export function QuietGate({ project, now, unlock, what }: { project?: Project; now: number; unlock: QuietUnlock; what: "thread" | "threads" | "search" }) {
  const name = project?.name ?? "This project";
  const note = quietNote(project, now);
  const head =
    what === "search" ? "Threads of quiet projects are left out of search." : `${name} is ${note ? note.replace(/^Quiet/, "quiet") : "quiet"}.`;
  const body = what === "thread" ? "This thread is in its quiet hours." : what === "threads" ? "Its threads are hidden in its quiet hours." : null;
  return (
    <div class="qgate" role="region" aria-label="Quiet hours">
      <div class="qgh">{head}</div>
      {body && <div class="qgb">{body}</div>}
      <QuietHold count={1} showUntil={0} show={unlock.show} hide={unlock.hide} text="Hold here for 30 s to open quiet projects for 30 minutes." />
    </div>
  );
}

const HOURS = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, "0"));
const MINUTES = Array.from({ length: 12 }, (_, i) => String(i * 5).padStart(2, "0"));

/** A 24-hour HH:MM picker from two selects, so no browser locale turns it into AM/PM (decision 0014 addendum). */
function TimePick({ value, label, onChange }: { value: string; label: string; onChange(v: string): void }) {
  const env = useSet();
  const m = /^(\d{1,2}):(\d\d)$/.exec(value);
  const h = m ? m[1].padStart(2, "0") : "00";
  const mm = m ? m[2] : "00";
  const mins = MINUTES.includes(mm) ? MINUTES : [...MINUTES, mm].sort();
  return (
    <span class="qtime" role="group" aria-label={label}>
      <select value={h} aria-label={`${label}, hour`} disabled={env.readOnly} onChange={(e) => onChange(`${(e.target as HTMLSelectElement).value}:${mm}`)}>
        {HOURS.map((x) => (
          <option value={x}>{x}</option>
        ))}
      </select>
      <span aria-hidden="true">:</span>
      <select value={mm} aria-label={`${label}, minutes`} disabled={env.readOnly} onChange={(e) => onChange(`${h}:${(e.target as HTMLSelectElement).value}`)}>
        {mins.map((x) => (
          <option value={x}>{x}</option>
        ))}
      </select>
    </span>
  );
}

/** V0.46, the decision 0014 addendum, V0.119: the project page's Quiet hours group. One row per window: from–to in
 *  24-hour time, an All day switch, ×, and the seven day chips under them; the last row adds a window or a preset
 *  that is not there yet; the foot says what all the rows do. No rows is off. */
export function QuietGroup({ value, onChange, note }: { value: QuietHours[]; onChange(q: QuietHours[]): void; note?: string | null }) {
  const env = useSet();
  const set = (i: number, w: QuietHours) => onChange(value.map((x, k) => (k === i ? w : x)));
  const presets = QUIET_PRESETS.filter((p) => !hasWindow(value, p.window));
  return (
    <Group title="Quiet hours" end={note ?? undefined} class="qfld" label="Quiet hours" foot={quietLine(value)}>
      {value.map((w, i) => {
        const days = new Set(w.days?.length ? w.days : WEEK.map((d) => d.key));
        const all = isAllDay(w);
        return (
          <div class="sr col qwin" key={i}>
            <div class="qw-top">
              {!all && (
                <span class="qtimes">
                  <TimePick value={w.from} label="Quiet from" onChange={(v) => set(i, { ...w, from: v })} />
                  <span aria-hidden="true">–</span>
                  <TimePick value={w.to} label="Quiet until" onChange={(v) => set(i, { ...w, to: v })} />
                </span>
              )}
              <label class={`allday qtog${all ? " first" : ""}`}>
                All day
                <Switch on={all} label="All day" onChange={(on) => set(i, setAllDay(w, on))} />
              </label>
              <button type="button" class="xb qrm" aria-label="Remove these quiet hours" title="Remove" disabled={env.readOnly} onClick={() => onChange(value.filter((_, k) => k !== i))}>
                ×
              </button>
            </div>
            <div class="qd qdays" role="group" aria-label="Days">
              {WEEK.map((d) => (
                <button type="button" class={days.has(d.key) ? "on" : ""} aria-pressed={days.has(d.key)} disabled={env.readOnly} onClick={() => set(i, { ...w, days: toggleDay(w.days, d.key) })}>
                  {d.label}
                </button>
              ))}
            </div>
          </div>
        );
      })}
      <div class="sr qadd">
        <div class="chips">
          <button type="button" class="btn" disabled={env.readOnly} onClick={() => onChange([...value, { ...DEFAULT_QUIET }])}>
            Add quiet hours
          </button>
          {presets.map((p) => (
            <button type="button" class="btn qpre" disabled={env.readOnly} onClick={() => onChange([...value, { ...p.window, days: p.window.days && [...p.window.days] }])}>
              {p.label} <span>{p.hint}</span>
            </button>
          ))}
        </div>
      </div>
    </Group>
  );
}
