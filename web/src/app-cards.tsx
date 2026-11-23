// The open decision and its card, the seen mark, the window title and the focus-in-a-text-field flag.
//
// Reads:   data (api, threadsRef); open (tracker, cards, gatedRef, viewThread and their setters); ui.setInText;
//          inbox.sec (the title counts what needs the owner).
// Returns: decision (the open thread's current decision) and card (its answer state, for an ask).
// Desktop and phone share it.

import { isTextTarget } from "./app-util.tsx";
import { useEffect, useRef } from "preact/hooks";
import type { Decision } from "./types.ts";
import { currentDecision } from "./decode.ts";
import { markSent, setWindowFocus, shouldMarkSeen } from "./seen.ts";
import { allowArgs, answerArgs, initCard, submitOf } from "./question.ts";

import type { AppState } from "./app-state.tsx";

export function useDecisionCards(s: Pick<AppState, "data" | "open" | "ui" | "inbox">) {
  const { api, threadsRef } = s.data;
  const { tracker, setTracker, cards, setCards, gatedRef, viewThread } = s.open;
  const { setInText } = s.ui;
  const { sec } = s.inbox;
  // Decision card state follows the open decision.
  const decision: Decision | null = viewThread ? currentDecision(viewThread) : null;
  useEffect(() => {
    if (decision?.kind === "ask" && !cards[decision.id]) setCards((c) => ({ ...c, [decision.id]: initCard(decision) }));
  }, [decision?.id]);
  const card = decision?.kind === "ask" ? (cards[decision.id] ?? null) : null;

  // ---- mark seen (SPEC 6.4) ----
  const trackerRef = useRef(tracker);
  trackerRef.current = tracker;
  useEffect(() => {
    const iv = setInterval(() => {
      const tr = trackerRef.current;
      const t = tr.threadId ? threadsRef.current[tr.threadId] : undefined;
      // V0.114: a thread behind the quiet gate is not on screen, so it is not seen.
      if (t && !gatedRef.current && shouldMarkSeen(tr, t, Date.now())) {
        setTracker((x) => markSent(x, t));
        // upTo: only what this view showed counts as seen (API v0, SPEC 6.4).
        void api.command(t.hostId, "thread.markSeen", t.id, { upTo: t.activityAt });
      }
    }, 250);
    const onFocus = () => setTracker((x) => setWindowFocus(x, true, Date.now()));
    const onBlur = () => setTracker((x) => setWindowFocus(x, false, Date.now()));
    window.addEventListener("focus", onFocus);
    window.addEventListener("blur", onBlur);
    return () => {
      clearInterval(iv);
      window.removeEventListener("focus", onFocus);
      window.removeEventListener("blur", onBlur);
    };
  }, []);

  // ---- title, toast timer ----
  useEffect(() => {
    const n = sec.needs.length;
    document.title = n ? `(${n}) Fyr` : "Fyr";
  }, [sec.needs.length]);

  // Track whether focus is in a text field (for the key-hint bar).
  useEffect(() => {
    const f = () => setInText(isTextTarget(document.activeElement));
    const out = () => setTimeout(f, 0);
    document.addEventListener("focusin", f);
    document.addEventListener("focusout", out);
    return () => {
      document.removeEventListener("focusin", f);
      document.removeEventListener("focusout", out);
    };
  }, []);

  return { decision, card };
}

/** The three ways a card or an approval answers its thread, for the desktop's keys and buttons and the phone's alike:
 *  each says whether the host took it. What follows an answer is the front's own (the desktop walks on to the next
 *  thread that needs the owner and shows an arrival bar; the phone stays on the thread it answered, which its own
 *  screen leaves). */
export function useAnswers(s: Pick<AppState, "cmds" | "open">) {
  const { cmds } = s;
  const { view, cards } = s.open;
  const submit = async (d: Decision): Promise<boolean> => {
    const cs = cards[d.id];
    const send = cs ? submitOf(cs, d) : null;
    if (!send || !view) return false;
    return (await cmds.run("thread.answer", [view.id], answerArgs(d, send))) > 0;
  };
  /** V0.ask-cancel: close the card without answering; the agent hears why it has no answer. */
  const cancel = async (d: Decision): Promise<boolean> => (view ? (await cmds.run("thread.cancelAsk", [view.id], { decisionId: d.id })) > 0 : false);
  const allow = async (d: Decision, ok: boolean): Promise<boolean> => (view ? (await cmds.run("thread.answer", [view.id], allowArgs(d, ok))) > 0 : false);
  return { answers: { submit, cancel, allow } };
}

export type DecisionCards = ReturnType<typeof useDecisionCards>;
export type Answers = ReturnType<typeof useAnswers>;
