// #41: the New thread form's model, one for the desktop dialog and the phone screen (SPEC 0.5 V0.23, V0.30,
// V0.42, V0.50, V0.54, V0.63). The project decides the rest: the folder follows from the project's folder, else a
// fresh scratch folder; the profile and model from the project's defaults, else what this device used last.
// V0.dialogs-review: what was typed is a draft that survives closing the form (`draftOf`, `restoreDraft`), and the
// New project step keeps its name and folder while the form is open. Pure: storage comes in as an argument.

import type { PickOptions } from "./folder-picker.tsx";
import { multiHost, pickerStart, type HostInfo, type HostProfiles } from "./hosts.ts";
import { asObject, parseJson } from "./decode.ts";
import { asHostId, type HostId } from "./hostid.ts";
import { findProject, localProject, memberOn, newThreadFolder, projectDefaults, projectRecords, recordOf, startProject } from "./projects.ts";
import type { Profile, Project } from "./types.ts";
import type { Chip } from "./attach.ts";
import { baseName } from "./paths.ts";

/** What thread.create gets. */
export interface NewThreadArgs {
  /** "" or missing: the project's main folder, else a fresh scratch folder (V0.42) */
  cwd?: string;
  text: string;
  /** uploaded attachment ids (SPEC V0.18) */
  attachments?: string[];
  profile: string;
  model: string;
  title?: string;
  /** the picked project (V0.23) */
  projectId?: string;
  /** V0.37: start with a live view */
  view?: boolean;
  /** V0.50: effort; "" is the provider's default */
  effort?: string;
  /** V0.132: the host it starts on (slice 2b: always named, by install id) */
  host?: HostId;
}

// ---- the one remembered-defaults record ----

/** What this device used for its last new thread. */
interface Remembered {
  /** "" for No project */
  projectId?: string;
  profile?: string;
  model?: string;
}

export const NEW_THREAD_KEY = "fyr.newThread";
/** Before #41 the desktop kept the project here (V0.30)... */
const OLD_PROJECT_KEY = "fyr.lastProject";
/** ...and the phone the folder, profile and model here. */
const OLD_DEFAULTS_KEY = "fyr.newDefaults";

type KeyValue = Pick<Storage, "getItem" | "setItem" | "removeItem">;

const str = (v: unknown): string | undefined => (typeof v === "string" ? v : undefined);

function parse(raw: string | null): Record<string, unknown> {
  return parseJson(raw, asObject, {});
}

function clean(r: Remembered): Remembered {
  const out: Remembered = {};
  if (r.projectId !== undefined) out.projectId = r.projectId;
  if (r.profile !== undefined) out.profile = r.profile;
  if (r.model !== undefined) out.model = r.model;
  return out;
}

/** The record; the two old keys migrate into it once and are removed. An old key wins: an older client wrote it. */
export function loadRemembered(kv: KeyValue | null): Remembered {
  if (!kv) return {};
  try {
    const j = parse(kv.getItem(NEW_THREAD_KEY));
    let r: Remembered = clean({ projectId: str(j.projectId), profile: str(j.profile), model: str(j.model) });
    const oldProject = kv.getItem(OLD_PROJECT_KEY);
    const oldDefaults = kv.getItem(OLD_DEFAULTS_KEY);
    if (oldProject === null && oldDefaults === null) return r;
    const d = parse(oldDefaults);
    r = clean({ ...r, ...(oldProject !== null ? { projectId: oldProject } : {}), ...(str(d.profile) !== undefined ? { profile: str(d.profile) } : {}), ...(str(d.model) !== undefined ? { model: str(d.model) } : {}) });
    kv.setItem(NEW_THREAD_KEY, JSON.stringify(r));
    kv.removeItem(OLD_PROJECT_KEY);
    kv.removeItem(OLD_DEFAULTS_KEY);
    return r;
  } catch {
    return {};
  }
}

export function saveRemembered(kv: KeyValue | null, r: Remembered): void {
  try {
    kv?.setItem(NEW_THREAD_KEY, JSON.stringify(clean(r)));
  } catch {
    /* storage blocked */
  }
}

// ---- the form ----

