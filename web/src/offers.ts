// Changes and actions in the client (SPEC 0.5 V0.59, V0.60, decision 0018). Pure: what a change shows, which
// actions the owner can press now, and the words for the conversation lines.

import type { Change, ChangePhase, Offer, OfferRun, PullRequest, Thread } from "./types.ts";
import type { Ask } from "./confirm.ts";

export const RAIL: ChangePhase[] = ["working", "review", "landed", "live"];

export const ended = (p: ChangePhase) => p === "abandoned" || p === "reverted";

/** The phase rail: each step done, current or ahead. An ended change has no steps (V0.73 (2)): its header says the end. */
export function rail(c: Pick<Change, "phase">): { steps: { phase: ChangePhase; state: "done" | "now" | "ahead" }[]; end?: ChangePhase } {
  if (ended(c.phase)) return { steps: [], end: c.phase };
  const at = RAIL.indexOf(c.phase);
  return { steps: RAIL.map((phase, i) => ({ phase, state: i < at ? "done" : i === at ? "now" : "ahead" })) };
}

/** The phase in words, "reported" when the agent set it (a fact Fyr did not see). */
export function phaseText(c: Pick<Change, "phase" | "phaseBy" | "label">): string {
  const reported = c.phaseBy === "agent" && c.phase !== "working" ? " (reported)" : "";
  return `${c.phase}${reported}${c.label ? ` · ${c.label}` : ""}`;
}

/** Failing first, then pending; ok conditions are not listed. */
export function openConditions(c: Pick<Change, "conditions">) {
  const list = c.conditions ?? [];
  return [...list.filter((k) => k.status === "fail"), ...list.filter((k) => k.status === "pending")];
}

/** Why an action cannot be pressed now, or null; mirrors the host (V0.60 (3)). */
export function unavailable(o: Offer, runs: OfferRun[] | undefined, c?: Pick<Change, "phase" | "revisions">): string | null {
  if (c) {
    if (o.when?.length ? !o.when.includes(c.phase) : ended(c.phase)) return `Not while the change is ${c.phase}`;
    const newest = c.revisions?.at(-1)?.id;
    if (o.revision && newest !== o.revision) return `The change has a newer revision than ${o.revision}`;
  }
  if (runs?.some((r) => r.action === o.id && r.running)) return "It is running";
  return null;
}

/** The actions to show, primary first. One outside its phases is left out; one bound to an old revision or
 *  running shows, disabled, with the reason. */
export function shownOffers(offers: Offer[] | undefined, c?: Pick<Change, "phase">): Offer[] {
  const inPhase = (o: Offer) => !c || (o.when?.length ? o.when.includes(c.phase) : !ended(c.phase));
  return (offers ?? []).filter(inPhase).sort((a, b) => Number(!!b.primary) - Number(!!a.primary));
}

/** What a press does, in one line, shown before the press (V0.60 (7)). */
export function offerDetail(o: Offer, cwd?: string): string {
  if (o.run === "script") return `Runs ${(o.argv ?? []).map(quote).join(" ")}${o.cwd || cwd ? ` in ${o.cwd || cwd}` : ""}${o.revision ? ` · only on revision ${o.revision}` : ""}`;
  if (o.run === "agent") return `${o.in === "new" ? "Starts a new thread" : "Asks this thread's agent"}: ${o.prompt ?? ""}`;
  return o.effect === "pr.merge" ? "Fyr merges the PR" : o.effect === "pr.rerunFailed" ? "Fyr re-runs the failed checks" : "Fyr runs it";
}

