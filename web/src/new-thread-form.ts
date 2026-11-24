// #41: the New thread form's state, one hook for the desktop dialog and the phone screen. The model and its rules
// are in new-thread.ts (pure); this hook adds the attachments, the folder picker, storage and the submit.
// V0.dialogs-review: it also keeps the draft (what was typed survives closing the form), says why Start is off, and
// goes one level up for Esc and Back.

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
  draftOf,
  draftReady,
  formFolder,
  formHost,
  formProfiles,
  formProjects,
  initialForm,
  loadDraft,
  loadRemembered,
  madeProject,
  makingProject,
  pickModel,
  pickProfile,
  restoreDraft,
  saveDraft,
  saveRemembered,
  settleForm,
  startBlock,
  type FormCtx,
  type NewThreadArgs,
  type NewThreadForm,
} from "./new-thread.ts";
import { projectChip, findProject, memberOn } from "./projects.ts";
import { effortsFor, startEffort } from "./providers.ts";
import { createProjects, isQuiet } from "./quiet.ts";
import type { Project } from "./types.ts";
import { useQuietUnlock } from "./quiet-ui.tsx";

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

/** Which field an error of Start is about, so the front can put the focus there. */
export type ErrorField = "prompt" | "name";

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
  // V0.dialogs-review (3): the draft of the form that was closed last. Its texts come back at once; its picks once
  // the projects and providers they name have arrived (`waiting`).
  const stored = useMemo(() => loadDraft(browserStorage()), []);
  const waiting = useRef(stored);
  const [restored, setRestored] = useState(!!stored);
  const [f, setF] = useState<NewThreadForm>(() => {
    const settled = settleForm(initialForm(), ctx, o.projectFilter, last);
    if (!stored) return settled;
    const ready = draftReady(stored, ctx);
    if (ready) waiting.current = null;
    return restoreDraft(settled, stored, ctx, last, ready ? "all" : "words");
  });
  // The projects and profiles may arrive after the form opens (phone deep link, reconnect).
  const profileCount = Object.values(o.profiles).reduce((n, ps) => n + ps.length, 0);
  /* eslint-disable react-hooks/exhaustive-deps -- settles the form when the offered choices change in number; the project filter and the last choice are read at that moment */
  useEffect(() => {
    const d = waiting.current && draftReady(waiting.current, ctxRef.current) ? waiting.current : null;
    if (d) waiting.current = null;
    setF((x) => {
      const settled = settleForm(x, ctxRef.current, o.projectFilter, last);
      return d ? restoreDraft(settled, d, ctxRef.current, last, "picks") : settled;
    });
  }, [offered.length, profileCount, o.hosts?.length]);
  /* eslint-enable react-hooks/exhaustive-deps */
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const picker = useFolderPick();
  /** the text the project list's search starts with: the new project's name after Back from its step */
  const [listQuery, setListQuery] = useState("");

  // V0.dialogs-review (3): the draft follows the form, so every way of closing it (Esc, the dimmed area, ×, Back, a
  // reload) keeps what was typed; a started thread removes it. Not while a stored draft's picks still wait: saving
  // then would drop them.
  const sent = useRef(false);
  useEffect(() => {
    if (!sent.current && !waiting.current) saveDraft(browserStorage(), draftOf(f, ctxRef.current));
  }, [f]);

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
  const set = (patch: Partial<NewThreadForm>) => {
    setErr(null); // an edit answers the last error
    setF((x) => ({ ...x, ...patch }));
  };
  const byHand = (x: NewThreadForm, what: keyof NonNullable<NewThreadForm["hand"]>): NewThreadForm => ({ ...x, hand: { ...x.hand, [what]: true } });
  // Decision 0034: the filter's project, when the host picked does not have it yet.
  const hereId = here;
  const wanted = findProject(ctx.projects, o.projectFilter);
  const missing = o.onAddProject && wanted && hereId && (o.hosts?.length ?? 0) > 1 && !making && !memberOn(wanted, hereId) ? wanted : null;
  // V0.dialogs-review (5, 23): why Start is off, one rule for both fronts: nothing to send, or the host cannot take it.
  const block = startBlock(plan.ok ? null : plan.reason, hostOfflineNote(here, o.hosts, now));

  const submit = async (): Promise<ErrorField | undefined> => {
    if (busyRef.current || busy) return;
    if (!plan.ok) return (setErr(block), "prompt");
    // V0.132: nothing is made on an offline host, and nothing falls back to another.
    const on = formHost(f, ctx);
    if (block || !on) return void setErr(block);
    busyRef.current = true;
    setBusy(true);
    setErr(null);
    try {
      // V0.63: Start with the New project step open makes the project first, then files the thread in it.
      let pid = f.projectId ?? undefined;
      if (making) {
        const name = making.name.trim();
        if (!name) return (setErr("A project needs a name."), "name");
        const r = await o.onNewProject(name, making.folder, on);
        if (r.error || !r.id) return void setErr(r.error ?? "The project was not made.");
        const id = r.id;
        setF((x) => madeProject(x, id, name, making.folder, ctxRef.current));
        pid = id;
      }
      const e = await o.onCreate(createArgs(f, ctx, plan.args, pid, effort));
      if (e) return void setErr(e);
      sent.current = true;
      saveDraft(browserStorage(), null);
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
    /** V0.dialogs-review (5, 23): why Start is off ("" when it is not), and the button's words: the one primary button
     *  of the form says that it makes the project too while the New project step is open (11) */
    block,
    startLabel: making ? "Create project and start" : "Start",
    /** V0.dialogs-review (3): the form opened with a draft; `clear` empties the form and forgets it */
    restored,
    clear: () => {
      waiting.current = null;
      setRestored(false);
      setErr(null);
      setListQuery("");
      setF(settleForm(initialForm(), ctxRef.current, o.projectFilter, last));
    },
    choose: (pr: Project | null) => {
      waiting.current = null;
      setF((x) => byHand(chooseProject(x, pr, ctxRef.current), "project"));
    },
    /** V0.132, V0.139: the host the thread starts on, picked first */
    host: here,
    /** V0.dialogs-review (7): its name, for the words of the form (never the install id) */
    hostName: o.hosts?.find((h) => h.id === here)?.name ?? "this host",
    chooseHost: (h: HostId) => {
      waiting.current = null;
      setF((x) => byHand(chooseHost(x, h, ctxRef.current, last), "host"));
    },
    pickProfile: (id: string) => setF((x) => byHand(pickProfile(x, id, ctxRef.current), "agent")),
    /** V0.model-choice: provider and model, picked as one */
    pickModel: (profile: string, model: string) => setF((x) => byHand(pickModel(x, profile, model), "agent")),
    pickEffort: (effortPick: string) => setF((x) => byHand({ ...x, effortPick }, "agent")),
    /** V0.model-choice (#23): why the project's default provider is not the one picked */
    note: f.note,
    // V0.dialogs-review (12): none of these drops the new project's name or folder (`draft`); only making it does.
    togglePicking: () => (setListQuery(""), set({ picking: !(f.picking || f.creating !== null), creating: null })),
    startCreating: (q: string) => {
      const name = q.trim() || f.draft?.name || "";
      set({ picking: true, creating: name, draft: { name, folder: f.draft?.folder ?? "" } });
    },
    /** Back in the New project step: to the project list, the name back in its search */
    cancelCreating: () => (setListQuery(f.draft?.name.trim() ?? ""), set({ picking: true, creating: null })),
    listQuery,
    setDraft: (name: string, folder: string) => setF((x) => ({ ...x, draft: { name, folder }, ...(folder ? { reset: "" } : {}) })),
    /** V0.dialogs-review (3): one level up, for Esc and the phone's Back: the New project step, then the project
     *  list. False when nothing is open (the caller then closes the form; the draft stays). */
    levelUp: (): boolean => {
      if (f.creating !== null) {
        setListQuery(f.draft?.name.trim() ?? "");
        set({ picking: true, creating: null });
        return true;
      }
      if (!f.picking) return false;
      set({ picking: false });
      return true;
    },
    /** NewProjectStep's Create: make the project and pick it */
    createProject: async (name: string, folder: string): Promise<string | null> => {
      const on = formHost(f, ctxRef.current);
      if (!on) return hostOfflineNote(on, o.hosts, Date.now() + off);
      const r = await o.onNewProject(name, folder, on);
      if (r.error) return r.error;
      const id = r.id;
      if (id) setF((x) => byHand(madeProject(x, id, name, folder, ctxRef.current), "project"));
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
    /** V0.42: "Change…" opens the host folder picker */
    changeFolder: async () => {
      const path = await picker.pick(changeOptions(cur, folder.cwd ?? "", formHost(f, ctx)));
      if (path) set({ picked: path, reset: "" });
    },
    submit,
  };
}