export interface FormCtx {
  projects: Project[];
  /** slice 2b: each host's profiles by install id; a thread starts with its host's */
  profiles: HostProfiles;
  /** V0.54: with several hosts a thread starts on its project's host, with that host's profiles */
  hosts?: HostInfo[];
}

export interface NewThreadForm {
  /** undefined: No project; null: not chosen yet (the projects may still be loading) */
  projectId: string | undefined | null;
  /** a project made in this form, until the host's copy arrives (V0.30) */
  made: Project | null;
  /** the project list is open */
  picking: boolean;
  /** V0.63: the New project step is open with this name */
  creating: string | null;
  /** V0.63: the project being made, as it is typed. V0.dialogs-review (12): kept when the step closes (Back, the
   *  project button, another project picked), so opening it again shows what was typed; gone once it is made. */
  draft: { name: string; folder: string } | null;
  /** V0.42: a folder picked for this thread; "" follows the project */
  picked: string;
  profile: string;
  model: string;
  /** V0.50: "" leaves the effort to the model's default */
  effortPick: string;
  /** V0.model-choice: why the provider is not the project's default (it cannot run); "" otherwise */
  note: string;
  text: string;
  title: string;
  liveView: boolean;
  /** V0.132: the host the thread starts on, by install id: set when the form settles (the filter's project's host, else
   *  the picker's start, shown in it), then by the owner. None until a host can be named. */
  host?: HostId;
  /** V0.dialogs-review (3): what the owner picked by hand here. The draft keeps these picks; the rest follows the
   *  defaults again when the form opens. */
  hand?: { host?: boolean; project?: boolean; agent?: boolean };
  /** V0.dialogs-review (12): what a change of host reset, said once under the Host field; "" or missing otherwise */
  reset?: string;
  /** V0.dialogs-review: the project and folder the owner picked by hand on a host, kept while the form is open, so a
   *  look at another host and back does not lose them (by install id) */
  byHost?: Record<string, { project?: { id: string | undefined }; picked: string }>;
}

export function initialForm(): NewThreadForm {
  return { projectId: null, made: null, picking: false, creating: null, draft: null, picked: "", profile: "", model: "", effortPick: "", note: "", text: "", title: "", liveView: false };
}

/** The profiles of one host; none for a host not known. */
function profilesOn(ctx: FormCtx, host: HostId | undefined): Profile[] {
  return (host && ctx.profiles[host]) || [];
}

/** V0.139: a host's projects (every project with one host). */
function hostProjects(ctx: FormCtx, host: HostId | undefined): Project[] {
  if (!multiHost(ctx.hosts)) return ctx.projects;
  return projectRecords(ctx.projects).filter((p) => !!host && p.hostId === host); // decision 0034: each host's own record
}

/** The projects to pick from: the thread's host's (V0.139: host first), and one made here that the host has not sent
 *  yet. */
export function formProjects(f: NewThreadForm, ctx: FormCtx): Project[] {
  const list = hostProjects(ctx, formHost(f, ctx));
  return f.made && !list.some((x) => x.id === f.made!.id) ? [...list, f.made] : list;
}

export function currentProject(f: NewThreadForm, ctx: FormCtx): Project | undefined {
  if (!f.projectId) return undefined;
  return recordOf(ctx.projects, f.projectId) ?? (f.made?.id === f.projectId ? f.made : undefined);
}

/** V0.63: the project being made while the New project step is open. */
export function makingProject(f: NewThreadForm): { name: string; folder: string } | null {
  return f.creating !== null ? (f.draft ?? { name: f.creating, folder: "" }) : null;
}

/** V0.54, V0.132, V0.139: the host the thread starts on: the one picked (host first), else its project's. Slice 2b: no
 *  first-host fallback; a form that names no host starts nothing (hosts.ts `hostOfflineNote` says so). */
export function formHost(f: NewThreadForm, ctx: FormCtx): HostId | undefined {
  if (f.host) return f.host;
  const cur = makingProject(f) ? undefined : currentProject(f, ctx);
  return cur?.hostId;
}

/** V0.132, V0.139: pick the host first: its projects follow (the remembered one when it is there, else its first; No
 *  project stays No project), a folder picked on another host is dropped, and the provider moves to one it has. */
