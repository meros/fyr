// V0.load-invariants, V0.thread-invariants: the load guard's and thread state machine's invariants as words.
// A violation is a card in Needs you (the evidence, Fix it, Dismiss) until the owner presses one;
// Settings → Automation lists each invariant with its last violation and count.
// Pure: invariants-ui.tsx prints them, on the desktop and the phone alike.
import type { HostId } from "./hostid.ts";
import type { Guard, InvariantCard, Invariants } from "./guard.ts";
import type { HostLoad } from "./load.ts";
import { relTime, whenLabel } from "./format.ts";

/** One open card and the host it is of. */
export interface HostCard {
  host: string;
  /** the host's install id, which Fix it and Dismiss name; none for a load read from no addressable host */
  hostId?: HostId;
  card: InvariantCard;
  /** the Fyr checkout Fix it starts its thread in; null when the host has none */
  fixDir: string | null;
}

const guardOf = (h: HostLoad) => (h.slots as { guard?: Guard } | undefined)?.guard;
const invOf = (h: HostLoad): Invariants | undefined => guardOf(h)?.invariants ?? h.invariants;
const kindOf = (x: { kind?: "load" | "thread" }): "load" | "thread" => (x.kind === "thread" ? "thread" : "load");

/** Every host's open cards, the newest first. */
export function invariantCards(load: HostLoad[]): HostCard[] {
  const out: HostCard[] = [];
  for (const h of load) {
    const inv = invOf(h);
    for (const card of inv?.cards ?? []) out.push({ host: h.host, ...(h.hostId ? { hostId: h.hostId } : {}), card, fixDir: inv?.fixDir || null });
  }
  return out.sort((a, b) => Date.parse(b.card.at) - Date.parse(a.card.at));
}

/** The card's head: what broke, and on which host when there are several. */
export function cardHead(c: HostCard, hosts: number): string {
  const kind = kindOf(c.card) === "thread" ? "Thread" : "Load";
  return `${kind} invariant broken: ${c.card.id}${hosts > 1 ? ` on ${c.host}` : ""}`;
}

/** "14:05 · 12 min ago". */
export function cardWhen(c: InvariantCard, now: number): string {
  return `${whenLabel(c.at, now)} · ${relTime(c.at, now)} ago`;
}

/** Whether Fix it can start its thread, and what the card says when it cannot. */
export function fixState(c: HostCard): { can: boolean; note: string } {
  if (c.fixDir) return { can: true, note: `Fix it starts a thread in ${c.fixDir} with this evidence.` };
  return { can: false, note: "This host has no project with a Fyr checkout. Add the Fyr repo as a project to fix it from here." };
}

/** The log lines' fold: guard lines for load invariants, recent transitions for thread invariants. */
export function linesLabel(c: InvariantCard): string {
  const n = c.lines?.length ?? 0;
  if (!n) return "";
  if (kindOf(c) === "thread") return `${n} recent ${n === 1 ? "transition" : "transitions"}`;
  return `${n} guard ${n === 1 ? "line" : "lines"} from the 10 minutes before`;
}

/** Settings → Automation: one row per invariant, its sentence and when it last broke. */
export function invariantRows(h: HostLoad, now: number, kind?: "load" | "thread"): { id: string; kind: "load" | "thread"; text: string; desc: string; open: boolean }[] {
  const inv = invOf(h);
  if (!inv) return [];
  const open = new Set((inv.cards ?? []).map((c) => c.id));
  return inv.stats
    .filter((s) => !kind || kindOf(s) === kind)
    .map((s) => ({
      id: s.id,
      kind: kindOf(s),
      text: s.text,
      open: open.has(s.id),
      desc: s.count && s.last ? `${s.count === 1 ? "once" : `${s.count} times`} · last ${whenLabel(s.last, now)}, ${relTime(s.last, now)} ago${open.has(s.id) ? " · in Needs you" : ""}` : "never broken",
    }));
}
