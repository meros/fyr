// SPEC 0.5 V0.170: a host's mark, the same everywhere a host is shown: a circle with the glyph of the machine it runs
// on (a laptop, a desktop's monitor, a server) in the host's colour. Circles are hosts; squares are projects. Pure.
import type { HostInfo } from "./hosts.ts";
import { PALETTE_NAMES } from "./projects.ts";

type HostGlyph = "laptop" | "monitor" | "server";

/** V0.171: the glyphs the owner can pick for a host (Settings → Hosts → a host → Mark), the automatic three first. */
export const HOST_ICON_NAMES = ["laptop", "monitor", "server", "house", "cloud", "cpu", "database", "terminal", "box"] as const;
type HostIcon = (typeof HOST_ICON_NAMES)[number];

/** V0.171: the icon and colour of a host, by its install id (the state's hostMarks); empty is automatic. V0.208: `by`
 *  says who set each ("owner" or "agent"; the owner may have chosen automatic), `setup` is the host's last setup run. */
type OwnerMark = { icon?: string; color?: string; by?: { icon?: string; color?: string }; setup?: { thread?: string; at: string; error?: string } };
export type OwnerMarks = Record<string, OwnerMark>;

const isIcon = (x: string | undefined): x is HostIcon => !!x && (HOST_ICON_NAMES as readonly string[]).includes(x);
const isColor = (x: string | undefined): x is string => !!x && PALETTE_NAMES.includes(x);

/** An icon value the owner set: one of the glyphs, or an emoji (its first grapheme); else nothing. */
export function ownerIcon(v: string | undefined): { glyph: HostIcon } | { emoji: string } | null {
  if (!v) return null;
  if (isIcon(v)) return { glyph: v };
  if (/^[a-z0-9-]+$/.test(v)) return null; // a glyph name this client does not know
  const Seg = (Intl as unknown as { Segmenter?: new (l?: string, o?: { granularity: string }) => { segment(s: string): Iterable<{ segment: string }> } }).Segmenter;
  const first = Seg ? [...new Seg(undefined, { granularity: "grapheme" }).segment(v)][0]?.segment : Array.from(v)[0];
  return first ? { emoji: first } : null;
}

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

/** Each host's colour from the project palette (never amber): the owner's (V0.171) when set, else picked from its
 *  install id (its name without one), the next free hue when another host has it, so a front's hosts differ while
 *  there are hues left. The owner's colours are taken first; then the hosts in the order of their ids, so every front
 *  gives a host the same colour, and a renamed host keeps its colour. */
export function hostColors(hosts: readonly Pick<HostInfo, "name" | "id">[], marks: OwnerMarks = {}): Map<string, string> {
  const out = new Map<string, string>();
  const used = new Set<number>();
  const n = PALETTE_NAMES.length;
  const rest: Pick<HostInfo, "name" | "id">[] = [];
  for (const h of hosts) {
    const c = h.id ? marks[h.id]?.color : undefined;
    if (isColor(c)) {
      out.set(h.name, c);
      used.add(PALETTE_NAMES.indexOf(c));
    } else rest.push(h);
  }
  for (const h of rest.sort((a, b) => keyOf(a).localeCompare(keyOf(b)))) {
    let i = hash(keyOf(h).toLowerCase()) % n;
    if (used.size < n) while (used.has(i)) i = (i + 1) % n;
    used.add(i);
    out.set(h.name, PALETTE_NAMES[i]);
  }
  return out;
}

export interface HostMark {
  /** the line glyph to draw; null when the owner picked an emoji */
  glyph: HostIcon | null;
  /** the owner's emoji (V0.171) */
  emoji?: string;
  /** a palette name (the CSS variable --p-<name>) */
  color: string;
  offline: boolean;
  /** the tooltip: the full name, and "offline" when it is */
  title: string;
  /** neither icon nor colour set by the owner */
  auto: boolean;
}

/** The mark of the host named `name` among the front's hosts (a host the front no longer lists: from its name). The
 *  owner's icon and colour (`marks`, by install id) come first; a value this client does not know is left out. */
export function hostMarkOf(name: string, hosts: readonly HostInfo[], colors?: Map<string, string>, marks: OwnerMarks = {}): HostMark {
  // Decision 0031 2.6: every host, the front's own too, is in hosts[]; there is no other source of a host's machine.
  const h = hosts.find((x) => x.name === name);
  const own = (h as HostInfo | undefined)?.id ? marks[(h as HostInfo).id!] : undefined;
  const icon = ownerIcon(own?.icon);
  const cs = colors ?? hostColors(hosts, marks);
  const color = cs.get(name) ?? hostColors([{ name }]).get(name)!;
  const offline = !!h && !h.online;
  return {
    glyph: icon ? ("glyph" in icon ? icon.glyph : null) : hostGlyph(h?.machine),
    ...(icon && "emoji" in icon ? { emoji: icon.emoji } : {}),
    color,
    offline,
    title: offline ? `${name} · offline` : name,
    auto: !icon && !isColor(own?.color),
  };
}
