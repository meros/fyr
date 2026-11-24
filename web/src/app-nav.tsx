// History on both fronts (the phone's screens; the desktop's Back and Forward over opened threads), the open thread
// following the phone's screen, the phone's thread actions, and the way Settings and undeclared work navigate on
// either front. A hook of the App in app.tsx; it takes and returns plain objects.

import { useEffect, useRef } from "preact/hooks";
import { type PageRef } from "./settings-nav.ts";
import { type UndNav } from "./undeclared-ctx.tsx";
import { type PhoneActions } from "./phone.tsx";
import { backTarget, listenPopState, type Nav } from "./nav.ts";
import { seedPhoneHistory } from "./back-layer.ts";
import { navTransition } from "./native.ts";
import { settleRun } from "./actions.ts";
import { leaveAfterHost } from "./leave.ts";
import { isDeskEntry, popStep } from "./desk-history.ts";
import type { useAppState } from "./app-state.tsx";
import type { useThreadActions } from "./app-actions.tsx";
import type { useProjectActions } from "./app-projects.tsx";
import type { useNavHistory } from "./app-history.tsx";

export function usePhoneNav(c: Pick<ReturnType<typeof useAppState>, "store" | "threadsRef" | "view" | "setDialog" | "phone" | "phoneRef" | "cmds" | "nav" | "setNav" | "openSettings"> & Pick<ReturnType<typeof useThreadActions>, "deskAtRef" | "openThread" | "closePeek" | "settleWhenDone" | "settleBulk" | "markUnseen"> & Pick<ReturnType<typeof useProjectActions>, "directory" | "create"> & Pick<ReturnType<typeof useNavHistory>, "navGo" | "navRef" | "leaveOpenThread" | "navReplace" | "navBack">) {
  const { store, threadsRef, view, setDialog, phone, phoneRef, cmds, nav, setNav, openSettings, deskAtRef, openThread, closePeek, settleWhenDone, settleBulk, markUnseen, directory, create, navGo, navRef, leaveOpenThread, navReplace, navBack } = c;
  // Build the history once (navInit; a layer that is up in the first render has built it already, back-layer.ts
  // `seedPhoneHistory`); the listener is added whenever the phone layout is on, and removed when it goes (a window
  // resized to the desktop and back must have it again).
  const navInit = useRef(false);
  useEffect(() => {
    if (!phone) return;
    if (!navInit.current) {
      navInit.current = true;
      setNav(seedPhoneHistory());
    }
    return listenPopState(window, { search: () => location.search, current: () => navRef.current, set: setNav, slide: (apply) => navTransition("back", apply) });
  }, [phone, navRef, setNav]);
  // V0.145: Back and Forward on the desktop (the mouse's side buttons, Alt+Left and Alt+Right, the browser's arrows)
  // open the thread of the entry, with the tab it had (ThreadPane reads it from the URL). A thread archived or deleted
  // since is skipped in the same direction. The browser handles the side buttons itself, in a tab and in the installed
  // window alike, so Fyr listens to popstate only.
  const viewRef = useRef(view);
  viewRef.current = view;
  /* eslint-disable react-hooks/exhaustive-deps -- the desktop's popstate listener is set once per front; openThread (new each render) only calls setters and reads refs, so the first one acts the same */
  useEffect(() => {
    if (phone) return;
    if (isDeskEntry(history.state)) deskAtRef.current = history.state.desk;
    const onPop = (e: PopStateEvent) => {
      const gone = (id: string) => {
        const t = threadsRef.current[id];
        return !t || t.attention.state === "archived";
      };
      const step = popStep(e.state, deskAtRef.current, viewRef.current?.id ?? null, gone);
      if (isDeskEntry(e.state)) deskAtRef.current = e.state.desk;
      if (!step) return;
      if ("skip" in step) history.go(step.skip);
      else openThread(step.open, "open", "auto");
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, [phone]);
  /* eslint-enable react-hooks/exhaustive-deps */
  // The open thread follows the screen.
  /* eslint-disable react-hooks/exhaustive-deps -- follows the phone's screen (its name or thread id) only; view, openThread and closePeek are read at that moment, so a view change alone does not reopen the thread */
  useEffect(() => {
    if (!phone) return;
    const s = nav.screen;
    if (s.name === "thread") {
      if (view?.id !== s.id || view.mode !== "open") openThread(s.id, "open");
    } else if (view) closePeek();
  }, [phone, nav.screen.name === "thread" ? nav.screen.id : nav.screen.name]);
  /* eslint-enable react-hooks/exhaustive-deps */

  // V0.app-shell-tidy: settle, archive and snooze leave the open thread once the host took them, as on the desktop.
  const phoneActions: PhoneActions = {
    settle: (ids, bulk, unreadKept) =>
      void leaveAfterHost(bulk ? settleBulk(ids, { unreadKept }) : cmds.took("thread.settle", ids, undefined, ids.length === 1 ? settleRun(store.threads[ids[0]]) : undefined), leaveOpenThread),
    settleWhenDone,
    archive: (ids) => void leaveAfterHost(cmds.took("thread.archive", ids), leaveOpenThread),
    snooze: (c) => void leaveAfterHost(cmds.took(c.action, c.ids, c.args), leaveOpenThread),
    markUnseen: (id) => markUnseen(id),
    create,
  };

  /** V0.119 phone: a Settings page is a screen; a project is its own screen; going to the level above goes Back. */
  const phoneSettingsScreen = (r: PageRef | null): Nav["screen"] =>
    r?.page === "projects" && r.sub ? { name: "project", id: r.sub } : r ? { name: "settings", page: r.page, ...(r.sub ? { sub: r.sub } : {}), ...(r.host ? { host: r.host } : {}) } : { name: "settings" };
  const phoneSettingsGo = (r: PageRef | null) => {
    const screen = phoneSettingsScreen(r);
    if (screen.name === "project") return navGo({ screen, sheet: null });
    const up = backTarget(navRef.current);
    if (up && JSON.stringify(up.screen) === JSON.stringify(screen)) navBack();
    else navGo({ screen, sheet: null });
  };
  const phoneSettingsReplace = (r: PageRef | null) => navReplace({ screen: phoneSettingsScreen(r), sheet: null });

  // V0.164: undeclared work's components find a thread's title and go to a thread or to Settings through this, on both fronts.
  const undNav: UndNav = {
    lookup: (id) => ({ title: store.threads[id]?.title || id, project: store.threads[id]?.project }),
    now: () => Date.now() + store.clockOffset,
    openThread: (id) => {
      if (!phoneRef.current) setDialog(null); // the desktop's Settings is a dialog over the list
      directory.openThread(id);
    },
    openSettings: (at) => (phoneRef.current ? phoneSettingsGo(at) : openSettings(at)),
  };


  return { phoneActions, phoneSettingsGo, phoneSettingsReplace, undNav };
}