export function chooseHost(f: NewThreadForm, host: HostId, ctx: FormCtx, last: Remembered = {}): NewThreadForm {
  const from = formHost(f, ctx);
  if (host === from) return f;
  // V0.dialogs-review: what the owner picked by hand on the host that is left waits there for the way back.
  const byHost = { ...f.byHost };
  if (from) {
    const project = f.hand?.project && f.projectId !== null ? { id: f.projectId } : undefined;
    if (project || f.picked) byHost[from] = { ...(project ? { project } : {}), picked: f.picked };
    else delete byHost[from];
  }
  const list = hostProjects(ctx, host);
  const mine = byHost[host];
  const back = mine?.project && (mine.project.id === undefined || list.some((x) => x.id === mine.project!.id)) ? mine.project : undefined;
  // V0.dialogs-review (12): the New project step stays open with its name; only its folder, a path on the other
  // host, goes. `reset` says what went.
  let next: NewThreadForm = { ...f, host, byHost, picked: mine?.picked ?? "", draft: f.draft ? { name: f.draft.name, folder: "" } : null, picking: false };
  let own = !!back;
  if (back) next = { ...next, projectId: back.id };
  else if (f.projectId !== undefined) {
    // Decision 0034: a linked project stays picked when the new host has it too.
    const same = memberOn(findProject(ctx.projects, f.projectId), host);
    const pr = same ?? list.find((x) => x.id === startProject(list, null, last.projectId));
    next = { ...next, projectId: pr?.id };
    own = !!same && !!f.hand?.project;
  } else own = !!f.hand?.project; // No project stays No project
  // A project the change of host assigned is not the owner's pick: the way back must not blame them for it.
  next = { ...next, hand: { ...f.hand, project: own }, reset: mine ? "" : hostResetNote(f, next, ctx) };
  const there = forProject(profilesOn(ctx, host), currentProject(next, ctx));
  if (!there.length) return next;
  return { ...next, ...profileFor(currentProject(next, ctx), there, f, last) };
}

/** V0.dialogs-review (12): what the move from `f` to `next` (another host) dropped of the owner's own picks, in one
 *  line; "" when nothing of theirs went. */
export function hostResetNote(f: NewThreadForm, next: NewThreadForm, ctx: FormCtx): string {
  const gone: string[] = [];
  if (f.hand?.project && f.projectId && next.projectId !== f.projectId && f.creating === null) gone.push("the project you picked");
  if (f.picked) gone.push("the folder you picked");
  if (f.creating !== null && f.draft?.folder) gone.push("the new project's folder");
  if (!gone.length) return "";
  const from = ctx.hosts?.find((h) => h.id === formHost(f, ctx))?.name;
  return `Not on this host: ${gone.join(" and ")}${from ? ` (${gone.length > 1 ? "they are" : "it is"} on ${from})` : ""}.`;
}

/** The profiles of that host that are offered to the form's project (a provider limited to other projects is not). */
export function formProfiles(f: NewThreadForm, ctx: FormCtx): Profile[] {
  return forProject(profilesOn(ctx, formHost(f, ctx)), currentProject(f, ctx));
}

/** The providers offered to a project: those with no project limit and those that name it. No project (scratch) is
 *  offered only the unlimited. */
export function forProject(ps: Profile[], pr: Pick<Project, "id" | "rid"> | undefined): Profile[] {
  const id = pr?.rid ?? pr?.id; // the host's own id of the project
  return ps.filter((p) => !p.projects?.length || (!!id && p.projects.includes(id)));
}

/** V0.42, V0.63: where the thread starts. */
export function formFolder(f: NewThreadForm, ctx: FormCtx): { cwd?: string; label: string; scratch: boolean } {
  const m = makingProject(f);
  return newThreadFolder(f.picked, m ? localProject("", m.name, m.folder) : currentProject(f, ctx));
}

const defaultModel = (p: Profile | undefined) => p?.defaultModel ?? p?.models[0] ?? "";

type ProfilePick = { profile: string; model: string; effortPick: string; note: string };

/**
 * The profile, model and effort on `there` (the profiles of the thread's host): the project's defaults (V0.23,
 * V0.model-choice: with its effort); else the current pick when that host has it; else the device's last (V0.30); else
 * the first that can run (V0.50). A project default that cannot run says so in `note` (V0.model-choice).
 */
