// Projects (SPEC 0.5 V0.23): palette, icons, ordering, filtering; and started-by labels (V0.22). Pure.

import { hostUrl } from "./urls.ts";
import type { NewProjectDraft, Profile, Project, ProjectIconSpec, QuietHours, SetupField, Thread } from "./types.ts";
import { cleanWindow } from "./quiet.ts";
import { LINE_ICONS } from "./lucide.ts";
import type { HostInfo } from "./hosts.ts";

/** Ten hues tuned to the Rail tokens, light and dark. Never amber: the beam marks only what needs the user. */
export const PALETTE: Record<string, { light: string; dark: string }> = {
  slate: { light: "#56616d", dark: "#9aa6b2" },
  blue: { light: "#2f6fb0", dark: "#6fa8e8" },
  sky: { light: "#1f7ea8", dark: "#5cb8e0" },
  teal: { light: "#1d7a74", dark: "#4cc1b6" },
  green: { light: "#2e7d5b", dark: "#5cc49a" },
  olive: { light: "#6b7a1f", dark: "#b3c25a" },
  violet: { light: "#7a4fc0", dark: "#b195ef" },
  indigo: { light: "#4a55b5", dark: "#8f98ee" },
  rose: { light: "#b8406a", dark: "#f08bae" },
  brown: { light: "#8a4f3a", dark: "#d39a86" },
};
export const PALETTE_NAMES = Object.keys(PALETTE);

function hash(s: string): number {
  let h = 2166136261;
  for (const ch of s) h = Math.imul(h ^ ch.codePointAt(0)!, 16777619);
  return h >>> 0;
}

/** The palette name for a project: its own when valid, else one picked from the name (never amber). */
export function colorName(color: string | undefined, name: string): string {
  if (color && color in PALETTE) return color;
  return PALETTE_NAMES[hash(name.toLowerCase()) % PALETTE_NAMES.length];
}

export function paletteColor(color: string | undefined, name: string, dark: boolean): string {
  const c = PALETTE[colorName(color, name)];
  return dark ? c.dark : c.light;
}

/** The CSS variable a project colour reads (the tokens are in app.css). */
export function colorVar(color: string | undefined, name: string): string {
  return `var(--p-${colorName(color, name)})`;
}

function graphemes(s: string): string[] {
  const Seg = (Intl as unknown as { Segmenter?: new (l?: string, o?: { granularity: string }) => { segment(s: string): Iterable<{ segment: string }> } }).Segmenter;
  if (Seg) return [...new Seg(undefined, { granularity: "grapheme" }).segment(s)].map((x) => x.segment);
  return Array.from(s);
}

/** 1 or 2 letters: the first letters of the first two words ("acme/web" → AW), else the first letter. */
export function monogram(name: string): string {
  const words = name.split(/[^\p{L}\p{N}]+/u).filter(Boolean);
  if (!words.length) return "?";
  const first = (w: string) => graphemes(w)[0].toLocaleUpperCase();
  return words.length >= 2 ? first(words[0]) + first(words[1]) : first(words[0]);
}

/** T3 and lucide names (PascalCase, kebab, snake) onto the curated set; older lucide names map to new ones. */
const ALIASES: Record<string, string> = {
  home: "house",
  "bar-chart": "chart-column",
  "bar-chart-3": "chart-column",
  "chart-bar": "chart-column",
  "folder-git": "folder-git-2",
  "git-pull-request-arrow": "git-pull-request",
  "message-circle": "message-square",
  building: "building-2",
  flask: "flask-conical",
  beaker: "flask-conical",
  "cog": "settings",
  "settings-2": "settings",
  laptop: "cpu",
  "book": "book-open",
  "notebook": "notebook-pen",
};

export function lineName(v: string): string | null {
  const k = v
    .trim()
    .replace(/([a-z0-9])([A-Z])/g, "$1-$2")
    .replace(/([A-Za-z])([0-9])/g, "$1-$2")
    .replace(/[_\s]+/g, "-")
    .toLowerCase()
    .replace(/-icon$/, "");
  const name = ALIASES[k] ?? k;
  return name in LINE_ICONS ? name : null;
}

