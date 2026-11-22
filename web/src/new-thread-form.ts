// #41: the New thread form's state, one hook for the desktop dialog and the phone screen. The model and its rules
// are in new-thread.ts (pure); this hook adds the attachments, the folder picker, storage and the submit.

import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { hostOfflineNote } from "./hosts.ts";
import { browserStorage } from "./prefs.ts";
import type { HostId } from "./hostid.ts";
import {
  changeOptions,
  chooseHost,
  chooseProject,
  createArgs,
  currentProject,
  formFolder,
  formHost,
  formProfiles,
  formProjects,
  initialForm,
  loadRemembered,
  madeProject,
  makingProject,
  pickModel,
  pickProfile,
  saveRemembered,
  settleForm,
  type FormCtx,
  type NewThreadArgs,
  type NewThreadForm,
} from "./new-thread.ts";
import { projectChip, findProject, memberOn } from "./projects.ts";
import { effortsFor, startEffort } from "./providers.ts";
import { createProjects, isQuiet } from "./quiet.ts";
import { useQuietUnlock } from "./project-ui.tsx";
import type { Project } from "./types.ts";

export interface NewThreadFormOptions extends FormCtx {
  projectFilter: string | null;
  /** the host's clock minus this browser's (store.clockOffset), so quietUntil reads on the host's time */
  clockOffset?: number;
  /** thread.create; resolves to the host's error text, or null */
  onCreate(a: NewThreadArgs): Promise<string | null>;
  /** V0.30, V0.63: project.create; V0.132: on the form's host */
  onNewProject(name: string, folder: string, host: HostId): Promise<{ error: string | null; id?: string }>;
  /** Decision 0034: the project gets a member on that host (by install id), in that folder */
  onAddProject?(id: string, host: HostId, folder: string): Promise<{ error: string | null; id?: string }>;
}

