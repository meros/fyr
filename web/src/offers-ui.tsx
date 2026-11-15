// The change list and the action buttons (SPEC 0.5 V0.59 (8), V0.60 (7)). One button bar for every holder: a
// change, the thread, the live view (its page runs them through the bridge).

import { useState } from "preact/hooks";
import type { ComponentChildren } from "preact";
import type { Change, Offer, OfferRun, Thread } from "./types.ts";
import { changeList, lastRun, offerDetail, openConditions, phaseText, pressArgs, rail, shownOffers, unavailable } from "./offers.ts";
import { offerAsk } from "./confirm.ts";
import { useAsk } from "./confirm-ui.tsx";

type Run = (action: string, args?: Record<string, unknown>) => void;

/** Buttons for the actions of one holder, primary first, with what each runs and the newest result. */
export function OfferBar({ on, offers, runs, change, cwd, onRun }: { on: string; offers?: Offer[]; runs?: OfferRun[]; change?: Change; cwd?: string; onRun: Run }) {
  const list = shownOffers(offers, change);
  const [open, setOpen] = useState<string | null>(null);
  const ask = useAsk();
  if (!list.length) return null;
  const press = async (o: Offer) => {
    // V0.68: the owner sees what runs before a script or a merge starts; an agent action is a message.
    const a = offerAsk(o, cwd);
    if (a && !(await ask(a))) return;
    onRun("action.run", pressArgs(on, o));
  };
  return (
    <div class="offers">
      <div class="offb">
        {list.map((o) => {
          const why = unavailable(o, runs, change);
          return (
            <button key={o.id} class={`btn sm${o.primary ? " p" : ""}`} disabled={!!why} title={why ?? offerDetail(o, cwd)} onClick={() => void press(o)}>
              {o.run === "agent" ? "✦ " : ""}
              {o.label}
            </button>
          );
        })}
      </div>
      {list.map((o) => {
        const r = lastRun(runs, o.id);
        if (!r) return null;
        const state = r.running ? "running…" : r.ok ? "succeeded" : `failed${r.exit ? ` (exit ${r.exit})` : ""}`;
        return (
          <div key={`r-${o.id}`} class={`offrun${r.running ? "" : r.ok ? " ok" : " fail"}`}>
            <button class="offrl" onClick={() => setOpen(open === o.id ? null : o.id)} aria-expanded={open === o.id} disabled={!r.output}>
              {o.label}: {state}
            </button>
            {open === o.id && r.output && <pre class="offout">{r.output}</pre>}
          </div>
        );
      })}
    </div>
  );
}

/** The thread's own actions (V0.60): in the header, above the tabs. */
export function ThreadOffers({ t, onRun }: { t: Thread; onRun: Run }) {
  return <OfferBar on="thread" offers={t.offers} runs={t.runs} cwd={t.cwd} onRun={onRun} />;
}

/** The view's actions run through its page; this runs one with its digest. */
export const viewAction = (onRun: Run) => async (o: Offer) => (onRun("action.run", pressArgs("view", o)), true);

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
      <ol class="chrail" aria-label={`Phase: ${c.phase}`}>
        {r.end ? (
          <li class="now end">{r.end}</li>
        ) : (
          r.steps.map((s) => (
            <li key={s.phase} class={s.state}>
              {s.phase}
            </li>
          ))
        )}
      </ol>
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
    </article>
  );
}