export type ResolvedIcon = { kind: "monogram"; value: string } | { kind: "emoji"; value: string } | { kind: "line"; value: string };

/** The icon to draw: emoji (its first grapheme), a known line icon, else the monogram. */
export function resolveIcon(icon: ProjectIconSpec | undefined, name: string): ResolvedIcon {
  // The host omits an empty value, so value may be missing.
  const v = (icon?.value ?? "").trim();
  if (icon?.kind === "emoji" && v) return { kind: "emoji", value: graphemes(v)[0] };
  if (icon?.kind === "line" && v) {
    const n = lineName(v);
    if (n) return { kind: "line", value: n };
  }
  if (icon?.kind === "monogram" && v) return { kind: "monogram", value: graphemes(v).slice(0, 2).join("").toLocaleUpperCase() };
  return { kind: "monogram", value: monogram(name) };
}

/** The name to show for a thread's project; "No project" when it has none. */
export function projectName(t: Pick<Thread, "projectId" | "project">, projects: Project[]): string {
  return projectOf(t, projects)?.name || t.project || "No project";
}

/** The project of a thread: by id, else by its display name. */
export function projectOf(t: Pick<Thread, "projectId" | "project">, projects: Project[]): Project | undefined {
  return (t.projectId ? findProject(projects, t.projectId) : undefined) ?? projects.find((p) => p.name === t.project);
}

// ---- Decision 0033: linked projects ----
// Each host keeps its own record. The store's `projects` has one entry per project as the owner sees it: an ordinary
// record, or for a link with several members the member shown first, with all of them in `members`. Without links the
// list is the records themselves.

/** One entry per project, in the order of `records`: a linked project takes the place of its earliest member. It shows
 *  its first online member in the owner's host order, and counts the threads of them all. */
export function linkProjects(records: readonly Project[], hosts: readonly HostInfo[]): Project[] {
  const byLink = new Map<string, Project[]>();
  for (const r of records) if (r.link) byLink.set(r.link, [...(byLink.get(r.link) ?? []), r]);
  const place = (p: Project) => {
    const i = hosts.findIndex((h) => h.id === p.hostId);
    return i < 0 ? hosts.length : i;
  };
  const down = (p: Project) => (hosts.find((h) => h.id === p.hostId)?.online === false ? 1 : 0);
  const out: Project[] = [];
  const done = new Set<string>();
  for (const r of records) {
    const ms = r.link ? byLink.get(r.link)! : [];
    if (ms.length < 2) {
      out.push(r);
    } else if (!done.has(r.link!)) {
      done.add(r.link!);
      const members = [...ms].sort((a, b) => down(a) - down(b) || place(a) - place(b));
      const lastUsed = members.map((m) => m.lastUsed ?? "").sort().at(-1);
      out.push({ ...members[0], members, threads: members.reduce((n, m) => n + (m.threads ?? 0), 0), ...(lastUsed ? { lastUsed } : {}) });
    }
  }
  return out;
}

/** The records behind the list: every member of every project. */
export function projectRecords(projects: readonly Project[]): Project[] {
  return projects.flatMap((p) => p.members ?? [p]);
}

/** The project (one list entry) that has this record's id. */
export function findProject(projects: readonly Project[], id: string | null | undefined): Project | undefined {
  return id ? projects.find((p) => p.id === id || p.members?.some((m) => m.id === id)) : undefined;
}

/** The record with this id, a member or not. */
export function recordOf(projects: readonly Project[], id: string | null | undefined): Project | undefined {
  return id ? projectRecords(projects).find((p) => p.id === id) : undefined;
}

/** The project's record on that host. */
export function memberOn(p: Project | undefined, hostId: string | undefined): Project | undefined {
  return p && hostId ? (p.members ?? [p]).find((m) => m.hostId === hostId) : undefined;
}

