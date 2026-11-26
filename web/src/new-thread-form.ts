// #41: the New thread form's state, one hook for the desktop dialog and the phone screen. The model and its rules
// are in new-thread.ts (pure); this hook adds the attachments, the folder picker, storage and the submit.
// V0.dialogs-review: it also keeps the draft (what was typed survives closing the form), says why Start is off, and
// goes one level up for Esc and Back.

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import { useAttachments } from "./attach-ui.tsx";
import { sendArgs, type Chip } from "./attach.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { hostOfflineNote } from "./hosts.ts";
import { browserStorage } from "./prefs.ts";
import type { HostId } from "./hostid.ts";
import {
  changeOptions,
  chooseHost,
  chooseProject,
  closeQuestion,
  createArgs,
  currentProject,
  DRAFT_ENTER_NOTE,
  draftLine,
  draftOf,
  draftReady,
  filesBack,
  keptFiles,
  NEW_DRAFT_KEY,
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

/** V0.dialogs-review: the uploaded files of the form that was closed last, for as long as this page lives. They are
 *  not in the stored draft: an upload is a file on the host and a chip in this page, and neither survives a reload. */
let held: { host: HostId | undefined; chips: Chip[] } | null = null;

/** How long the draft waits after a change before it is written (a pasted text of many kB costs that per write). */
const DRAFT_WRITE_MS = 300;

export function useNewThreadForm(o: NewThreadFormOptions) {
  // V0.74: quiet projects stay out of the list until a 30 s hold shows them for 30 minutes; the picked one stays.
  // V0.114: the unlock is the one every path shares.
  const off = o.clockOffset ?? 0;
  const unlock = useQuietUnlock(off);
  const showUntil = unlock.showUntil;
  const keep = useRef<string | undefined>(undefined);
  const now = Date.now() + off;
  const last = useMemo(() => loadRemembered(browserStorage()), []);
  // V0.dialogs-review (3): the draft of the form that was closed last. Its texts come back at once; its picks once
  // the projects and providers they name have arrived (`waiting`).
  const stored = useMemo(() => loadDraft(browserStorage()), []);
  const waiting = useRef(stored);
  // A quiet project the draft was written for is offered while its picks wait, as the picked one is.
  const offered = createProjects(o.projects, now, showUntil, keep.current, waiting.current?.project || undefined);
  const quietCount = o.projects.filter((x) => isQuiet(x, now)).length;
  const ctx: FormCtx = { projects: offered, profiles: o.profiles, hosts: o.hosts };
  const ctxRef = useRef(ctx);
  ctxRef.current = ctx;
  /** the line above the prompt: the form opened with a draft, or the owner just discarded what was in it */
  const [line, setLine] = useState<"draft" | "discarded" | null>(stored ? "draft" : null);
  const [f, setF] = useState<NewThreadForm>(() => {
    const settled = settleForm(initialForm(), ctx, o.projectFilter, last);
    if (!stored) return settled;
    const ready = draftReady(stored, ctx);
    if (ready) waiting.current = null;
    return restoreDraft(settled, stored, ctx, last, ready ? "all" : "words", !!o.projectFilter);
  });
  // The projects and profiles may arrive after the form opens (phone deep link, reconnect).
  const profileCount = Object.values(o.profiles).reduce((n, ps) => n + ps.length, 0);
  /* eslint-disable react-hooks/exhaustive-deps -- settles the form when the offered choices change in number; the project filter and the last choice are read at that moment */
  useEffect(() => {
    const d = waiting.current && draftReady(waiting.current, ctxRef.current) ? waiting.current : null;
    if (d) waiting.current = null;
    setF((x) => {
      const settled = settleForm(x, ctxRef.current, o.projectFilter, last);
      return d ? restoreDraft(settled, d, ctxRef.current, last, "picks", !!o.projectFilter) : settled;
    });
  }, [offered.length, profileCount, o.hosts?.length]);
  /* eslint-enable react-hooks/exhaustive-deps */
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const picker = useFolderPick();
  /** the text the project list's search starts with: the new project's name after Back from its step, or from a draft */
  const [listQuery, setListQuery] = useState("");
  const carry = useRef(!!stored?.newProject?.name.trim());

  // V0.dialogs-review (3): the draft follows the form, so every way of closing it (Esc, the dimmed area, ×, Back, a
  // reload) keeps what was typed; a started thread removes it. While a stored draft's picks still wait for the
  // host's state, the words follow the form and those picks stay in the draft as they were stored.
  const sent = useRef(false);
  /** another tab started or discarded this draft: nothing is written until the owner changes the form here */
  const stale = useRef(false);
  /** when the owner last changed the form (the draft's time) */
  const edited = useRef(stored?.at ?? 0);
  const formRef = useRef(f);
  formRef.current = f;
  const chipsRef = useRef<Chip[]>([]);
  const hostRef = useRef<HostId | undefined>(undefined);
  const keepDraft = () => {
    if (sent.current || stale.current) return;
    const d = draftOf(formRef.current, ctxRef.current, { at: edited.current || Date.now(), files: chipsRef.current.filter((c) => c.status !== "error").length });
    const w = waiting.current;
    saveDraft(browserStorage(), w && d ? { ...w, text: d.text, title: d.title, liveView: d.liveView, newProject: d.newProject, at: d.at, files: d.files } : d);
  };
  // The first render's files: the chips the closed form kept, when this form starts on the host they were uploaded to.
  const back = useMemo(() => {
    const h = held;
    held = null;
    const r = filesBack(h, formHost(f, ctx), stored);
    for (const c of h?.chips ?? []) if (c.preview && !r.chips.includes(c)) URL.revokeObjectURL(c.preview);
    return r;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once, as the form opens
  }, []);
  const here = formHost(f, ctx);
  hostRef.current = here;
  // V0.54: the files upload to the host the thread starts on.
  const rawAtt = useAttachments(here, {
    initial: back.chips,
    onClose: (chips) => {
      if (sent.current || stale.current) return [];
      const k = keptFiles(chips);
      held = k.length ? { host: hostRef.current, chips: k } : null;
      return k;
    },
  });
  chipsRef.current = rawAtt.chips;
  const fileCount = rawAtt.chips.length;
  // V0.draft-close-sync: layout effects, because their cleanup runs as the form leaves the page. Preact 11 runs a
  // useEffect's cleanup on unmount only after the next paint, so a form opened again within that frame (Esc, then c)
  // read the storage before the closed one had written its draft, and opened empty; the closed form's late timer could
  // then write over the new one.
  /* eslint-disable react-hooks/exhaustive-deps -- keepDraft reads refs only: it runs a moment after the form or its files change (not on every key, a long prompt is written whole each time), and at once as the form closes or the page goes */
  useLayoutEffect(() => {
    const t = setTimeout(keepDraft, DRAFT_WRITE_MS);
    return () => clearTimeout(t);
  }, [f, fileCount]);
  useLayoutEffect(() => {
    // Another tab started the thread or discarded the draft: this form must not put it back as it closes.
    const gone = (e: StorageEvent) => {
      if (e.key === NEW_DRAFT_KEY && e.newValue === null) stale.current = true;
    };
    window.addEventListener("storage", gone);
    window.addEventListener("pagehide", keepDraft);
    return () => {
      window.removeEventListener("storage", gone);
      window.removeEventListener("pagehide", keepDraft);
      keepDraft();
    };
  }, []);
  /* eslint-enable react-hooks/exhaustive-deps */
  // A form of files alone cannot come back after a reload (the chips are this page's): the browser asks first.
  const filesOnly = fileCount > 0 && !f.text.trim();
  useEffect(() => {
    if (!filesOnly) return;
    const ask = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", ask);
    return () => window.removeEventListener("beforeunload", ask);
  }, [filesOnly]);

  keep.current = f.projectId ?? undefined;
  const cur = currentProject(f, ctx);
  const making = makingProject(f);
  const profiles = formProfiles(f, ctx);
  const folder = formFolder(f, ctx);
  const prof = profiles.find((x) => x.id === f.profile);
  const effort = startEffort(prof, f.model, f.effortPick);
  const plan = sendArgs(f.text, rawAtt.chips);
  /** the last Discard, for its Undo: gone with the next change */
  const undo = useRef<{ form: NewThreadForm; chips: Chip[] } | null>(null);
  /** Enter was pressed once on a draft nobody touched */
  const [enterNote, setEnterNote] = useState(false);
  /** The owner changed the form: the draft is theirs of now, and the line about it goes. */
  const touch = () => {
    edited.current = Date.now();
    stale.current = false;
    undo.current = null;
    setLine(null);
    setEnterNote(false);
  };
  /** what is shown (the project list, the New project step): not a change of the draft */
  const show = (patch: Partial<NewThreadForm>) => {
    setErr(null);
    formRef.current = { ...formRef.current, ...patch }; // at once, for a draft kept as the form closes in the same turn
    setF((x) => ({ ...x, ...patch }));
  };
  const set = (patch: Partial<NewThreadForm>) => {
    touch();
    show(patch); // an edit answers the last error
  };
  const byHand = (x: NewThreadForm, what: keyof NonNullable<NewThreadForm["hand"]>): NewThreadForm => ({ ...x, hand: { ...x.hand, [what]: true } });
  /** A pick by hand wins over the same pick of a draft that still waits for the host's state. */
  const dropWaiting = (...keys: ("folder" | "profile" | "model" | "effort")[]) => {
    if (!waiting.current) return;
    const w = { ...waiting.current };
    for (const k of keys) delete w[k];
    waiting.current = w;
  };
  const att = {
    ...rawAtt,
    add: (list: Parameters<typeof rawAtt.add>[0]) => (touch(), rawAtt.add(list)),
    remove: (key: string) => (touch(), rawAtt.remove(key)),
    onPaste: (e: ClipboardEvent) => {
      if (e.clipboardData?.files.length) touch();
      rawAtt.onPaste(e);
    },
    onDrop: (e: DragEvent) => {
      if (e.dataTransfer?.files.length) touch();
      rawAtt.onDrop(e);
    },
  };
  // Decision 0034: the filter's project, when the host picked does not have it yet.
  const hereId = here;
  const wanted = findProject(ctx.projects, o.projectFilter);
  const missing = o.onAddProject && wanted && hereId && (o.hosts?.length ?? 0) > 1 && !making && !memberOn(wanted, hereId) ? wanted : null;
  // V0.dialogs-review (5, 23): why Start is off, one rule for both fronts: nothing to send, or the host cannot take it.
  const block = startBlock(plan.ok ? null : plan.reason, hostOfflineNote(here, o.hosts, now));

  /** `by`: "enter" for the key. A restored draft nobody touched does not start on the first Enter (the owner may
   *  not have seen what is in the form); a press of the button is a look at it. */
  const submit = async (by?: "enter"): Promise<ErrorField | undefined> => {
    if (busyRef.current || busy) return;
    if (!plan.ok) return (setErr(block), "prompt");
    // V0.132: nothing is made on an offline host, and nothing falls back to another.
    const on = formHost(f, ctx);
    if (block || !on) return void setErr(block || "There is no host to start the thread on.");
    if (by === "enter" && line === "draft" && !enterNote) return void setEnterNote(true);
    busyRef.current = true;
    setBusy(true);
    setErr(null);
    let started = false;
    try {
      // V0.63: Start with the New project step open makes the project first, then files the thread in it.
      let pid = f.projectId ?? undefined;
      if (making) {
        const name = making.name.trim();
        if (!name) return (setErr("A project needs a name."), "name");
        const r = await o.onNewProject(name, making.folder, on);
        if (r.error || !r.id) return void setErr(r.error ?? "The project was not made.");
        const id = r.id;
        formRef.current = madeProject(formRef.current, id, name, making.folder, ctxRef.current);
        setF(formRef.current);
        pid = id;
      }
      // The draft stops following the form before the thread starts: the form closes while the reply is on its way,
      // and must not keep what was just sent.
      sent.current = true;
      const e = await o.onCreate(createArgs(f, ctx, plan.args, pid, effort));
      if (e) return void setErr(e);
      started = true;
      saveDraft(browserStorage(), null);
      saveRemembered(browserStorage(), { projectId: pid ?? "", profile: f.profile, model: f.model });
    } finally {
      // Not started (an error, or the call threw): the draft follows the form again, and is written as the form is
      // now. A project made on the way is in it as the picked one, so a reload does not make it a second time.
      if (!started) {
        sent.current = false;
        keepDraft();
      }
      busyRef.current = false;
      setBusy(false);
    }
  };
  const hasDraft = !!draftOf(f, ctx, { files: fileCount });
  const discard = () => {
    undo.current = { form: formRef.current, chips: keptFiles(rawAtt.chips) };
    waiting.current = null;
    stale.current = false;
    rawAtt.put([]);
    setErr(null);
    setEnterNote(false);
    setListQuery("");
    formRef.current = settleForm(initialForm(), ctxRef.current, o.projectFilter, last);
    setF(formRef.current);
    setLine("discarded");
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
    /** V0.dialogs-review (3): the line above the prompt. `draft`: the form opened with a draft, and the line says what
     *  it is (new-thread.ts `draftLine`) until the owner changes the form or hides the line. `discarded`: the owner
     *  emptied the form, and can take that back until the next change. */
    draftLine:
      line === "draft" && stored
        ? { kind: "draft" as const, text: draftLine(stored, f, ctx, Date.now(), { pending: !!waiting.current, lost: back.lost }) }
        : line === "discarded"
          ? { kind: "discarded" as const, text: "Discarded" }
          : null,
    hideLine: () => (setLine(null), setEnterNote(false)),
    /** the form holds something of the owner's: Discard is offered */
    hasDraft,
    /** Empty the form and forget the draft; `undoDiscard` puts both back, files included. */
    discard,
    undoDiscard: () => {
      const u = undo.current;
      if (!u) return;
      undo.current = null;
      rawAtt.put(u.chips);
      formRef.current = u.form;
      setF(u.form);
      setLine(null);
    },
    /** Enter on an untouched draft did not start it: the foot says to press it again */
    enterNote: enterNote ? DRAFT_ENTER_NOTE : "",
    /** what closing now would lose for good (a file still uploading in a form without words), as a question; else null */
    closeQuestion: closeQuestion(f.text, rawAtt.chips),
    choose: (pr: Project | null) => {
      waiting.current = null;
      touch();
      setF((x) => byHand(chooseProject(x, pr, ctxRef.current), "project"));
    },
    /** V0.132, V0.139: the host the thread starts on, picked first */
    host: here,
    /** V0.dialogs-review (7): its name, for the words of the form (never the install id) */
    hostName: o.hosts?.find((h) => h.id === here)?.name ?? "this host",
    chooseHost: (h: HostId) => {
      waiting.current = null;
      touch();
      setF((x) => byHand(chooseHost(x, h, ctxRef.current, last), "host"));
    },
    pickProfile: (id: string) => (dropWaiting("profile", "model", "effort"), touch(), setF((x) => byHand(pickProfile(x, id, ctxRef.current), "agent"))),
    /** V0.model-choice: provider and model, picked as one */
    pickModel: (profile: string, model: string) => (dropWaiting("profile", "model", "effort"), touch(), setF((x) => byHand(pickModel(x, profile, model), "agent"))),
    pickEffort: (effortPick: string) => (dropWaiting("profile", "model", "effort"), touch(), setF((x) => byHand({ ...x, effortPick }, "agent"))),
    /** V0.model-choice (#23): why the project's default provider is not the one picked */
    note: f.note,
    // V0.dialogs-review (12): none of these drops the new project's name or folder (`draft`); only making it does.
    togglePicking: () => {
      // A restored draft's new-project name is in the search the first time the list opens (the step itself comes
      // back closed); after that the list opens on every project again.
      setListQuery(carry.current ? (f.draft?.name.trim() ?? "") : "");
      carry.current = false;
      show({ picking: !(f.picking || f.creating !== null), creating: null });
    },
    startCreating: (q: string) => {
      const name = q.trim() || f.draft?.name || "";
      touch();
      show({ picking: true, creating: name, draft: { name, folder: f.draft?.folder ?? "" } });
    },
    /** Back in the New project step: to the project list, the name back in its search */
    cancelCreating: () => (setListQuery(f.draft?.name.trim() ?? ""), show({ picking: true, creating: null })),
    listQuery,
    setDraft: (name: string, folder: string) => {
      const was = formRef.current.draft;
      if (was?.name === name && was.folder === folder) return; // the step reports what it opened with
      touch();
      setF((x) => ({ ...x, draft: { name, folder }, ...(folder ? { reset: "" } : {}) }));
    },
    /** V0.dialogs-review (3): one level up, for Esc and the phone's Back: the New project step, then the project
     *  list. False when nothing is open (the caller then closes the form; the draft stays). */
    levelUp: (): boolean => {
      if (f.creating !== null) {
        setListQuery(f.draft?.name.trim() ?? "");
        show({ picking: true, creating: null });
        return true;
      }
      if (!f.picking) return false;
      show({ picking: false });
      return true;
    },
    /** NewProjectStep's Create: make the project and pick it */
    createProject: async (name: string, folder: string): Promise<string | null> => {
      const on = formHost(f, ctxRef.current);
      if (!on) return hostOfflineNote(on, o.hosts, Date.now() + off);
      const r = await o.onNewProject(name, folder, on);
      if (r.error) return r.error;
      const id = r.id;
      touch();
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
    /** V0.42: "Change…" opens the host folder picker */
    changeFolder: async () => {
      const path = await picker.pick(changeOptions(cur, folder.cwd ?? "", formHost(f, ctx)));
      if (path) (dropWaiting("folder"), set({ picked: path, reset: "" }));
    },
    submit,
  };
}