export function useNewThreadForm(o: NewThreadFormOptions) {
  // V0.74: quiet projects stay out of the list until a 30 s hold shows them for 30 minutes; the picked one stays.
  // V0.114: the unlock is the one every path shares.
  const off = o.clockOffset ?? 0;
  const unlock = useQuietUnlock(off);
  const showUntil = unlock.showUntil;
  const keep = useRef<string | undefined>(undefined);
  const now = Date.now() + off;
  const offered = createProjects(o.projects, now, showUntil, keep.current);
  const quietCount = o.projects.filter((x) => isQuiet(x, now)).length;
  const ctx: FormCtx = { projects: offered, profiles: o.profiles, hosts: o.hosts };
  const ctxRef = useRef(ctx);
  ctxRef.current = ctx;
  const last = useMemo(() => loadRemembered(browserStorage()), []);
  const [f, setF] = useState<NewThreadForm>(() => settleForm(initialForm(), ctx, o.projectFilter, last));
  // The projects and profiles may arrive after the form opens (phone deep link, reconnect).
  const profileCount = Object.values(o.profiles).reduce((n, ps) => n + ps.length, 0);
  useEffect(() => setF((x) => settleForm(x, ctxRef.current, o.projectFilter, last)), [offered.length, profileCount, o.hosts?.length]);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const picker = useFolderPick();

  keep.current = f.projectId ?? undefined;
  const cur = currentProject(f, ctx);
  const making = makingProject(f);
  const profiles = formProfiles(f, ctx);
  // V0.54: the files upload to the host the thread starts on.
  const here = formHost(f, ctx);
  const att = useAttachments(here);
  const folder = formFolder(f, ctx);
  const prof = profiles.find((x) => x.id === f.profile);
  const effort = startEffort(prof, f.model, f.effortPick);
  const plan = sendArgs(f.text, att.chips);
  const set = (patch: Partial<NewThreadForm>) => setF((x) => ({ ...x, ...patch }));
  // Decision 0034: the filter's project, when the host picked does not have it yet.
  const hereId = here;
  const wanted = findProject(ctx.projects, o.projectFilter);
  const missing = o.onAddProject && wanted && hereId && (o.hosts?.length ?? 0) > 1 && !making && !memberOn(wanted, hereId) ? wanted : null;

  const submit = async () => {
    if (busyRef.current || busy) return;
    if (!plan.ok) return setErr(plan.reason === "Nothing to send" ? "Write a prompt or attach a file." : plan.reason);
    // V0.132: nothing is made on an offline host, and nothing falls back to another.
    const on = formHost(f, ctx);
    const down = hostOfflineNote(on, o.hosts, Date.now() + off);
    if (down || !on) return setErr(down);
    busyRef.current = true;
    setBusy(true);
    setErr(null);
    try {
      // V0.63: Start with the New project step open makes the project first, then files the thread in it.
      let pid = f.projectId ?? undefined;
      if (making) {
        const name = making.name.trim();
        if (!name) return setErr("A project needs a name.");
        const r = await o.onNewProject(name, making.folder, on);
        if (r.error || !r.id) return setErr(r.error ?? "The project was not made.");
        const id = r.id;
        setF((x) => madeProject(x, id, name, making.folder, ctxRef.current));
        pid = id;
      }
      const e = await o.onCreate(createArgs(f, ctx, plan.args, pid, effort));
      if (e) return setErr(e);
      saveRemembered(browserStorage(), { projectId: pid ?? "", profile: f.profile, model: f.model });
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  return {
    form: f,
    set,
    projects: formProjects(f, ctx),
    /** V0.74: the hold row under the project list */
    quiet: {
      count: quietCount,
      showUntil,
      show: unlock.show,
      hide: unlock.hide,
    },
    cur,
    making,
    chip: projectChip(cur, making),
    folder,
    /** V0.79: whose `/` commands the prompt lists: the newest of this provider and folder; none for a new scratch folder */
    commandSource: folder.cwd && f.profile && here ? { cwd: folder.cwd, profile: f.profile, host: here } : null,
    /** the profiles of the host the thread starts on */
    profiles,
    prof,
    efforts: effortsFor(prof, f.model),
    effort,
    att,
    plan,
    err,
    setErr,
    busy,
    choose: (pr: Project | null) => setF((x) => chooseProject(x, pr, ctxRef.current)),
    /** V0.132, V0.139: the host the thread starts on, picked first */
    host: here,
    chooseHost: (h: HostId) => setF((x) => chooseHost(x, h, ctxRef.current, last)),
    pickProfile: (id: string) => setF((x) => pickProfile(x, id, ctxRef.current)),
    /** V0.model-choice: provider and model, picked as one */
    pickModel: (profile: string, model: string) => setF((x) => pickModel(x, profile, model)),
    /** V0.model-choice (#23): why the project's default provider is not the one picked */
    note: f.note,
    togglePicking: () => set({ picking: !f.picking, creating: null, draft: null }),
    startCreating: (q: string) => set({ creating: q.trim() }),
    cancelCreating: () => set({ creating: null, draft: null }),
    setDraft: (name: string, folder: string) => set({ draft: { name, folder } }),
    /** NewProjectStep's Create: make the project and pick it */
    createProject: async (name: string, folder: string): Promise<string | null> => {
      const on = formHost(f, ctxRef.current);
      if (!on) return hostOfflineNote(on, o.hosts, Date.now() + off);
      const r = await o.onNewProject(name, folder, on);
      if (r.error) return r.error;
      const id = r.id;
      if (id) setF((x) => madeProject(x, id, name, folder, ctxRef.current));
      return null;
    },
    missing,
    /** Decision 0034: choose a folder on this host and add the project there */
    addHere: async () => {
      if (!missing || !hereId) return;
      const path = await picker.pick({ title: `Folder for ${missing.name}`, host: hereId });
      if (!path) return;
      const r = await o.onAddProject!(missing.id!, hereId, path);
      setErr(r.error);
      const id = r.id;
      if (id) setF((x) => madeProject(x, id, missing.name, path, ctxRef.current));
    },
    /** V0.42: "Change" opens the host folder picker */
    changeFolder: async () => {
      const path = await picker.pick(changeOptions(cur, folder.cwd ?? "", formHost(f, ctx)));
      if (path) set({ picked: path });
    },
    submit,
  };
}
