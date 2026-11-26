// Quiet projects in the UI (V0.74, V0.114, V0.46, V0.quiet-lift): the gate, the New thread line, and the quiet-hours
// group of the project page, where the quiet is started by hand and lifted with a 30 s hold.
import { useEffect, useRef, useState } from "preact/hooks";
import type { Project, QuietHours } from "./types.ts";
import { DEFAULT_QUIET, QUIET_HOLD_MS, QUIET_PRESETS, WEEK, endWords, hasWindow, isAllDay, liftUntil, manualUntil, nextClock, quietLiftChoices, quietLine, quietNote, quietNowChoices, quietNowLine, setAllDay, toggleDay } from "./quiet.ts";
import { Group, Switch, useSet } from "./settings-rows.tsx";

/** V0.quiet-lift: one lift choice of the project page, a row of the list ("30 minutes", "15:30"). Holding it (finger
 *  or mouse button) for 30 s lifts the quiet; lifting, moving off or the row going away before then starts over. No
 *  key and no tap, on purpose: the quiet is there to keep the owner out. */
function HoldChoice({ label, when, disabled, onDone }: { label: string; when: string; disabled?: boolean; onDone(): void }) {
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
  const begin = (e: PointerEvent) => {
    if (e.button !== 0 || disabled) return;
    e.preventDefault();
    clearTimeout(done.current);
    const t = Date.now();
    setStart(t);
    setNow(t);
    done.current = setTimeout(() => {
      setStart(null);
      onDone();
    }, QUIET_HOLD_MS);
  };
  const part = start === null ? 0 : Math.min(1, (now - start) / QUIET_HOLD_MS);
  const left = Math.max(1, Math.ceil((QUIET_HOLD_MS - (now - (start ?? now))) / 1000));
  return (
    <div
      class={`qli qhold${start !== null ? " held" : ""}${disabled ? " off" : ""}`}
      role="button"
      aria-disabled={disabled || undefined}
      aria-label={`Lift for ${label}: hold for 30 seconds`}
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
      <span class="qht">{start === null ? label : `Keep holding · ${left} s`}</span>
      <span class="qhw tnum">{when}</span>
    </div>
  );
}

/** V0.74, V0.quiet-lift: the line under New thread's project list while a project is quiet: they are hidden, and where
 *  their quiet is lifted. */
export function QuietHidden({ count }: { count: number }) {
  if (!count) return null;
  const n = count === 1 ? "1 quiet project hidden" : `${count} quiet projects hidden`;
  return (
    <div class="qhold on">
      <span class="qht">{n}. To start a thread in one, lift its quiet on its project page.</span>
    </div>
  );
}

/** V0.114: what shows instead of a quiet project's threads (the project filter, search) or a quiet thread (a link, back
 *  and forward, a notice, a board card, ?open=, a push): the project is quiet until HH:MM, and, V0.quiet-lift, the way
 *  to its project page, where the quiet is lifted. onProject opens it, from the owner's press only. */
