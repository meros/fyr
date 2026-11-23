// How a path is shown: the home folder as ~, a scratch folder or a long path shortened. Pure.

// A home folder: /home/<user> (Linux), /Users/<user> (macOS) or /root (V0.web-helpers).
const HOME_RE = /^(?:\/(?:home|Users)\/[^/]+|\/root)(?=\/|$)/;

/** A path as the owner reads it: the home folder as ~ (`home` when known, else the usual places). The one way a
 *  path is shortened to ~. */
export function tildePath(p: string, home = ""): string {
  if (home && (p === home || p.startsWith(home + "/"))) return "~" + p.slice(home.length);
  return p.replace(HOME_RE, "~");
}

/** The last segment of a path or URL: "/src/acme" → "acme"; "" when it ends in a slash. */
export function baseName(p: string): string {
  return p.slice(p.lastIndexOf("/") + 1);
}

/** The folder part of a path: "~/src/fy" → "~/src"; "/" for a child of the root. */
export function dirName(p: string): string {
  return p.slice(0, Math.max(p.lastIndexOf("/"), 0)) || "/";
}

/** The non-empty segments of a path: "/src//acme/" → ["src", "acme"]. */
export function pathSegments(p: string): string[] {
  return p.split("/").filter(Boolean);
}

/** Shortens a path in the middle to at most `max` characters; the file name stays whole when it fits. */
export function shortenPath(path: string, max: number): string {
  if (path.length <= max) return path;
  const cut = path.lastIndexOf("/");
  const name = path.slice(cut + 1);
  if (name.length + 2 > max) return "…" + path.slice(-(max - 1));
  let prefix = "";
  for (const seg of path.slice(0, Math.max(cut, 0)).split("/")) {
    const next = `${prefix}${seg}/`;
    if (next.length + 2 + name.length > max) break;
    prefix = next;
  }
  return `${prefix}…/${name}`;
}

// <anything>/scratch/<name><rest>: Fyr's scratch folders (V0.42) live under ~/.local/share/fyr/scratch.
const SCRATCH = /^(.*)\/scratch\/([^/]+)(\/.*)?$/;

/** A folder for the muted "in …" line: the home folder as ~, a scratch folder as ~/…/scratch/<name>. */
export function shortFolder(p: string): string {
  const t = tildePath(p);
  const m = SCRATCH.exec(t);
  if (!m) return t;
  return `${m[1].startsWith("~") ? "~/…" : "…"}/scratch/${m[2]}${m[3] ?? ""}`;
}

/** One argument: an absolute path inside a scratch folder as …/<folder>/<rest>, else the home folder as ~;
 *  anything else as written. */
export function shortArg(a: string): string {
  if (!a.startsWith("/")) return a;
  const m = SCRATCH.exec(a);
  if (m?.[3]) return `…/${m[2]}${m[3]}`;
  return tildePath(a);
}
