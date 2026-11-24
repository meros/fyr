// V0.list-motion: the plan for one change of the inbox list, on both fronts. Pure: no DOM, so the tests run it
// directly; still-list-ui.ts measures the list before and after and plays the plan.
//
// Every element that takes space in the list has a key (`data-mk`): a section band `sec:<key>`, a group label
// `gh:<label>`, a row `row:<id>`, a "Show all" `more:<key>`, the filter's empty note `empty`. A count `n:<key>` is
// inside its band: it never moves by itself, it rolls.

/** An element's place on the screen: its top and height in px, the section a row is in, a count's text. */
export interface Snap {
  top: number;
  height: number;
  group?: string;
  text?: string;
}

export interface Move {
  key: string;
  /** from its old place to its new one, in px (negative: it came from above) */
  dy: number;
  delay: number;
  /** a row that changed section: drawn above the bands it crosses, and washed when it lands */
  lift: boolean;
}
export interface Enter {
  key: string;
  delay: number;
}
export interface Exit {
  key: string;
  /** drop: fades and shrinks a little; fade: fades in place (a filter); tuck: travels into the folded band `to` */
  kind: "drop" | "fade" | "tuck";
  to?: string;
  dy?: number;
}
export interface Roll {
  key: string;
  from: string;
  to: string;
  up: boolean;
  delay: number;
}
export interface Plan {
  /** too much changed to move: the list cross-fades as a whole */
  whole: boolean;
  moves: Move[];
  enters: Enter[];
  exits: Exit[];
  rolls: Roll[];
}

export const ENTER_MS = 240;
export const ENTER_DELAY_MS = 60;
export const EXIT_MS = 160;
/** a filter: what leaves fades this fast, and what stays waits this long before it moves */
export const FILTER_EXIT_MS = 110;
export const FILTER_WAIT_MS = 70;
/** a filter: what comes in fades in from the top down, this far apart, and no later than STAGGER_MAX_MS */
export const STAGGER_MS = 18;
export const STAGGER_MAX_MS = 180;
export const FILTER_ENTER_DELAY_MS = 90;
/** above this many elements arriving, leaving or moving, nothing moves: the list cross-fades */
export const MAX_CHANGED = 40;

export interface PlanOpts {
  /** the change is a filter or a search: who is in the list changed, nothing happened to the threads */
  filter: boolean;
  /** the list's visible box on the screen */
  view: { top: number; bottom: number };
  /** the duration of the list spring, for the counts' timing */
  moveMs: number;
  /** a row that leaves the drawn list: the band of the section it went to, when that band is drawn */
  tuckTo?: (key: string) => string | null;
}

const isCount = (k: string) => k.startsWith("n:");
const seen = (s: Snap, v: PlanOpts["view"]) => s.top + s.height > v.top && s.top < v.bottom;

export function planLayout(before: ReadonlyMap<string, Snap>, after: ReadonlyMap<string, Snap>, o: PlanOpts): Plan {
  const span = Math.max(1, o.view.bottom - o.view.top);
  const moves: Move[] = [];
  const enters: Enter[] = [];
  const exits: Exit[] = [];
  const rolls: Roll[] = [];
  const tucked = new Set<string>();

  for (const [key, b] of before) {
    if (isCount(key) || after.has(key) || !seen(b, o.view)) continue;
    // a row tucks only into another section's band: a fold of its own section is no move, it drops
    const dest = o.filter ? null : (o.tuckTo?.(key) ?? null);
    const band = dest && dest !== `sec:${b.group}` ? dest : null;
    const to = band ? after.get(band) : undefined;
    if (band && to && seen(to, o.view)) {
      exits.push({ key, kind: "tuck", to: band, dy: to.top + to.height / 2 - (b.top + b.height / 2) });
      tucked.add(band.slice(band.indexOf(":") + 1));
    } else exits.push({ key, kind: o.filter ? "fade" : "drop" });
  }

  const arriving = [...after].filter(([k, a]) => !isCount(k) && !before.has(k) && seen(a, o.view)).sort((x, y) => x[1].top - y[1].top);
  arriving.forEach(([key], i) => {
    const delay = !o.filter ? ENTER_DELAY_MS : key === "empty" ? FILTER_ENTER_DELAY_MS + STAGGER_MAX_MS : FILTER_ENTER_DELAY_MS + Math.min(i * STAGGER_MS, STAGGER_MAX_MS);
    enters.push({ key, delay });
  });

  for (const [key, a] of after) {
    const b = before.get(key);
    if (!b) continue;
    if (isCount(key)) {
      if (b.text !== undefined && a.text !== undefined && b.text !== a.text && seen(a, o.view)) {
        const from = num(b.text);
        const to = num(a.text);
        // a count whose row is flying into its band ticks when the row gets there
        const delay = Math.round(o.moveMs * (tucked.has(key.slice(2)) ? 0.7 : 0.25));
        rolls.push({ key, from: b.text, to: a.text, up: to >= from, delay });
      }
      continue;
    }
    const dy = b.top - a.top;
    if (Math.abs(dy) < 0.5 || (!seen(a, o.view) && !seen(b, o.view))) continue;
    // from far off the screen an element comes in from just past the edge, not across the whole list
    const lift = !o.filter && key.startsWith("row:") && b.group !== undefined && a.group !== undefined && b.group !== a.group;
    moves.push({ key, dy: Math.max(-span, Math.min(span, dy)), delay: o.filter ? FILTER_WAIT_MS : 0, lift });
  }

  const whole = moves.length + enters.length + exits.length > MAX_CHANGED;
  return whole ? { whole, moves: [], enters: [], exits: [], rolls: [] } : { whole, moves, enters, exits, rolls };
}

function num(s: string): number {
  const n = Number(s.replace(/[^\d]/g, ""));
  return Number.isFinite(n) ? n : 0;
}
