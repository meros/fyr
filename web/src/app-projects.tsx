// Projects (V0.23) and the shared dialogs around them: the commands on projects, the folder picker, the host confirm,
// the hosts context, new threads, the restart and the theme. A hook of the App in app.tsx; it takes and returns plain
// objects.

import { useMemo, useRef, useState } from "preact/hooks";
import {
  setProjectOrder,
  bannerDeploy,
} from "./store.ts";
import { Modal } from "./dialogs.tsx";
import type { NewThreadArgs } from "./new-thread.ts";
import { type Directory } from "./project-ui.tsx";
import { archiveBlock, archiveTargets, archiveToast, isArchived, projectThreads, type ArchiveInfo } from "./archived.ts";
import { expandOrder, findProject, memberOn, moveToast, recordOf, splitSharedPatch } from "./projects.ts";
import { FolderPicker, type FolderPick, type FsApi, type PickOptions } from "./folder-picker.tsx";
import { failed } from "./api.ts";
import { hostOfflineNote, multiHost } from "./hosts.ts";
import { asHostId, type HostId } from "./hostid.ts";
import { restartNowAsk } from "./deploy.ts";
import { BottomSheet } from "./motion-ui.tsx";
import type { useAppState } from "./app-state.tsx";
import type { useThreadActions } from "./app-actions.tsx";
import type { useNavHistory } from "./app-history.tsx";
import { type ProjectPatch } from "./project-editor.tsx";

