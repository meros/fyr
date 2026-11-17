// SPEC 0.5 V0.170: a host's mark, the same everywhere a host is shown: a circle with the glyph of the machine it runs
// on (a laptop, a desktop's monitor, a server) in the host's colour. Circles are hosts; squares are projects. Pure.
import type { HostInfo } from "./hosts.ts";
import { PALETTE_NAMES } from "./projects.ts";

export type HostGlyph = "laptop" | "monitor" | "server";

/** The glyph for what the host reports it runs on (V0.170); a server when it reports nothing. */
export function hostGlyph(machine: string | undefined): HostGlyph {
  return machine === "laptop" ? "laptop" : machine === "desktop" ? "monitor" : "server";
}

function hash(s: string): number {
  let h = 2166136261;
  for (const ch of s) h = Math.imul(h ^ ch.codePointAt(0)!, 16777619);
  return h >>> 0;
}

const keyOf = (h: Pick<HostInfo, "name" | "id">) => h.id || h.name;

/** Each host's colour from the project palette (never amber): picked from its install id (its name without one), the
 *  next free hue when another host has it, so a front's hosts differ while there are hues left. The hosts are taken in
 *  the order of their ids, so every front gives a host the same colour, and a renamed host keeps its colour. */
export function hostColors(hosts: readonly Pick<HostInfo, "name" | "id">[]): Map<string, string> {
  const out = new Map<string, string>();
  const used = new Set<number>();
  const n = PALETTE_NAMES.length;
  for (const h of [...hosts].sort((a, b) => keyOf(a).localeCompare(keyOf(b)))) {
    let i = hash(keyOf(h).toLowerCase()) % n;
    if (used.size < n) while (used.has(i)) i = (i + 1) % n;
    used.add(i);
    out.set(h.name, PALETTE_NAMES[i]);
  }
  return out;
}

export interface HostMark {
  glyph: HostGlyph;
  /** a palette name (the CSS variable --p-<name>) */
  color: string;
  offline: boolean;
  /** the tooltip: the full name, and "offline" when it is */
  title: string;
}

/** The mark of the host named `name` among the front's hosts (a host the front no longer lists: from its name). */
export function hostMarkOf(name: string, hosts: readonly HostInfo[], colors: Map<string, string> = hostColors(hosts), self?: { name: string; machine?: string }): HostMark {
  // One host: hosts[] is empty, and this install's own machine comes from the state.
  const h = hosts.find((x) => x.name === name) ?? (self && self.name === name ? { name, online: true, machine: self.machine } : undefined);
  const color = colors.get(name) ?? hostColors([{ name }]).get(name)!;
  const offline = !!h && !h.online;
  return { glyph: hostGlyph(h?.machine), color, offline, title: offline ? `${name} · offline` : name };
}
