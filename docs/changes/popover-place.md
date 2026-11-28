# V0.popover-place: info cards open at what they belong to, stay there, and the phone's is a sheet

→ V0.hover-cards, V0.load-card, V0.folder-chip, V0.quota-reset-wake. *Amends V0.hover-cards (1) (where a card opens) and (3) (a tap never opens a hover card), and V0.folder-chip (2) and (4) (the phone's card is a bottom sheet).*

*Cause:* the info cards (the rail's load, undeclared and leftovers cards, the quota chip and lines, the folder chip) were placed once, from the box their anchor had when they opened, and then pushed inside the window. So they read as being in the wrong place: a card stayed where it opened when the page scrolled or the window changed size; a card with no room beside its anchor was pushed against the window's edge, away from the chip it described (the quota chip at the header's end); a card taller than the room ran past the window's bottom with no way to scroll it. On a phone a tap sends mouseenter and focus too, so a tap on a quota line in Settings opened a desktop hover card that nothing closed. The phone's folder card opened under a chip in a row that scrolls sideways.

(1) **Placement** is one pure function (`web/src/popover.ts`, `placePopover`), tested on its own. Under a chip: below it when it fits, else above, else on the side with more room with a height cap (the card scrolls). Across: its left edge lines up with the chip's, else its right edge with the chip's right, and only then is it pushed inside an 8 px margin. Beside the rail: right of it, else left; its top lines up with the anchor's and moves up only as far as the window's bottom needs. The area is the visual viewport (a phone's keyboard or a pinch shrinks it).

(2) **It follows its anchor.** The card reads the anchor's box again on every scroll, resize, visual viewport change and change of its own size (one frame at a time), so it never floats where it opened. When the anchor leaves the view or the page, the card closes.

(3) **A nub points at the anchor** on the edge that faces it, so a card that had to move still shows what it belongs to.

(4) **A hover card opens after the mouse rests 180 ms** on its anchor (a mouse crossing the rail no longer flashes cards), at once on keyboard focus, and **never from a touch or a pen**: Fyr remembers the last pointer type and ignores the mouseenter and focus a tap sends.

(5) **The phone's folder card is a bottom sheet** (the app's `BottomSheet`): the whole width with the page's gutter, the phone's text size, the path selectable, Copy path as wide as the sheet and 44 px; Back, a tap on the dim page or a drag down closes it. The desktop keeps the card under the chip, closed by a click outside or Escape (one `usePopoverDismiss` for the folder and quota cards). Touch size and no hover are the reasons the fronts differ.

*Not in this change:* the right-click menu and the header's ⋯ menu (they already open at the pointer or under their button and close on scroll), and the Today chart's tooltip, which stays inside its chart.

*Tests:* `web/test/popover.test.ts` (each side, the flip, the line-up with the anchor's end, the cap, the nub, the visual viewport, a lost anchor). `web/test/layout.e2e.mjs`, V0.folder-chip: on the desktop the card hangs from its chip and follows it when the window narrows and widens again; on the phone the card is the sheet, a tap on the dim page closes it, and a tap on a Settings quota line opens no hover card.