export function useProjectActions(c: Pick<ReturnType<typeof useAppState>, "api" | "setStore" | "fullStore" | "hostOfId" | "store" | "threadsRef" | "setDialog" | "setTheme" | "phone" | "phoneRef" | "cmds" | "now" | "openSettings" | "openPicker" | "filterId" | "filterOn" | "confirmHost"> & Pick<ReturnType<typeof useThreadActions>, "openThread"> & Pick<ReturnType<typeof useNavHistory>, "navGo" | "navReplace" | "navRef">) {
  const { api, setStore, fullStore, hostOfId, store, threadsRef, setDialog, setTheme, phone, phoneRef, cmds, now, openSettings, openPicker, filterId, filterOn, confirmHost, openThread, navGo, navRef, navReplace } = c;
  // ---- projects (V0.23) ----
  const projectCommand = async (action: string, args: Record<string, unknown>, threadId?: string): Promise<string | null> => {
    // A project command goes to the project's host, a thread's to the thread's (decision 0031).
    const pid = typeof args.id === "string" ? args.id : typeof args.projectId === "string" ? args.projectId : "";
    // V0.207: from the full store, because Settings also edits what this device hides.
    const host = threadId ? hostOfId(threadId) : recordOf(fullStore.projects, pid)?.hostId;
    if (!host) return threadId ? "This thread's host is not one Fyr follows now" : "This project's host is not one Fyr follows now";
    const r = await api.command(host, action, threadId, args);
    return r.ok ? null : (r.error ?? "That did not work");
  };
  const projectActions = {
    /** Decision 0034: on a linked project the shared values go to every host through the front; the rest is this host's. */
    update: async (id: string, patch: ProjectPatch): Promise<string | null> => {
      const link = findProject(fullStore.projects, id)?.members ? recordOf(fullStore.projects, id)?.link : undefined; // V0.207: Settings edits hidden ones too
      if (!link) return projectCommand("project.update", { id, ...patch });
      const { shared, own } = splitSharedPatch(patch as Record<string, unknown>);
      if (Object.keys(shared).length) {
        const r = await api.frontCommand("front.projectShared", { link, ...shared });
        if (!r.ok) return r.error ?? "That did not work";
        const failed = (r.result as { failed?: { error?: string }[] } | undefined)?.failed;
        if (failed?.[0]?.error) return failed[0].error;
      }
      return Object.keys(own).length ? projectCommand("project.update", { id, ...own }) : null;
    },
    /** V0.55: the project's one folder, from the picker. */
    setFolder: (id: string, path: string) => projectCommand("project.update", { id, folder: path }),
    /** New project (V0.30): with threadIds, the host files one thread in the same write; more are filed after. */
    create: async (name: string, folder: string, threadIds: string[] = [], host?: HostId): Promise<{ error: string | null; id?: string }> => {
      const args: Record<string, unknown> = { name };
      // V0.132: on the host picked, else the threads' host (a project made from threads lives where they run). Slice 2b:
      // never a default host; the forms name one.
      const on = host ?? (threadIds.length ? hostOfId(threadIds[0]) : undefined);
      if (!on) return { error: "Pick the host the project lives on" };
      if (folder) args.folder = folder;
      if (threadIds.length === 1) args.threadId = threadIds[0];
      const r = await api.command(on, "project.create", undefined, args);
      if (!r.ok) return { error: r.error ?? "That did not work" };
      const id = r.result?.id;
      if (threadIds.length > 1 && id) await Promise.all(threadIds.map((t) => projectCommand("thread.setProject", { projectId: id }, t)));
      cmds.notify(threadIds.length ? `Made project ${name} and filed ${threadIds.length === 1 ? "this thread" : `${threadIds.length} threads`}` : `Made project ${name}`);
      return { error: null, id };
    },
    /** V0.58: delete a project; its threads match again on the host. */
    delete: async (id: string): Promise<string | null> => {
      const name = recordOf(fullStore.projects, id)?.name ?? "the project";
      const e = await projectCommand("project.delete", { id });
      if (!e) cmds.notify(`Deleted ${name}`);
      return e;
    },
    /** V0.project-archive: Archive (on) or Unarchive. Decision 0034: on a linked project every member, each on its own
     *  host, so the project leaves the lists everywhere; a member already there gets nothing. Archive offers Undo. */
    archive: async (id: string, on: boolean): Promise<string | null> => {
      const p = findProject(fullStore.projects, id); // V0.207: Settings acts on hidden ones too
      if (!p) return "This project is not here anymore";
      const errs: string[] = [];
      for (const m of archiveTargets(p, on)) {
        const r = m.hostId ? await api.command(m.hostId, on ? "project.archive" : "project.unarchive", undefined, { id: m.id }) : { ok: false, error: "This host reports no install id" };
        const e = r.ok ? null : (r.error ?? "That did not work");
        if (e) errs.push(p.members ? `${m.host ?? "A host"}: ${e}` : e);
      }
      if (errs.length) return errs[0];
      const text = archiveToast(p.name, on);
      if (on) cmds.offerUndo(text, () => projectActions.archive(id, false));
      else cmds.notify(text);
      return null;
    },
    /** V0.project-archive: whether it is archived, why Archive is off now (a thread works or asks), and its threads. */
    archiveInfo: (id: string): ArchiveInfo => {
      const p = findProject(fullStore.projects, id);
      return p ? { archived: isArchived(p), block: archiveBlock(p, fullStore.threads), threads: projectThreads(p, fullStore.threads) } : { archived: false, block: null, threads: [] };
    },
    /** V0.121: the owner's one project order. It shows at once on this device; a refusal puts the last order back. */
    order: async (ids: string[]): Promise<string | null> => {
      const before = store.projectOrder;
      const all = expandOrder(ids, store.projects); // decision 0034: a linked project's members move together
      setStore((s) => setProjectOrder(s, all));
      const r = await api.frontCommand("front.projectOrder", { ids: all });
      if (r.ok) return null;
      setStore((s) => (before ? setProjectOrder(s, before) : { ...s, projectOrder: null }));
      return r.error ?? "That did not work";
    },
    /** File threads into a project; null: No project. Shows the result or the error as a toast. */
    move: async (ids: string[], projectId: string | null): Promise<string | null> => {
      // Decision 0034: a thread is filed in its own host's record of a linked project.
      const pr = findProject(store.projects, projectId);
      const errs = (await Promise.all(ids.map((id) => projectCommand("thread.setProject", { projectId: memberOn(pr, store.threads[id]?.hostId)?.id ?? projectId ?? "" }, id)))).filter(Boolean);
      const where = projectId ? (pr?.name ?? "the project") : null;
      // Both shells show the result here, the error too (review 2026-11-15).
      const e = errs[0] ?? null;
      const m = moveToast(ids.length, where, e);
      cmds.notify(m.text, m.error);
      return e;
    },
    /** Decision 0034: make two projects on different hosts one. A host that could not store it is the error. */
    link: async (id: string, other: string): Promise<string | null> => {
      const r = await api.frontCommand("front.projectLink", { refs: [id, other] });
      if (!r.ok) return r.error ?? "That did not work";
      const failed = (r.result as { failed?: { error?: string }[] } | undefined)?.failed;
      return failed?.[0]?.error ?? null;
    },
    /** Decision 0034: the project gets a member on another host, in a folder there. */
    add: async (id: string, host: HostId, folder: string): Promise<{ error: string | null; id?: string }> => {
      const r = await api.frontCommand("front.projectAdd", { ref: id, host, folder });
      if (!r.ok) return { error: r.error ?? "That did not work" };
      return { error: null, id: (r.result as { id?: string } | undefined)?.id };
    },
    /** Decision 0034: this host's record leaves the link, with its values and threads. */
    unlink: async (id: string): Promise<string | null> => {
      const r = await api.frontCommand("front.projectUnlink", { ref: id });
      return r.ok ? null : (r.error ?? "That did not work");
    },
  };
  // V0.42: the host folder picker, a dialog on the desktop and a sheet on the phone, over whatever asked for it.
  const [folderPick, setFolderPick] = useState<(PickOptions & { at: number; resolve(p: string | null): void }) | null>(null);
  const folderPickRef = useRef(folderPick);
  folderPickRef.current = folderPick;
  const closeFolderPick = (path: string | null) => {
    folderPickRef.current?.resolve(path);
    setFolderPick(null);
  };
  const folderPicker: FolderPick = {
    pick: (o = {}) =>
      new Promise((resolve) => {
        folderPickRef.current?.resolve(null);
        setFolderPick({ ...o, at: Date.now(), resolve });
      }),
  };
  // V0.132: the picker reads the folders of the host it was opened for; an offline host says so, and no other host's
  // folders stand in.
  // Slice 2b: by install id (V0.207: Settings may name a hidden host); a picker opened for no host says so.
  const pickId = folderPick?.host;
  const pickHost = fullStore.hostList.find((h) => h.id === pickId)?.name ?? "";
  const noHost = () => Promise.resolve(failed("No host to read folders from"));
  const pickFs: FsApi = pickId ? { fsList: (p) => api.fsList(pickId, p), fsRecent: () => api.fsRecent(pickId), fsRepos: () => api.fsRepos(pickId), fsMkdir: (a, b) => api.fsMkdir(pickId, a, b) } : { fsList: noHost, fsRecent: noHost, fsRepos: noHost, fsMkdir: noHost };
  const pickTitle = `${folderPick?.title ?? "Pick a folder"}${pickHost && multiHost(fullStore.hostList) ? ` on ${pickHost}` : ""}`;
  const pickNote = hostOfflineNote(pickId, fullStore.hostList, now);
  const pickBody = (autoFocus: boolean) =>
    pickNote ? (
      <div class="note fnote" role="alert">
        {pickNote}
      </div>
    ) : (
      <FolderPicker key={pickId ?? ""} fs={pickFs} first={folderPick?.first} onPick={closeFolderPick} autoFocus={autoFocus} />
    );
  // V0.132: the hosts a new project or thread can live on (HostField, NewProjectStep).
  const hostsCtx = useMemo(() => ({ hosts: store.hostList, now }), [store.hostList, Math.floor(now / 60_000)]);
  const allHostsCtx = useMemo(() => ({ ...hostsCtx, hosts: fullStore.hostList }), [hostsCtx, fullStore.hostList]); // V0.207: Settings
  const folderPickView = folderPick
    ? phone ? (
        <BottomSheet onClose={() => closeFolderPick(null)} label={pickTitle} cls="fsheet" scrimCls="fscrim">
          <div class="fsh">
            <span class="m-shh">{pickTitle}</span>
            <button class="m-fchg" onClick={() => closeFolderPick(null)}>
              Cancel
            </button>
          </div>
          {pickBody(false)}
        </BottomSheet>
      ) : (
        <div class="fmodal">
          <Modal title={pickTitle} onClose={() => closeFolderPick(null)} wide>
            <div class="db">
              {pickBody(true)}
            </div>
          </Modal>
        </div>
      )
    : null;

  /** V0.119: a project's page. Desktop: Settings at projects/<id> (the separate Project dialog is gone); phone: the
   *  project screen. */
  const openProjectScreen = (id: string) => {
    if (phoneRef.current) navGo({ screen: { name: "project", id }, sheet: null });
    else openSettings({ page: "projects", sub: id });
  };
  const directory: Directory = {
    threads: store.threads,
    projects: store.projects,
    openThread: (id) => (phoneRef.current ? navGo({ screen: { name: "thread", id }, sheet: null }) : openThread(id, "open")),
    openProject: openProjectScreen,
    pickProject: (id) => (phoneRef.current ? navGo({ ...navRef.current, sheet: { kind: "project", ids: [id] } }) : openPicker([id])),
    projectFilter: filterOn ? filterId : null,
  };

  const create = async (a: NewThreadArgs): Promise<string | null> => {
    // V0.42: no cwd starts in the project's folder, or in a fresh scratch folder; the host files the project.
    const args: Record<string, unknown> = { text: a.text, profile: a.profile, model: a.model };
    if (a.cwd) args.cwd = a.cwd;
    if (a.projectId) args.projectId = a.projectId;
    if (a.attachments?.length) args.attachments = a.attachments;
    if (a.title) args.title = a.title;
    if (a.view) args.view = true;
    if (a.effort) args.effort = a.effort;
    // V0.132: the host the form names (slice 2b: it always names one, else nothing starts); else the project's.
    const on = a.host ?? (a.projectId ? recordOf(store.projects, a.projectId)?.hostId : undefined);
    if (!on) return "Pick the host the thread starts on";
    const r = await api.command(on, "thread.create", undefined, args);
    if (!r.ok) return r.error ?? "Could not start the thread";
    setDialog(null);
    if (r.threadId) {
      const id = r.threadId;
      // The upsert may arrive after the reply; open once it is there.
      const tryOpen = (n: number) => {
        if (threadsRef.current[id] || n > 20) {
          if (phoneRef.current) navReplace({ screen: { name: "thread", id }, sheet: null });
          else openThread(id);
        }
        else setTimeout(() => tryOpen(n + 1), 100);
      };
      tryOpen(0);
    }
    return null;
  };

  // V0.83: Restart now on the update banner: Fyr's confirmation first, then the deploy stops waiting and switches.
  const restartNow = async () => {
    const p = bannerDeploy(store);
    const host = asHostId(p?.host);
    if (!p || !host || !(await confirmHost.ask(restartNowAsk(p, store.threads)))) return;
    const r = await api.command(host, "host.deployNow"); // decision 0031: the host whose update it is
    if (!r.ok) cmds.notify(r.error ?? "Fyr could not restart now");
  };

  const cycleTheme = () => setTheme((t) => (t === "system" ? "light" : t === "light" ? "dark" : "system"));


  return { projectActions, folderPickRef, closeFolderPick, folderPicker, confirmHost, hostsCtx, allHostsCtx, folderPickView, openProjectScreen, directory, create, restartNow, cycleTheme };
}