/** The owner's order as stored: a linked project's members stay next to each other. */
export function expandOrder(ids: readonly string[], projects: readonly Project[]): string[] {
  return ids.flatMap((id) => findProject(projects, id)?.members?.flatMap((m) => (m.id ? [m.id] : [])) ?? [id]);
}

/** What can join this project: a project on a host it is not on, and not in another link. */
export function linkCandidates(p: Project, projects: readonly Project[]): Project[] {
  const taken = new Set((p.members ?? [p]).map((m) => m.hostId));
  return projects.filter((x) => x.id && !x.members && x.hostId && !taken.has(x.hostId) && (!x.link || x.link === p.link));
}

/** The inbox filter: only this project's threads; null shows all. */
export function filterByProject(threads: Record<string, Thread>, projectId: string | null, projects: Project[]): Record<string, Thread> {
  if (!projectId) return threads;
  const want = findProject(projects, projectId)?.id ?? projectId;
  const out: Record<string, Thread> = {};
  for (const t of Object.values(threads)) if (projectOf(t, projects)?.id === want) out[t.id] = t;
  return out;
}

/** Where a project lives, for a subtitle: its folder, else that threads are filed by hand (V0.55). */
export function projectSubtitle(p: Project): string {
  if (p.members) return `On ${p.members.length} hosts`; // decision 0033: each has its own folder
  return p.folder ? shortPath(p.folder) : "No folder: filed by hand";
}

/** V0.121 (amends V0.30's order by last use): the owner's one project order. The projects named in `order` first,
 *  in that order; then the rest by creation time, so a new project lands last; an id that no longer exists is
 *  ignored. Before the owner has set an order (null), by name, as Settings listed them before. The store keeps its
 *  projects in this order, so every list of projects (New thread, the picker, the filter sheet, the chips, Settings)
 *  shows it by keeping the order it is given. */
export function orderProjects(projects: readonly Project[], order: readonly string[] | null | undefined): Project[] {
  if (!order) return [...projects].sort((a, b) => a.name.localeCompare(b.name));
  const at = new Map(order.map((id, i) => [id, i]));
  // The order names projects by ref (decision 0031); one saved before that names the host's own id: it still places it.
  const pos = (p: Project) => (p.id ? (at.get(p.id) ?? (p.rid ? at.get(p.rid) : undefined)) : undefined);
  return [...projects].sort((a, b) => {
    const ia = pos(a);
    const ib = pos(b);
    if (ia !== undefined || ib !== undefined) return (ia ?? Infinity) - (ib ?? Infinity);
    return (a.createdAt ?? "").localeCompare(b.createdAt ?? "") || a.name.localeCompare(b.name);
  });
}

/** Search in the project list (Move to project, the picker): it filters, it does not reorder (V0.121). */
export function matchProjects(projects: Project[], query: string): Project[] {
  const q = query.trim().toLowerCase();
  if (!q) return projects;
  return projects.filter((p) => `${p.name} ${projectSubtitle(p)}`.toLowerCase().includes(q));
}

/** Started-by (V0.22): "from <parent title>", or null. */
export function startedBy(t: Pick<Thread, "parent">, threads: Record<string, Thread>): { id: string; title: string } | null {
  if (!t.parent) return null;
  const p = threads[t.parent];
  return { id: t.parent, title: p?.title || "another thread" };
}

/** A message another thread's agent sent: who sent it. */
export function messageFrom(fromId: string | undefined, threads: Record<string, Thread>, projects: Project[]): { id: string; title: string; project?: Project } | null {
  if (!fromId) return null;
  const t = threads[fromId];
  return { id: fromId, title: t?.title || "another thread", project: t ? projectOf(t, projects) : undefined };
}

/** The remembered inbox filter (per device). */
export const FILTER_KEY = "fyr.projectFilter";