function profileFor(pr: Project | undefined, there: Profile[], current: { profile: string; model: string; effortPick?: string }, last: Remembered): ProfilePick {
  const runs = (id: string | undefined) => !!id && there.some((x) => x.id === id && !x.unavailable);
  const d = pr ? projectDefaults(pr, there) : {};
  if (d.profile && runs(d.profile)) return { profile: d.profile, model: d.model ?? defaultModel(there.find((x) => x.id === d.profile)), effortPick: d.effort ?? "", note: "" };
  const note = pr ? defaultNote(pr, there) : "";
  const fell = (p: { profile: string; model: string; effortPick?: string }): ProfilePick => ({ effortPick: current.effortPick ?? "", ...p, note: note && p.profile ? `${note}; it starts on ${there.find((x) => x.id === p.profile)?.name ?? p.profile}.` : "" });
  if (current.profile && there.some((x) => x.id === current.profile)) return fell({ profile: current.profile, model: current.model });
  const lp = runs(last.profile) ? there.find((x) => x.id === last.profile) : undefined;
  if (lp) return fell({ profile: lp.id, model: last.model && lp.models.includes(last.model) ? last.model : defaultModel(lp) });
  const first = there.find((x) => !x.unavailable);
  return fell({ profile: first?.id ?? "", model: defaultModel(first) });
}

/** V0.model-choice (#23): why a project's default provider is not used: it cannot run, or this host has no such
 *  provider; "" when the project has no default. */
function defaultNote(pr: Project, there: Profile[]): string {
  if (!pr.profile) return "";
  const p = there.find((x) => x.id === pr.profile);
  if (!p) return `${pr.name}'s default provider is not on this host`;
  return p.unavailable ? `${pr.name}'s default, ${p.name}, cannot run (${p.unavailable.replace(/\.$/, "")})` : "";
}

/** Pick the start project once the projects are there, and a profile once the profiles are. */
export function settleForm(f: NewThreadForm, ctx: FormCtx, filterId: string | null, last: Remembered): NewThreadForm {
  let out = f;
  // V0.139: host first. The start host is the project filter's project's, else where the host picker starts (the first
  // in the owner's order, shown in the picker; the only host when there is one); then that host's projects.
  if (!out.host) {
    const filtered = findProject(ctx.projects, filterId);
    const start = filtered?.hostId ?? pickerStart(ctx.hosts);
    if (start) out = { ...out, host: start };
  }
  if (out.projectId === null && ctx.projects.length) {
    const list = hostProjects(ctx, formHost(out, ctx));
    // Decision 0034: the filter's linked project, as this host has it.
    const here = memberOn(findProject(ctx.projects, filterId), formHost(out, ctx));
    const id = startProject(list, here?.id ?? filterId, last.projectId);
    const pr = list.find((x) => x.id === id);
    out = pr ? chooseProject(out, pr, ctx, last) : { ...out, projectId: undefined };
  }
  if (!out.profile) {
    const there = formProfiles(out, ctx);
    if (there.length) out = { ...out, ...profileFor(currentProject(out, ctx), there, out, last) };
  }
  return out;
}

/** Pick a project (null: No project): its folder follows, and its default profile and model. */
export function chooseProject(f: NewThreadForm, pr: Project | null, ctx: FormCtx, last: Remembered = {}): NewThreadForm {
  // V0.139: a project of another host (a filter, a deep link) moves the thread's host to it.
  const host = pr?.hostId ?? f.host;
  const next: NewThreadForm = { ...f, picking: false, creating: null, projectId: pr?.id, picked: "", reset: "", ...(host ? { host } : {}) };
  const there = forProject(profilesOn(ctx, host), pr ?? undefined);
  if (!there.length) return next;
  return { ...next, ...profileFor(pr ?? undefined, there, f, last) };
}

/** A new profile starts on its default model. */
export function pickProfile(f: NewThreadForm, id: string, ctx: FormCtx): NewThreadForm {
  return { ...f, profile: id, model: defaultModel(formProfiles(f, ctx).find((x) => x.id === id)), note: "" };
}

