// The change list and the action buttons (SPEC 0.5 V0.59 (8), V0.60 (7)). One button bar for every holder: a
// change, the thread, the live view (its page runs them through the bridge).

import { useState } from "preact/hooks";
import type { ComponentChildren } from "preact";
import type { Change, Offer, OfferRun, Thread } from "./types.ts";
import { allowArgs, askYes, barOffers, stripMerge, canDone, canEnd, canPark, canRefresh, checkedText, declineArgs, doneArgs, doneText, openAndDone, endArgs, endChangeAsk, lastRun, needLook, offerDetail, offerLook, openConditions, parkArgs, parkedLine, phaseText, pressArgs, rail, shownOffers, unavailable } from "./offers.ts";
import { press } from "./confirm.ts";
import { headLabel, type MergeAsk } from "./pr.ts";
import { useAsk } from "./confirm-ui.tsx";
import { autoMergeLine } from "./automerge.ts";
import { changeSensitive, mergeAfterAsk, sensitiveLine } from "./sensitive.ts";
import { useMerge } from "./merge-ui.tsx";
import { useApi } from "./api-context.ts";

type Run = (action: string, args?: Record<string, unknown>) => void;
/** A command whose refusal the Merge dialog shows: resolves to the host's error, or null when the host took it. */
type Send = (action: string, args?: Record<string, unknown>) => Promise<string | null>;

/** V0.dialogs-review: what the Merge dialog asks about a change's merge. The method is the change's own here
 *  (`action.run` and `thread.answer` take none), so the dialog offers no choice of it. */
const changeMerge = (c: Change | undefined, sha: string | undefined, run: () => Promise<string | null>): MergeAsk => ({ number: c?.pr?.number, title: c?.pr?.title ?? c?.title, repo: c?.pr?.repo, sha, sensitive: changeSensitive(c) || undefined, run });

/** V0.101 (decision 0024 sections 5 and 9): why the change waits on the owner, inline on its card. An ask shows its
 *  answer (a merge ask bound to its head) and Not now, which declines it; Ready shows Merge bound to the head and Not
 *  now; a failure the agent stopped on shows Not now; a failure while the agent runs is muted, without the beam.
 *  V0.dialogs-review: its Merge is the card's one Merge button (offers.ts `stripMerge`): it carries the state of the
 *  merge it runs (running, failed), and a sensitive one asks in the Merge dialog, as every merge that asks. */
function NeedLine({ c, onRun, send }: { c: Change; onRun: Run; send: Send }) {
  const n = c.need!;
  const look = needLook(n);
  const merge = stripMerge(c);
  const askMerge = useMerge();
  let yes: { label: string; title?: string; cls?: string; busy?: boolean; off?: string; run: () => void } | null = null;
  if (n.cause === "ask") {
    const args = allowArgs(n);
    // V0.sensitive-pr: a sensitive merge asks first, with its reason.
    const run = n.action === "pr.merge" ? () => void mergeAfterAsk(changeMerge(c, n.head, () => send("thread.answer", args)), askMerge, () => (onRun("thread.answer", args), true)) : () => onRun("thread.answer", args);
    yes = { ...askYes(n), run };
  } else if (n.cause === "ready" && merge && n.head) {
    const args = pressArgs(c.id, merge, n.head);
    const st = offerLook(merge, c.runs, hhmm);
    const off = unavailable(merge, c.runs, c) ?? undefined;
    // the mark says what the time after the label is: the merge ran then (✓), or failed (✕)
    yes = { label: `${st.mark}${look.button}${st.word}`, cls: st.cls, busy: st.state === "running", off, run: () => void mergeAfterAsk(changeMerge(c, n.head, () => send("action.run", args)), askMerge, () => (onRun("action.run", args), true)) };
  }
  return (
    <div class={`chneed ${n.cause}${n.muted ? " muted" : ""}`} role={n.muted ? undefined : "status"}>
      {look.beam && <span class="beamdot" aria-hidden="true" />}
      <span class="chnq">{look.line}</span>
      {yes && (
        <button class={`btn p sm${yes.cls && yes.cls !== " p" ? yes.cls : ""}`} title={yes.off ?? yes.title} disabled={!!yes.off} aria-busy={yes.busy || undefined} onClick={yes.run}>
          {yes.label}
        </button>
      )}
      {n.cause === "ask" && (
        <button class="btn sm" title="Decline the ask" onClick={() => onRun("thread.answer", declineArgs(n))}>
          Not now
        </button>
      )}
      {canPark(c) && (
        <button class="btn sm chpark" title="Out of Needs you until the head changes" onClick={() => onRun("change.park", parkArgs(c))}>
          Not now
        </button>
      )}
    </div>
  );
}

