# V0.open-row-glides: the open row moves with its state and glides there; j/k still steps from where it was

→ V0.open-row-only, V0.phone-row-moves, V0.207. *Replaces V0.open-row-only (2) (the open row keeps its section and place) and V0.207 (5)'s open row as the anchor.*

(1) **Why.** A held open row told a stale story: a thread the owner had just answered sat in Your move with a Working spinner, under the wrong header and against the counts, and jumped away the moment another thread was opened, so the list moved under the click that opened it.

(2) **The open row moves at once**, like every other row: when its state changes it goes to the section and place the live list has for it. Nothing is held.

(3) **It glides there.** When the drawn rows change, every row in view whose place changed slides from where it was to where it is (220 ms; none under reduced motion), so the eye follows the row that left and sees the others close the gap. A row from far off comes in from just past the list's edge.

(4) **The list does not chase it.** The open row is never the anchor of V0.207 (5): when it would be (under the pointer, or the open row in view), the row above it stands in, else the one below, so the rows under it rise into its place and the scroll stays.

(5) **Where the owner was stays.** j/k, the next row after a settle and the step past a row that leaves walk the list with the open row back in the place it had when it was opened: after an answer moves it to Working, j opens the row that took its place in Your move. It ends when another thread is current, or when the owner marks it seen or unseen by hand. On the phone the place is not kept (V0.phone-row-moves).

(6) **Selects** draw their own chevron and have no native look, the same in every engine: WebKitGTK (the Linux app) put its own padding inside a native menulist and cut the text of a 34 px field.

(7) **Both fronts.** One implementation (`web/src/still-list.ts`, `still-list-ui.ts`, `app.css`).
