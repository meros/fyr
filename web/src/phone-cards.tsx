// The phone's decision cards: the ask card, the approval card and their header (split from phone.tsx).

import { I } from "./phone-icons.tsx";
import { haptic } from "./native.ts";
import { type ComponentChildren } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import type { Decision, Thread } from "./types.ts";
import { StateIcon } from "./icons.tsx";
import { approvalView } from "./approval-view.ts";
import { prForEffect } from "./offers.ts";
import { answerOf, back, goTo, pickStay, chooseOther, questionsOf, setOther, settledOf, skip, type CardState } from "./question.ts";
import { ADVANCE_MS, allowCommit, askCommit, stepLabel, type Choice } from "./mobile.ts";
import { EffectBody, ApprovalBody } from "./decision-card.tsx";

// ---- decisions ----

function CardHead({ children, collapsed, onCollapse, onCancel }: { children: ComponentChildren; collapsed: boolean; onCollapse(): void; onCancel?(): void }) {
  return (
    <div class="m-ah" onClick={onCollapse}>
      <StateIcon state="asking" size={14} />
      {children}
      <span class="sp" />
      {onCancel && (
        <button class="b44 m-ahx" aria-label="Cancel the question" title="Cancel the question" onClick={(e) => (e.stopPropagation(), onCancel())}>
          {I.close}
        </button>
      )}
      <button class="b44" aria-label={collapsed ? "Show the question" : "Show the conversation"} onClick={(e) => (e.stopPropagation(), onCollapse())}>
        {collapsed ? I.up : I.down}
      </button>
    </div>
  );
}

export function MAsk({ d, card, onCard, collapsed, onCollapse, submit, cancel }: { d: Decision; card: CardState; onCard(fn: (cs: CardState) => CardState): void; collapsed: boolean; onCollapse(): void; submit(): void; cancel(): void }) {
  const qs = questionsOf(d);
  const q = qs[card.index];
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const qRef = useRef<HTMLDivElement>(null);
  const [clamped, setClamped] = useState(false);
  const [readAll, setReadAll] = useState(false);
  const otherRef = useRef<HTMLInputElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const [below, setBelow] = useState(0);
  /** Options under the fold get a "↓ N more options" band above the Answer bar (README, Decisions). */
  const measure = () => {
    const body = bodyRef.current;
    if (!body) return setBelow(0);
    const bottom = body.getBoundingClientRect().bottom;
    setBelow([...body.querySelectorAll(".m-op")].filter((el) => el.getBoundingClientRect().top > bottom - 24).length);
  };
  useLayoutEffect(measure, [card.index, readAll, collapsed, card.otherOpen]);
  useEffect(() => () => {
    if (timer.current) clearTimeout(timer.current);
  }, []);
  useLayoutEffect(() => {
    setReadAll(false);
  }, [card.index]);
  useLayoutEffect(() => {
    const el = qRef.current;
    setClamped(!!el && el.scrollHeight > el.clientHeight + 2);
  }, [card.index, readAll, q?.question]);
  useEffect(() => {
    if (card.otherOpen) otherRef.current?.focus();
  }, [card.otherOpen, card.index]);
  if (!q) return null;
  const step = stepLabel(card.index, qs.length);
  const commit = askCommit(card, d);
  const picks = card.picks[card.index] ?? [];
  const otherOn = card.other[card.index] !== null || card.otherOpen;
  const passed = settledOf(card, d, card.index) && !picks.length && !otherOn;

  const tap = (i: number) => {
    const idx = card.index;
    onCard((cs) => pickStay(cs, d, i));
    if (!q.multiSelect && idx < qs.length - 1) {
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => onCard((cs) => (cs.index === idx ? goTo(cs, d, idx + 1) : cs)), ADVANCE_MS);
    }
  };
  const onCommit = () => {
    if (commit.act === "submit") submit();
    else if (commit.act === "cancel") cancel();
    else if (commit.act === "next") onCard((cs) => goTo(cs, d, cs.index + 1));
  };

  return (
    <>
      <div class={`m-ask${collapsed ? " collapsed" : ""}`}>
        <CardHead collapsed={collapsed} onCollapse={onCollapse} onCancel={cancel}>
          <b>{step.text}</b>
          {step.dots && qs.length > 1 && (
            <span class="m-dots">
              {qs.map((_, i) => (
                <i class={i === card.index ? "on" : settledOf(card, d, i) ? "done" : ""} />
              ))}
            </span>
          )}
          {q.multiSelect && <span class="mt">multi-select</span>}
        </CardHead>
        {!collapsed && (
          <div class="m-abody" ref={bodyRef} onScroll={measure}>
            {card.index === 0 && <div class="m-asker">{d.agent ? `Asked by subagent “${d.agent}”` : "Asked by the thread"}</div>}
            {qs.slice(0, card.index).map((pq, i) => (
              <div class="m-prev">
                <b>{i + 1}</b>
                <span class="ell">{answerOf(card, d, i) ?? (card.skipped[i] ? "Skipped" : "Not answered")}</span>
                <button class="ed" onClick={() => onCard((cs) => goTo(cs, d, i))} aria-label={`Change the answer to: ${pq.question}`}>
                  Change
                </button>
              </div>
            ))}
            <div ref={qRef} class={`m-q${readAll ? "" : " clamp"}`}>
              {q.question}
            </div>
            {(clamped || readAll) && (
              <button class="m-rd" onClick={() => setReadAll(!readAll)}>
                {readAll ? "Show less" : "Read all"}
              </button>
            )}
            <div class="m-opts" role={q.multiSelect ? "group" : "radiogroup"}>
              {q.options.map((o, i) => {
                const on = picks.includes(o.label);
                return (
                  <button class={`m-op${q.multiSelect ? " multi" : ""}${on ? " on" : ""}`} role={q.multiSelect ? "checkbox" : "radio"} aria-checked={on} onClick={() => tap(i)}>
                    <i class="mk" />
                    <span class="ot">
                      <span class="a">{o.label}</span>
                      {o.description && <span class="d">{o.description}</span>}
                    </span>
                  </button>
                );
              })}
              <button class={`m-op${q.multiSelect ? " multi" : ""}${otherOn ? " on" : ""}`} role={q.multiSelect ? "checkbox" : "radio"} aria-checked={otherOn} onClick={() => onCard((cs) => chooseOther(cs, d))}>
                <i class="mk" />
                <span class="ot">
                  <span class="a">Other…</span>
                  {!otherOn && <span class="d">Answer in your own words.</span>}
                </span>
              </button>
              {otherOn && (
                <input
                  ref={otherRef}
                  class="m-otherf"
                  value={card.other[card.index] ?? ""}
                  placeholder="Your answer"
                  enterKeyHint={commit.act === "next" ? "next" : "send"}
                  onInput={(e) => {
                    const v = (e.target as HTMLInputElement).value;
                    onCard((cs) => setOther({ ...cs, otherOpen: true }, d, v));
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      onCommit();
                    }
                  }}
                />
              )}
            </div>
            {/* V0.ask-cancel: Back to the question before, and Skip to leave this one for the agent */}
            <div class="m-asknav">
              {card.index > 0 && (
                <button class="m-navb" onClick={() => onCard((cs) => back(cs, d))}>
                  {I.back}
                  Back
                </button>
              )}
              <button class={`m-navb${passed ? " on" : ""}`} onClick={() => onCard((cs) => skip(cs, d))}>
                {passed ? "Unskip" : "Skip this question"}
              </button>
            </div>
          </div>
        )}
        {!collapsed && below > 0 && (
          <button class="m-below" onClick={() => bodyRef.current?.scrollBy({ top: bodyRef.current.clientHeight * 0.8, behavior: "smooth" })}>
            ↓ {below} more {below === 1 ? "option" : "options"}
          </button>
        )}
      </div>
      <div class="m-abar">
        <button class="m-bt p" disabled={!commit.enabled} onClick={onCommit}>
          {commit.label}
        </button>
      </div>
    </>
  );
}

