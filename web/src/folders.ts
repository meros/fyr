// The host folder picker (SPEC 0.5 V0.42): pure helpers for search, typed paths, completion, breadcrumbs and new
// folder names. The host resolves and confines every path; these only shape what the owner types and sees.

import type { FsDir } from "./types.ts";

const HOME_RE = /^\/home\/[^/]+(?=\/|$)/;

/** A path as the owner reads it: the home folder as ~. */
export function tildePath(p: string, home = ""): string {
  if (home && (p === home || p.startsWith(home + "/"))) return "~" + p.slice(home.length);
  return p.replace(HOME_RE, "~");
}

/** The picker's one field: a path (~ or /) browses, anything else searches. */
export function isPathQuery(q: string): boolean {
  const t = q.trim();
  return t.startsWith("/") || t.startsWith("~");
}

/** A typed path split into the folder to list and the start of the child being typed. "~/src/fy" → ~/src + fy. */
export function splitPath(q: string): { dir: string; prefix: string } {
  const t = q.trim();
  if (t === "~" || t === "/") return { dir: t, prefix: "" };
  const i = t.lastIndexOf("/");
  if (i < 0) return { dir: "~", prefix: t };
  const dir = t.slice(0, i) || "/";
  return { dir, prefix: t.slice(i + 1) };
}

export function joinPath(dir: string, name: string): string {
  return dir.endsWith("/") ? dir + name : `${dir}/${name}`;
}

/** Hidden folders show only when asked for (a name starting with a dot). */
export function visibleDirs(dirs: FsDir[], prefix = ""): FsDir[] {
  return prefix.startsWith(".") ? dirs : dirs.filter((d) => !d.name.startsWith("."));
}

/** The children whose names start with prefix (case-insensitive), hidden ones only for a dotted prefix. */
export function completions(dirs: FsDir[], prefix: string): FsDir[] {
  const p = prefix.toLowerCase();
  return visibleDirs(dirs, prefix).filter((d) => d.name.toLowerCase().startsWith(p));
}

/** Tab in the path field: one match completes to that folder with a trailing /; several complete to their common
 *  start; none leaves the text. */
export function complete(q: string, dirs: FsDir[]): string {
  const { dir, prefix } = splitPath(q);
  const m = completions(dirs, prefix);
  if (!m.length) return q;
  if (m.length === 1) return joinPath(dir, m[0].name) + "/";
  let common = m[0].name;
  for (const d of m) {
    let i = 0;
    while (i < common.length && i < d.name.length && common[i].toLowerCase() === d.name[i].toLowerCase()) i++;
    common = common.slice(0, i);
  }
  return common.length > prefix.length ? joinPath(dir, common) : q;
}

/** Breadcrumbs from the home folder down to path: ~ › src › app. A path outside home starts at it. */
export function breadcrumbs(path: string, home: string): { label: string; path: string }[] {
  if (!home || (path !== home && !path.startsWith(home + "/"))) return [{ label: path, path }];
  const out = [{ label: "~", path: home }];
  let at = home;
  for (const part of path.slice(home.length).split("/").filter(Boolean)) {
    at = `${at}/${part}`;
    out.push({ label: part, path: at });
  }
  return out;
}

export interface PickerSection {
  key: string;
  title: string;
  dirs: FsDir[];
}

/** A search across the sections: folders whose name or path contains every word; a folder shows once, in the first
 *  section that has it; empty sections go. */
export function filterSections(sections: PickerSection[], q: string, home = ""): PickerSection[] {
  const words = q.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const seen = new Set<string>();
  return sections
    .map((s) => ({
      ...s,
      dirs: s.dirs.filter((d) => {
        if (seen.has(d.path)) return false;
        if (words.length && !words.every((w) => `${d.name} ${tildePath(d.path, home)}`.toLowerCase().includes(w))) return false;
        seen.add(d.path);
        return true;
      }),
    }))
    .filter((s) => s.dirs.length > 0);
}

/** Why a new folder name cannot be used, or null. */
export function folderNameError(name: string, existing: string[]): string | null {
  const n = name.trim();
  if (!n) return "Name the folder.";
  if (n.includes("/")) return "A folder name cannot contain /.";
  if (n === "." || n === "..") return `${n} is not a folder name.`;
  if (existing.includes(n)) return `${n} is already there.`;
  return null;
}

/** V0.131: a folder picker call names the host whose folders it reads ("" or missing: the front's first host). */
export function fsQuery(url: string, host: string | undefined): string {
  return host ? `${url}${url.includes("?") ? "&" : "?"}host=${encodeURIComponent(host)}` : url;
}
