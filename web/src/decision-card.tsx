// The docked decision card (V0.27, V0.readable-approvals): effect and approval bodies, the question card.
import { approvalView } from "./approval-view.ts";
import { type ApprovalField, type ApprovalText } from "./approvals.ts";
import { type EffectText } from "./effects.ts";
import { useState } from "preact/hooks";
import type { Decision, PullRequest } from "./types.ts";
import { StateIcon } from "./icons.tsx";
import { back, pick, progressText, questionsOf, goTo, settledOf, skip, type CardState, nextOpen, submitState } from "./question.ts";

interface CardProps {
  d: Decision;
  more: number;
  card: CardState | null;
  /** V0.app-shell-tidy: an updater on the card as it is now, as the phone's card takes it */
  onCard(fn: (cs: CardState) => CardState): void;
  onAllow(d: Decision, allow: boolean): void;
  /** U15: send the answers of an ask card */
  onSubmitAsk?(d: Decision): void;
  /** V0.ask-cancel: close the card without an answer */
  onCancelAsk?(d: Decision): void;
  /** the thread's PR, to name it in a merge card */
  pr?: PullRequest | null;
  /** the thread's working directory and host, for readable approval cards */
  cwd?: string;
  host?: string;
}

function asker(d: Decision): string {
  return d.agent ? `asked by subagent “${d.agent}”` : "asked by this thread";
}

/** One sentence for an effect decision (V0.27), for rows and previews. */
/** The plain body of an effect card (V0.27): reason, what happens, details behind a toggle. */
export function EffectBody({ d, x, cls = "" }: { d: Decision; x: EffectText; cls?: string }) {
  return (
    <div class={`eff ${cls}`}>
      {x.why && <p class="eff-why">{x.why}</p>}
      <p class="eff-what">{x.happens}</p>
      <details class="eff-det">
        <summary>Details</summary>
        <div class="eff-who">{d.agent ? `Asked by subagent “${d.agent}”` : "Asked by this thread"} · the agent does not wait for the result</div>
        <pre>{x.details.action + "\n" + JSON.stringify(x.details.args, null, 2)}</pre>
      </details>
    </div>
  );
}

const LONG_FIELD_CHARS = 260;
const LONG_FIELD_LINES = 5;

function isLongField(f: ApprovalField): boolean {
  return f.value.length > LONG_FIELD_CHARS || f.value.split("\n").length > LONG_FIELD_LINES;
}

function ApprovalFieldRow({ f }: { f: ApprovalField }) {
  const [open, setOpen] = useState(false);
  const long = !f.mono && isLongField(f);
  return (
    <div class="appr-f">
      <div class="appr-k">{f.label}</div>
      {f.mono ? (
        <pre class="appr-v mono">{f.value}</pre>
      ) : (
        <>
          <div class={`appr-v prose${long && !open ? " clamp" : ""}`}>{f.value}</div>
          {long && (
            <button type="button" class="appr-more" onClick={() => setOpen(!open)}>
              {open ? "Show less" : "Show full text"}
            </button>
          )}
        </>
      )}
    </div>
  );
}

/** V0.readable-approvals: the human-readable body of an approval card, shared between desktop and phone.
 *  V0.tool-approval-judge: the sandbox badge and the judge's note stand above it on both fronts.
 *  The raw tool name and full JSON input stay behind Details. */
export function ApprovalBody({ a, host, cls = "" }: { a: ApprovalText; host?: string; cls?: string }) {
  const hasInput = Object.keys(a.details.input).length > 0;
  const whereLine = [a.where ? `in ${a.where}` : "", host ? `on ${host}` : ""].filter(Boolean).join(" · ");
  const metaLine = [a.details.tool || "tool", a.details.cwd ? `in ${a.details.cwd}` : "", host ? `on ${host}` : ""].filter(Boolean).join(" · ");
  return (
    <div class={`appr ${cls}`}>
      {a.outsideSandbox && <span class="ap-badge">Outside sandbox</span>}
      {a.judgeNote && <div class="ap-note">Judge: {a.judgeNote}</div>}
      {a.summary && <p class="appr-sum">{a.summary}</p>}
      {a.command && <pre class="appr-cmd">{a.command}</pre>}
      {a.fields.length > 0 && (
        <div class="appr-fields">
          {a.fields.map((f, i) => (
            <ApprovalFieldRow key={`${f.label}-${i}`} f={f} />
          ))}
        </div>
      )}
      {a.hiddenFields > 0 && <div class="appr-hidden">+{a.hiddenFields} more in Details</div>}
      {a.showWhere && whereLine && <div class="appr-where ell">{whereLine}</div>}
      <details class="eff-det">
        <summary>Details</summary>
        <div class="eff-who">{metaLine}</div>
        {hasInput && <pre>{JSON.stringify(a.details.input, null, 2)}</pre>}
      </details>
    </div>
  );
}