/** V0.model-choice: the owner picked a provider and model in one choice; the effort asked stays when the model takes it. */
export function pickModel(f: NewThreadForm, profile: string, model: string): NewThreadForm {
  return { ...f, profile, model, note: "" };
}

/** V0.63: a project made in the form is picked at once. V0.dialogs-review: it is the owner's pick by hand, whichever
 *  button made it (the step's own, or Start with the step open), so the draft keeps it (`draftOf`): a thread that
 *  then fails to start is not filed in the default project the next time. */
export function madeProject(f: NewThreadForm, id: string, name: string, folder: string, ctx: FormCtx): NewThreadForm {
  const on = formHost(f, ctx);
  const np: Project = { ...localProject(id, name, folder), ...(on ? { hostId: on, host: ctx.hosts?.find((h) => h.id === on)?.name } : {}) }; // V0.132: made on the form's host
  const next = chooseProject({ ...f, made: np, draft: null }, np, { ...ctx, projects: [...ctx.projects.filter((x) => x.id !== id), np] });
  return { ...next, hand: { ...next.hand, project: true } };
}

/** What thread.create gets; `pid` is the project (made just now when the New project step was open). */
export function createArgs(f: NewThreadForm, ctx: FormCtx, send: { text: string; attachments?: string[] }, pid: string | undefined, effort?: string): NewThreadArgs {
  const m = makingProject(f);
  return {
    cwd: m && !f.picked ? m.folder || undefined : formFolder(f, ctx).cwd,
    text: send.text,
    attachments: send.attachments,
    profile: f.profile,
    model: f.model,
    effort: f.effortPick && effort ? effort : undefined,
    title: f.title.trim() || undefined,
    projectId: pid,
    view: f.liveView || undefined,
    host: formHost(f, ctx),
  };
}

/** "Change" offers the project's folder first when another one is picked (V0.42, V0.55). */
export function changeOptions(pr: Project | undefined, current: string, host?: HostId): PickOptions {
  const f = pr?.folder;
  const dirs = f && f !== current ? [{ name: baseName(f) || f, path: f }] : [];
  return { title: "Folder for this thread", first: dirs.length ? { title: `${pr!.name}'s folder`, dirs } : undefined, ...(host ? { host } : {}) }; // V0.132: the thread's host
}

/** One line under the folder: how the folder and the project relate (V0.42, V0.55). V0.dialogs-review (6): while
 *  the New project step is open (`making`) the line is about the project being made, never the one picked before. */
export function folderHint(cur: Project | undefined, folder: { cwd?: string; label?: string; scratch: boolean }, making?: { name: string; folder: string } | null): string {
  if (making) {
    const name = making.name.trim();
    const who = name ? `${name} is new` : "The new project";
    return folder.scratch ? `${who}${name ? " and" : ""} has no folder: the thread starts in a fresh scratch folder.` : `${who}${name ? "" : " is not named yet"}: the thread starts in ${folder.label ?? folder.cwd}.`;
  }
  if (folder.scratch) return cur ? `${cur.name} has no folder; the thread is filed in it anyway.` : "No project: a new folder just for this thread.";
  if (!cur) return "No project.";
  return cur.folder && cur.folder === folder.cwd ? `${cur.name}'s folder.` : `Filed in ${cur.name}.`;
}

/** V0.dialogs-review (5, 23): why Start is off, in one short line for the form's foot: nothing to send (or what the
 *  attachments wait for), else what the host's note says first ("lab is offline since 19:40."); "" when the thread
 *  can start. `plan` is the send plan's reason when it is not ok; `hostNote` is hosts.ts `hostOfflineNote`. */
export function startBlock(plan: string | null, hostNote: string): string {
  if (plan) return plan === "Nothing to send" ? "Write a prompt or attach a file." : `${plan}.`;
  return /^.*?[.!?](?=\s|$)/.exec(hostNote)?.[0] ?? hostNote;
}

// ---- V0.dialogs-review (3): the draft ----

export const NEW_DRAFT_KEY = "fyr.newThreadDraft";

