// V0.message-links-files: the file paths a message names, and which of them are files on the thread's host. Pure
// but for the lookup the caller passes in.

import type { HostId } from "./hostid.ts";

/** A path as a message writes it, with the line it points at. */
export interface PathRef {
  path: string;
  line?: number;
}

/** A file of the thread's folders, as its host answers (POST /threads/{id}/files/stat). */
export interface FileStat {
  /** the path as it was asked */
  path: string;
  /** the file's real path on its host */
  abs: string;
  size: number;
  kind: "text" | "image" | "other";
}

// ":12", ":12:5", ":12-20" or "#L12" after the path.
const LINE = /(?::(\d+)(?:[:-]\d+)?|#L(\d+)(?:-L?\d+)?)$/;

/**
 * Reads `s` (the text of an inline code span, or a link's target that is no URL) as a file path: "/abs/x.go",
 * "~/notes.md", "./x", "web/src/a.ts:12", "README.md". null for anything that cannot be one. It only picks
 * candidates: the host says which exist, so a wrong guess costs one lookup and shows nothing.
 */
export function pathRef(s: string): PathRef | null {
  let p = s.trim();
  if (!p || p.length > 400 || /[\s<>"'`|*?(){}[\]=;,$\\]/.test(p)) return null;
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(p) || /^(mailto|data|javascript|blob|about):/i.test(p)) return null;
  let line: number | undefined;
  const m = LINE.exec(p);
  if (m) {
    line = Number(m[1] ?? m[2]) || undefined;
    p = p.slice(0, m.index);
  }
  if (!p || p.endsWith("/") || p.includes(":")) return null;
  const rooted = /^(\/|~\/|\.\.?\/)/.test(p);
  const name = p.slice(p.lastIndexOf("/") + 1);
  // A bare name needs an extension with a letter ("main.go", not "v1.2"); a path with a folder needs a name.
  const ext = /^[^.].*\.(?=[A-Za-z0-9]*[A-Za-z])[A-Za-z0-9]{1,10}$/.test(name) || /^\.[A-Za-z][\w.-]*$/.test(name);
  if (!rooted && !p.includes("/") && !ext) return null;
  if (!/[A-Za-z0-9]/.test(name)) return null;
  return line ? { path: p, line } : { path: p };
}

/** A URL of a server on the machine the agent runs on (V0.forwarded-ports): `http://localhost:5173/x`, 127.0.0.1,
 *  [::1] or 0.0.0.0. Its port, and the rest of the address (path, query and fragment, "/" when there is none). null
 *  for any other URL. Plain http only: the forward speaks no TLS to the server. */
export function localUrl(href: string): { port: number; rest: string } | null {
  const m = /^http:\/\/(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])(?::(\d{1,5}))?(?=[/?#]|$)(.*)$/i.exec(href.trim());
  if (!m) return null;
  const port = m[1] ? Number(m[1]) : 80;
  if (port < 1 || port > 65535) return null;
  const rest = m[2] ?? "";
  return { port, rest: rest.startsWith("/") ? rest : "/" + rest };
}

type Lookup = (paths: string[]) => Promise<{ ok: true; data: FileStat[] } | { ok: false; error: string }>;

interface Known {
  /** null: the host has no such file for this thread */
  stat: FileStat | null;
  at: number;
}

/** How long "no such file" holds: the agent may write the file a moment after it names it. */
export const MISSING_TTL_MS = 30_000;
/** Paths in one call (the host's limit). */
export const STAT_BATCH = 64;
const MAX_THREADS = 24;
const MAX_PATHS = 4000;

const threads = new Map<string, Map<string, Known>>();
const asking = new Map<string, Promise<void>>();

function tableOf(host: HostId, threadId: string): Map<string, Known> {
  const key = `${host}\u0000${threadId}`;
  let t = threads.get(key);
  if (t) threads.delete(key); // most recently used last
  else t = new Map();
  threads.set(key, t);
  if (threads.size > MAX_THREADS) threads.delete(threads.keys().next().value as string);
  return t;
}

/** What is known of a path: its file, null when the host has none, undefined when it has not been asked (or the
 *  answer "none" is old). */
export function knownFile(host: HostId, threadId: string, path: string, now = Date.now()): FileStat | null | undefined {
  const k = tableOf(host, threadId).get(path);
  if (!k) return undefined;
  if (!k.stat && now - k.at > MISSING_TTL_MS) return undefined;
  return k.stat;
}

/** Asks the host about the paths that are not known yet; resolves when knownFile can answer all of them. A failed
 *  call records nothing, so the next render asks again. */
export async function learnFiles(host: HostId, threadId: string, paths: string[], lookup: Lookup, now: () => number = Date.now): Promise<void> {
  const table = tableOf(host, threadId);
  const pending: Promise<void>[] = [];
  const ask: string[] = [];
  for (const p of new Set(paths)) {
    if (knownFile(host, threadId, p, now()) !== undefined) continue;
    const running = asking.get(`${host}\u0000${threadId}\u0000${p}`);
    if (running) pending.push(running);
    else ask.push(p);
  }
  for (let i = 0; i < ask.length; i += STAT_BATCH) {
    const batch = ask.slice(i, i + STAT_BATCH);
    const call = lookup(batch)
      .then((r) => {
        if (!r.ok) return;
        const found = new Map(r.data.map((f) => [f.path, f]));
        for (const p of batch) table.set(p, { stat: found.get(p) ?? null, at: now() });
        while (table.size > MAX_PATHS) table.delete(table.keys().next().value as string);
      })
      .catch(() => {})
      .finally(() => {
        for (const p of batch) asking.delete(`${host}\u0000${threadId}\u0000${p}`);
      });
    for (const p of batch) asking.set(`${host}\u0000${threadId}\u0000${p}`, call);
    pending.push(call);
  }
  await Promise.all(pending);
}

/** Tests start from nothing. */
export function resetFileLinks(): void {
  threads.clear();
  asking.clear();
}

/** A text file's lines for the viewer: no empty last line for a file that ends in a newline. */
export function fileLines(text: string): string[] {
  const lines = text.split(/\r\n|\n/);
  if (lines.length > 1 && lines[lines.length - 1] === "") lines.pop();
  return lines;
}

/** The viewer shows text up to this size; a larger file is offered as a download. */
export const MAX_VIEW_BYTES = 2 * 1024 * 1024;
