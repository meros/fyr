// SPEC V0.149: the Run card. Pure: which run requests a thread has, what a card shows (the host, why, the folder, the
// buttons and the run's state), what Copy takes, and the commands an agent message asks the owner to run in prose.
// runcard-ui.tsx draws it, the same component on the desktop and the phone.

import type { Offer, OfferRun, Thread } from "./types.ts";
import { lastRun, pressArgs, quote } from "./offers.ts";
import type { Ask } from "./confirm.ts";
import { shortFolder } from "./paths.ts";
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

/**
 * V0.next-step-dock: the run request that stands in the dock above the composer, or null. A question or an approval
 * comes first (it holds the keys and may hold the agent's turn); then the newest request not yet run. `waiting` is how
 * many requests wait in all, so the dock can say "+N more".
 */
export function dockRun(t: Pick<Thread, "runs" | "decisions"> & { offers?: Offer[] }): { o: Offer; waiting: number } | null {
  const open = requests(t).filter((o) => !o.optional && !lastRun(t.runs, o.id));
  if (!open.length || (t.decisions?.length ?? 0) > 0) return null;
  return { o: open[open.length - 1], waiting: open.length };
}

/** How many run requests wait behind what the dock shows: counted in a question's or approval's "+N more". */
export function waitingRuns(t: Pick<Thread, "runs"> & { offers?: Offer[] }): number {
  return requests(t).filter((o) => !o.optional && !lastRun(t.runs, o.id)).length;
}

/**
 * V0.next-step-chips (2), (4): the chips under the newest message: the thread's actions the agent offered (a prompt, or a
 * script from before this version), then the optional run requests not yet run. A request that ran is a card at its
 * place instead. Fyr drops the chips when the next run starts, so what is here belongs to the newest message.
 */
export function chips(t: Pick<Thread, "runs"> & { offers?: Offer[] }): { prompts: Offer[]; runs: Offer[] } {
  const all = t.offers ?? [];
  return {
    prompts: all.filter((o) => !o.request && o.by === "agent"),
    runs: all.filter((o) => o.request && o.optional && !lastRun(t.runs, o.id)),
  };
}

/** V0.settle-request: the agent's word that its work is done, as a Settle chip under the newest message, once the run
 *  has ended and while the thread is not settled; null otherwise. V0.settle-chip-rationale: the chip says why the work
 *  is done ("Settle: PR merged"); a why that only repeats the thread's title says nothing the row does not, so it falls
 *  back to the plain line. */
export function settleChip(t: Pick<Thread, "settleAsk" | "running" | "settledAt"> & { title?: string }): { label: string; why: string } | null {
  if (!t.settleAsk || t.running || t.settledAt) return null;
  let why = t.settleAsk.why?.trim() ?? "";
  if (why && repeatsTitle(why, t.title ?? "")) why = "";
  return { label: why ? `Settle: ${why}` : "Settle: the agent is done", why };
}

/** Whether a why is the title again: the same words, or one wholly inside the other, ignoring case and punctuation. */
function repeatsTitle(why: string, title: string): boolean {
  const norm = (s: string) => s.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, " ").trim();
  const w = norm(why), ti = norm(title);
  return !!w && !!ti && (w === ti || ti.includes(w) || w.includes(ti));
}

/** V0.next-step-chips (4): an optional request's card stands in the conversation only once it ran; before, it is a chip. */
export function cardShows(o: Offer, runs: OfferRun[] | undefined): boolean {
  return !o.optional || !!lastRun(runs, o.id);
}

/** The first line of a command, with … when there are more. */
export function firstLine(cmd: string): string {
  const lines = cmd.replace(/\n+$/, "").split("\n");
  return lines.length > 1 ? `${lines[0]} …` : lines[0];
}

/** The element id of a request's full card in the conversation, for Show in conversation. */
export const cardDomId = (id: string) => `runc-${id.replace(/[^A-Za-z0-9_-]/g, "_")}`;

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

/** V0.run-card-rerun: Fyr's confirmation before running a succeeded run request again. Fresh and failed cards return
 *  null and run directly from their button. */
export function rerunAsk(o: Offer, state: CardState, opts: { host?: string } = {}): Ask | null {
  if (state !== "ok") return null;
  const cmd = o.command ?? (o.argv ?? []).map(quote).join(" ");
  const why = o.why ?? o.label;
  const where = opts.host ? ` on ${opts.host}` : "";
  return {
    title: "Run again?",
    text: `“${why}” already succeeded${where}.`,
    command: {
      short: shortStore(cmd),
      full: cmd,
      ...(o.cwd ? { folder: shortFolder(o.cwd), folderFull: o.cwd } : {}),
      ...(o.root ? { note: "Runs as root" } : {}),
    },
    confirm: o.root ? "Run again as root" : "Run again",
  };
}

/** V0.run-card-rerun: press a Run card's run button. A succeeded card asks through Fyr's confirmation first so a stray
 *  click or tap after a layout shift cannot re-run it. Returns true when action.run was sent. */
export async function pressRunCard(
  o: Offer,
  state: CardState,
  opts: { host?: string },
  ask: (a: Ask) => Promise<boolean>,
  onRun: (action: string, args?: Record<string, unknown>) => unknown,
): Promise<boolean> {
  const a = rerunAsk(o, state, opts);
  if (a && !(await ask(a))) return false;
  onRun("action.run", pressArgs("thread", o));
  return true;
}

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
