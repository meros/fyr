// Reloading the profiles, and what a host owns: the Settings pages, the This device settings and the rail's push and
// install controls.
//
// Reads:   data (api, setStore, raw/full/store, deviceHidden, byHost, now); ui (dialogs, settings, theme); device (push,
//          install, env); h.conn (push prefs); h.actions.openThread; h.projects (projectActions, allHostsCtx); h.history.navGo.
// Returns: pwaControls (the rail's bell and install), settingsFor (the Settings pages for a dialog or a phone screen).
// Desktop and phone share the Settings pages; the phone passes a header and its own go and replace (screens, not a dialog).

import {
  setProfiles,
} from "./store.ts";
import { type PageRef } from "./settings-nav.ts";
import { Settings, type SettingsDevice, type SettingsProps } from "./settings-ui.tsx";
import { disablePush, testPush, enablePush, isStandalone, promptInstall } from "./pwa.ts";
import { PwaControls } from "./pwa-ui.tsx";
import { HostsContext } from "./host-pick-ui.tsx";

import type { AppState } from "./app-state.tsx";
import type { Connection } from "./app-connection.tsx";
import type { ThreadActions } from "./app-actions.tsx";
import type { ProjectActions } from "./app-projects.tsx";
import type { NavHistory } from "./app-history.tsx";

export function useHostActions(s: Pick<AppState, "data" | "ui" | "device">, h: { conn: Connection; actions: ThreadActions; projects: ProjectActions; history: NavHistory }) {
  const { api, setStore, fullStore, deviceHidden, setHidden, store, byHost, now } = s.data;
  const { setDialog, theme, setTheme, openSettings } = s.ui;
  const { pushSt, setPushSt, canInstall, setCanInstall, env, pushReason } = s.device;
  const { pushPrefs, setPushPrefs } = h.conn;
  const { openThread } = h.actions;
  const { projectActions, allHostsCtx } = h.projects;
  const { navGo } = h.history;
  // Slice 2b: each host's own, into that host's state.
  const reloadProfiles = () => {
    for (const h of fullStore.hostList) {
      const id = h.id;
      if (id && h.online) void api.profiles(id).then((ps) => ps.length && setStore((s) => setProfiles(s, id, ps)));
    }
  };
  // V0.119: the rail's bell opens Settings at This device (the Notifications dialog is gone).
  const pwaControls = (
    <PwaControls
      push={pushSt}
      pushReason={pushReason}
      showUnsupported={env.ios && !env.standalone}
      canInstall={canInstall && !isStandalone()}
      onBell={() => openSettings({ page: "device" })}
      onInstall={() => void promptInstall().then((ok) => ok && setCanInstall(false))}
    />
  );
  /** V0.119: This device, in Settings on the desktop and the phone. */
  const settingsDevice: SettingsDevice = {
    push: pushSt,
    pushReason,
    setPush: async (on) => {
      const r = on ? await enablePush(api) : await disablePush(api);
      setPushSt(r.status);
      return r.error ?? null;
    },
    testPush: async () => {
      const r = await testPush(api);
      return r.ok ? null : (r.error ?? "The test was not sent");
    },
    pushPrefs,
    setPushPrefs,
    theme,
    setTheme,
    hidden: deviceHidden,
    setHidden,
    canInstall: canInstall && !isStandalone(),
    onInstall: () => void promptInstall().then((ok) => ok && setCanInstall(false)),
  };
  /** The Settings pages, the same on the desktop (a dialog) and the phone (screens). */
  const settingsFor = (at: PageRef | null, go: (r: PageRef | null) => void, phoneHeader?: SettingsProps["header"], replace?: (r: PageRef | null) => void) => (
    <HostsContext.Provider value={allHostsCtx}>
    <Settings
      api={api}
      front={store.front.name}
      hosts={fullStore.hostList}
      user={store.user}
      phone={!!phoneHeader}
      at={at}
      go={go}
      replace={replace}
      header={phoneHeader}
      offline={store.conn === "offline"}
      onChanged={reloadProfiles}
      clockKnown={store.loaded}
      device={settingsDevice}
      projects={fullStore.projects}
      profilesByHost={byHost}
      now={now}
      projectActions={{
        update: projectActions.update,
        setFolder: projectActions.setFolder,
        delete: projectActions.delete,
        create: async (name, folder, host) => (await projectActions.create(name, folder, [], host)).error,
        order: projectActions.order,
        link: projectActions.link,
        unlink: projectActions.unlink,
        add: async (id, host, folder) => (await projectActions.add(id, host, folder)).error,
      }}
      onOpenThread={(id) => {
        if (phoneHeader) navGo({ screen: { name: "thread", id }, sheet: null });
        else {
          setDialog(null);
          openThread(id, "open");
        }
      }}
    />
    </HostsContext.Provider>
  );

  return { pwaControls, settingsFor };
}

export type HostActions = ReturnType<typeof useHostActions>;
