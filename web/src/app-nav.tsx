// Phone navigation and the open thread following the screen. (split from the App of app.tsx; takes and returns plain objects).

// The app shell: state, commands and the keyboard.

import { useEffect, useRef } from "preact/hooks";
import { type PageRef } from "./settings-nav.ts";
import { type UndNav } from "./undeclared-ctx.tsx";
import { type PhoneActions } from "./phone.tsx";
import { backTarget, initialEntries, navFromHistory, urlFor, type Nav } from "./nav.ts";
import { navTransition } from "./native.ts";
import { settleToast } from "./actions.ts";
import { isDeskEntry, popStep } from "./desk-history.ts";
import type { useAppState } from "./app-state.tsx";
import type { useThreadActions } from "./app-actions.tsx";
import type { useProjectActions } from "./app-projects.tsx";
import type { useNavHistory } from "./app-history.tsx";

export function usePhoneNav(c: Pick<ReturnType<typeof useAppState>, "store" | "threadsRef" | "view" | "setDialog" | "phone" | "phoneRef" | "cmds" | "nav" | "setNav" | "openSettings"> & Pick<ReturnType<typeof useThreadActions>, "deskAtRef" | "openThread" | "closePeek" | "settleWhenDone" | "settleBulk" | "markUnseen"> & Pick<ReturnType<typeof useProjectActions>, "directory" | "create"> & Pick<ReturnType<typeof useNavHistory>, "navGo" | "navRef" | "leaveOpenThread" | "navReplace" | "navBack">) {
  const { store, threadsRef, view, setDialog, phone, phoneRef, cmds, nav, setNav, openSettings, deskAtRef, openThread, closePeek, settleWhenDone, settleBulk, markUnseen, directory, create, navGo, navRef, leaveOpenThread, navReplace, navBack } = c;
  // Build the history once: a deep link (a push) gets the inbox under it, so Back never leaves the app.
  const navInit = useRef(false);
  useEffect(() => {
    if (!phone || navInit.current) return;
    navInit.current = true;
    const entries = initialEntries(location.search);
    history.replaceState(entries[0], "", urlFor(entries[0], location.search));
    for (const e of entries.slice(1)) history.pushState(e, "", urlFor(e, location.search));
    setNav(entries[entries.length - 1]);
    const onPop = (e: PopStateEvent) => {
      const n = navFromHistory(e.state, location.search);
      // V0.native-feel: after the system's own swipe back the screen has already slid; a second slide would replay it.
      const slid = (e as PopStateEvent & { hasUAVisualTransition?: boolean }).hasUAVisualTransition;
      if (!slid && JSON.stringify(n.screen) !== JSON.stringify(navRef.current.screen)) navTransition("back", () => setNav(n));
      else setNav(n);
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, [phone]);
  // V0.145: Back and Forward on the desktop (the mouse's side buttons, Alt+Left and Alt+Right, the browser's arrows)
  // open the thread of the entry, with the tab it had (ThreadPane reads it from the URL). A thread archived or deleted
  // since is skipped in the same direction. The browser handles the side buttons itself, in a tab and in the installed
  // window alike, so Fyr listens to popstate only.
  const viewRef = useRef(view);
  viewRef.current = view;
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
  // The open thread follows the screen.
  useEffect(() => {
    if (!phone) return;
    const s = nav.screen;
    if (s.name === "thread") {
      if (view?.id !== s.id || view.mode !== "open") openThread(s.id, "open");
    } else if (view) closePeek();
  }, [phone, nav.screen.name === "thread" ? nav.screen.id : nav.screen.name]);

  const phoneActions: PhoneActions = {
    settle: (ids, bulk, unreadKept) =>
      bulk
        ? void settleBulk(ids, { unreadKept }).then((done) => done.length && leaveOpenThread(done))
        : void cmds.run("thread.settle", ids, undefined, (() => {
              const label = ids.length === 1 ? settleToast(store.threads[ids[0]]) : undefined;
              return label ? { label: () => label } : undefined;
            })()).then((n) => n && leaveOpenThread(ids)),
    settleWhenDone,
    archive: (ids) => void cmds.run("thread.archive", ids).then((n) => n && leaveOpenThread(ids)),
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
