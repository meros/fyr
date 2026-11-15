// Changes and actions in the client (SPEC 0.5 V0.59, V0.60, decision 0018). Pure: what a change shows, which
// actions the owner can press now, and the words for the conversation lines.

import type { Change, ChangePhase, Offer, OfferRun, Thread } from "./types.ts";

export const RAIL: ChangePhase[] = ["working", "review", "landed", "live"];

export const ended = (p: ChangePhase) => p === "abandoned" || p === "reverted";

/** The phase rail: each step done, current or ahead; an ended change shows its end word. */
export function rail(c: Pick<Change, "phase">): { steps: { phase: ChangePhase; state: "done" | "now" | "ahead" }[]; end?: ChangePhase } {
  if (ended(c.phase)) return { steps: RAIL.map((phase) => ({ phase, state: "ahead" as const })), end: c.phase };
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

const quote = (a: string) => (/^[\w./:=@%+-]+$/.test(a) ? a : `'${a.replace(/'/g, `'\\''`)}'`);

/** The newest run of an action. */
export function lastRun(runs: OfferRun[] | undefined, id: string): OfferRun | undefined {
  return [...(runs ?? [])].reverse().find((r) => r.action === id);
}

/** The command args of a press: what the owner saw is what runs. */
export function pressArgs(on: string, o: Offer) {
  return { on, action: o.id, digest: o.digest };
}

/** The changes to list, newest first; PR changes too (V0.59 (8)). */
export function changeList(t: Pick<Thread, "changes">): Change[] {
  return [...t.changes].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
}

/** The inbox chip when the thread has no open PR: the most urgent change that is not a PR (V0.59 (8)). */
export function changeChip(t: Pick<Thread, "changes" | "pr">, short = false): { text: string; cls: string; title: string } | null {
  if (t.pr && (t.pr.state ?? "OPEN") === "OPEN") return null;
  const live = t.changes.filter((c) => !ended(c.phase) && !(c.evidence ?? []).some((e) => e.kind === "pr"));
  if (!live.length) return null;
  const rank = (c: Change) => (c.conditions?.some((k) => k.status === "fail") ? 0 : c.needs === "owner" ? 1 : 2);
  const c = [...live].sort((a, b) => rank(a) - rank(b) || b.updatedAt.localeCompare(a.updatedAt))[0];
  const fail = c.conditions?.find((k) => k.status === "fail");
  const word = fail ? `${fail.type} failed` : c.needs === "owner" ? "needs you" : c.label || c.phase;
  const more = live.length > 1 ? ` +${live.length - 1}` : "";
  return { text: short ? `${word}${more}` : `${clip(c.title)} · ${word}${more}`, cls: fail ? "fail" : c.phase === "live" ? "ok" : "", title: `${c.title} · ${phaseText(c)}` };
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