/** What a closed form keeps: the typed text, and the picks the owner made by hand (the rest follows the defaults
 *  again). The attached files themselves are not in it (an upload is the host's, and its chip lives in the page:
 *  new-thread-form.ts keeps the chips of a closed form while the page lives); `files` is how many there were, so a
 *  form that cannot put them back says so. */
export interface NewThreadDraft {
  text: string;
  title: string;
  liveView?: boolean;
  /** when the owner last changed it (this browser's clock, ms) */
  at?: number;
  /** how many files were attached */
  files?: number;
  /** the host the picks below are on; a draft whose host is gone keeps its text only */
  host?: HostId;
  /** the owner picked that host by hand */
  handHost?: boolean;
  /** "" is No project */
  project?: string;
  /** the folder picked for the thread */
  folder?: string;
  profile?: string;
  model?: string;
  effort?: string;
  /** the New project step: what was typed in it */
  newProject?: { name: string; folder: string };
}

/** The draft of a form, or null when the form holds nothing of the owner's (a prompt, a title, a new project's
 *  name, an attached file): picks alone are not a draft, the form opens on its defaults again. */
export function draftOf(f: NewThreadForm, ctx: FormCtx, o: { at?: number; files?: number } = {}): NewThreadDraft | null {
  const np = f.draft && (f.draft.name.trim() || f.draft.folder) ? { name: f.draft.name, folder: f.draft.folder } : undefined;
  if (!f.text.trim() && !f.title.trim() && !np?.name.trim() && !o.files) return null;
  const d: NewThreadDraft = { text: f.text, title: f.title };
  if (f.liveView) d.liveView = true;
  if (o.at) d.at = o.at;
  if (o.files) d.files = o.files;
  const host = formHost(f, ctx);
  if (host && (f.hand?.host || f.hand?.project || f.hand?.agent || f.picked || np?.folder)) d.host = host;
  if (d.host && f.hand?.host) d.handHost = true;
  if (f.hand?.project && f.projectId !== null) d.project = f.projectId ?? "";
  if (f.picked) d.folder = f.picked;
  if (f.hand?.agent && f.profile) {
    d.profile = f.profile;
    d.model = f.model;
    if (f.effortPick) d.effort = f.effortPick;
  }
  if (np) d.newProject = np;
  return d;
}

export function loadDraft(kv: KeyValue | null): NewThreadDraft | null {
  try {
    const j = parse(kv?.getItem(NEW_DRAFT_KEY) ?? null);
    const text = str(j.text);
    const title = str(j.title);
    if (text === undefined || title === undefined) return null;
    const d: NewThreadDraft = { text, title };
    if (j.liveView === true) d.liveView = true;
    if (typeof j.at === "number" && j.at > 0) d.at = j.at;
    if (typeof j.files === "number" && j.files > 0) d.files = Math.floor(j.files);
    const host = asHostId(str(j.host));
    if (host) d.host = host;
    if (host && j.handHost === true) d.handHost = true;
    for (const k of ["project", "folder", "profile", "model", "effort"] as const) {
      const v = str(j[k]);
      if (v !== undefined) d[k] = v;
    }
    const np = asObject(j.newProject);
    if (np && str(np.name) !== undefined) d.newProject = { name: str(np.name)!, folder: str(np.folder) ?? "" };
    return d;
  } catch {
    return null;
  }
}

/** Keep the draft, or remove it (null: nothing typed, or the thread was started). */
export function saveDraft(kv: KeyValue | null, d: NewThreadDraft | null): void {
  try {
    if (d) kv?.setItem(NEW_DRAFT_KEY, JSON.stringify(d));
    else kv?.removeItem(NEW_DRAFT_KEY);
  } catch {
    /* storage blocked: the draft lives as long as the form */
  }
}

/** True when the picks of `d` can be applied now: its project and provider have arrived (a phone deep link opens the
 *  form before the host's state). */
export function draftReady(d: NewThreadDraft, ctx: FormCtx): boolean {
  if (!d.host) return true; // words alone: nothing to wait for
  return Object.values(ctx.profiles).some((ps) => ps.length > 0) && (!d.project || ctx.projects.length > 0);
}

