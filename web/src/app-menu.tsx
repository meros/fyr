// The desktop's right-click menu (V0.31) and its way to carry out a thread action, shared by the row menu and the
// header's ⋯ menu. A hook of the App in app.tsx; it takes and returns plain objects.

import { useEffect, useState } from "preact/hooks";
import { actionsFor, runAction, type ActionHandlers, type ThreadAction } from "./actions.ts";
import { menuFor, targetAt, type MenuEntry, type MenuItem, type MenuTarget } from "./menu.ts";
import { ContextMenu } from "./menu-ui.tsx";
import type { useAppState } from "./app-state.tsx";
import type { useThreadActions } from "./app-actions.tsx";

export function useContextMenu(c: Pick<ReturnType<typeof useAppState>, "store" | "threadsRef" | "setDialog" | "setHandoverId" | "phone" | "cmds" | "openPicker"> & Pick<ReturnType<typeof useThreadActions>, "openThread" | "settle" | "settleWhenDone" | "archive" | "openSnooze" | "markUnseen">) {
  const { store, threadsRef, setDialog, setHandoverId, phone, cmds, openPicker, openThread, settle, settleWhenDone, archive, openSnooze, markUnseen } = c;
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
  }, [phone, threadsRef]);
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

  return { runDeskAction, ctxMenu };
}
