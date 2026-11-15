// #41: the New thread form's state, one hook for the desktop dialog and the phone screen. The model and its rules
// are in new-thread.ts (pure); this hook adds the attachments, the folder picker, storage and the submit.

import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { useFolderPick } from "./folder-picker.tsx";
import {
  changeOptions,
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
import { createProjects, isQuiet, loadQuietShow, saveQuietShow } from "./quiet.ts";
import type { Project } from "./types.ts";

export interface NewThreadFormOptions extends FormCtx {
  projectFilter: string | null;
  /** the host's clock minus this browser's (store.clockOffset), so quietUntil reads on the host's time */
  clockOffset?: number;
  /** thread.create; resolves to the host's error text, or null */
  onCreate(a: NewThreadArgs): Promise<string | null>;
  /** V0.30, V0.63: project.create */
  onNewProject(name: string, folder: string): Promise<{ error: string | null; id?: string }>;
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
  const [showUntil, setShowUntil] = useState(() => loadQuietShow(storage()));
  const [, setTick] = useState(0);
  const keep = useRef<string | undefined>(undefined);
  const off = o.clockOffset ?? 0;
  const now = Date.now() + off;
  const offered = createProjects(o.projects, now, showUntil, keep.current);
  const quietCount = o.projects.filter((x) => isQuiet(x, now)).length;
  useEffect(() => {
    const left = showUntil - (Date.now() + off);
    if (left <= 0) return;
    const t = setTimeout(() => setTick((n) => n + 1), Math.min(left + 250, 2 ** 31 - 1));
    return () => clearTimeout(t);
  }, [showUntil]);
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
    setBusy(true);
    setErr(null);
    // V0.63: Start with the New project step open makes the project first, then files the thread in it.
    let pid = f.projectId ?? undefined;
    if (making) {
      const name = making.name.trim();
      if (!name) return (setBusy(false), setErr("A project needs a name."));
      const r = await o.onNewProject(name, making.folder);
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
      showUntil: showUntil > now ? showUntil : 0,
      show: () => setShowUntil(saveQuietShow(storage(), Date.now() + off)),
      hide: () => setShowUntil(saveQuietShow(storage(), null)),
    },
    cur,
    making,
    chip: projectChip(cur, making),
    folder,
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
    pickProfile: (id: string) => setF((x) => pickProfile(x, id, ctxRef.current)),
    togglePicking: () => set({ picking: !f.picking, creating: null, draft: null }),
    startCreating: (q: string) => set({ creating: q.trim() }),
    cancelCreating: () => set({ creating: null, draft: null }),
    setDraft: (name: string, folder: string) => set({ draft: { name, folder } }),
    /** NewProjectStep's Create: make the project and pick it */
    createProject: async (name: string, folder: string): Promise<string | null> => {
      const r = await o.onNewProject(name, folder);
      if (r.error) return r.error;
      const id = r.id;
      if (id) setF((x) => madeProject(x, id, name, folder, ctxRef.current));
      return null;
    },
    /** V0.42: "Change" opens the host folder picker */
    changeFolder: async () => {
      const path = await picker.pick(changeOptions(cur, folder.cwd ?? ""));
      if (path) set({ picked: path });
    },
    submit,
  };
}
