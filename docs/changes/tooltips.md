# V0.tooltips: every tooltip is Fyr's own: in the theme, quick, on keyboard focus, with its key as a key cap

→ V0.31 (the reason as a tooltip), V0.34 (the full name in the tooltip), V0.popover-place, V0.hover-cards, SPEC U26.

Before this, the desktop's tooltips were the browser's: a grey box after about a second, in the system's font and not in
Fyr's theme, never on keyboard focus, sometimes off the window's edge, with the key a plain "(e)" at the end of the
sentence. About four hundred elements carry a `title`.

(1) **One layer, no markup changes.** `tooltip-ui.ts` shows every `title` in the app in one tooltip of Fyr's own. An
element keeps its `title`, which is still its accessible name or description and what the tests and the code read. While
the pointer rests on it the title is held back, so the browser's own box does not open as well, and it comes back when
the pointer leaves. An element whose title was its only name gets that title as its `aria-label` while it is held.
A title that changes while shown (a reason, a count) changes the tooltip.

(2) **When it opens.** After 450 ms of a resting pointer the first time. Within 500 ms of the last one, the next opens
at once, so moving along a row of buttons reads each one. It opens on keyboard focus (`:focus-visible`) too. It closes
when the pointer leaves (after 120 ms, so the gap between two buttons keeps it), on a press (until the pointer leaves
that element), on any key except a modifier, on Escape (which still reaches the dialog under it), when the window loses
focus, when its anchor leaves the page or the view, and when a hover card or a menu opens. A scroll moves it along with
its anchor.

(3) **What it says.** A key hint at the end in brackets, as Fyr writes it (`Settle now (e)`, `Previous question (⇧Tab
or ←)`, `Inbox (g1)`), is drawn as key caps beside the sentence. Other words in brackets (`(optional)`, `(exit 2)`) stay
words. A tooltip that only repeats the text the element shows (a button's label, a name in full) opens only while that
text is cut off, as V0.34 meant it for the rail's host name.

(4) **Where.** Above its anchor, centred, else below it, pushed inside the window with the nub still at the anchor. In
the rail it opens on the anchor's right. It is in the top layer (the popover API) where the browser has it, so it is
above an open dialog too. It never takes a press (`pointer-events: none`), so a click meant for what is under it lands
there.

(5) **Both fronts.** A finger or a pen opens no tooltip, because there is no hover on a phone. The phone keeps saying as
text what a tooltip says on the desktop (V0.31, V0.hover-cards).

(6) **No interruption** (U26). The tooltip never takes the focus or a key, it is not a dialog, and it opens only for the
owner's own pointer or focus. Something that arrives by itself never opens one.