/** One argument as a shell would need it written. */
export const quote = (a: string) => (/^[\w./:=@%+-]+$/.test(a) ? a : `'${a.replace(/'/g, `'\\''`)}'`);

/** The newest run of an action. */
export function lastRun(runs: OfferRun[] | undefined, id: string): OfferRun | undefined {
  return [...(runs ?? [])].reverse().find((r) => r.action === id);
}

/** V0.75 (1), #62: an action's state from its newest run: fresh (never run, or not since the last look), running,
 *  ok, failed. The bar draws a used action quiet in place instead of adding a line for its result. */
export type RunState = "fresh" | "running" | "ok" | "failed";
export function runState(runs: OfferRun[] | undefined, id: string): RunState {
  const r = lastRun(runs, id);
  return !r ? "fresh" : r.running ? "running" : r.ok ? "ok" : "failed";
}

/** V0.75 (1), #62: how a button shows its action: the state from the newest run, the class (a used action is quiet,
 *  a failure loud), the mark before the label and the result after it. `clock` turns a run's time into hh:mm. */
export function offerLook(o: Offer, runs: OfferRun[] | undefined, clock: (iso: string) => string) {
  const state = runState(runs, o.id);
  const last = lastRun(runs, o.id);
  const cls = o.primary && state === "fresh" ? " p" : state === "ok" ? " used" : state === "failed" ? " bad" : "";
  const mark = state === "ok" ? "✓ " : state === "failed" ? "✕ " : o.run === "agent" ? "✦ " : "";
  const word =
    state === "running" ? " · running…" : state === "ok" ? ` · ${clock((last!.endedAt ?? last!.at)!)}` : state === "failed" ? ` · failed${last!.exit ? ` (exit ${last!.exit})` : ""}` : "";
  return { state, last, cls, mark, word };
}

/** The command args of a press: what the owner saw is what runs. */
export function pressArgs(on: string, o: Offer, head?: string) {
  return head ? { on, action: o.id, digest: o.digest, head } : { on, action: o.id, digest: o.digest };
}

/** V0.72: the owner can end every change of the thread that has not ended (a host record on the board is not one). */
export const canEnd = (c: Pick<Change, "phase" | "threads">) => !ended(c.phase) && !c.threads?.length;

/** V0.72: Fyr's confirmation before End this change: the end (abandoned by default, or reverted) and a reason. */
export function endChangeAsk(c: Pick<Change, "evidence">): Ask & { choice: NonNullable<Ask["choice"]>; reason: NonNullable<Ask["reason"]> } {
  const pr = (c.evidence ?? []).some((e) => e.kind === "pr");
  return {
    title: "End this change?",
    text: `Fyr stops watching ${pr ? "its PR" : "it"} and removes its actions. The agent is told; more work on it becomes a new change.`,
    choice: { label: "Ended as", options: [{ value: "abandoned", label: "Abandoned" }, { value: "reverted", label: "Reverted" }], value: "abandoned" },
    reason: { label: "Reason (optional)", value: "", max: 200 },
    confirm: "End change",
  };
}

/** The command args of change.end from what the owner picked in the confirmation. */
export function endArgs(c: Pick<Change, "id">, a: Pick<Ask, "choice" | "reason">): { change: string; phase: string; reason?: string } {
  const reason = (a.reason?.value ?? "").trim();
  return { change: c.id, phase: a.choice?.value || "abandoned", ...(reason ? { reason } : {}) };
}

/** V0.59 (6): a change that has not ended and fails or waits for the owner, before any Not now. */
export const blocks = (c: Pick<Change, "phase" | "needs" | "conditions">) =>
  !ended(c.phase) && (c.needs === "owner" || !!c.conditions?.some((k) => k.status === "fail"));

/** V0.84: a change in Needs you: it blocks and the owner has not parked it. The host clears `parked` when the change
 *  moves after the park, so a parked change here is still as the owner saw it. */
export const needsOwner = (c: Pick<Change, "phase" | "needs" | "conditions" | "parked">) => blocks(c) && !c.parked;

/** V0.84: Not now on a change of the thread (not a host record) that is in Needs you. */
export const canPark = (c: Pick<Change, "phase" | "needs" | "conditions" | "parked" | "threads">) => needsOwner(c) && !c.threads?.length;

/** The command args of change.park and change.unpark. */
export const parkArgs = (c: Pick<Change, "id">) => ({ change: c.id });

/** V0.84: the quiet line of a parked change, or null. `clock` turns the time into hh:mm. */
export const parkedLine = (c: Pick<Change, "parked">, clock: (iso: string) => string): string | null => (c.parked ? `Not now · ${clock(c.parked.at)}` : null);

/** The changes to list, newest first; PR changes too (V0.59 (8)). */
export function changeList(t: Pick<Thread, "changes">): Change[] {
  // V0.87 (3), #69: one card per id, the newest: two changes with one id are one work item.
  const seen = new Set<string>();
  return [...t.changes].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)).filter((c) => !seen.has(c.id) && !!seen.add(c.id));
}

/** The inbox chip when the thread has no open PR: the most urgent change that is not a PR (V0.59 (8)). */
export function changeChip(t: Pick<Thread, "changes" | "pr">, short = false): { text: string; cls: string; title: string } | null {
  if (t.pr && (t.pr.state ?? "OPEN") === "OPEN") return null;
  const live = t.changes.filter((c) => !ended(c.phase) && !(c.evidence ?? []).some((e) => e.kind === "pr"));
  if (!live.length) return null;
  // V0.84: a parked change ranks and reads as quiet work.
  const rank = (c: Change) => (c.parked ? 2 : c.conditions?.some((k) => k.status === "fail") ? 0 : c.needs === "owner" ? 1 : 2);
  const c = [...live].sort((a, b) => rank(a) - rank(b) || b.updatedAt.localeCompare(a.updatedAt))[0];
  const fail = c.parked ? undefined : c.conditions?.find((k) => k.status === "fail");
  const word = fail ? `${fail.type} failed` : c.parked ? "not now" : c.needs === "owner" ? "needs you" : c.label || c.phase;
  const more = live.length > 1 ? ` +${live.length - 1}` : "";
  return { text: short ? `${word}${more}` : `${clip(c.title)} · ${word}${more}`, cls: fail ? "fail" : c.phase === "live" ? "ok" : "", title: `${c.title} · ${phaseText(c)}` };
}

/** The thread's main PR for the header chip (V0.73 (2)): none once its change has ended (closed, missing on GitHub,
 *  or ended by the owner); a merged PR keeps its chip. */
export function headerPr(t: Pick<Thread, "changes" | "pr">): PullRequest | null {
  const pr = t.pr;
  if (!pr || pr.state === "MERGED") return pr ?? null;
  const its = (c: Change) => c.key === `pr:${pr.repo}#${pr.number}` || (c.evidence ?? []).some((e) => e.kind === "pr" && e.url === pr.url);
  return t.changes.some((c) => its(c) && ended(c.phase)) ? null : pr;
}

