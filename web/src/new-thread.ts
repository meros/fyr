// #41: the New thread form's model, one for the desktop dialog and the phone screen (SPEC 0.5 V0.23, V0.30,
// V0.42, V0.50, V0.54, V0.63). Project first; the folder follows from the project's folder, else a fresh scratch
// folder; the profile and model from the project's defaults, else what this device used last. Pure: storage
// comes in as an argument.

import type { PickOptions } from "./folder-picker.tsx";
import { hostForProject, multiHost, profilesOn, type HostInfo } from "./hosts.ts";
import { findProject, localProject, memberOn, newThreadFolder, projectDefaults, projectRecords, recordOf, startProject } from "./projects.ts";
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
  /** V0.132: the host it starts on, with several hosts */
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
  /** V0.model-choice: why the provider is not the project's default (it cannot run); "" otherwise */
  note: string;
  text: string;
  title: string;
  liveView: boolean;
  /** V0.132: the host the owner picked, for a thread with no project or a project made here; "" the front's first */
  host: string;
}

export function initialForm(): NewThreadForm {
  return { projectId: null, made: null, picking: false, creating: null, draft: null, picked: "", profile: "", model: "", effortPick: "", note: "", text: "", title: "", liveView: false, host: "" };
}

/** The install id of the host with that name. */
function hostId(ctx: FormCtx, name: string | undefined): string | undefined {
  return ctx.hosts?.find((h) => h.name === name)?.id;
}

/** V0.139: a host's projects (every project with one host). */
export function hostProjects(ctx: FormCtx, host: string | undefined): Project[] {
  if (!multiHost(ctx.hosts)) return ctx.projects;
  return projectRecords(ctx.projects).filter((p) => hostForProject(p.host, ctx.hosts) === host); // decision 0034: each host's own record
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

/** V0.54, V0.132, V0.139: the host the thread starts on: the one the owner picked (host first), else its project's,
 *  else the first in the owner's order. */
export function formHost(f: NewThreadForm, ctx: FormCtx): string | undefined {
  if (f.host) return f.host;
  const cur = makingProject(f) ? undefined : currentProject(f, ctx);
  return hostForProject(cur?.host, ctx.hosts);
}

/** V0.132, V0.139: pick the host first: its projects follow (the remembered one when it is there, else its first; No
 *  project stays No project), a folder picked on another host is dropped, and the provider moves to one it has. */
export function chooseHost(f: NewThreadForm, host: string, ctx: FormCtx, last: Remembered = {}): NewThreadForm {
  if (host === formHost(f, ctx)) return f;
  let next: NewThreadForm = { ...f, host, picked: "", creating: null, draft: null, picking: false };
  if (f.projectId !== undefined) {
    const list = hostProjects(ctx, host);
    // Decision 0034: a linked project stays picked when the new host has it too.
    const same = memberOn(findProject(ctx.projects, f.projectId), hostId(ctx, host));
    const pr = same ?? list.find((x) => x.id === startProject(list, null, last.projectId));
    next = { ...next, projectId: pr?.id };
  }
  const there = profilesOn(host, ctx.hosts, ctx.profiles);
  if (!there.length) return next;
  return { ...next, ...profileFor(currentProject(next, ctx), there, f, last) };
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
export function defaultNote(pr: Project, there: Profile[]): string {
  if (!pr.profile) return "";
  const p = there.find((x) => x.id === pr.profile);
  if (!p) return `${pr.name}'s default provider is not on this host`;
  return p.unavailable ? `${pr.name}'s default, ${p.name}, cannot run (${p.unavailable.replace(/\.$/, "")})` : "";
}

/** Pick the start project once the projects are there, and a profile once the profiles are. */
export function settleForm(f: NewThreadForm, ctx: FormCtx, filterId: string | null, last: Remembered): NewThreadForm {
  let out = f;
  // V0.139: host first. The start host is the project filter's project's, else the first in the owner's order (not
  // the host this browser is connected to); then that host's projects.
  if (!out.host && multiHost(ctx.hosts)) {
    const filtered = findProject(ctx.projects, filterId);
    out = { ...out, host: hostForProject(filtered?.host, ctx.hosts) ?? "" };
  }
  if (out.projectId === null && ctx.projects.length) {
    const list = hostProjects(ctx, formHost(out, ctx));
    // Decision 0034: the filter's linked project, as this host has it.
    const here = memberOn(findProject(ctx.projects, filterId), hostId(ctx, formHost(out, ctx)));
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
  const host = pr && multiHost(ctx.hosts) ? (hostForProject(pr.host, ctx.hosts) ?? f.host) : f.host;
  const next: NewThreadForm = { ...f, picking: false, creating: null, draft: null, projectId: pr?.id, picked: "", host };
  const there = profilesOn(hostForProject(pr?.host, ctx.hosts), ctx.hosts, ctx.profiles);
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
  const np = { ...localProject(id, name, folder), host: formHost(f, ctx) }; // V0.132: made on the form's host
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
  return { title: "Folder for this thread", first: dirs.length ? { title: `${pr!.name}'s folder`, dirs } : undefined, ...(host ? { host } : {}) }; // V0.132: the thread's host
}

/** One line under the folder: how the folder and the project relate (V0.42, V0.55). */
export function folderHint(cur: Project | undefined, folder: { cwd?: string; scratch: boolean }): string {
  if (folder.scratch) return cur ? `${cur.name} has no folder; the thread is filed in it anyway.` : "No project: a new folder just for this thread.";
  if (!cur) return "No project.";
  return cur.folder && cur.folder === folder.cwd ? `${cur.name}'s folder.` : `Filed in ${cur.name}.`;
}
