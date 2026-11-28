// V0.tooltips: the desktop's tooltip, as pure functions: when it opens, what it says (the text and the keys as key
// caps) and where it goes. tooltip-ui.ts draws it for every `title` in the app, so no markup has to change.

/** The wait before the first tooltip opens under a resting pointer: long enough that a pointer crossing the screen
 *  shows none, short enough to feel at once (the browser's own waits about a second). */
export const OPEN_DELAY = 450;
/** After a tooltip closes, the next one opens with no wait within this time: moving along a row of icon buttons
 *  reads each one's name as the pointer reaches it. */
export const WARM_MS = 500;
/** The tooltip stays this long after the pointer leaves its anchor, so a pointer passing over a gap between two
 *  buttons keeps it. It never takes a press (pointer-events: none): a click meant for what is under it lands there. */
export const CLOSE_GRACE = 120;
/** The gap between the anchor and the tooltip, and its margin from the window's edges. */
export const TIP_GAP = 6;
export const TIP_MARGIN = 8;
const NUB_INSET = 10;

/** True when the tooltip should open at once: one was shown, or closed, within WARM_MS. */
export function isWarm(lastShownOrClosedAt: number, now: number): boolean {
  return lastShownOrClosedAt > 0 && now - lastShownOrClosedAt <= WARM_MS;
}

export interface TipText {
  /** the sentence, with the trailing key hint taken off */
  text: string;
  /** the keys of the hint, each one a key cap; alternatives are separate entries ("⇧Tab or ←" → two) */
  keys: string[];
}

// One key in a hint: a single character (e, [, \, ←), a modifier glyph before a key (⇧S, ⇧Tab, ⌘K), or a named key.
const NAMED = "Esc|Enter|Tab|Space|Backspace|Delete|Home|End|PgUp|PgDn|Up|Down|Left|Right";
const KEY = new RegExp(`^(?:[⇧⌘⌥⌃]{0,3}(?:${NAMED}|[^\\s()]))$|^(?:(?:Ctrl|Alt|Shift|Cmd|Mod)\\+)+(?:${NAMED}|[^\\s()+])$|^g[0-9]$`);

/** Splits a tooltip's text into the sentence and its key hint. Fyr's titles end with the key in brackets,
 *  "Settle now (e)", "Previous question (⇧Tab or ←)"; anything else in brackets ("(optional)", "(exit 2)") is words. */
export function splitTip(raw: string): TipText {
  const s = raw.trim();
  const m = /^(.*\S)\s+\(([^()]+)\)$/s.exec(s);
  if (!m) return { text: s, keys: [] };
  const parts = m[2].split(/\s+or\s+|\s*,\s*|\s*\/\s*/).filter(Boolean);
  if (parts.length === 0 || !parts.every((p) => KEY.test(p))) return { text: s, keys: [] };
  return { text: m[1], keys: parts };
}

/** True when a tooltip only repeats what its element already shows (a button's label, a name shown in full): the
 *  tooltip then opens only while that text is cut off. Case, spaces and a final full stop do not count. */
export function sameWords(tip: string, shown: string): boolean {
  const norm = (x: string) => x.replace(/\s+/g, " ").trim().replace(/[.…]$/, "").toLowerCase();
  return norm(tip) !== "" && norm(tip) === norm(shown);
}

export interface Box {
  left: number;
  top: number;
  width: number;
  height: number;
}

export interface TipPlaced {
  left: number;
  top: number;
  side: "above" | "below" | "right";
  /** the nub's offset from the tooltip's left (above, below) or top (right) */
  nub: number;
}

/** Where the tooltip goes: above its anchor, centred, else below it when the room above is short; pushed inside
 *  the window across, with the nub still at the anchor's middle. An anchor at the window's left edge whose tooltip
 *  would not be centred on it (the rail's icons) gets it on its right, as a menu's label sits. */
export function placeTip(anchor: Box, tip: { width: number; height: number }, area: Box, prefer: "above" | "below" | "right" = "above"): TipPlaced {
  const minX = area.left + TIP_MARGIN;
  const maxX = area.left + area.width - TIP_MARGIN;
  const minY = area.top + TIP_MARGIN;
  const maxY = area.top + area.height - TIP_MARGIN;
  const midX = anchor.left + anchor.width / 2;
  const midY = anchor.top + anchor.height / 2;
  if (prefer === "right" && anchor.left + anchor.width + TIP_GAP + tip.width <= maxX) {
    const top = clamp(midY - tip.height / 2, minY, maxY - tip.height);
    return { left: anchor.left + anchor.width + TIP_GAP, top, side: "right", nub: nubAt(midY - top, tip.height) };
  }
  const w = Math.min(tip.width, maxX - minX);
  const left = clamp(midX - w / 2, minX, maxX - w);
  const roomAbove = anchor.top - TIP_GAP - minY;
  const roomBelow = maxY - (anchor.top + anchor.height + TIP_GAP);
  // the preferred side when it fits, else the other when that fits, else the side with more room
  const wantAbove = prefer !== "below";
  const fitsAbove = tip.height <= roomAbove;
  const fitsBelow = tip.height <= roomBelow;
  const above = wantAbove ? fitsAbove || (!fitsBelow && roomAbove > roomBelow) : !fitsBelow && (fitsAbove || roomAbove > roomBelow);
  const top = above ? anchor.top - TIP_GAP - tip.height : anchor.top + anchor.height + TIP_GAP;
  return { left, top: clamp(top, minY, Math.max(minY, maxY - tip.height)), side: above ? "above" : "below", nub: nubAt(midX - left, w) };
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(v, Math.max(lo, hi)));
}

function nubAt(at: number, length: number): number {
  return clamp(at, NUB_INSET, length - NUB_INSET);
}
