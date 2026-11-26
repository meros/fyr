// The snooze list of both fronts (SPEC 0.5 V0.67, V0.185): the timed presets, then "until work is done". The row's and
// the section's words are here too, so desktop and phone say the same.
import { untilLabel } from "./format.ts";
import { presetForKey, SNOOZE_MORNING_H, snoozePresets, type SnoozePreset } from "./store.ts";
import type { Thread } from "./types.ts";

export const SNOOZE_DONE_ACTION = "thread.snoozeWhenDone";
const SNOOZE_DONE_LINE = "until work is done";
/** core.SnoozeDoneMax: the host wakes the thread after this time, however the work goes. */
const SNOOZE_DONE_MAX_H = 24;

export interface SnoozeChoice extends SnoozePreset {
  /** until work is done: `ids` are the threads that can take it */
  done?: boolean;
  ids?: string[];
  /** why the choice is off (the host's words) */
  off?: string;
}

function doneChoice(now: Date, threads: Thread[]): SnoozeChoice {
  const ids = threads.filter((t) => !t.unavailable[SNOOZE_DONE_ACTION]).map((t) => t.id);
  const off = ids.length ? undefined : (threads[0]?.unavailable[SNOOZE_DONE_ACTION] ?? "No thread");
  return { label: "Until work is done", until: new Date(now.getTime() + SNOOZE_DONE_MAX_H * 3600_000), when: `at most ${SNOOZE_DONE_MAX_H} h`, done: true, ids, off };
}

/** The list, in the order shown. The new choice is last, so the keys of the timed ones stay. */
export function snoozeChoices(now: Date, threads: Thread[]): SnoozeChoice[] {
  return [...snoozePresets(now), doneChoice(now, threads)];
}

// ---- V0.dialogs-review (15): a date and time of the owner's own, one field on both fronts ----

/** The last row of the list on both fronts, and the desktop's key for it. */
export const SNOOZE_CUSTOM_LABEL = "Pick a date and time…";
export const SNOOZE_CUSTOM_KEY = "t";

const two = (n: number) => String(n).padStart(2, "0");

/** A local time as the value of a datetime-local field: "2026-11-25T09:00". */
export function fieldTime(d: Date): string {
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}T${two(d.getHours())}:${two(d.getMinutes())}`;
}

/** What the field starts on: tomorrow morning, at the hour the "Tomorrow morning" preset means (one idea of morning
 *  in the dialog), so Snooze works at once and only the part that differs is changed. */
export function customStart(now: Date): Date {
  const d = new Date(now);
  d.setDate(now.getDate() + 1);
  d.setHours(SNOOZE_MORNING_H, 0, 0, 0);
  return d;
}

/** The field's label, above it on both fronts. */
export const SNOOZE_CUSTOM_FIELD = "Date and time";

/** Why the field's Snooze button is off, as text under the field on both fronts (a disabled button's tooltip does
 *  not exist on a phone, and is not read on the desktop); "" when the time can be used. */
export function customProblem(value: string, now: Date): string {
  if (customUntil(value, now)) return "";
  const d = value ? new Date(value) : null;
  return d && !Number.isNaN(d.getTime()) ? "Pick a time after now." : "Pick a date and time.";
}

/** The field's time, or null when it is empty, not a time, or not after now. */
export function customUntil(value: string, now: Date): Date | null {
  const d = value ? new Date(value) : null;
  return d && !Number.isNaN(d.getTime()) && d.getTime() > now.getTime() ? d : null;
}

/** Where the arrows go from row `i` of `n` (-1: the focus is on no row, the × or the date field): Down to the first,
 *  Up to the last, and around at the ends. */
export function arrowRow(i: number, n: number, down: boolean): number {
  if (n <= 0) return -1;
  if (i < 0) return down ? 0 : n - 1;
  return (i + (down ? 1 : -1) + n) % n;
}

/** The desktop key's choice; a choice that is off has none. */
export function snoozeChoiceForKey(list: SnoozeChoice[], key: string): SnoozeChoice | null {
  const c = presetForKey(list, key) as SnoozeChoice | null;
  return c && !c.off ? c : null;
}

/** How many choices have a key: the one that can be off is the last. */
export function snoozeKeyCount(list: SnoozeChoice[]): number {
  return list.filter((c) => !c.off).length;
}

/** The command a snooze choice runs. */
export interface SnoozeCmd {
  action: string;
  ids: string[];
  args?: Record<string, unknown>;
}

export function snoozeCommand(c: Pick<SnoozeChoice, "until" | "done" | "ids">, ids: string[]): SnoozeCmd {
  if (c.done) return { action: SNOOZE_DONE_ACTION, ids: c.ids ?? ids, args: undefined };
  return { action: "thread.snooze", ids, args: { until: c.until.toISOString() } };
}

/** A snoozed row's second line. */
export function snoozeLine(t: Thread, now: number): string {
  if (t.view?.snoozeWhenDone) return SNOOZE_DONE_LINE;
  if (t.view?.snoozeWait) return SNOOZE_WAIT_LINE;
  return `until ${untilLabel(t.view?.snoozeUntil, now)}`;
}

/** V0.snooze-every-wait: a snooze until a wait on no time ends. */
const SNOOZE_WAIT_LINE = "until the wait ends";

/** The Snoozed section's hint: the next time a thread wakes; a snooze until work is done has no such time. */
export function snoozedHint(list: Thread[], now: number): string {
  const next = list
    .filter((t) => !t.view?.snoozeWhenDone && !t.view?.snoozeWait)
    .map((t) => t.view?.snoozeUntil)
    .filter((x): x is string => !!x)
    .sort()[0];
  if (next) return `next: ${untilLabel(next, now)}`;
  return list.some((t) => t.view?.snoozeWhenDone) ? SNOOZE_DONE_LINE : "none";
}

// ---- V0.snooze-until-wake: the snooze of the waits strip (waits-ui.tsx) ----

type Wait = NonNullable<Thread["waits"]>[number];

/** V0.snooze-every-wait: the owner's snooze until one wait ends; args {rule}. */
export const SNOOZE_WAIT_ACTION = "thread.snoozeUntilWait";

/** Whether a wait has the strip's snooze: every wait does (V0.snooze-every-wait), but a wait on a time that has passed
 *  (it fires now), a thread that is snoozed already, and one the host does not snooze (settled, archived) have none. */
export function canSnoozeWait(t: Pick<Thread, "unavailable" | "attention">, w: Wait, now: number): boolean {
  if (w.at && !(new Date(w.at).getTime() > now)) return false;
  return t.attention?.state !== "snoozed" && !t.unavailable[SNOOZE_WAIT_ACTION];
}
