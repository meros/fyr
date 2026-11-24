// Changes and actions in the client (SPEC 0.5 V0.59, V0.60, decision 0018). Pure: what a change shows, which
// actions the owner can press now, and the words for the conversation lines.
import { dayMonth, clockTime, ago, clip } from "./format.ts";

import type { Change, ChangeNeed, ChangePhase, Offer, OfferRun, PullRequest, Thread } from "./types.ts";
import { shortHead } from "./pr.ts";
import type { Ask } from "./confirm.ts";

const RAIL: ChangePhase[] = ["working", "review", "landed", "live"];

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

/** V0.dialogs-review: the merge the card's need strip offers (Ready with a head, or the agent's merge ask), as the
 *  offer it runs; null when the strip offers none. The strip's button is then the card's one Merge: the offer's own
 *  button is left out of the action bar (`barOffers`), and the strip carries its state (running, failed). */
export function stripMerge(c: Pick<Change, "need" | "offers">): Offer | null {
  const n = c.need;
  if (!n || n.muted) return null;
  const merge = (c.offers ?? []).find((o) => o.run === "fyr" && o.effect === "pr.merge") ?? null;
  if (n.cause === "ready") return merge && n.head ? merge : null;
  return n.cause === "ask" && n.action === "pr.merge" ? merge : null;
}

