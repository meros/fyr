// SPEC 0.5 V0.68: what Fyr's own confirmation says. Pure: an action in, the sheet's words out; confirm-ui.tsx shows
// them as a bottom sheet on the phone and a dialog on the desktop.

import type { Offer, Project } from "./types.ts";
import { tildePath } from "./folders.ts";
import { pressArgs, quote } from "./offers.ts";

export interface AskCommand {
  /** the command with its paths shortened */
  short: string;
  /** the command exactly as it runs; Copy takes this */
  full: string;
  folder?: string;
  folderFull?: string;
  /** a line under the folder, e.g. the revision it is bound to */
  note?: string;
}

export interface Ask {
  title: string;
  /** what happens, or the consequence, in words */
  text?: string;
  command?: AskCommand;
  /** the primary button: names the action, never "OK" */
  confirm: string;
  /** destructive: the primary button is red */
  danger?: boolean;
  /** V0.72: one pick among options; the sheet writes the picked value back here */
  choice?: { label: string; options: { value: string; label: string }[]; value: string };
  /** V0.72: an optional line of text; the sheet writes it back here */
  reason?: { label: string; value: string; max?: number };
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

/** The argument shortened, quoted as the original would need. */
const shortQuoted = (a: string) => {
  const s = shortArg(a);
  if (quote(a) === a) return s;
  const q = quote(s);
  return q === s ? `'${s}'` : q;
};

const EFFECT: Record<string, string> = {
  "pr.merge": "Fyr merges the pull request on GitHub.",
  "pr.rerunFailed": "Fyr re-runs the failed checks on GitHub.",
};

/** The confirmation before a press (V0.60 (3)); null for an agent action, which is sent at once. */
export function offerAsk(o: Offer, cwd?: string): Ask | null {
  if (o.run === "agent") return null;
  if (o.run === "fyr") {
    const what = EFFECT[o.effect ?? ""] ?? `Fyr runs ${o.effect || "it"}.`;
    return { title: o.label, text: `${what} The agent gets the result.`, confirm: o.label };
  }
  const argv = o.argv ?? [];
  const dir = o.cwd || cwd;
  return {
    title: o.label,
    command: {
      short: argv.map(shortQuoted).join(" "),
      full: argv.map(quote).join(" "),
      ...(dir ? { folder: shortFolder(dir), folderFull: dir } : {}),
      ...(o.revision ? { note: `Only on revision ${o.revision}` } : {}),
    },
    confirm: "Run",
  };
}

/** V0.58 (2): the one question before Delete project. */
export function deleteProjectAsk(p: Pick<Project, "name" | "threads">): Ask {
  const n = p.threads ?? 0;
  const threads = n === 0 ? "It has no threads." : n === 1 ? "Its 1 thread stays and lands in a project whose folder covers it, or in No project." : `Its ${n} threads stay and land in a project whose folder covers them, or in No project.`;
  return { title: `Delete ${p.name}?`, text: `${threads} The folder on disk is not touched.`, confirm: "Delete project", danger: true };
}

/** Settings, Providers: Remove. */
export function removeProviderAsk(p: { name: string }): Ask {
  return { title: `Remove ${p.name}?`, text: "Its stored key is deleted too.", confirm: "Remove provider", danger: true };
}

/** V0.68 (8): one press for every holder, the buttons and the live view's page alike: a script or a fyr action runs
 *  only after the owner confirms it; an agent action is a message and goes at once. True when it was sent. */
export async function press(
  on: string,
  o: Offer,
  cwd: string | undefined,
  ask: (a: Ask) => Promise<boolean>,
  run: (action: string, args?: Record<string, unknown>) => void,
): Promise<boolean> {
  const a = offerAsk(o, cwd);
  if (a && !(await ask(a))) return false;
  run("action.run", pressArgs(on, o));
  return true;
}