/** A settled form with the draft put back. The typed text always comes back; a pick comes back only when it still
 *  exists (its host, its project, its provider and model), else the form's default stays. `part`: "words" puts back
 *  the texts and the new project's name only (the host's state has not arrived, `draftReady`); "picks" then adds the
 *  picks and leaves the texts as they are (the owner may have typed on).
 *
 *  The form was opened somewhere (`filtered`: on the project the inbox is filtered to), and the draft does not move
 *  it silently: the form goes to the draft's host only for a project picked by hand in the draft (the line above the
 *  prompt then says so, `draftLine`), or for a host picked by hand when no filter chose the project. A provider or a
 *  folder picked on another host than the form's is simply dropped.
 *
 *  The New project step comes back closed: its name and folder wait in the project list (the search carries the
 *  name), so no restored form has a primary button that makes a project. */
export function restoreDraft(f: NewThreadForm, d: NewThreadDraft, ctx: FormCtx, last: Remembered = {}, part: "all" | "words" | "picks" = "all", filtered = false): NewThreadForm {
  const np = d.newProject;
  const step = (x: NewThreadForm, folder: boolean): NewThreadForm => (np ? { ...x, draft: { name: np.name, folder: folder ? np.folder : "" } } : x);
  let out: NewThreadForm = part === "picks" ? f : { ...f, text: d.text, title: d.title, liveView: !!d.liveView };
  if (part === "words") return step(out, true);
  const hand: NonNullable<NewThreadForm["hand"]> = { ...out.hand };
  // The picks are one host's: with that host gone (or another install's id after a rotation) only the texts return.
  let bound = !d.host || d.host === formHost(out, ctx);
  if (!bound && ctx.hosts?.some((h) => h.id === d.host) && (d.project !== undefined || (d.handHost && !filtered))) {
    out = { ...chooseHost(out, d.host!, ctx, last), reset: "" };
    bound = true;
  }
  if (bound) {
    if (d.handHost && multiHost(ctx.hosts)) hand.host = true;
    if (d.project !== undefined) {
      const pr = d.project ? formProjects(out, ctx).find((x) => x.id === d.project) : null;
      if (pr !== undefined) {
        out = chooseProject(out, pr, ctx, last);
        hand.project = true;
      }
    }
    if (d.folder) out = { ...out, picked: d.folder };
    const prof = d.profile ? formProfiles(out, ctx).find((x) => x.id === d.profile && !x.unavailable) : undefined;
    if (prof && d.model && prof.models.includes(d.model)) {
      out = { ...pickModel(out, prof.id, d.model), effortPick: d.effort ?? "" };
      hand.agent = true;
    }
  }
  // The New project step last: picking a host or a project above closes it. Arriving late ("picks"), the step is as
  // the owner has it now; a folder on a host that is gone does not stay in it.
  if (part === "picks") out = { ...out, picking: f.picking, creating: f.creating, draft: f.draft && !bound ? { name: f.draft.name, folder: "" } : f.draft };
  else out = step(out, bound);
  return { ...out, hand: { ...out.hand, ...hand } };
}

/** When a draft was last changed, in the owner's words: "today 16:40", "yesterday 16:40", "Mon 16:40" within a
 *  week, else the date ("12 Nov", with the year when it is another one). This browser's time zone. */
