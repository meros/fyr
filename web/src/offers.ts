// Changes and actions in the client (SPEC 0.5 V0.59, V0.60, decision 0018). Pure: what a change shows, which
// actions the owner can press now, and the words for the conversation lines.

import type { Change, ChangeNeed, ChangePhase, Offer, OfferRun, PullRequest, Thread } from "./types.ts";
import { shortHead } from "./pr.ts";
import type { Ask } from "./confirm.ts";

export const RAIL: ChangePhase[] = ["working", "review", "landed", "live"];

export const ended = (p: ChangePhase) => p === "abandoned" || p === "reverted";

/** V0.98: a finished change; the host marks it, the client only reads the mark. */
export const isDone = (c: Pick<Change, "done">) => !!c.done;

/** V0.98 (3): why a done change finished, for its card: "merged 10:42", "abandoned by you", "marked done by you". */
export function doneText(c: Pick<Change, "done">, clock: (iso: string) => string): string {
  const d = c.done;
  if (!d) return "";
  if (d.reason === "done") return "marked done by you";
  if (d.by === "owner") return `${d.reason} by you`;
  if (d.reason.includes(" ")) return d.reason;
  return `${d.reason} ${clock(d.at)}`;
}

/** V0.98 (3): the thread's Changes tab: open changes, then the done ones ("Done here"), newest done first. */
export function openAndDone(t: Pick<Thread, "changes">): { open: Change[]; done: Change[] } {
  const list = changeList(t);
  const done = list.filter(isDone).sort((a, b) => Date.parse(b.done!.at) - Date.parse(a.done!.at));
  return { open: list.filter((c) => !isDone(c)), done };
}

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

/** V0.72, V0.101: the owner can end (Abandon) every change that has not ended, of the thread or a host record; the host routes it. */
export const canEnd = (c: Pick<Change, "phase" | "done">) => !ended(c.phase) && !isDone(c);

/** V0.98 (4): Done on an open change, of the thread or a host record; the host routes it. */
export const canDone = (c: Pick<Change, "done">) => !isDone(c);

/** The command args of change.done. */
export const doneArgs = (c: Pick<Change, "id">) => ({ change: c.id });

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

/** V0.101: a change in Needs you. The host computes it (an open ask, Ready, Stopped on a failure) and leaves out a done
 *  change, a parked one and a failure the agent is fixing (muted); the client only reads `need`. */
export const needsOwner = (c: Pick<Change, "need">) => !!c.need && !c.need.muted;

/** V0.101: Not now on a change of the thread whose cause is Ready or a failure; Not now on an ask declines the ask. */
export const canPark = (c: Pick<Change, "need" | "threads">) => !c.threads?.length && (c.need?.cause === "ready" || c.need?.cause === "failure");

/** The command args of change.park and change.unpark. */
export const parkArgs = (c: Pick<Change, "id">) => ({ change: c.id });

/** V0.101: Not now on an ask declines it: the owner's answer with allow false. */
export const declineArgs = (n: Pick<ChangeNeed, "ask">) => ({ decisionId: n.ask ?? "", allow: false });

/** V0.101: Yes on an ask (a merge ask runs the merge bound to the head the agent's ask fixed). */
export const allowArgs = (n: Pick<ChangeNeed, "ask">) => ({ decisionId: n.ask ?? "", allow: true });

/** V0.101: when a parked change comes back (decision 0024 section 5: Not now is keyed to the head). */
export const BACK_TEXT = "back when the head changes";

/** V0.84, V0.101: the quiet line of a parked change, or null. `clock` turns the time into hh:mm. */
export const parkedLine = (c: Pick<Change, "parked">, clock: (iso: string) => string): string | null => (c.parked ? `Not now · ${clock(c.parked.at)} · ${BACK_TEXT}` : null);

/** V0.101: the words of a need on its card and row: the button and the line. A merge (Ready or a merge ask) is bound to
 *  the head; an ask is answered; a failure says what failed. */