const clip = (s: string) => (s.length > 24 ? `${s.slice(0, 23)}…` : s);

/** A `change` event as one conversation line (V0.59 (5)). */
export function changeEventLine(d: Record<string, unknown> | undefined): string {
  const c = (d?.change ?? {}) as Partial<Change>;
  const verb: Record<string, string> = { open: "Change", revise: "New revision of", status: "Change", attach: "Proof on", action: "Action on" };
  return `${verb[String(d?.op)] ?? "Change"} “${c.title ?? "?"}” · ${phaseText({ phase: (c.phase ?? "working") as ChangePhase, phaseBy: c.phaseBy ?? "agent", label: c.label })}`;
}

/** An `action` event (a press or its result) as one conversation line (V0.60 (4)). */
export function actionEventLine(d: Record<string, unknown> | undefined): { line: string; ok: boolean } {
  const label = String(d?.label ?? "Action");
  if (!d?.ended) return { line: `You pressed “${label}” on ${d?.title ?? "this thread"}`, ok: true };
  const ok = !!d.ok;
  return { line: ok ? `“${label}” succeeded` : `“${label}” failed${d.exit ? ` (exit ${d.exit})` : ""}`, ok };
}

/** The thread's "settles when …" chip, or null when nothing is armed. A change is found by id, then key (V0.59 (7)). */
export function settleLine(t: Pick<Thread, "settleWhenDone" | "settleWhenMerged" | "settleWhen" | "changes">): string | null {
  if (t.settleWhenDone) return "settles when done";
  if (t.settleWhenMerged) return "settles when merged";
  const w = t.settleWhen;
  if (!w?.change) return null;
  const cs = t.changes;
  const c = cs.find((x) => x.id === w.change) ?? cs.find((x) => x.key === w.change);
  return `settles when ${c?.title || w.change} is ${w.phase}`;
}