/** Buttons for the actions of one holder, primary first, with what each runs and the newest result. */
/** V0.75 (1), #62: the hour:minute of a run, in the owner's own time. */
const hhmm = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toTimeString().slice(0, 5);
};

function OfferBar({ on, offers, runs, change, cwd, onRun, send }: { on: string; offers?: Offer[]; runs?: OfferRun[]; change?: Change; cwd?: string; onRun: Run; send?: Send }) {
  // V0.dialogs-review: a merge the need strip already offers has no second button here; its newest output stays one
  // press away (the ▤ reads every shown offer's runs).
  const list = barOffers(change, offers);
  const [open, setOpen] = useState<string | null>(null);
  const ask = useAsk();
  const askMerge = useMerge();
  const withOut = [...shownOffers(offers, change)].reverse().find((o) => lastRun(runs, o.id)?.output);
  if (!list.length && !withOut) return null;
  // V0.97: Merge names and sends the head this card shows.
  const headOf = (o: Offer) => (o.run === "fyr" && o.effect === "pr.merge" ? change?.pr?.head : undefined);
  const onPress = (o: Offer) => {
    if (o.run !== "fyr" || o.effect !== "pr.merge") return press(on, o, cwd, ask, onRun, headOf(o));
    const args = pressArgs(on, o, headOf(o));
    // A merge asks in the Merge dialog, like every merge that asks.
    return askMerge(changeMerge(change, headOf(o), send ? () => send("action.run", args) : async () => (onRun("action.run", args), null)));
  };
  return (
    <div class="offers">
      <div class="offb">
        {list.map((o) => {
          const why = unavailable(o, runs, change);
          // V0.75 (1), #62: a used action carries its result on the button and turns quiet; a failure keeps its weight.
          const { state: st, last, cls, mark, word } = offerLook(o, runs, hhmm);
          const title = st === "fresh" ? (why ?? offerDetail(o, cwd)) : `${offerDetail(o, cwd)} · the last run ${last?.running ? "is running" : last?.ok ? "succeeded" : "failed"}`;
          return (
            <button key={o.id} class={`btn sm${cls}`} disabled={!!why} title={title} aria-busy={st === "running" || undefined} onClick={() => void onPress(o)}>
              {mark}
              {headLabel(o.label, headOf(o))}
              {word}
            </button>
          );
        })}
        {/* the newest run's output, one press away, on a control of its own: a button cannot hold a button */}
        {withOut && (
          <button class={`offout-b${open === withOut.id ? " on" : ""}`} title={`${withOut.label}: the last run's output`} aria-expanded={open === withOut.id} onClick={() => setOpen(open === withOut.id ? null : withOut.id)}>
            ▤
          </button>
        )}
      </div>
      {open !== null && <pre class="offout">{lastRun(runs, open)?.output}</pre>}
    </div>
  );
}

/** The view's actions run through its page (fyr.run, data-action); the owner confirms a script or a fyr action
 *  first, as on a button (V0.68 (8)), so a page cannot run one by itself. */
export function useViewAction(onRun: Run, cwd?: string) {
  const ask = useAsk();
  return (o: Offer) => press("view", o, cwd, ask, onRun);
}

const KIND: Record<string, string> = { code: "⌥", infra: "☁", host: "▣", data: "⛁", doc: "▤", design: "◇", other: "•" };

/** The Changes tab's list (V0.59 (8)): the open changes, newest first, above the branch diff; V0.98 (3): then the
 *  done ones, folded under "Done here", each with why it finished. */
export function ChangeList({ t, onRun }: { t: Thread; onRun: Run }) {
  const { open, done } = openAndDone(t);
  if (!open.length && !done.length) return null;
  return (
    <section class="chl" aria-label="Changes">
      {open.map((c) => (
        <ChangeCard key={c.id} c={c} cwd={t.cwd} onRun={onRun} thread={t} />
      ))}
      {done.length > 0 && (
        <details class="chdone">
          <summary>
            Done here <span class="bdn">{done.length}</span>
          </summary>
          {done.map((c) => (
            <div key={c.id} class="chdrow" title={c.key}>
              <span class="ell">{c.title}</span>
              <span class="ok">{doneText(c, hhmm)}</span>
            </div>
          ))}
        </details>
      )}
    </section>
  );
}