export function QuietGate({ project, now, what, onProject }: { project?: Project; now: number; what: "thread" | "threads" | "search"; onProject?(id: string): void }) {
  const name = project?.name ?? "This project";
  const note = quietNote(project, now);
  const head =
    what === "search" ? "Threads of quiet projects are left out of search." : `${name} is ${note ? note.replace(/^Quiet/, "quiet") : "quiet"}.`;
  const body = what === "thread" ? "This thread is in its quiet hours." : what === "threads" ? "Its threads are hidden in its quiet hours." : null;
  const id = project?.id;
  return (
    <div class="qgate" role="region" aria-label="Quiet hours">
      <div class="qgh">{head}</div>
      {body && <div class="qgb">{body}</div>}
      <div class="qgf">
        <span class="qgb">{what === "search" ? "Lift a project's quiet on its page to search its threads." : "Lift the quiet on its project page."}</span>
        {id && onProject && what !== "search" && (
          <button type="button" class="btn sm" onClick={() => onProject(id)}>
            Project page
          </button>
        )}
      </div>
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

/** V0.quiet-now, V0.quiet-lift: the first row of Quiet hours, one place to quiet the project and to lift its quiet.
 *  Not quiet: Quiet now, a list of times like snooze's (1 hour, 3 hours, until 08:00, until a picked time), one tap
 *  each. Quiet: Lift the quiet, a list of times, each held for 30 s; End now ends a stretch started by hand. Lifted:
 *  the lift's end and Quiet again. now is the host's clock; quietEnd the project's quietUntil, or 0. */
function QuietNowRow({ manual, lift, quietEnd, now, onManual, onLift }: { manual?: string; lift?: string; quietEnd: number; now: number; onManual(end: string | null): void; onLift?(end: string | null): void }) {
  const env = useSet();
  const [at, setAt] = useState("22:00");
  const byHand = manualUntil({ quietNow: manual }, now) > 0;
  const lifted = liftUntil({ quietLift: lift }, now);
  const quiet = quietEnd > now;
  const iso = (end: number) => new Date(end).toISOString();
  if (lifted && onLift)
    return (
      <div class="sr col qnow">
        <div class="qw-top">
          <span class="sr-b">
            <span class="sr-t">Quiet lifted</span>
            <span class="sr-d">Its threads show and notify as usual, on every device, until {endWords(lifted, now)}.</span>
          </span>
          <button type="button" class="btn" disabled={env.readOnly} onClick={() => onLift(null)}>
            Quiet again
          </button>
        </div>
      </div>
    );
  if (quiet && onLift)
    return (
      <div class="sr col qnow">
        <div class="qw-top">
          <span class="sr-b">
            <span class="sr-t">Lift the quiet</span>
            <span class="sr-d">
              {byHand ? quietNowLine({ quietNow: manual }, now) : `${quietNote({ quietUntil: iso(quietEnd) }, now)}.`} Hold a time for 30 s to show its threads and send
              notifications again, on every device.
            </span>
          </span>
          {byHand && (
            <button type="button" class="btn" disabled={env.readOnly} onClick={() => onManual(null)}>
              End now
            </button>
          )}
        </div>
        <div class="qlist" role="group" aria-label="Lift the quiet for">
          {quietLiftChoices(now, quietEnd).map((c) => (
            <HoldChoice key={c.label} label={c.label} when={c.when} disabled={env.readOnly} onDone={() => onLift(iso(c.end))} />
          ))}
        </div>
      </div>
    );
  return (
    <div class="sr col qnow">
      <div class="qw-top">
        <span class="sr-b">
          <span class="sr-t">Quiet now</span>
          <span class="sr-d">{quietNowLine({ quietNow: manual }, now)}</span>
        </span>
        {byHand && (
          <button type="button" class="btn" disabled={env.readOnly} onClick={() => onManual(null)}>
            End now
          </button>
        )}
      </div>
      {!byHand && (
        <div class="qlist" role="group" aria-label="Quiet now for">
          {quietNowChoices(now).map((c) => (
            <button type="button" class="qli" disabled={env.readOnly} onClick={() => onManual(iso(c.end))}>
              <span class="qht">{c.label}</span>
              <span class="qhw tnum">{c.when}</span>
            </button>
          ))}
          <span class="qli qnow-at">
            <TimePick value={at} label="Quiet now until" onChange={setAt} />
            <button type="button" class="btn" disabled={env.readOnly} onClick={() => onManual(iso(nextClock(at, now)))}>
              Until {at}
            </button>
          </span>
        </div>
      )}
    </div>
  );
}

/** V0.46, the decision 0014 addendum, V0.119: the project page's Quiet hours group. One row per window: from–to in
 *  24-hour time, an All day switch, ×, and the seven day chips under them; the last row adds a window or a preset
 *  that is not there yet; the foot says what all the rows do. No rows is off. */
export function QuietGroup({ value, onChange, note, now, manual, onManual, lift, quietEnd, onLift }: { value: QuietHours[]; onChange(q: QuietHours[]): void; note?: string | null; /** V0.quiet-now: the host's clock, the stretch started by hand, and how to start or end it */ now?: number; manual?: string; onManual?(end: string | null): void; /** V0.quiet-lift: the lift's end, the project's quietUntil (ms, 0 when not quiet), and how to start or end a lift */ lift?: string; quietEnd?: number; onLift?(end: string | null): void }) {
  const env = useSet();
  const set = (i: number, w: QuietHours) => onChange(value.map((x, k) => (k === i ? w : x)));
  const presets = QUIET_PRESETS.filter((p) => !hasWindow(value, p.window));
  return (
    <Group title="Quiet hours" end={note ?? undefined} class="qfld" label="Quiet hours" foot={quietLine(value)}>
      {onManual && now !== undefined && <QuietNowRow manual={manual} lift={lift} quietEnd={quietEnd ?? 0} now={now} onManual={onManual} onLift={onLift} />}
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
              {/* "+": a preset is a window to add; as a bare chip it read as one already set */}
              + {p.label} <span>{p.hint}</span>
            </button>
          ))}
        </div>
      </div>
    </Group>
  );
}
