// Projects (SPEC 0.5 V0.23): palette, icons, ordering, filtering; and started-by labels (V0.22). Pure.

import type { Profile, Project, ProjectIconSpec, ProjectSuggestion, ThreadDTO } from "./types.ts";
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

/** One input adds a source: a path (/… or ~…) is a folder on this host, anything else a repo URL. The host checks and cleans it. */
export function sourceFromInput(v: string): { kind: "folder"; path: string } | { kind: "repo"; url: string } | null {
  const t = v.trim();
  if (!t) return null;
  return t.startsWith("/") || t.startsWith("~") ? { kind: "folder", path: t } : { kind: "repo", url: t };
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

/** Where a project lives, for a subtitle: its folder here, else its repo, else that it is a label. */
export function projectSubtitle(p: Project): string {
  if (p.cwd) return p.cwd.replace(/^\/home\/[^/]+(?=\/|$)/, "~");
  const repo = p.sources?.find((s) => s.kind === "repo") as { url?: string } | undefined;
  if (repo?.url) return repo.url;
  const folder = p.sources?.find((s) => s.kind === "folder") as { host?: string; path?: string } | undefined;
  if (folder?.path) return `${folder.host ?? "another host"}: ${folder.path}`;
  return "No source: a label";
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
