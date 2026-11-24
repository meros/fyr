# V0.list-motion: the inbox list moves as one layout on one clock: bands, group labels, counts and rows together, filters and folds included

→ V0.open-row-glides (3), V0.native-feel (4), V0.207. *Replaces V0.open-row-glides (3) (only the rows glide, 220 ms) and V0.native-feel (4)'s gap that closes before the thread settles.*

(1) **Why.** Only rows glided: the glide measured `[data-id]` and nothing else. Section bands, the Answer and Unblock labels, the counts and the bands' fill jumped to their new place at once, while the rows took 220 ms to get there, so for a moment the headers and their rows disagreed. A row that arrived popped in, one that left vanished, and a fold or a filter changed the list in one frame.

(2) **Everything that takes space has a key and moves.** A section band `sec:<key>`, a group label `gh:<label>`, a row `row:<id>`, "Show all" `more:<key>`, the filter's empty note `empty` (`data-mk`). The list measures each before a change and after it. `web/src/list-motion.ts` (pure) plans what moves, arrives, leaves and rolls, and `still-list-ui.ts` plays the plan. The scroll correction of V0.207 and the anchor rule of V0.open-row-glides (4) run first, unchanged.

(3) **One curve and one duration.** Every move plays one spring, `LIST_SPRING` (critically damped, about 320 ms, no overshoot), scaled by its own distance, so the elements that move together start and land together. A move longer than the list comes in from just past its edge, and an element off the screen before and after is not animated. A change during a move goes on from where things are on the screen.

(4) **A move is a move.** A row that changes section keeps its key and travels. It is drawn above the bands it crosses, and a light amber wash fades out where it lands.

(5) **Arrivals and departures.** What arrives fades in and grows from .97, 60 ms after its neighbours start to make room; no height is animated. What leaves becomes a copy in the list's own layer, at its last place, and fades (160 ms) while the rows under it close the gap. A row that goes into a section whose rows are not drawn (Settled, Snoozed, a folded section) flies into that band when it is in view, and the band's count ticks when it arrives. A fold drops its own rows and does not tuck them into its band.

(6) **Counts and bands.** A changed count rolls, up when it rises and down when it falls (`Count` in `row-ui.tsx`). A band whose section goes from none to some, or back, fades its fill and ink, and the chevron turns, on the same clock.

(7) **What is shown changes: a filter, a search, a fold, "Show all".** This is one change of who is in the list, not a series of events. Rows that go fade in place (110 ms), with no shrink, tuck or wash. Rows that stay wait 70 ms for them to clear, then move. Rows that come in fade in from the top down, 18 ms apart and no later than 180 ms, and the empty note last. The handlers say so before they change the state (`markListFilter`). Above 40 changed elements nothing moves and the list cross-fades as a whole.

(8) **The phone's swipe** (V0.native-feel (4)): the settled row flies off and the thread settles at once. The list then lets the row leave as every row leaves, by (5), and the box's height is no longer animated. The row still comes back when its thread does not leave.

(9) **Reduced motion:** nothing moves; what arrived or changed place fades in over 120 ms.

(10) **Both fronts.** One implementation; the keys are on the desktop's rows, bands and "Show all" (`inbox.tsx`, `slot-queue-ui.tsx`) and on the phone's (`phone.tsx`, `phone-row.tsx`), and `NeedsGroupHead`, `SectionTitle` and `FilterEmpty` are shared.

Tests: `web/test/list-motion.test.ts`.