export function draftWhen(at: number, now: number): string {
  const d = new Date(at);
  const n = new Date(now);
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const days = Math.round((day(n) - day(d)) / 86_400_000);
  const time = `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  if (days <= 0) return `today ${time}`;
  if (days === 1) return `yesterday ${time}`;
  if (days < 7) return `${["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"][d.getDay()]} ${time}`;
  const date = `${d.getDate()} ${["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"][d.getMonth()]}`;
  return d.getFullYear() === n.getFullYear() ? date : `${date} ${d.getFullYear()}`;
}

/** The line above the prompt of a form that opened with a draft: what it is, where it will start, and what did not
 *  come back. `f` is the form now; `pending`: the draft's picks still wait for the host's state; `lost`: attached
 *  files that could not be put back. */
export function draftLine(d: NewThreadDraft, f: NewThreadForm, ctx: FormCtx, now: number, o: { pending?: boolean; lost?: number } = {}): string {
  const parts = [d.at ? `Draft from ${draftWhen(d.at, now)}` : "Draft"];
  const name = currentProject(f, ctx)?.name;
  const here = name ? `in ${name}` : "no project";
  if (d.project === undefined || o.pending) parts.push(here);
  else if ((f.projectId ?? "") === d.project && f.hand?.project) parts.push(name ? `${here}, the project picked in the draft` : "no project, as picked in the draft");
  else parts.push(here, "the draft's project is not offered here"); // a part of its own: it is not about the project the form is on
  const lost = o.lost ?? 0;
  if (lost > 0) parts.push(lost === 1 ? "1 file was not kept" : `${lost} files were not kept`);
  return parts.join(" · ");
}

/** The chips of a closing form that can be put back honestly while the page lives: the ones whose upload is on the
 *  host. One still on its way stops with the form, and a failed one has nothing to keep. */
export function keptFiles(chips: Chip[]): Chip[] {
  return chips.filter((c) => c.status === "done" && !!c.id);
}

/** The files of a draft as its form opens: `held` are the chips the closed form kept, uploaded to its host. They come
 *  back only on that same host (an upload is one host's); `lost` is how many of the draft's files did not, which the
 *  line above the prompt says. */
export function filesBack(held: { host: HostId | undefined; chips: Chip[] } | null, host: HostId | undefined, d: NewThreadDraft | null): { chips: Chip[]; lost: number } {
  const chips = d && held && held.host && held.host === host ? held.chips : [];
  return { chips, lost: Math.max(0, (d?.files ?? 0) - chips.length) };
}

/** What closing the form would throw away for good, as the question to ask first; null when closing loses nothing.
 *  Only a form of files alone asks: with words the draft comes back and says what did not. */
export function closeQuestion(text: string, chips: Chip[]): string | null {
  if (text.trim()) return null;
  const n = chips.filter((c) => c.status === "uploading").length;
  if (!n) return null;
  return n === 1 ? "A file is still uploading. It is not kept when you close now." : `${n} files are still uploading. They are not kept when you close now.`;
}

/** What the foot says when Enter is pressed on a restored draft nobody touched: the first Enter does not start it. */
export const DRAFT_ENTER_NOTE = "This is the draft, unchanged. Press Enter again to start it.";

// ---- V0.dialogs-review: the form's words, one spelling for the desktop dialog and the phone screen ----

/** The prompt's placeholder; "the agent", never a provider's name (the provider is a choice of this form). */
export function promptPlaceholder(hasFiles: boolean): string {
  return hasFiles ? "Add a note (optional)" : "What should the agent do?";
}

/** What Live view does, as text beside the box on both fronts (a tooltip does not exist on a phone). */
export const LIVE_VIEW_HELP = "The agent keeps a page of its state pinned above the conversation.";

/** The line under the thread's folder, only when it says something the Project field does not: "" when the folder is
 *  simply the picked project's own. */
export function folderNote(cur: Project | undefined, folder: { cwd?: string; label?: string; scratch: boolean }, making?: { name: string; folder: string } | null): string {
  if (!making && cur?.folder && cur.folder === folder.cwd) return "";
  return folderHint(cur, folder, making);
}

/** The Folder row's value. V0.dialogs-review: when the thread simply starts in the picked project's own folder, the
 *  row says so in words instead of printing the path a third time (the Project row above already shows it);
 *  `plain` marks a value that is words, not a path. */
export function folderValue(cur: Project | undefined, folder: { cwd?: string; label: string; scratch: boolean }): { label: string; plain: boolean } {
  if (folder.scratch) return { label: folder.label, plain: true };
  if (cur?.folder && cur.folder === folder.cwd) return { label: "The project's folder", plain: true };
  return { label: folder.label, plain: false };
}

/** The summary of the More fold, on both fronts: what is inside while nothing in it is set, else what is set, so a
 *  closed fold never hides a value. `extra` names what one front holds there besides (the phone: every provider's
 *  quota). */
export function moreSummary(f: Pick<NewThreadForm, "title" | "liveView">, extra: string[] = []): string {
  const set = [f.title.trim() ? "a title" : "", f.liveView ? "Live view on" : ""].filter(Boolean);
  return `More: ${set.length ? set.join(", ") : ["title", "live view", ...extra].join(", ")}`;
}
