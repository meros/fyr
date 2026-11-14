// Projects (SPEC 0.5 V0.23): palette, icons, ordering, filtering; and started-by labels (V0.22). Pure.

import type { Profile, Project, ProjectIconSpec, ProjectSource, ProjectSuggestion, ThreadDTO } from "./types.ts";
import { LINE_ICONS } from "./lucide.ts";

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

/** 1 or 2 letters: the first letters of the first two words ("norban/web" → NW), else the first letter. */
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
export function projectName(t: Pick<ThreadDTO, "projectId" | "project">, projects: Project[]): string {
  return projectOf(t, projects)?.name || t.project || "No project";
}

/** The project of a thread: by id, else by its display name. */
export function projectOf(t: Pick<ThreadDTO, "projectId" | "project">, projects: Project[]): Project | undefined {
  return (t.projectId ? projects.find((p) => p.id === t.projectId) : undefined) ?? projects.find((p) => p.name === t.project);
}

/** The project a thread without one could get (V0.28): its suggestion, only while it has no project. */
export function suggestionFor(t: Pick<ThreadDTO, "projectId" | "project" | "suggest">, projects: Project[]): ProjectSuggestion | null {
  if (projectOf(t, projects)) return null;
  return t.suggest?.name ? t.suggest : null;
}

/** The action's label: Make project “name”. */
export function makeProjectLabel(s: ProjectSuggestion): string {
  return `Make project “${s.name}”`;
}

/** The inbox filter: only this project's threads; null shows all. */
export function filterByProject(threads: Record<string, ThreadDTO>, projectId: string | null, projects: Project[]): Record<string, ThreadDTO> {
  if (!projectId) return threads;
  const out: Record<string, ThreadDTO> = {};
  for (const t of Object.values(threads)) if (projectOf(t, projects)?.id === projectId) out[t.id] = t;
  return out;
}

/** Where a project lives, for a subtitle: its main folder here, else its repo, else a folder elsewhere, else that it is
 *  a label. */
export function projectSubtitle(p: Project): string {
  if (p.cwd) return p.cwd.replace(/^\/home\/[^/]+(?=\/|$)/, "~");
  const repo = projectRepos(p)[0];
  if (repo) return repo;
  const folder = p.sources?.find((s) => s.kind === "folder") as { host?: string; path?: string } | undefined;
  if (folder?.path) return `${folder.host ?? "another host"}: ${folder.path}`;
  return "No folder: filed by hand";
}

/** The new-thread picker: projects by last use (newest first), then by name. */
export function pickerOrder(projects: Project[]): Project[] {
  return [...projects].sort((a, b) => (b.lastUsed ?? "").localeCompare(a.lastUsed ?? "") || a.name.localeCompare(b.name));
}

/** Search in the project list (Move to project, the picker). */
export function matchProjects(projects: Project[], query: string): Project[] {
  const q = query.trim().toLowerCase();
  const list = pickerOrder(projects);
  if (!q) return list;
  return list.filter((p) => `${p.name} ${projectSubtitle(p)}`.toLowerCase().includes(q));
}

/** Started-by (V0.22): "from <parent title>", or null. */
export function startedBy(t: Pick<ThreadDTO, "parent">, threads: Record<string, ThreadDTO>): { id: string; title: string } | null {
  if (!t.parent) return null;
  const p = threads[t.parent];
  return { id: t.parent, title: p?.title || "another thread" };
}

/** A message another thread's agent sent: who sent it. */
export function messageFrom(fromId: string | undefined, threads: Record<string, ThreadDTO>, projects: Project[]): { id: string; title: string; project?: Project } | null {
  if (!fromId) return null;
  const t = threads[fromId];
  return { id: fromId, title: t?.title || "another thread", project: t ? projectOf(t, projects) : undefined };
}

/** The remembered inbox filter (per device). */
export const FILTER_KEY = "fyr.projectFilter";

/** Picking a project fills the folder with its folder here and its default profile and model (V0.23). */
export function projectDefaults(pr: Project, profiles: Profile[]): { cwd?: string; profile?: string; model?: string } {
  const prof = profiles.find((x) => x.id === pr.profile);
  return { cwd: pr.cwd, profile: prof?.id, model: prof ? (pr.model && prof.models.includes(pr.model) ? pr.model : (prof.defaultModel ?? prof.models[0])) : undefined };
}

// ---- V0.30: one picker, one create step ----

