// The right-click menu (V0.31), the desktop's way to carry out a thread action, and the answers to a decision card.
//
// Reads:   data (store, threadsRef); cmds; open (view, marked row, setCards, setArrival); ui (dialogs it opens);
//          device.phone; inbox.sec; h.cards (decision, card); h.answers; h.actions (openThread, settle, ...).
// Returns: ctxMenu (the element to draw), runDeskAction, submitAsk, cancelAsk, answerField, allow, nextNeeding.
// Desktop only: the menu opens on a right click and never on a phone (it still stops the browser's menu there).

import { useEffect, useState } from "preact/hooks";
import type { Decision } from "./types.ts";
import { arrive } from "./arrived.ts";
import { answerThenNext, cardKey, setOther } from "./question.ts";
import { actionsFor, runAction, type ActionHandlers, type ThreadAction } from "./actions.ts";
import { menuFor, targetAt, type MenuEntry, type MenuItem, type MenuTarget } from "./menu.ts";
import { ContextMenu } from "./menu-ui.tsx";
import type { AppState } from "./app-state.tsx";
import type { Answers, DecisionCards } from "./app-cards.tsx";
import type { ThreadActions } from "./app-actions.tsx";

export function useContextMenu(s: Pick<AppState, "data" | "cmds" | "open" | "ui" | "device" | "inbox">, h: { cards: DecisionCards; answers: Answers; actions: ThreadActions }) {
  const { store, threadsRef } = s.data;
  const { cmds } = s;
  const { view, sel, setCards, setArrival } = s.open;
  const { setDialog, setHandoverId, openPicker } = s.ui;
  const { phone } = s.device;
  const { sec } = s.inbox;
  const { decision, card } = h.cards;
  const { answers } = h.answers;
  const { openThread, settle, settleWhenDone, archive, openSnooze, markUnseen } = h.actions;
  // ---- right-click menu (V0.31) ----
  const [ctx, setCtx] = useState<{ x: number; y: number; items: MenuEntry[]; target: MenuTarget; anchor: Element | null } | null>(null);
  useEffect(() => {
    const on = (e: MouseEvent) => {
      const selection = String(window.getSelection?.() ?? "").trim();
      const target = targetAt(e.target as Element | null, selection, threadsRef.current);
      const items = menuFor(target);
      if (items === null) return; // text fields keep the browser menu (spelling, Paste)
      e.preventDefault();
      if (phone || !items.length) return;
      setCtx({ x: e.clientX, y: e.clientY, items, target, anchor: (e.target as Element | null)?.closest?.("[data-id]") ?? (e.target as Element | null) });
    };
    document.addEventListener("contextmenu", on);
    return () => document.removeEventListener("contextmenu", on);
  }, [phone]);
  const copyText = (text: string) => {
    void navigator.clipboard?.writeText(text).then(
      () => cmds.notify("Copied"),
      () => cmds.showError("Could not copy"),
    );
  };
  /** V0.one-action-list: the desktop's way to carry out a thread action, from the row menu and the header's ⋯ menu. */
  const deskHandlers: ActionHandlers = {
    command: (command, id, args) => void cmds.run(command, [id], args),
    sheet: (sheet, id) => {
      if (sheet === "snooze") openSnooze([id]);
      else if (sheet === "project") openPicker([id]);
      else if (sheet === "handover") (setHandoverId(id), setDialog("handover"));
    },
    navigate: (id) => openThread(id, "open"),
    copy: copyText,
    own: { settle, settleWhenDone, archive: (id) => archive([id]), markUnseen, regenTitle: (id) => void cmds.regenerateTitle(id) },
  };
  const runDeskAction = (a: ThreadAction, id: string) => runAction(a, id, deskHandlers, location.origin);
  const pickMenu = (it: MenuItem, target: MenuTarget) => {
    setCtx(null);
    if (target.kind === "thread") {
      const a = actionsFor("desktop", target.t, "row").find((x) => x.id === it.id);
      if (a) runDeskAction(a, target.t.id);
      return;
    }
    if (it.id === "copy" && (target.kind === "text" || target.kind === "link")) copyText(target.kind === "text" ? target.selection : (target.selection ?? ""));
    else if (it.id === "openLink" && target.kind === "link") window.open(target.href, "_blank", "noopener");
    else if (it.id === "copyLink" && target.kind === "link") copyText(target.href);
    else if (it.id === "openImage" && target.kind === "image") window.open(target.src, "_blank", "noopener");
  };
  // A thread's menu follows the thread while it is open (its state can change under the pointer); the action goes to
  // the row's own thread id, the same settle() as the thread's button and `e`.
  const ctxTarget: MenuTarget | null = ctx && (ctx.target.kind === "thread" ? (store.threads[ctx.target.t.id] ? { kind: "thread", t: store.threads[ctx.target.t.id] } : null) : ctx.target);
  const ctxMenu = ctx && ctxTarget && (
    <ContextMenu x={ctx.x} y={ctx.y} items={ctx.target.kind === "thread" ? (menuFor(ctxTarget) ?? []) : ctx.items} anchor={ctx.anchor} onPick={(it) => pickMenu(it, ctxTarget)} onClose={() => setCtx(null)} />
  );

  /** The desktop walks on to the next thread that needs the owner once the host took the answer, and says so in an
   *  arrival bar. The phone has no such step: it stays on the thread, whose screen it leaves itself. */
  const submitAsk = (d: Decision) => {
    if (!view) return;
    const id = view.id;
    const from = { id, title: store.threads[id]?.title ?? "" };
    void answerThenNext(
      () => answers.submit(d),
      () => {
        const to = nextNeeding(1, id);
        if (to) setArrival(arrive(from, to, sec.needs.map((t) => t.id)));
      },
    );
  };

  const cancelAsk = (d: Decision) => void answers.cancel(d);

  /** Enter in the answer field: the text answers the active question, then submit or move on. */
  const answerEnter = () => {
    if (!decision || decision.kind !== "ask" || !card) return;
    const r = cardKey(card, decision, "Enter", { inOther: true });
    setCards((c) => ({ ...c, [decision.id]: r.state }));
    if (r.submit) submitAsk(decision);
    if (r.cancel) cancelAsk(decision);
  };
  const answerField =
    decision?.kind === "ask" && card && view?.mode === "open"
      ? {
          index: card.index,
          text: card.other[card.index] ?? "",
          setText: (text: string) => setCards((c) => (c[decision.id] ? { ...c, [decision.id]: setOther(c[decision.id], decision, text) } : c)),
          submit: answerEnter,
        }
      : null;

  const allow = (d: Decision, ok: boolean) => void answers.allow(d, ok);

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

  return { runDeskAction, ctxMenu, submitAsk, cancelAsk, answerField, allow, nextNeeding };
}

export type ContextMenuState = ReturnType<typeof useContextMenu>;
