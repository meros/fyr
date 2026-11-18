// &long=word or &long=words in mock mode: every value a person or an agent chose comes back very long, so the layout
// test sees each screen at its worst. word adds one unbroken run (a long branch or file name), words a long sentence.
// Only what is read leaves the mock long: what the front sends back is made short again, so the fixtures still match.
// Loaded through mock-api.ts, so none of it is in the production bundle.

import type { Api } from "./api.ts";

export type LongMode = "word" | "words";

const TOKENS: Record<LongMode, string> = {
  word: "Wm".repeat(70),
  words: " and then it goes on for a good while longer".repeat(6),
};

/** Shown as text. Ids, times and enums are left out: the front compares them. */
const TEXT = new Set([
  "activity", "addr", "author", "branch", "check", "checkError", "checkNote", "command", "configChanged", "description",
  "error", "header", "headline", "host", "label", "login", "machine", "message", "model", "name", "need", "node", "pattern",
  "project", "prompt", "question", "repo", "subject", "summary", "text", "title", "why", "workflow",
]);
const PATHS = new Set(["configDir", "cwd", "file_path", "folder", "oldPath", "path", "root"]);
const LISTS = new Set(["argv", "targets"]);
/** A message's own words. A tool's `name` is left alone: the conversation picks its row by it. */
const EVENT_TEXT = new Set(["command", "content", "description", "file_path", "header", "label", "pattern", "prompt", "question", "summary", "text", "title"]);

const plain = (v: object) => Array.isArray(v) || Object.getPrototypeOf(v) === Object.prototype;

function grow(s: string, key: string, token: string): string {
  if (!s || s.includes(token)) return s;
  if (key === "baseURL") return `${s}/${token}`;
  if (!PATHS.has(key) || !s.includes("/")) return s + token;
  const cut = s.lastIndexOf("/");
  return `${s.slice(0, cut)}/${token}${s.slice(cut)}`; // the file keeps its name and its extension
}

function walk(v: unknown, token: string, wanted: (key: string) => boolean): unknown {
  if (!v || typeof v !== "object" || !plain(v)) return v;
  if (Array.isArray(v)) return v.map((x) => walk(x, token, wanted));
  const out: Record<string, unknown> = {};
  for (const [k, x] of Object.entries(v)) {
    if (typeof x === "string") out[k] = wanted(k) ? grow(x, k, token) : x;
    else if (LISTS.has(k) && Array.isArray(x)) out[k] = x.map((e) => (typeof e === "string" ? grow(e, k, token) : e));
    else out[k] = walk(x, token, wanted);
  }
  return out;
}

const isEvent = (v: unknown): v is { seq: number; data: unknown } => !!v && typeof v === "object" && "seq" in v && "data" in v;

/** The value as a screen should get it. */
export function lengthen<T>(v: T, mode: LongMode): T {
  const token = TOKENS[mode];
  const data = (k: string) => TEXT.has(k) || PATHS.has(k) || k === "baseURL";
  const one = (x: unknown): unknown => (isEvent(x) ? { ...x, data: walk(x.data, token, (k) => EVENT_TEXT.has(k)) } : walk(x, token, data));
  return (Array.isArray(v) ? v.map(one) : one(v)) as T;
}

/** The value as the fixtures know it. */
export function shorten<T>(v: T, mode: LongMode): T {
  const token = TOKENS[mode];
  const cut = (x: unknown): unknown => {
    if (typeof x === "string") return x.replaceAll(`/${token}`, "").replaceAll(token, "");
    if (!x || typeof x !== "object" || !plain(x)) return x;
    if (Array.isArray(x)) return x.map(cut);
    return Object.fromEntries(Object.entries(x).map(([k, e]) => [k, cut(e)]));
  };
  return cut(v) as T;
}

export function withLongValues(api: Api, mode: LongMode): Api {
  const out = <T,>(v: T) => lengthen(v, mode);
  return new Proxy(api, {
    get(target, prop) {
      const f = Reflect.get(target, prop) as unknown;
      if (typeof f !== "function") return f;
      if (prop === "start")
        return (h: Record<string, unknown>) =>
          target.start(
            new Proxy(h, {
              get(handlers, name) {
                const cb = Reflect.get(handlers, name) as unknown;
                return typeof cb === "function" ? (...a: unknown[]) => cb.apply(handlers, a.map(out)) : cb;
              },
            }) as never,
          );
      return (...args: unknown[]) => {
        const r = f.apply(target, args.map((a) => shorten(a, mode))) as unknown;
        return r instanceof Promise ? r.then(out) : r;
      };
    },
  });
}