/** What "New project from this thread" starts with: the host's prefill, else the suggestion (an older host), else a name only. A copy. */
export function newProjectDraft(t: Pick<ThreadDTO, "newProject" | "suggest">): { name: string; sources: ProjectSource[] } {
  const d = t.newProject ?? t.suggest;
  return { name: d?.name ?? "", sources: [...(d?.sources ?? [])] };
}

// ---- V0.42: folders, the main folder, the automatic repo line ----

/** A project with no folder: the words for it. */
export const NO_FOLDER_LINE = "No folder: you file threads here by hand";
export const FOLDERS_LINE = "Threads started in these folders land here.";
export const MAIN_LINE = "Main folder: new threads start here.";

/** The project's folders with their index into sources, and which one is main on this host (the marked one, else the
 *  first here). */
export function projectFolders(sources: ProjectSource[], host: string): { index: number; path: string; host: string; main: boolean; here: boolean }[] {
  const out = sources.flatMap((s, index) => (s.kind === "folder" ? [{ index, path: String((s as { path?: string }).path ?? ""), host: String((s as { host?: string }).host ?? ""), main: false, here: false, marked: !!(s as { main?: boolean }).main }] : []));
  for (const f of out) f.here = !f.host || !host || f.host === host;
  const here = out.filter((f) => f.here);
  const main = here.find((f) => f.marked) ?? here[0];
  return out.map(({ marked: _m, ...f }) => ({ ...f, main: f.index === main?.index }));
}

/** The repos a project also matches: the host's list, else (an older host) its repo sources. */
export function projectRepos(p: Pick<Project, "repos" | "sources">): string[] {
  if (p.repos?.length) return p.repos;
  return (p.sources ?? []).flatMap((s) => (s.kind === "repo" && (s as { url?: string }).url ? [String((s as { url?: string }).url)] : []));
}

/** "Also matches clones of a", "… of a and b", "… of a, b and c"; "" for none. */
export function repoLine(repos: string[]): string {
  if (!repos.length) return "";
  const list = repos.length === 1 ? repos[0] : `${repos.slice(0, -1).join(", ")} and ${repos.at(-1)}`;
  return `Also matches clones of ${list}`;
}

/** The create step's line for one folder. */
export function createFolderLine(path: string): string {
  return `Threads started in ${shortPath(path)} land in this project.`;
}

export function shortPath(p: string): string {
  return p.replace(/^\/home\/[^/]+(?=\/|$)/, "~");
}

/** A folder as text: a folder here as a short path, a folder elsewhere with its host, a repo as its URL. */
export function sourceText(s: ProjectSource, host: string): string {
  if (s.kind === "folder") {
    const h = String((s as { host?: string }).host ?? "");
    const p = String((s as { path?: string }).path ?? "");
    return h && host && h !== host ? `${h}: ${p}` : shortPath(p);
  }
  if (s.kind === "repo") return String((s as { url?: string }).url ?? "");
  return JSON.stringify(s);
}

/** The new thread's folder (V0.42): the picked one, else the project's main folder here, else a fresh scratch folder. */
export function newThreadFolder(picked: string, project: Pick<Project, "cwd"> | undefined): { cwd?: string; label: string; scratch: boolean } {
  const cwd = picked.trim() || project?.cwd || "";
  return cwd ? { cwd, label: shortPath(cwd), scratch: false } : { label: "A fresh scratch folder", scratch: true };
}

/** The picker's first row: from a thread "New project from this thread"; typed text that matches nothing names it. */
export function newRowLabel(q: string, matches: number, fromThread: boolean): string {
  const t = q.trim();
  if (t && !matches) return `New project “${t}”`;
  return fromThread ? "New project from this thread" : "New project";
}

/** The project a new thread starts on: the inbox filter, else the last one used on this device, else the most recent. */
export function startProject(projects: Project[], filterId: string | null | undefined, lastId: string | null | undefined): string | undefined {
  if (filterId && projects.some((p) => p.id === filterId)) return filterId;
  if (lastId && projects.some((p) => p.id === lastId)) return lastId;
  return pickerOrder(projects)[0]?.id;
}

/** The project of the last new thread on this device (V0.30); "" for No project. */
export const LAST_PROJECT_KEY = "fyr.lastProject";

/** A project the client just made, until the host's copy arrives: its main folder here is its cwd (V0.30, V0.42). */
export function localProject(id: string, name: string, sources: ProjectSource[], host: string): Project {
  const main = projectFolders(sources, host).find((f) => f.main);
  return { id, name, sources, threads: 0, lastUsed: new Date().toISOString(), cwd: main?.path, repos: projectRepos({ sources }) };
}