export function MAllow({ d, t, host, collapsed, onCollapse, commit }: { d: Decision; t: Thread; host: string; collapsed: boolean; onCollapse(): void; commit(ok: boolean): void }) {
  const [choice, setChoice] = useState<Choice>(null);
  useEffect(() => setChoice(null), [d.id]);
  const v = approvalView(d, t, prForEffect(t, d));
  const { approval: a, effect: x } = v;
  const c = allowCommit(d, choice);
  const tile = (value: Choice, label: string, desc: string) => (
    <button class={`m-op${choice === value ? " on" : ""}`} role="radio" aria-checked={choice === value} onClick={() => setChoice(value)}>
      <i class="mk" />
      <span class="ot">
        <span class="a">{label}</span>
        {desc && <span class="d">{desc}</span>}
      </span>
    </button>
  );
  return (
    <>
      <div class={`m-ask${collapsed ? " collapsed" : ""}`}>
        <CardHead collapsed={collapsed} onCollapse={onCollapse}>
          <b>{v.label}</b>
        </CardHead>
        {!collapsed && (
          <div class="m-abody">
            {a && <div class="m-asker">{d.agent ? `Asked by subagent “${d.agent}”` : "Asked by the thread"}</div>}
            <div class="m-q">{v.title}</div>
            {a ? <ApprovalBody a={a} host={host || v.host} cls="m-appr" /> : x && <EffectBody d={d} x={x} cls="m-eff" />}
            <div class="m-opts" role="radiogroup">
              {tile("allow", v.yes, v.yesDesc)}
              {tile("deny", v.no, v.noDesc)}
            </div>
          </div>
        )}
      </div>
      <div class="m-abar">
        <button class="m-bt p" disabled={!c.enabled} onClick={() => choice && (haptic("firm"), commit(choice === "allow"))}>
          {c.label}
        </button>
      </div>
    </>
  );
}