/** The action bar's buttons: the shown offers without the merge the need strip already offers. */
export function barOffers(c: Pick<Change, "need" | "offers" | "phase"> | undefined, offers: Offer[] | undefined): Offer[] {
  const strip = c ? stripMerge(c) : null;
  return shownOffers(offers, c).filter((o) => o.id !== strip?.id);
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
type RunState = "fresh" | "running" | "ok" | "failed";
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
export function endChangeAsk(c: Pick<Change, "evidence" | "title">): Ask & { choice: NonNullable<Ask["choice"]>; reason: NonNullable<Ask["reason"]> } {
  const pr = (c.evidence ?? []).some((e) => e.kind === "pr");
  return {
    // V0.dialogs-review: the title names the change it ends (over a board of cards "this change" names none).
    title: c.title ? `End “${c.title}”?` : "End this change?",
    text: `Fyr stops watching ${pr ? "its PR" : "it"} and removes its actions. The agent is told; more work on it becomes a new change.`,
    choice: { label: "Ended as", options: [{ value: "abandoned", label: "Abandoned" }, { value: "reverted", label: "Reverted" }], value: "abandoned" },
    reason: { label: "Reason (optional)", value: "", max: 200 },
    confirm: "End change",
    danger: true, // V0.dialogs-review (28): it cannot be taken back (more work becomes a new change), so it is red like Delete project
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
const BACK_TEXT = "back when the head changes";

/** V0.84, V0.101: the quiet line of a parked change, or null. `clock` turns the time into hh:mm. */
export const parkedLine = (c: Pick<Change, "parked">, clock: (iso: string) => string): string | null => (c.parked ? `Not now · ${clock(c.parked.at)} · ${BACK_TEXT}` : null);

/** V0.101: the words of a need on its card and row: the button and the line. A merge (Ready or a merge ask) is bound to
 *  the head; an ask is answered; a failure says what failed. */
export function needLook(n: ChangeNeed): { button: string; line: string; beam: boolean } {
  // V0.merge-label: the button says what it does ("Merge"); the head it is bound to is named in its line, the
  // button's title on the row, not as a hash on the button.
  const on = n.head ? ` Merges ${shortHead(n.head)}.` : "";
  switch (n.cause) {
    case "ready":
      return { button: n.text === "Review and merge" ? "Review and merge" : "Merge", line: (n.text === "Review and merge" ? "Green; a required review waits for you." : "Green and waiting for your merge.") + on, beam: true };
    case "ask":
      return { button: "Answer", line: n.text || "The agent asks you.", beam: true };
    case "waiting": // V0.136: a merge ask held until CI is green on its head; no button, no beam
      return { button: "", line: `${n.text || "Waiting for CI"}${n.head ? ` on ${shortHead(n.head)}` : ""}. Fyr asks you to merge when it is green.`, beam: false };
    case "failure":
      return { button: "Open", line: `Stopped on a failure: ${n.text ?? "failed"}`, beam: true };
    default:
      return { button: "", line: n.text || "Agent is fixing CI", beam: false };
  }
}

/** An ask's yes: "Merge" for a merge ask, bound to its head (V0.97; V0.merge-label: the head is in its title), else
 *  Yes. Since V0.136 a merge ask is shown only when CI is green on its head, so the yes always runs. */
export function askYes(n: ChangeNeed): { label: string; title?: string } {
  if (n.action !== "pr.merge") return { label: "Yes" };
  return { label: "Merge", title: n.head ? `Merges ${shortHead(n.head)}` : undefined };
}

/** V0.101: Refresh now on an open change that Fyr reads: a PR, or a branch (a host record or a thread change on one). */
export const canRefresh = (c: Pick<Change, "phase" | "done" | "pr" | "branch" | "threads">) => !ended(c.phase) && !isDone(c) && (!!c.pr || !!c.branch || !!c.threads?.length);

/** V0.101: "checked 30 s ago", from when Fyr last read the change's PR. */
export function checkedText(iso: string, now: number): string {
  const t = Date.parse(iso);
  return Number.isNaN(t) ? "" : `checked ${ago(now - t)} ago`;
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
  if (headerPrs(t).length > 0) return null;
  const live = t.changes.filter((c) => !ended(c.phase) && !isDone(c) && !(c.evidence ?? []).some((e) => e.kind === "pr"));
  if (!live.length) return null;
  // V0.84: a parked change ranks and reads as quiet work.
  // V0.101: the host's need ranks first; a failure without one (the agent is on it) next.
  const rank = (c: Change) => (c.parked ? 3 : needsOwner(c) ? 0 : c.conditions?.some((k) => k.status === "fail") ? 1 : 2);
  const c = [...live].sort((a, b) => rank(a) - rank(b) || b.updatedAt.localeCompare(a.updatedAt))[0];
  const fail = c.parked ? undefined : c.conditions?.find((k) => k.status === "fail");
  // V0.189: the phase "working" means the change is not finished, not that an agent is at it: this chip shows on threads
  // whose agent has stopped, so it says what is true of the change.
  const phase = c.phase === "working" ? "change open" : c.phase;
  const word = fail ? `${fail.type} failed` : c.parked ? "not now" : needsOwner(c) ? "needs you" : c.label || phase;
  const more = live.length > 1 ? ` +${live.length - 1}` : "";
  return { text: short ? `${word}${more}` : `${clip(c.title, 24)} · ${word}${more}`, cls: fail ? "fail" : c.phase === "live" ? "ok" : "", title: `${c.title} · ${c.phase === "working" ? [phase, c.label].filter(Boolean).join(" · ") : phaseText(c)}` };
}

/** Canonical key for deduplicating PR URLs (ignores /files, #fragments, and case). */
function prUrlKey(url: string): string {
  return (/^(.*?\/pull\/\d+)/i.exec(url)?.[1] ?? url).toLowerCase();
}

function prOfChange(c: Change, main?: PullRequest | null): PullRequest | null {
  if (c.pr) return c.pr;
  if (!main) return null;
  const matchesMain = c.key === `pr:${main.repo}#${main.number}` || (c.evidence ?? []).some((e) => e.kind === "pr" && prUrlKey(e.url ?? "") === prUrlKey(main.url));
  return matchesMain ? main : null;
}

/** V0.multi-pr: all non-dismissed PRs of a thread for the PR tab and details sheet, open ones first (newest first),
 *  followed by non-dismissed merged/closed PRs (newest first). */
export function threadPrs(t: Pick<Thread, "pr"> & { changes?: Change[] }): PullRequest[] {
  const main = t.pr ?? null;
  const changes = [...(t.changes ?? [])].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  const open: PullRequest[] = [];
  const rest: PullRequest[] = [];
  const seen = new Set<string>();
  const owned = new Set<string>();
  for (const c of changes) {
    const p = prOfChange(c, main);
    if (!p?.url) continue;
    const k = prUrlKey(p.url);
    owned.add(k);
    if (c.dismissed || seen.has(k)) continue;
    seen.add(k);
    const isOpen = !ended(c.phase) && !isDone(c) && (p.state ?? "OPEN") === "OPEN";
    (isOpen ? open : rest).push(p);
  }
  if (main?.url) {
    const k = prUrlKey(main.url);
    if (!owned.has(k) && !seen.has(k)) {
      ((main.state ?? "OPEN") === "OPEN" ? open : rest).push(main);
    }
  }
  return [...open, ...rest];
}

/** V0.multi-pr, V0.98 (6): the PRs shown as chips in the thread header: every open change's PR (never a done, ended
 *  or dismissed one), newest first. */
export function headerPrs(t: Pick<Thread, "pr"> & { changes?: Change[] }): PullRequest[] {
  const main = t.pr ?? null;
  const changes = t.changes ?? [];
  const seen = new Set<string>();
  const owned = new Set<string>();
  const out: PullRequest[] = [];
  for (const c of [...changes].sort((a, b) => b.createdAt.localeCompare(a.createdAt))) {
    const p = prOfChange(c, main);
    if (!p?.url) continue;
    const k = prUrlKey(p.url);
    owned.add(k);
    if (c.dismissed || ended(c.phase) || isDone(c) || seen.has(k)) continue;
    seen.add(k);
    out.push(p);
  }
  if (main?.url && (main.state ?? "OPEN") === "OPEN") {
    const k = prUrlKey(main.url);
    if (!owned.has(k) && !seen.has(k)) out.push(main);
  }
  return out;
}

/** V0.multi-pr: the PR an effect decision acts on (matching args.pr across the thread's PRs), falling back to t.pr. */
export function prForEffect(t: Pick<Thread, "pr"> & { changes?: Change[] }, d?: { effect?: { args?: Record<string, unknown> } } | null): PullRequest | null | undefined {
  const url = typeof d?.effect?.args?.pr === "string" ? d.effect.args.pr : "";
  if (url) {
    const k = prUrlKey(url);
    const match = threadPrs(t).find((p) => prUrlKey(p.url) === k);
    if (match) return match;
  }
  return t.pr;
}

/** A `change` event as one conversation line (V0.59 (5)). */
export function changeEventLine(d: Record<string, unknown> | undefined): string {
  const c = (d?.change ?? {}) as Partial<Change>;
  const verb: Record<string, string> = { open: "Change", revise: "New revision of", status: "Change", attach: "Proof on", action: "Action on" };
  return `${verb[String(d?.op)] ?? "Change"} “${c.title ?? "?"}” · ${phaseText({ phase: (c.phase ?? "working") as ChangePhase, phaseBy: c.phaseBy ?? "agent", label: c.label })}`;
}

/** An `action` event (a press or its result) as one conversation line (V0.60 (4)). */
export function actionEventLine(d: Record<string, unknown> | undefined): { line: string; ok: boolean } {
  if (d?.request) {
    // V0.149 (6): a run request's lines name its why
    const why = String(d.why ?? d.label ?? "the command");
    if (d.dismissed) return { line: d.ran ? `You closed “${why}”` : `You chose Not now for “${why}”`, ok: true };
    if (!d.ended) return { line: `You ran “${why}”${d.root ? " as root" : ""}`, ok: true };
    const ok = !!d.ok;
    return { line: ok ? `“${why}” succeeded` : `“${why}” failed${d.exit ? ` (exit ${d.exit})` : ""}`, ok };
  }
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

/** V0.wakeups-via-watch, V0.quota-reset-wake: the soonest wake-up as a chip, in the owner's local time: "wakes 06:00",
 *  with the date when it is not today ("wakes 21 Nov 06:00"), or "wakes 18:00, when the 5 h window resets" for a quota
 *  watch; null when none is armed. V0.watch-waits: the note is only in the title, never in the chip's text. */
export function wakeChip(t: Pick<Thread, "wakes">, now: number): { text: string; title: string; rule: string } | null {
  const w = t.wakes?.[0];
  if (!w) return null;
  const at = new Date(Date.parse(w.at));
  if (Number.isNaN(at.getTime())) return null;
  const n = new Date(now);
  const hm = clockTime(w.at);
  const today = at.getFullYear() === n.getFullYear() && at.getMonth() === n.getMonth() && at.getDate() === n.getDate();
  const when = today ? hm : `${dayMonth(at)} ${hm}`;
  const quota = w.window ? `, when the ${w.window} window resets` : w.provider ? ", when quota resets" : "";
  const note = (w.note ?? "").trim();
  const n2 = (t.wakes?.length ?? 0) - 1;
  return {
    text: n2 > 0 ? `wakes ${when}${quota} +${n2}` : `wakes ${when}${quota}`,
    title: `The agent wakes at ${when}${quota}${note ? ` and runs: ${note}` : ""}${n2 > 0 ? ` (+${n2} more)` : ""}`,
    rule: w.rule,
  };
}
