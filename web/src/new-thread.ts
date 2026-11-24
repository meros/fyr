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
  if (host === formHost(f, ctx)) return f;
  // V0.dialogs-review (12): the New project step stays open with its name; only its folder, a path on the other
  // host, goes. `reset` says what went.
  let next: NewThreadForm = { ...f, host, picked: "", draft: f.draft ? { name: f.draft.name, folder: "" } : null, picking: false };
  if (f.projectId !== undefined) {
    const list = hostProjects(ctx, host);
    // Decision 0034: a linked project stays picked when the new host has it too.
    const same = memberOn(findProject(ctx.projects, f.projectId), host);
    const pr = same ?? list.find((x) => x.id === startProject(list, null, last.projectId));
    next = { ...next, projectId: pr?.id };
  }
  next = { ...next, reset: hostResetNote(f, next, ctx) };
  const there = profilesOn(ctx, host);
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

/** The profiles of that host. */
export function formProfiles(f: NewThreadForm, ctx: FormCtx): Profile[] {
  return profilesOn(ctx, formHost(f, ctx));
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
  const there = profilesOn(ctx, host);
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

/** V0.63: a project made in the form is picked at once. */
export function madeProject(f: NewThreadForm, id: string, name: string, folder: string, ctx: FormCtx): NewThreadForm {
  const on = formHost(f, ctx);
  const np: Project = { ...localProject(id, name, folder), ...(on ? { hostId: on, host: ctx.hosts?.find((h) => h.id === on)?.name } : {}) }; // V0.132: made on the form's host
  return chooseProject({ ...f, made: np, draft: null }, np, { ...ctx, projects: [...ctx.projects.filter((x) => x.id !== id), np] });
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
    const name = making.name.trim() || "The new project";
    return folder.scratch ? `${name} is new and has no folder: the thread starts in a fresh scratch folder.` : `${name} is new: the thread starts in ${folder.label ?? folder.cwd}.`;
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
 *  again). Attached files are not kept: an upload belongs to the form that made it. */
export interface NewThreadDraft {
  text: string;
  title: string;
  liveView?: boolean;
  /** the host the picks below are on; a draft whose host is gone keeps its text only */
  host?: HostId;
  /** "" is No project */
  project?: string;
  /** the folder picked for the thread */
  folder?: string;
  profile?: string;
  model?: string;
  effort?: string;
  /** the New project step: what was typed, and whether it was open */
  newProject?: { name: string; folder: string; open: boolean };
}

/** The draft of a form, or null when nothing was typed (a prompt, a title or a new project's name): picks alone are
 *  not a draft, the form opens on its defaults again. */
export function draftOf(f: NewThreadForm, ctx: FormCtx): NewThreadDraft | null {
  const np = f.draft && (f.draft.name.trim() || f.draft.folder) ? { name: f.draft.name, folder: f.draft.folder, open: f.creating !== null } : undefined;
  if (!f.text.trim() && !f.title.trim() && !np?.name.trim()) return null;
  const d: NewThreadDraft = { text: f.text, title: f.title };
  if (f.liveView) d.liveView = true;
  const host = formHost(f, ctx);
  if (host && (f.hand?.host || f.hand?.project || f.hand?.agent || f.picked || np?.folder)) d.host = host;
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
    const host = asHostId(str(j.host));
    if (host) d.host = host;
    for (const k of ["project", "folder", "profile", "model", "effort"] as const) {
      const v = str(j[k]);
      if (v !== undefined) d[k] = v;
    }
    const np = asObject(j.newProject);
    if (np && str(np.name) !== undefined) d.newProject = { name: str(np.name)!, folder: str(np.folder) ?? "", open: np.open === true };
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
 *  picks and leaves the texts as they are (the owner may have typed on). */
export function restoreDraft(f: NewThreadForm, d: NewThreadDraft, ctx: FormCtx, last: Remembered = {}, part: "all" | "words" | "picks" = "all"): NewThreadForm {
  const np = d.newProject;
  const step = (x: NewThreadForm, folder: boolean): NewThreadForm => (np ? { ...x, draft: { name: np.name, folder: folder ? np.folder : "" }, creating: np.open ? np.name : null } : x);
  let out: NewThreadForm = part === "picks" ? f : { ...f, text: d.text, title: d.title, liveView: !!d.liveView };
  if (part === "words") return step(out, true);
  const hand: NonNullable<NewThreadForm["hand"]> = { ...out.hand };
  // The picks are one host's: with that host gone (or another install's id after a rotation) only the texts return.
  let bound = !d.host || d.host === formHost(out, ctx);
  if (!bound && ctx.hosts?.some((h) => h.id === d.host)) {
    out = { ...chooseHost(out, d.host!, ctx, last), reset: "" };
    bound = true;
  }
  if (bound) {
    if (d.host && multiHost(ctx.hosts)) hand.host = true;
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
  return { ...out, hand };
}

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
