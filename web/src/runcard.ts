// SPEC V0.149: the Run card. Pure: which run requests a thread has, what a card shows (the host, why, the folder, the
// buttons and the run's state), what Copy takes, and the commands an agent message asks the owner to run in prose.
// runcard-ui.tsx draws it, the same component on the desktop and the phone.

import type { Offer, OfferRun, Thread } from "./types.ts";
import { lastRun, quote } from "./offers.ts";
import { shortFolder } from "./confirm.ts";
import type { Item } from "./conversation.ts";

/** A Nix store path shortened in the middle: /nix/store/<32-char hash>-name as /nix/store/<4>…-name. */
export function shortStore(s: string): string {
  return s.replace(/\/nix\/store\/([0-9a-z]{4})[0-9a-z]{28}-/g, "/nix/store/$1…-");
}

/** The thread's run requests, in the order the agent asked. */
export function requests(t: Pick<Thread, "offers"> | { offers?: Offer[] }): Offer[] {
  return (t.offers ?? []).filter((o) => o.request);
}

/**
 * V0.run-cards-in-conversation: where the cards stand. A request's card takes the place of the newest time it was asked
 * (the same id again replaces the request); a closed request has no card. `loose` are the requests whose place is not
 * in the loaded log (asked before this version, or further back than what is loaded): they stand at the end, so a card
 * that waits for the owner is never out of reach.
 */
export function placeCards(items: Item[], ids: string[]): { items: Item[]; loose: string[] } {
  const open = new Set(ids);
  const place = new Map<string, string>();
  for (const it of items) if (it.kind === "runcard" && open.has(it.action)) place.set(it.action, it.key);
  return { items: items.filter((it) => it.kind !== "runcard" || place.get(it.action) === it.key), loose: ids.filter((id) => !place.has(id)) };
}

/** What changes a card's height: at the bottom of the conversation, the view follows it. */
export function cardsKey(t: Pick<Thread, "runs"> & { offers?: Offer[] }): string {
  return requests(t)
    .map((o) => {
      const r = lastRun(t.runs, o.id);
      return `${o.id}:${r ? `${r.running ? "r" : r.ok ? "k" : "f"}${r.output?.length ?? 0}` : ""}`;
    })
    .join(",");
}

export type CardState = "fresh" | "running" | "ok" | "failed";

export interface CardLook {
  /** "Run on lab", or "Run" when the host is not known */
  title: string;
  why: string;
  /** the folder, "~/src/fyr", with " · as root" for a root request */
  folder: string;
  /** the run button: Run, Run as root, Run again */
  run: string;
  /** why the run button is off, or null */
  runOff: string | null;
  /** a root request: where the password prompt shows, or why it cannot */
  rootLine: string | null;
  /** Not now before the first run, Close after */
  close: string;
  state: CardState;
  /** "Succeeded (exit 0)", "Failed (exit 2)" after a run */
  result: string | null;
  output: string;
}

/** What one card shows. `host` is the thread's host; `rootPrompt` the host's word on why root cannot ask (V0.149 (4)). */
export function cardLook(o: Offer, runs: OfferRun[] | undefined, opts: { host?: string; rootPrompt?: string }): CardLook {
  const r = lastRun(runs, o.id);
  const state: CardState = !r ? "fresh" : r.running ? "running" : r.ok ? "ok" : "failed";
  const host = opts.host || "";
  const folder = `${o.cwd ? shortFolder(o.cwd) : "the thread's folder"}${o.root ? " · as root" : ""}`;
  let rootLine: string | null = null;
  let runOff: string | null = state === "running" ? "It is running" : null;
  if (o.root) {
    const where = host || "the host";
    if (opts.rootPrompt) {
      rootLine = `No password prompt can show on ${where}: ${opts.rootPrompt}. Copy the command and run it in a terminal there.`;
      runOff = runOff ?? rootLine;
    } else rootLine = `The password prompt shows on ${host ? `${host}'s` : "the host's"} screen.`;
  }
  return {
    title: host ? `Run on ${host}` : "Run",
    why: o.why ?? o.label,
    folder,
    run: state === "fresh" || state === "running" ? (o.root ? "Run as root" : "Run") : "Run again",
    runOff,
    rootLine,
    close: state === "fresh" ? "Not now" : "Close",
    state,
    result: state === "ok" ? `Succeeded (exit ${r?.exit ?? 0})` : state === "failed" ? `Failed${r?.exit ? ` (exit ${r.exit})` : ""}` : null,
    output: r?.output ?? "",
  };
}

/** Copy takes the full command; a root request's with sudo, so it runs the same way in a terminal. */
export function copyText(o: Pick<Offer, "command" | "root" | "argv">): string {
  const cmd = o.command ?? (o.argv ?? []).map(quote).join(" ");
  if (!o.root) return cmd;
  return cmd.includes("\n") ? `sudo bash -euo pipefail -c ${quote(cmd)}` : `sudo ${cmd}`;
}

/** The output's last 12 lines, and how many are folded above them. */
export function foldOutput(out: string, max = 12): { text: string; hidden: number } {
  const lines = out.replace(/\n$/, "").split("\n");
  if (lines.length <= max) return { text: lines.join("\n"), hidden: 0 };
  return { text: lines.slice(-max).join("\n"), hidden: lines.length - max };
}

export const stopArgs = (o: Pick<Offer, "id">) => ({ on: "thread", action: o.id });
export const dismissArgs = (o: Pick<Offer, "id">) => ({ on: "thread", action: o.id });

// V0.149 (8): the same blocks the host finds (internal/core/runrequest.go, ProseCommands).
const FENCE = /^[ \t]*```([A-Za-z0-9_+-]*)[^\n]*\n([\s\S]*?)\n?[ \t]*```[ \t]*$/gm;
const SHELL = new Set(["sh", "bash", "shell", "console", "zsh"]);
const ASK_RUN = /\b(run this|run these|please run|can you run|could you run|you need to run|you'll need to run|you will need to run|run the following|run:)/i;

export type ProsePart = { kind: "md"; text: string } | { kind: "cmd"; text: string };

/** An agent message cut into markdown and the commands it asks the owner to run (a `! cmd` block, or a shell block
 *  after a line that asks to run it), each block one script with "! " dropped. */
export function splitProse(text: string): ProsePart[] {
  const parts: ProsePart[] = [];
  let at = 0;
  for (const m of text.matchAll(FENCE)) {
    const start = m.index!;
    const lang = m[1];
    const lines = m[2].replace(/\n+$/, "").split("\n");
    const bang = lines.length > 0 && lines.every((l) => !l.trim() || l.trimStart().startsWith("! "));
    let cmd: string | null = null;
    if (bang) {
      cmd = lines
        .map((l) => l.trim())
        .filter(Boolean)
        .map((l) => l.replace(/^! /, ""))
        .join("\n");
    } else if (SHELL.has(lang.toLowerCase())) {
      const before = text.slice(0, start).trim().split("\n").slice(-2).join("\n");
      if (ASK_RUN.test(before)) cmd = m[2].trim();
    }
    if (cmd === null) continue;
    const md = text.slice(at, start).trim();
    if (md) parts.push({ kind: "md", text: md });
    parts.push({ kind: "cmd", text: cmd });
    at = start + m[0].length;
  }
  const rest = text.slice(at).trim();
  if (rest || !parts.length) parts.push({ kind: "md", text: parts.length ? rest : text });
  return parts;
}
