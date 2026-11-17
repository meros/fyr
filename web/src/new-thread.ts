// #41: the New thread form's model, one for the desktop dialog and the phone screen (SPEC 0.5 V0.23, V0.30,
// V0.42, V0.50, V0.54, V0.63). Project first; the folder follows from the project's folder, else a fresh scratch
// folder; the profile and model from the project's defaults, else what this device used last. Pure: storage
// comes in as an argument.

import type { PickOptions } from "./folder-picker.tsx";
import { hostForProject, multiHost, profilesOn, type HostInfo } from "./hosts.ts";
import { localProject, newThreadFolder, projectDefaults, startProject } from "./projects.ts";
import type { Profile, Project } from "./types.ts";

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
  /** V0.131: the host it starts on, with several hosts */
  host?: string;
}

// ---- the one remembered-defaults record ----

/** What this device used for its last new thread. */
export interface Remembered {
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

export type KeyValue = Pick<Storage, "getItem" | "setItem" | "removeItem">;

const str = (v: unknown): string | undefined => (typeof v === "string" ? v : undefined);

function parse(raw: string | null): Record<string, unknown> {
  try {
    const j = JSON.parse(raw ?? "");
    return j && typeof j === "object" && !Array.isArray(j) ? j : {};
  } catch {
    return {};
  }
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
  profiles: Profile[];
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
  /** V0.63: the project being made, as it is typed */
  draft: { name: string; folder: string } | null;
  /** V0.42: a folder picked for this thread; "" follows the project */
  picked: string;
  profile: string;
  model: string;
  /** V0.50: "" leaves the effort to the model's default */
  effortPick: string;
  text: string;
  title: string;
  liveView: boolean;
  /** V0.131: the host the owner picked, for a thread with no project or a project made here; "" the front's first */
  host: string;
}

export function initialForm(): NewThreadForm {
  return { projectId: null, made: null, picking: false, creating: null, draft: null, picked: "", profile: "", model: "", effortPick: "", text: "", title: "", liveView: false, host: "" };
}

/** The projects to pick from: the host's, and one made here that the host has not sent yet. */
export function formProjects(f: NewThreadForm, ctx: FormCtx): Project[] {
  return f.made && !ctx.projects.some((x) => x.id === f.made!.id) ? [...ctx.projects, f.made] : ctx.projects;
}

export function currentProject(f: NewThreadForm, ctx: FormCtx): Project | undefined {
  return f.projectId ? formProjects(f, ctx).find((x) => x.id === f.projectId) : undefined;
}

/** V0.63: the project being made while the New project step is open. */
export function makingProject(f: NewThreadForm): { name: string; folder: string } | null {
  return f.creating !== null ? (f.draft ?? { name: f.creating, folder: "" }) : null;
}

/** V0.54, V0.131: the host the thread starts on: its project's; with no project, or a project made here, the one the
 *  owner picked, else the front's first. */
export function formHost(f: NewThreadForm, ctx: FormCtx): string | undefined {
  const cur = makingProject(f) ? undefined : currentProject(f, ctx);
  if (cur) return hostForProject(cur.host, ctx.hosts);
  return f.host || hostForProject(undefined, ctx.hosts);
}

/** V0.131: the project that holds the host (the Host field is locked to it), or "" when the owner picks it. */
export function hostLock(f: NewThreadForm, ctx: FormCtx): string {
  if (makingProject(f)) return "";
  return currentProject(f, ctx)?.name ?? "";
}

/** V0.131: pick the host (no project, or a project made here): a folder picked on another host is dropped, and the
 *  provider moves to one that host has. */
export function chooseHost(f: NewThreadForm, host: string, ctx: FormCtx, last: Remembered = {}): NewThreadForm {
  if (hostLock(f, ctx)) return f;
  const next: NewThreadForm = { ...f, host, picked: host === formHost(f, ctx) ? f.picked : "" };
  const there = profilesOn(host, ctx.hosts, ctx.profiles);
  if (!there.length) return next;
  return { ...next, ...profileFor(undefined, there, f, last) };
}

/** The profiles of that host. */
export function formProfiles(f: NewThreadForm, ctx: FormCtx): Profile[] {
  return profilesOn(formHost(f, ctx), ctx.hosts, ctx.profiles);
}

/** V0.42, V0.63: where the thread starts. */
export function formFolder(f: NewThreadForm, ctx: FormCtx): { cwd?: string; label: string; scratch: boolean } {
  const m = makingProject(f);
  return newThreadFolder(f.picked, m ? localProject("", m.name, m.folder) : currentProject(f, ctx));
}

const defaultModel = (p: Profile | undefined) => p?.defaultModel ?? p?.models[0] ?? "";

/**
 * The profile and model on `there` (the profiles of the thread's host): the project's defaults (V0.23); else the
 * current pick when that host has it; else the device's last (V0.30); else the first that can run (V0.50).
 */
function profileFor(pr: Project | undefined, there: Profile[], current: { profile: string; model: string }, last: Remembered): { profile: string; model: string } {
  const runs = (id: string | undefined) => !!id && there.some((x) => x.id === id && !x.unavailable);
  const d = pr ? projectDefaults(pr, there) : {};
  if (d.profile && runs(d.profile)) return { profile: d.profile, model: d.model ?? defaultModel(there.find((x) => x.id === d.profile)) };
  if (current.profile && there.some((x) => x.id === current.profile)) return { profile: current.profile, model: current.model };
  const lp = runs(last.profile) ? there.find((x) => x.id === last.profile) : undefined;
  if (lp) return { profile: lp.id, model: last.model && lp.models.includes(last.model) ? last.model : defaultModel(lp) };
  const first = there.find((x) => !x.unavailable);
  return { profile: first?.id ?? "", model: defaultModel(first) };
}

/** Pick the start project once the projects are there, and a profile once the profiles are. */
export function settleForm(f: NewThreadForm, ctx: FormCtx, filterId: string | null, last: Remembered): NewThreadForm {
  let out = f;
  if (out.projectId === null && ctx.projects.length) {
    const id = startProject(ctx.projects, filterId, last.projectId);
    const pr = ctx.projects.find((x) => x.id === id);
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
  const next: NewThreadForm = { ...f, picking: false, creating: null, draft: null, projectId: pr?.id, picked: "" };
  const there = profilesOn(hostForProject(pr?.host, ctx.hosts), ctx.hosts, ctx.profiles);
  if (!there.length) return next;
  return { ...next, ...profileFor(pr ?? undefined, there, f, last) };
}

/** A new profile starts on its default model. */
export function pickProfile(f: NewThreadForm, id: string, ctx: FormCtx): NewThreadForm {
  return { ...f, profile: id, model: defaultModel(formProfiles(f, ctx).find((x) => x.id === id)) };
}

/** V0.63: a project made in the form is picked at once. */
export function madeProject(f: NewThreadForm, id: string, name: string, folder: string, ctx: FormCtx): NewThreadForm {
  const np = { ...localProject(id, name, folder), host: formHost(f, ctx) }; // V0.131: made on the form's host
  return chooseProject({ ...f, made: np }, np, { ...ctx, projects: [...ctx.projects.filter((x) => x.id !== id), np] });
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
    ...(multiHost(ctx.hosts) ? { host: formHost(f, ctx) } : {}),
  };
}

/** "Change" offers the project's folder first when another one is picked (V0.42, V0.55). */
export function changeOptions(pr: Project | undefined, current: string, host?: string): PickOptions {
  const f = pr?.folder;
  const dirs = f && f !== current ? [{ name: f.split("/").pop() || f, path: f }] : [];
  return { title: "Folder for this thread", first: dirs.length ? { title: `${pr!.name}'s folder`, dirs } : undefined, ...(host ? { host } : {}) }; // V0.131: the thread's host
}

/** One line under the folder: how the folder and the project relate (V0.42, V0.55). */
export function folderHint(cur: Project | undefined, folder: { cwd?: string; scratch: boolean }): string {
  if (folder.scratch) return cur ? `${cur.name} has no folder; the thread is filed in it anyway.` : "No project: a new folder just for this thread.";
  if (!cur) return "No project.";
  return cur.folder && cur.folder === folder.cwd ? `${cur.name}'s folder.` : `Filed in ${cur.name}.`;
}
