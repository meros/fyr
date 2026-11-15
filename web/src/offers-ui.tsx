// The change list and the action buttons (SPEC 0.5 V0.59 (8), V0.60 (7)). One button bar for every holder: a
// change, the thread, the live view (its page runs them through the bridge).

import { useState } from "preact/hooks";
import type { ComponentChildren } from "preact";
import type { Change, Offer, OfferRun, Thread } from "./types.ts";
import { canEnd, changeList, endArgs, endChangeAsk, lastRun, offerDetail, openConditions, phaseText, rail, runState, shownOffers, unavailable } from "./offers.ts";
import { press } from "./confirm.ts";
import { useAsk } from "./confirm-ui.tsx";

type Run = (action: string, args?: Record<string, unknown>) => void;

/** Buttons for the actions of one holder, primary first, with what each runs and the newest result. */
/** V0.75 (1), #62: the hour:minute of a run, in the owner's own time. */
const hhmm = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toTimeString().slice(0, 5);
};

export function OfferBar({ on, offers, runs, change, cwd, onRun }: { on: string; offers?: Offer[]; runs?: OfferRun[]; change?: Change; cwd?: string; onRun: Run }) {
  const list = shownOffers(offers, change);
  const [open, setOpen] = useState<string | null>(null);
  const ask = useAsk();
  if (!list.length) return null;
  const onPress = (o: Offer) => press(on, o, cwd, ask, onRun);
  return (
    <div class="offers">
      <div class="offb">
        {list.map((o) => {
          const why = unavailable(o, runs, change);
          const st = runState(runs, o.id);
          const last = lastRun(runs, o.id);
          // V0.75 (1), #62: a used action carries its result on the button and turns quiet; a failure keeps its weight.
          const word = st === "running" ? " · running…" : st === "ok" ? ` · ${hhmm((last!.endedAt ?? last!.at)!)}` : st === "failed" ? ` · failed${last!.exit ? ` (exit ${last!.exit})` : ""}` : "";
          const mark = st === "ok" ? "✓ " : st === "failed" ? "✕ " : o.run === "agent" ? "✦ " : "";
          const title = st === "fresh" ? (why ?? offerDetail(o, cwd)) : `${offerDetail(o, cwd)} · the last run ${last?.running ? "is running" : last?.ok ? "succeeded" : "failed"}`;
          return (
            <button key={o.id} class={`btn sm${o.primary && st === "fresh" ? " p" : ""}${st === "ok" ? " used" : st === "failed" ? " bad" : ""}`} disabled={!!why} title={title} aria-busy={st === "running" || undefined} onClick={() => void onPress(o)}>
              {mark}
              {o.label}
              {word}
            </button>
          );
        })}
        {/* the newest run's output, one press away, on a control of its own: a button cannot hold a button */}
        {(() => {
          const withOut = [...list].reverse().find((o) => lastRun(runs, o.id)?.output);
          if (!withOut) return null;
          return (
            <button class={`offout-b${open === withOut.id ? " on" : ""}`} title={`${withOut.label}: the last run's output`} aria-expanded={open === withOut.id} onClick={() => setOpen(open === withOut.id ? null : withOut.id)}>
              ▤
            </button>
          );
        })()}
      </div>
      {open !== null && <pre class="offout">{lastRun(runs, open)?.output}</pre>}
    </div>
  );
}

/** The thread's own actions (V0.60): in the header, above the tabs. */
export function ThreadOffers({ t, onRun }: { t: Thread; onRun: Run }) {
  return <OfferBar on="thread" offers={t.offers} runs={t.runs} cwd={t.cwd} onRun={onRun} />;
}

/** The view's actions run through its page (fyr.run, data-action); the owner confirms a script or a fyr action
 *  first, as on a button (V0.68 (8)), so a page cannot run one by itself. */
export function useViewAction(onRun: Run, cwd?: string) {
  const ask = useAsk();
  return (o: Offer) => press("view", o, cwd, ask, onRun);
}

const KIND: Record<string, string> = { code: "⌥", infra: "☁", host: "▣", data: "⛁", doc: "▤", design: "◇", other: "•" };

/** The Changes tab's list (V0.59 (8)): every change, newest first, above the branch diff. */
export function ChangeList({ t, onRun }: { t: Thread; onRun: Run }) {
  const list = changeList(t);
  if (!list.length) return null;
  return (
    <section class="chl" aria-label="Changes">
      {list.map((c) => (
        <ChangeCard key={c.id} c={c} cwd={t.cwd} onRun={onRun} />
      ))}
    </section>
  );
}

/** One change: rail, conditions, targets, proof and its actions. `from` is a line above the title (the board names
 *  the thread there). */
export function ChangeCard({ c, cwd, onRun, from }: { c: Change; cwd?: string; onRun: Run; from?: ComponentChildren }) {
  const r = rail(c);
  const conds = openConditions(c);
  const rev = c.revisions?.at(-1);
  const ask = useAsk();
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
        <span class="chp">{phaseText(c)}</span>
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
      {c.needs === "owner" && <div class="chneed">Needs you</div>}
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
      <OfferBar on={c.id} offers={c.offers} runs={c.runs} change={c} cwd={cwd} onRun={onRun} />
      {canEnd(c) && (
        <button class="chend" onClick={() => void end()}>
          End this change
        </button>
      )}
    </article>
  );
}
