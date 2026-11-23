// The snooze list of both fronts (SPEC 0.5 V0.67, V0.185): the timed presets, then "until work is done". The row's and
// the section's words are here too, so desktop and phone say the same.
import { untilLabel } from "./format.ts";
import { presetForKey, snoozePresets, type SnoozePreset } from "./store.ts";
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
  return t.view?.snoozeWhenDone ? SNOOZE_DONE_LINE : `until ${untilLabel(t.view?.snoozeUntil, now)}`;
}

/** The Snoozed section's hint: the next time a thread wakes; a snooze until work is done has no such time. */
export function snoozedHint(list: Thread[], now: number): string {
  const next = list
    .filter((t) => !t.view?.snoozeWhenDone)
    .map((t) => t.view?.snoozeUntil)
    .filter((x): x is string => !!x)
    .sort()[0];
  if (next) return `next: ${untilLabel(next, now)}`;
  return list.some((t) => t.view?.snoozeWhenDone) ? SNOOZE_DONE_LINE : "none";
}