/** Picking a project fills the folder with its folder here and its default profile and model (V0.23). */
export function projectDefaults(pr: Project, profiles: Profile[]): { cwd?: string; profile?: string; model?: string } {
  const prof = profiles.find((x) => x.id === pr.profile);
  return { cwd: pr.folder || undefined, profile: prof?.id, model: prof ? (pr.model && prof.models.includes(pr.model) ? pr.model : (prof.defaultModel ?? prof.models[0])) : undefined };
}

// ---- V0.30: one picker, one create step ----

/** What "New project from this thread" starts with (V0.30, V0.55): the host's prefill, else a name only. */
export function newProjectDraft(t: Pick<Thread, "newProject">): NewProjectDraft {
  return { name: t.newProject?.name ?? "", folder: t.newProject?.folder ?? "" };
}

// ---- V0.55: one folder (decision 0017) ----

/** A project with no folder: the words for it. */
export const NO_FOLDER_LINE = "No folder: you file threads here by hand";
export const FOLDER_LINE = "Threads started in this folder or below land here.";

export function shortPath(p: string): string {
  return p.replace(/^\/home\/[^/]+(?=\/|$)/, "~");
}

/** The new thread's folder (V0.42): the picked one, else the project's folder, else a fresh scratch folder. */
export function newThreadFolder(picked: string, project: Pick<Project, "folder"> | undefined): { cwd?: string; label: string; scratch: boolean } {
  const cwd = picked.trim() || project?.folder || "";
  return cwd ? { cwd, label: shortPath(cwd), scratch: false } : { label: "A fresh scratch folder", scratch: true };
}

/** The picker's first row: from a thread "New project from this thread"; typed text that matches nothing names it. */
export function newRowLabel(q: string, exact: boolean, fromThread: boolean): string {
  const t = q.trim();
  if (t && !exact) return `New project “${t}”`; // V0.61: typed text names the new project, matches or not
  return fromThread ? "New project from this thread" : "New project";
}

/** The project whose name is exactly the typed text (any case), or undefined. */
export function exactProject(projects: Project[], q: string): Project | undefined {
  const t = q.trim().toLowerCase();
  return t ? projects.find((p) => p.name.trim().toLowerCase() === t) : undefined;
}

/** What Enter in the picker's search does (V0.61): with a New project row, it picks only a project named exactly
 *  like the text, else it starts New project with the text; without one (the filter), the first match. */
export function enterAction(projects: Project[], matches: Project[], q: string, canCreate: boolean): { pick: Project } | { create: string } | null {
  if (!q.trim()) return null;
  if (!canCreate) return matches.length ? { pick: matches[0] } : null;
  const exact = exactProject(projects, q);
  return exact ? { pick: exact } : { create: q.trim() };
}

/** The project a new thread starts on: the inbox filter, else the last one used on this device, else the first in
 *  the owner's order (V0.121). */
export function startProject(projects: Project[], filterId: string | null | undefined, lastId: string | null | undefined): string | undefined {
  if (filterId && projects.some((p) => p.id === filterId)) return filterId;
  if (lastId && projects.some((p) => p.id === lastId)) return lastId;
  return projects[0]?.id;
}


/** A project the client just made, until the host's copy arrives (V0.30, V0.55). */
/** V0.63: what the new-thread project chip shows. While the New project step is open it shows the project being
 *  made, never the one picked before. */
export function projectChip(cur: Project | undefined, draft: { name: string; folder: string } | null): { name: string; sub: string; isNew: boolean } {
  if (draft) return { name: draft.name.trim() || "New project", sub: draft.folder ? `New · ${shortPath(draft.folder)}` : "New · no folder", isNew: true };
  return cur ? { name: cur.name, sub: projectSubtitle(cur), isNew: false } : { name: "No project", sub: "Not filed", isNew: false };
}

export function localProject(id: string, name: string, folder: string): Project {
  return { id, name, folder: folder || undefined, threads: 0, lastUsed: new Date().toISOString() };
}