export function needLook(n: ChangeNeed): { button: string; line: string; beam: boolean } {
  const head = n.head ? ` ${shortHead(n.head)}` : "";
  switch (n.cause) {
    case "ready":
      return { button: `${n.text === "Review and merge" ? "Review and merge" : "Merge"}${head}`, line: n.text === "Review and merge" ? "Green; a required review waits for you." : "Green and waiting for your merge.", beam: true };
    case "ask":
      return { button: "Answer", line: n.text || "The agent asks you.", beam: true };
    case "failure":
      return { button: "Open", line: `Stopped on a failure: ${n.text ?? "failed"}`, beam: true };
    default:
      return { button: "", line: n.text || "Agent is fixing CI", beam: false };
  }
}

/** V0.101: Refresh now on an open change that Fyr reads: a PR, or a branch (a host record or a thread change on one). */
export const canRefresh = (c: Pick<Change, "phase" | "done" | "pr" | "branch" | "threads">) => !ended(c.phase) && !isDone(c) && (!!c.pr || !!c.branch || !!c.threads?.length);

/** V0.101: "checked 30 s ago", from when Fyr last read the change's PR. */
export function checkedText(iso: string, now: number): string {
  const s = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (Number.isNaN(s)) return "";
  return s < 60 ? `checked ${s} s ago` : s < 3600 ? `checked ${Math.round(s / 60)} min ago` : `checked ${Math.round(s / 3600)} h ago`;
}

/** V0.101: the inbox row's cause: the first change of the thread in Needs you, newest first. */
export function rowNeed(t: Pick<Thread, "changes">): { change: Change; need: ChangeNeed } | null {
  for (const c of changeList(t)) if (c.need && !c.need.muted) return { change: c, need: c.need };
  return null;
}

/** The changes to list, newest first; PR changes too (V0.59 (8)). */
export function changeList(t: Pick<Thread, "changes">): Change[] {
  // V0.87 (3), #69: one card per id, the newest: two changes with one id are one work item.
  const seen = new Set<string>();
  return [...t.changes].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)).filter((c) => !seen.has(c.id) && !!seen.add(c.id));
}

/** The inbox chip when the thread has no open PR: the most urgent change that is not a PR (V0.59 (8)). */
export function changeChip(t: Pick<Thread, "changes" | "pr">, short = false): { text: string; cls: string; title: string } | null {
  if (t.pr && (t.pr.state ?? "OPEN") === "OPEN") return null;
  const live = t.changes.filter((c) => !ended(c.phase) && !isDone(c) && !(c.evidence ?? []).some((e) => e.kind === "pr"));
  if (!live.length) return null;
  // V0.84: a parked change ranks and reads as quiet work.
  // V0.101: the host's need ranks first; a failure without one (the agent is on it) next.
  const rank = (c: Change) => (c.parked ? 3 : needsOwner(c) ? 0 : c.conditions?.some((k) => k.status === "fail") ? 1 : 2);
  const c = [...live].sort((a, b) => rank(a) - rank(b) || b.updatedAt.localeCompare(a.updatedAt))[0];
  const fail = c.parked ? undefined : c.conditions?.find((k) => k.status === "fail");
  const word = fail ? `${fail.type} failed` : c.parked ? "not now" : needsOwner(c) ? "needs you" : c.label || c.phase;
  const more = live.length > 1 ? ` +${live.length - 1}` : "";
  return { text: short ? `${word}${more}` : `${clip(c.title)} · ${word}${more}`, cls: fail ? "fail" : c.phase === "live" ? "ok" : "", title: `${c.title} · ${phaseText(c)}` };
}

/** The header chip (V0.98 (6), amends V0.73 (2)): the PR of the thread's newest open change that has one; never a
 *  done or ended change's, and none when no change is open. A change without its own snapshot shows the thread's PR
 *  when that PR is its own. */
export function headerPr(t: Pick<Thread, "changes" | "pr">): PullRequest | null {
  const main = t.pr;
  const its = (c: Change) => !!main && (c.key === `pr:${main.repo}#${main.number}` || (c.evidence ?? []).some((e) => e.kind === "pr" && e.url === main.url));
  const prOf = (c: Change) => c.pr ?? (its(c) ? main! : null);
  const open = t.changes.filter((c) => !ended(c.phase) && !isDone(c) && prOf(c)).sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  return open.length ? prOf(open[0]) : null;
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
