// V0.popover-place: where a popover (the desktop's hover and click cards) opens, as a pure function of its anchor's box,
// its own size and the visible area. It stays tied to its anchor: it opens on the preferred side when it fits, else on
// the opposite side, else on the side with more room with a height cap (it scrolls); across the other axis it lines up
// with the anchor's start, then with its end, and only then is it pushed inside the area. The nub's offset says where
// the anchor is, so a card that had to move still points at what it belongs to.

export type Side = "below" | "right";

export interface Box {
  left: number;
  top: number;
  width: number;
  height: number;
}

export interface Placed {
  left: number;
  top: number;
  /** the height the card may take on its side; it scrolls past it */
  maxHeight: number;
  /** the side it opened on: below, above, right or left of the anchor */
  side: "below" | "above" | "right" | "left";
  /** the nub's offset along the card's edge that faces the anchor, from the card's own left (below, above) or top (right, left) */
  nub: number;
}

/** The gap between the anchor and the card, and the card's margin from the area's edges. */
export const GAP = 8;
export const MARGIN = 8;
const NUB_INSET = 14;

export function placePopover(anchor: Box, card: { width: number; height: number }, area: Box, side: Side): Placed {
  const aRight = anchor.left + anchor.width;
  const aBottom = anchor.top + anchor.height;
  const minX = area.left + MARGIN;
  const maxX = area.left + area.width - MARGIN;
  const minY = area.top + MARGIN;
  const maxY = area.top + area.height - MARGIN;
  const w = Math.min(card.width, maxX - minX);
  if (side === "below") {
    const roomBelow = maxY - (aBottom + GAP);
    const roomAbove = anchor.top - GAP - minY;
    const down = card.height <= roomBelow || (card.height > roomAbove && roomBelow >= roomAbove);
    const maxHeight = Math.max(0, down ? roomBelow : roomAbove);
    const h = Math.min(card.height, maxHeight);
    // the anchor's start, else its end, else inside the area
    let left = anchor.left;
    if (left + w > maxX) left = aRight - w;
    left = clamp(left, minX, maxX - w);
    const top = down ? aBottom + GAP : anchor.top - GAP - h;
    return { left, top, maxHeight, side: down ? "below" : "above", nub: nubAt(anchor.left + anchor.width / 2 - left, w) };
  }
  const roomRight = maxX - (aRight + GAP);
  const roomLeft = anchor.left - GAP - minX;
  const right = w <= roomRight || roomRight >= roomLeft;
  const left = right ? aRight + GAP : Math.max(minX, anchor.left - GAP - w);
  const maxHeight = maxY - minY;
  const h = Math.min(card.height, maxHeight);
  const top = clamp(anchor.top, minY, maxY - h);
  return { left, top, maxHeight, side: right ? "right" : "left", nub: nubAt(anchor.top + Math.min(anchor.height / 2, 12) - top, h) };
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(v, Math.max(lo, hi)));
}

/** The nub stays on the card's edge, clear of its rounded corners. */
function nubAt(at: number, length: number): number {
  return clamp(at, NUB_INSET, length - NUB_INSET);
}

/** True when the anchor's box is out of the area (scrolled away under a header, or gone): the card closes, not floats. */
export function anchorGone(anchor: Box | null, area: Box): boolean {
  if (!anchor || (anchor.width === 0 && anchor.height === 0)) return true;
  return anchor.top + anchor.height <= area.top || anchor.top >= area.top + area.height || anchor.left + anchor.width <= area.left || anchor.left >= area.left + area.width;
}
