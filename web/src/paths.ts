// How a path is shown: the home folder as ~, a scratch folder or a long path shortened. Pure.

const HOME_RE = /^\/home\/[^/]+(?=\/|$)/;

/** A path as the owner reads it: the home folder as ~. */
export function tildePath(p: string, home = ""): string {
  if (home && (p === home || p.startsWith(home + "/"))) return "~" + p.slice(home.length);
  return p.replace(HOME_RE, "~");
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
