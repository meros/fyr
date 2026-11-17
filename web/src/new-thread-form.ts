// #41: the New thread form's state, one hook for the desktop dialog and the phone screen. The model and its rules
// are in new-thread.ts (pure); this hook adds the attachments, the folder picker, storage and the submit.

import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { hostOfflineNote } from "./hosts.ts";
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
  hostLock,
  initialForm,
  loadRemembered,
  madeProject,
  makingProject,
  pickProfile,
  saveRemembered,
  settleForm,
  type FormCtx,
  type KeyValue,
  type NewThreadArgs,
  type NewThreadForm,
} from "./new-thread.ts";
import { projectChip } from "./projects.ts";
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
  onNewProject(name: string, folder: string, host?: string): Promise<{ error: string | null; id?: string }>;
}

function storage(): KeyValue | null {
  try {
    return typeof localStorage !== "undefined" ? localStorage : null;
  } catch {
    return null;
  }
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
  const last = useMemo(() => loadRemembered(storage()), []);
  const [f, setF] = useState<NewThreadForm>(() => settleForm(initialForm(), ctx, o.projectFilter, last));
  // The projects and profiles may arrive after the form opens (phone deep link, reconnect).
  useEffect(() => setF((x) => settleForm(x, ctxRef.current, o.projectFilter, last)), [offered.length, o.profiles.length]);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const picker = useFolderPick();

  keep.current = f.projectId ?? undefined;
  const cur = currentProject(f, ctx);
  const making = makingProject(f);
  const profiles = formProfiles(f, ctx);
  // V0.54: the files upload to the host the thread starts on.
  const att = useAttachments(formHost(f, ctx));
  const folder = formFolder(f, ctx);
  const prof = profiles.find((x) => x.id === f.profile);
  const effort = startEffort(prof, f.model, f.effortPick);
  const plan = sendArgs(f.text, att.chips);
  const set = (patch: Partial<NewThreadForm>) => setF((x) => ({ ...x, ...patch }));

  const submit = async () => {
    if (busy) return;
    if (!plan.ok) return setErr(plan.reason === "Nothing to send" ? "Write a prompt or attach a file." : plan.reason);
    // V0.132: nothing is made on an offline host, and nothing falls back to another.
    const down = hostOfflineNote(formHost(f, ctx), o.hosts, Date.now() + off);
    if (down) return setErr(down);
    setBusy(true);
    setErr(null);
    // V0.63: Start with the New project step open makes the project first, then files the thread in it.
    let pid = f.projectId ?? undefined;
    if (making) {
      const name = making.name.trim();
      if (!name) return (setBusy(false), setErr("A project needs a name."));
      const r = await o.onNewProject(name, making.folder, formHost(f, ctx));
      if (r.error || !r.id) return (setBusy(false), setErr(r.error ?? "The project was not made."));
      const id = r.id;
      setF((x) => madeProject(x, id, name, making.folder, ctxRef.current));
      pid = id;
    }
    const e = await o.onCreate(createArgs(f, ctx, plan.args, pid, effort));
    setBusy(false);
    if (e) return setErr(e);
    saveRemembered(storage(), { projectId: pid ?? "", profile: f.profile, model: f.model });
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
    commandSource: folder.cwd && f.profile ? { cwd: folder.cwd, profile: f.profile, host: formHost(f, ctx) } : null,
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
    /** V0.132: the host the thread starts on, and the project that holds it ("" when the owner picks it) */
    host: formHost(f, ctx) ?? "",
    hostLock: hostLock(f, ctx),
    chooseHost: (h: string) => setF((x) => chooseHost(x, h, ctxRef.current, last)),
    pickProfile: (id: string) => setF((x) => pickProfile(x, id, ctxRef.current)),
    togglePicking: () => set({ picking: !f.picking, creating: null, draft: null }),
    startCreating: (q: string) => set({ creating: q.trim() }),
    cancelCreating: () => set({ creating: null, draft: null }),
    setDraft: (name: string, folder: string) => set({ draft: { name, folder } }),
    /** NewProjectStep's Create: make the project and pick it */
    createProject: async (name: string, folder: string): Promise<string | null> => {
      const r = await o.onNewProject(name, folder, formHost(f, ctxRef.current));
      if (r.error) return r.error;
      const id = r.id;
      if (id) setF((x) => madeProject(x, id, name, folder, ctxRef.current));
      return null;
    },
    /** V0.42: "Change" opens the host folder picker */
    changeFolder: async () => {
      const path = await picker.pick(changeOptions(cur, folder.cwd ?? "", formHost(f, ctx)));
      if (path) set({ picked: path });
    },
    submit,
  };
}
