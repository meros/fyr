// The open decision and its answers (useDecisionCards, useAnswers), the seen mark, the window title and whether focus
// is in a text field. Hooks of the App in app.tsx; each takes and returns plain objects.

import { isTextTarget } from "./app-util.tsx";
import { useEffect, useRef } from "preact/hooks";
import type { Decision } from "./types.ts";
import { currentDecision } from "./decode.ts";
import { markSent, setWindowFocus, shouldMarkSeen } from "./seen.ts";
import { arrive } from "./arrived.ts";
import { allowArgs, answerArgs, answerThenNext, cardKey, initCard, setOther, submitOf, updateCard, type CardState } from "./question.ts";
import type { useAppState } from "./app-state.tsx";
import type { useThreadActions } from "./app-actions.tsx";

export function useDecisionCards(c: Pick<ReturnType<typeof useAppState>, "api" | "threadsRef" | "tracker" | "setTracker" | "cards" | "setCards" | "setInText" | "sec" | "gatedRef" | "viewThread">) {
  const { api, threadsRef, tracker, setTracker, cards, setCards, setInText, sec, gatedRef, viewThread } = c;
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

  // ---- the window title ----
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

/** Answering the open thread's decision, for both fronts: the card's state, the three commands (each says whether the
 *  host took it), and the desktop's walk on to the next thread that needs the owner.
 *
 *  `answers` is what the phone calls: it stays on the thread it answered, which its own screen leaves. `submitAsk`,
 *  `nextNeeding` and `answerField` are the desktop's: it walks on once the host took the answer and says so in an
 *  arrival bar, and its composer is the answer field of an open question (the phone's card has its own field). */
export function useAnswers(c: Pick<ReturnType<typeof useAppState>, "cmds" | "view" | "cards" | "setCards" | "store" | "sel" | "sec" | "setArrival"> & Pick<ReturnType<typeof useDecisionCards>, "decision" | "card"> & Pick<ReturnType<typeof useThreadActions>, "openThread">) {
  const { cmds, view, cards, setCards, store, sel, sec, setArrival, decision, card, openThread } = c;
  const submit = async (d: Decision): Promise<boolean> => {
    const cs = cards[d.id];
    const send = cs ? submitOf(cs, d) : null;
    if (!send || !view) return false;
    return (await cmds.run("thread.answer", [view.id], answerArgs(d, send))) > 0;
  };
  /** V0.ask-cancel: close the card without answering; the agent hears why it has no answer. */
  const cancel = async (d: Decision): Promise<boolean> => (view ? (await cmds.run("thread.cancelAsk", [view.id], { decisionId: d.id })) > 0 : false);
  const allow = async (d: Decision, ok: boolean): Promise<boolean> => (view ? (await cmds.run("thread.answer", [view.id], allowArgs(d, ok))) > 0 : false);

  /** The one way both fronts change the open card: an updater on the card as it is now. */
  const onCard = (fn: (cs: CardState) => CardState) => {
    if (decision) setCards((x) => updateCard(x, decision.id, fn));
  };

  /** Opens the next (or previous) thread that needs the owner; returns its id. */
  const nextNeeding = (dir: 1 | -1, from?: string | null): string | undefined => {
    const list = sec.needs.map((t) => t.id).filter((id) => id !== from);
    if (!list.length) return;
    const cur = from ?? sel;
    const i = cur ? list.indexOf(cur) : -1;
    const j = i < 0 ? (dir === 1 ? 0 : list.length - 1) : (i + dir + list.length) % list.length;
    openThread(list[j], "open");
    return list[j];
  };

  /** The desktop's submit: once the host took the answer, the next thread that needs the owner opens, with an arrival bar. */
  const submitAsk = (d: Decision) => {
    if (!view) return;
    const id = view.id;
    const from = { id, title: store.threads[id]?.title ?? "" };
    void answerThenNext(
      () => submit(d),
      () => {
        const to = nextNeeding(1, id);
        if (to) setArrival(arrive(from, to, sec.needs.map((t) => t.id)));
      },
    );
  };

  /** Enter in the desktop's answer field: the text answers the active question, then submit or move on. */
  const answerEnter = () => {
    if (!decision || decision.kind !== "ask" || !card) return;
    const r = cardKey(card, decision, "Enter", { inOther: true });
    setCards((x) => ({ ...x, [decision.id]: r.state }));
    if (r.submit) submitAsk(decision);
    if (r.cancel) void cancel(decision);
  };
  const answerField =
    decision?.kind === "ask" && card && view?.mode === "open"
      ? {
          index: card.index,
          text: card.other[card.index] ?? "",
          setText: (text: string) => onCard((cs) => setOther(cs, decision, text)),
          submit: answerEnter,
        }
      : null;

  return { answers: { submit, cancel, allow }, onCard, submitAsk, nextNeeding, answerField };
}