/** The toast after filing `n` threads in `where` (null: No project), or the host's error (review 2026-11-15). */
export function moveToast(n: number, where: string | null, error: string | null): { text: string; error: boolean } {
  if (error) return { text: error, error: true };
  const text = n === 1 ? (where ? `Filed in ${where}` : "No project now") : where ? `Filed ${n} threads in ${where}` : `${n} threads have no project now`;
  return { text, error: false };
}

// ---- V0.116: image icons and project setup ----

/** The URL of a project's image icon at a size that is sharp at `px` CSS pixels, or null when it has none. The
 *  version in the URL lets the browser keep it; the front sends the request to the project's host. */
export function projectIconUrl(p: Pick<Project, "id" | "image" | "hostId"> | undefined, px: number): string | null {
  if (!p?.id || !p.image || !p.hostId) return null;
  return hostUrl(p.hostId, `/projects/${encodeURIComponent(p.id)}/icon?size=${px > 32 ? 128 : 64}&v=${encodeURIComponent(p.image)}`);
}

/** Whether the setup agent set this field (the project screen marks it "Set by agent"). */
export function setByAgent(p: Pick<Project, "by">, f: SetupField): boolean {
  return p.by?.[f] === "agent";
}

/** The setup line of the project screen: what the last run did, or why it could not start; null before any. */
export function setupLine(p: Pick<Project, "setup" | "folder">): { text: string; error: boolean } | null {
  if (!p.setup) return null;
  if (p.setup.error) return { text: `Automatic setup could not start: ${p.setup.error}`, error: true };
  return { text: "Set up automatically: an agent filled in what you had not set.", error: false };
}

/** The owner's upload is checked before it is sent: an image, at most 2 MB (the host checks it again). */
export const ICON_MAX_BYTES = 2 * 1024 * 1024;
export function iconFileError(f: { type: string; size: number }): string | null {
  if (!/^image\/(png|jpeg|gif)$/.test(f.type)) return "Choose a PNG, JPEG or GIF image.";
  if (f.size > ICON_MAX_BYTES) return "The image is over 2 MB.";
  return null;
}

// ---- V0.119: Settings → Projects and the project page ----

/** The fields of project.update the project page saves, one at a time. */
export interface ProjectFields {
  name: string;
  color: string;
  icon: { kind: string; value: string };
  profile: string;
  model: string;
  quiet: QuietHours[];
}
export type ProjectFieldPatch = { name?: string; color?: string; icon?: { kind: string; value: string }; profile?: string; model?: string; quiet?: QuietHours[] | null };

/** project.update's args for one field, and only that field, so an untouched field never becomes the owner's
 *  (V0.116 (2)). A new provider clears the model; no quiet windows turns quiet hours off. */
export function patchFor<K extends keyof ProjectFields>(field: K, value: ProjectFields[K]): ProjectFieldPatch {
  switch (field) {
    case "name":
      return { name: (value as string).trim() };
    case "profile":
      return { profile: value as string, model: "" };
    case "quiet": {
      const q = value as QuietHours[];
      return { quiet: q.length ? q.map(cleanWindow) : null };
    }
  }
  return { [field]: value } as ProjectFieldPatch;
}

/** The name field's refusal, shown on its row. */
export function nameError(name: string): string | null {
  return name.trim() ? null : "A project needs a name.";
}

/** Settings → Projects: the saved projects, in the owner's order (V0.121; the store keeps it). */
export function settingsProjects(projects: Project[]): Project[] {
  return projects.filter((p) => p.id);
}

/** The search over Settings → Projects: the name or the folder. */
export function searchProjects(projects: Project[], q: string): Project[] {
  const s = q.trim().toLowerCase();
  if (!s) return projects;
  return projects.filter((p) => p.name.toLowerCase().includes(s) || (p.folder ?? "").toLowerCase().includes(s));
}

export function projectSearchLine(n: number, total: number): string {
  return `${n ? n : "None"} of ${total} match.`;
}