/** One change: rail, conditions, targets, proof and its actions. `from` is a line above the title (the board names
 *  the thread there). */
export function ChangeCard({ c, cwd, onRun, from, thread }: { c: Change; cwd?: string; onRun: Run; from?: ComponentChildren; /** the thread the change is of: its merge then reports the host's refusal in the Merge dialog */ thread?: Pick<Thread, "id" | "hostId"> }) {
  const api = useApi();
  const send: Send = async (action, args) => {
    if (!thread) return (onRun(action, args), null);
    const r = await api.command(thread.hostId, action, thread.id, args);
    return r.ok ? null : (r.error ?? "The merge did not start.");
  };
  const r = rail(c);
  const conds = openConditions(c);
  const rev = c.revisions?.at(-1);
  const ask = useAsk();
  const parked = parkedLine(c, hhmm);
  // V0.72: the owner ends any change that has not ended, through Fyr's own confirmation.
  const end = async () => {
    const a = endChangeAsk(c);
    if (await ask(a)) onRun("change.end", endArgs(c, a));
  };
  return (
    <article class={`chc${r.end ? " end" : ""}${conds.some((k) => k.status === "fail") ? " fail" : ""}`}>
      {from}
      <header class="chh">
        <span class="chk" title={c.kind} aria-hidden="true">
          {KIND[c.kind] ?? "•"}
        </span>
        <span class="cht ell" title={c.key}>
          {c.title}
        </span>
        <span class="chp" title={phaseText(c)}>{phaseText(c)}</span>
      </header>
      {r.steps.length > 0 && (
        <ol class="chrail" aria-label={`Phase: ${c.phase}`}>
          {r.steps.map((s) => (
            <li key={s.phase} class={s.state}>
              {s.phase}
            </li>
          ))}
        </ol>
      )}
      {conds.length > 0 && (
        <ul class="chcond">
          {conds.map((k) => (
            <li key={k.type} class={k.status}>
              <b>{k.type}</b> {k.reason || k.status}
              {k.source === "agent" ? <span class="setd"> · reported</span> : null}
            </li>
          ))}
        </ul>
      )}
      {c.sensitive && (
        <div class="chsens" role="note">
          {sensitiveLine(c.sensitive)}
        </div>
      )}
      {c.need && <NeedLine c={c} onRun={onRun} send={send} />}
      {autoMergeLine(c) && <div class="chauto setd">{autoMergeLine(c)}</div>}
      {parked && (
        <div class="chparked setd" title="It comes back to Needs you when something on it changes">
          {parked}
          <button class="chundo" onClick={() => onRun("change.unpark", parkArgs(c))}>
            Undo
          </button>
        </div>
      )}
      <div class="chmeta setd">
        {[c.branch && `branch ${c.branch}`, ...(c.targets ?? []), c.group && `group ${c.group}`, rev && `rev ${rev.id.slice(0, 12)}${rev.summary ? `: ${rev.summary}` : ""}`].filter(Boolean).join(" · ")}
      </div>
      {(c.evidence?.length ?? 0) > 0 && (
        <div class="chev">
          {c.evidence!.map((e) =>
            e.url ? (
              <a key={e.url} href={e.url} target="_blank" rel="noreferrer">
                {e.title || e.kind}
              </a>
            ) : (
              <span key={e.attachment}>{e.title || e.kind}</span>
            ),
          )}
        </div>
      )}
      <OfferBar on={c.id} offers={c.offers} runs={c.runs} change={c} cwd={cwd} onRun={onRun} send={send} />
      {c.done && <div class="chdonel setd">{doneText(c, hhmm)}</div>}
      {(canEnd(c) || canDone(c) || canRefresh(c)) && (
        <div class="chfoot">
          {/* V0.101: Refresh now reads GitHub and git now; the line says when Fyr last read them. */}
          {canRefresh(c) && (
            <button class="chpark" title="Read GitHub and git now" onClick={() => onRun("change.refresh", { change: c.id })}>
              Refresh now
            </button>
          )}
          {c.pr?.checkedAt && <span class="chchk setd">{checkedText(c.pr.checkedAt, Date.now())}</span>}
          {/* V0.98 (4): the owner's Done; the host sends it to a host record when the card is one. */}
          {canDone(c) && (
            <button class="chpark" title="Mark this change done: it leaves the board" onClick={() => onRun("change.done", doneArgs(c))}>
              Done
            </button>
          )}
          {canEnd(c) && (
            <button class="chend" onClick={() => void end()}>
              End change…
            </button>
          )}
        </div>
      )}
    </article>
  );
}