export function DecisionCard(p: CardProps) {
  const { d } = p;
  if (d.kind === "ask") return p.card && p.card.decisionId === d.id ? <AskCard {...p} card={p.card} /> : null;
  const v = approvalView(d, { cwd: p.cwd, host: p.host }, p.pr);
  const { approval: a, effect: x } = v;
  return (
    <div class="ask">
      <div class="ah">
        <StateIcon state="asking" size={14} />
        <b>{v.label}</b>
        <span class="ell">{asker(d)}</span>
        {p.more > 0 && <span class="ml">+{p.more} more</span>}
      </div>
      <div class="ab">
        <div class="q">{v.title}</div>
        <div class="payload">
          {a && <ApprovalBody a={a} host={v.host} />}
          {x && <EffectBody d={d} x={x} />}
        </div>
        <div class="grid4">
          <button class="op" onClick={() => p.onAllow(d, true)}>
            <span class="a ell">{v.yes}</span>
            <kbd>1</kbd>
            {v.yesDesc && <span class="d">{v.yesDesc}</span>}
          </button>
          <button class="op" onClick={() => p.onAllow(d, false)}>
            <span class="a ell">{v.no}</span>
            <kbd>2</kbd>
            <span class="d">{v.noDesc}</span>
          </button>
        </div>
      </div>
    </div>
  );
}

function AskCard(p: CardProps & { card: CardState }) {
  const { d, card } = p;
  const qs = questionsOf(d);
  const q = qs[card.index];
  if (!q) return null;
  const picks = card.picks[card.index] ?? [];
  const other = card.other[card.index];
  const passed = settledOf(card, d, card.index) && picks.length === 0 && !other;
  // U15: always a visible way to finish (Enter still works). V0.ask-cancel: Back (⇧Tab, ←), Skip (⇧S)
  // and Cancel (⇧Esc) sit beside it, and the button sends, moves on, or cancels what the card holds.
  const st = submitState(card, d);
  const primary = () => (st.act === "submit" ? p.onSubmitAsk?.(d) : st.act === "cancel" ? p.onCancelAsk?.(d) : st.act === "next" && p.onCard((cs) => goTo(cs, d, nextOpen(cs, d))));
  return (
    <div class="ask">
      <div class="ah">
        <StateIcon state="asking" size={14} />
        <b class="qn" onClick={() => qs.length > 1 && p.onCard((cs) => goTo(cs, d, (cs.index + 1) % qs.length))} title={qs.length > 1 ? "Next question (Tab)" : undefined}>
          Question {card.index + 1} of {qs.length}
        </b>
        <span class="ell">{asker(d)}</span>
        <span class="ml">
          {p.more > 0 && `+${p.more} more · `}
          <kbd>Tab</kbd> next question
        </span>
        <button class="ahx" title="Cancel the question without answering it (⇧Esc)" onClick={() => p.onCancelAsk?.(d)}>
          Cancel
        </button>
      </div>
      <div class="ab">
        <div class="q">{q.question}</div>
        <div class="grid4">
          {q.options.map((o, i) => (
            <button class={`op${picks.includes(o.label) ? " on" : ""}`} title={`${o.label}${o.description ? `\n${o.description}` : ""}`} onClick={() => p.onCard((cs) => pick(cs, d, i))}>
              <span class="a ell">{o.label}</span>
              <kbd>{i + 1}</kbd>
              <span class="d">{o.description ?? ""}</span>
            </button>
          ))}
          <button class={`op${other ? " on" : ""}`} onClick={() => document.getElementById("composer")?.focus()} title="Answer in your own words in the field below (0)">
            <span class="a ell">Other…</span>
            <kbd>0</kbd>
            <span class="d">{other ?? ""}</span>
          </button>
        </div>
        <div class="askf">
          <span class="askp">{progressText(st)}</span>
          {card.index > 0 && (
            <button class="askb" title="Previous question (⇧Tab or ←)" onClick={() => p.onCard((cs) => back(cs, d))}>
              ‹ Back
            </button>
          )}
          <button class={`askb${passed ? " on" : ""}`} title={passed ? "Take the skip back (⇧S)" : "Leave this question without an answer (⇧S)"} onClick={() => p.onCard((cs) => skip(cs, d))}>
            {passed ? "Unskip" : "Skip"}
          </button>
          <button class={`btn ${st.act === "submit" || st.act === "cancel" ? "p" : ""}`} disabled={st.act === "none"} onClick={primary}>
            {st.label}
            {st.act === "submit" && <kbd>↵</kbd>}
          </button>
        </div>
      </div>
    </div>
  );
}

